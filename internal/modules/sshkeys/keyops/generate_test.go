package keyops

// Generate 的用例：两种算法 × 有无口令，产物用 x/crypto 真解一遍；
// 覆盖/名字/类型/位数的拒绝分支；失败清场。

import (
	"crypto/rsa"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestGenerateCombos(t *testing.T) {
	cases := []struct {
		name       string
		keyType    string
		rsaBits    int
		passphrase string
		comment    string
		wantType   string
	}{
		{"ed25519 无口令", "ed25519", 0, "", "deploy@laptop", "ssh-ed25519"},
		{"ed25519 有口令", "ed25519", 0, testPass1, "guard@laptop", "ssh-ed25519"},
		// rsa 且 RSABits=0：一并验默认 3072
		{"rsa 默认位数无口令", "rsa", 0, "", "rsa-plain", "ssh-rsa"},
		{"rsa 有口令", "rsa", 2048, testPass1, "rsa-guard", "ssh-rsa"},
		{"空类型走默认 ed25519", "", 0, "", "", "ssh-ed25519"},
		{"类型大小写宽容", "RSA", 2048, "", "", "ssh-rsa"},
	}

	for i, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			keyName := fmt.Sprintf("id_combo%d", i)
			dir := t.TempDir()
			res := mustGenerate(t, dir, GenerateParams{
				Name:       keyName,
				Type:       c.keyType,
				RSABits:    c.rsaBits,
				Comment:    c.comment,
				Passphrase: c.passphrase,
			})

			// 返回值只有路径与指纹，不该有别的
			if res.Type != c.wantType {
				t.Errorf("Type 应为 %s，实得 %q", c.wantType, res.Type)
			}
			if !strings.HasPrefix(res.Fingerprint, testPubPfx) {
				t.Errorf("指纹应为 SHA256:… 形态，实得 %q", res.Fingerprint)
			}
			if res.Guarded != (c.passphrase != "") {
				t.Errorf("Guarded 应随口令有无，实得 %v", res.Guarded)
			}

			// 盘上正好两个文件：多一个就是临时件没清干净
			if got := listNames(t, dir); len(got) != 2 {
				t.Errorf("目录里应只有私钥与 .pub 两个文件，实得 %v", got)
			}
			if !fileExists(res.PrivatePath) || !fileExists(res.PublicPath) {
				t.Fatalf("返回的路径在盘上不存在：%q / %q", res.PrivatePath, res.PublicPath)
			}
			if filepath.Base(res.PrivatePath) != keyName {
				t.Errorf("私钥名应为 %q，实得 %q", keyName, res.PrivatePath)
			}
			if filepath.Base(res.PublicPath) != keyName+".pub" {
				t.Errorf("公钥名应为 %q，实得 %q", keyName+".pub", res.PublicPath)
			}
			if !filepath.IsAbs(res.PrivatePath) || !filepath.IsAbs(res.PublicPath) {
				t.Errorf("返回的路径应是绝对路径，实得 %q / %q", res.PrivatePath, res.PublicPath)
			}

			// 私钥：有口令的用 ParsePrivateKey 必须报 PassphraseMissingError
			if c.passphrase == "" {
				if err := parsePriv(t, res.PrivatePath, ""); err != nil {
					t.Errorf("未加密私钥应能直接解开: %v", err)
				}
				if _, err := ssh.ParsePrivateKey(readFileBytes(t, res.PrivatePath)); err != nil {
					t.Errorf("ssh.ParsePrivateKey 应成功，实得: %v", err)
				}
			} else {
				mustPassphraseMissing(t, res.PrivatePath)
				if err := parsePriv(t, res.PrivatePath, c.passphrase); err != nil {
					t.Errorf("用口令 %d 个字符应解得开，实得: %v", len(c.passphrase), err)
				}
				if err := parsePriv(t, res.PrivatePath, "Wrong-Passphrase-99"); err == nil {
					t.Error("错误口令竟然解开了，口令没有真的生效")
				}
			}

			// 显式位数与默认位数（0 → 3072）都要真的落在密钥本体上
			if c.keyType == "rsa" {
				assertRSABits(t, res.PrivatePath, c.rsaBits, c.passphrase)
			}

			// 公钥：authorized_keys 形态，尾字段是注释，指纹与返回值一致
			pubRaw := readFileBytes(t, res.PublicPath)
			pub, comment, _, _, perr := ssh.ParseAuthorizedKey(pubRaw)
			if perr != nil {
				t.Fatalf("公钥文件解不开: %v", perr)
			}
			if pub.Type() != c.wantType {
				t.Errorf("公钥算法应为 %s，实得 %s", c.wantType, pub.Type())
			}
			if ssh.FingerprintSHA256(pub) != res.Fingerprint {
				t.Errorf("返回值指纹与 .pub 算出的不一致：%q vs %q", res.Fingerprint, ssh.FingerprintSHA256(pub))
			}
			line := strings.TrimRight(string(pubRaw), "\n")
			fields := strings.Fields(line)
			if c.comment == "" {
				if len(fields) != 2 {
					t.Errorf("空注释时 .pub 应只有两段，实得 %d 段：%q", len(fields), line)
				}
			} else {
				if len(fields) != 3 || fields[2] != c.comment {
					t.Errorf(".pub 尾字段应是注释 %q，实得 %q（整行 %q）", c.comment, fields, line)
				}
			}

			// 未加密的那把，注释也在私钥自己的明文头部里（改口令时靠它带回去）
			if c.comment != "" && comment != c.comment {
				t.Errorf("ParseAuthorizedKey 的 comment 字段应为 %q，实得 %q", c.comment, comment)
			}

			assertNoSecret(t, "Generate", res, nil, c.passphrase)
		})
	}
}

func TestGenerateRSADefaultBits(t *testing.T) {
	dir := t.TempDir()
	res := mustGenerate(t, dir, GenerateParams{Name: "id_rsa3072", Type: "rsa"})
	assertRSABits(t, res.PrivatePath, defaultRSABits)
}

// TestGenerateRSABitsRange 验位数闸门。**位数校验排在生成之前**，
// 所以被拒的用例连"生成一把 32768 位钥匙"这种耗时几十秒的事都不会发生——
// 这也是这个闸门存在的意义之一：把贵的失败挡在便宜的地方。
func TestGenerateRSABitsRange(t *testing.T) {
	cases := []struct {
		bits    int
		wantErr bool
		why     string
	}{
		{1024, true, "低于 2048 一律拒，不再产出可被暴力分解的短钥匙"},
		{-1, true, "负数"},
		{32768, true, "超过 OpenSSH 自己的上限，写了没人能用"},
		{2048, false, "下限合法"},
	}
	for _, c := range cases {
		t.Run(c.why, func(t *testing.T) {
			dir := t.TempDir()
			res, err := Generate(dir, GenerateParams{Name: "id_bits", Type: "rsa", RSABits: c.bits})
			if c.wantErr {
				if !errors.Is(err, ErrInvalidInput) {
					t.Fatalf("位数 %d 应报参数不合法，实得 res=%+v err=%v", c.bits, res, err)
				}
				if len(listNames(t, dir)) != 0 {
					t.Errorf("被拒绝的生成不应留下任何文件，实得 %v", listNames(t, dir))
				}
				assertNoSecret(t, "Generate-位数", Result{}, err)
				return
			}
			if err != nil {
				t.Fatalf("位数 %d 应合法，实得错误: %v", c.bits, err)
			}
			assertRSABits(t, res.PrivatePath, c.bits)
		})
	}
}

func TestGenerateRejectsInvalidNames(t *testing.T) {
	cases := []struct {
		name string
		want string // 期望错误里出现的关键词，确保拒的是**对的那条理由**
	}{
		{"", "为空"},
		{".", "不是合法名字"},
		{"..", "不是合法名字"},
		{"../escape", "路径分隔符"},
		{`..\.pub`, "路径分隔符"},
		{"sub/id_x", "路径分隔符"},
		{`sub\\id_x`, "路径分隔符"},
		{"/abs/id_x", "路径分隔符"},
		{"C:\\Windows\\id_x", "路径分隔符"},
		{"C:id_x", "冒号"},
		{"id:stream", "冒号"},
		{"id_x\nssh-rsa AAAAFakeKey", "控制字符"},
		{"id_x\r", "首尾有空格"},
		{"id_x\x00", "控制字符"}, // 含 NUL：文件名注入的经典形态
		{" nul", "首尾有空格"},
		{"nul", "保留设备名"},
		{"CON", "保留设备名"},
		{"com1.pub", "以 .pub 结尾"},
		{"id_x.", "以点或空格结尾"},
		{"id_x ", "首尾有空格"},
		{"id" + strings.Repeat("x", maxNameLen), "超过上限"},
		{"bad*name", "保留字符"},
		{"bad?name", "保留字符"},
		{"bad|name", "保留字符"},
	}
	for i, c := range cases {
		// 用例名里带控制字符与反斜杠，直接当 subtest 名会打出乱码，编号 + %q 更可读。
		t.Run(fmt.Sprintf("拒绝_%02d", i), func(t *testing.T) {
			dir := t.TempDir()
			res, err := Generate(dir, GenerateParams{Name: c.name, Comment: "x"})
			if err == nil {
				t.Fatalf("非法文件名 %q 竟然生成了：%+v", c.name, res)
			}
			if !errors.Is(err, ErrInvalidInput) {
				t.Errorf("非法文件名 %q 应报参数不合法，实得: %v", c.name, err)
			}
			if c.want != "" && !strings.Contains(err.Error(), c.want) {
				t.Errorf("非法文件名 %q 的拒绝理由应含 %q，实得: %v", c.name, c.want, err)
			}
			// 被拒的调用必须一个文件都不留（含临时件）
			if got := listNames(t, dir); len(got) != 0 {
				t.Errorf("非法文件名 %q 被拒后目录里多出东西：%v", c.name, got)
			}
			assertNoSecret(t, "Generate-非法名", res, err)
		})
	}
}

func TestGenerateRejectsInvalidTypeAndComment(t *testing.T) {
	cases := []struct {
		kind  string
		p     GenerateParams
		subst string
	}{
		{"类型不支持", GenerateParams{Name: "id_x", Type: "ecdsa"}, "只支持 ed25519 与 rsa"},
		{"类型是垃圾", GenerateParams{Name: "id_x", Type: "ssh-rsa-9000"}, "只支持 ed25519 与 rsa"},
		{"注释含换行即注入", GenerateParams{Name: "id_x", Comment: "ok\nssh-rsa AAAAFakeInjectedLine"}, "控制字符"},
		{"注释含制表符", GenerateParams{Name: "id_x", Comment: "ok\tfake"}, "控制字符"},
		{"注释超长", GenerateParams{Name: "id_x", Comment: strings.Repeat("c", maxCommentLen+1)}, "超过上限"},
	}
	for _, c := range cases {
		t.Run(c.kind, func(t *testing.T) {
			dir := t.TempDir()
			res, err := Generate(dir, c.p)
			if err == nil || !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("应报参数不合法，实得 res=%+v err=%v", res, err)
			}
			if !strings.Contains(err.Error(), c.subst) {
				t.Errorf("拒绝理由应含 %q，实得: %v", c.subst, err)
			}
			if len(listNames(t, dir)) != 0 {
				t.Errorf("被拒后不得留下文件：%v", listNames(t, dir))
			}
		})
	}
}

func TestGenerateMissingDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "not-created-yet")
	res, err := Generate(dir, GenerateParams{Name: "id_x"})
	if err == nil {
		t.Fatalf("目录不存在应被拒绝（本包不替调用方建目录），实得 %+v", res)
	}
	if !errors.Is(err, ErrTargetMissing) {
		t.Errorf("应报目标不存在，实得: %v", err)
	}
	if fileExists(dir) {
		t.Error("被拒的调用不该顺手把目录建出来")
	}
}

func TestGenerateOverwriteGuard(t *testing.T) {
	dir := t.TempDir()
	first := mustGenerate(t, dir, GenerateParams{Name: "id_dup", Type: "ed25519", Passphrase: testPass1})
	before := readFileBytes(t, first.PrivatePath)

	// Overwrite=false：拒绝，且原文件一字节未动
	res, err := Generate(dir, GenerateParams{Name: "id_dup", Type: "ed25519"})
	if !errors.Is(err, ErrExists) {
		t.Fatalf("已存在且未确认覆盖应报 ErrExists，实得: %v", err)
	}
	if !strings.Contains(err.Error(), "已存在") || !strings.Contains(err.Error(), "确认") {
		t.Errorf("拒绝文案要把「已存在」和「需明确确认」说清楚，实得: %v", err)
	}
	if string(readFileBytes(t, first.PrivatePath)) != string(before) {
		t.Error("被拒的覆盖把原私钥改掉了")
	}
	if len(listNames(t, dir)) != 2 {
		t.Errorf("被拒后不得多出临时件：%v", listNames(t, dir))
	}
	assertNoSecret(t, "Generate-覆盖拒绝", res, err, testPass1)

	// Overwrite=true：换掉整对，指纹跟着变
	second := mustGenerate(t, dir, GenerateParams{Name: "id_dup", Type: "ed25519", Comment: "replaced", Overwrite: true})
	if second.Fingerprint == first.Fingerprint {
		t.Error("覆盖后指纹还是旧的，说明根本没换新钥匙")
	}
	if len(listNames(t, dir)) != 2 {
		t.Errorf("覆盖后仍应只有两个文件：%v", listNames(t, dir))
	}
	if err := parsePriv(t, second.PrivatePath, ""); err != nil {
		t.Errorf("覆盖时未给口令，新私钥应可直接解开: %v", err)
	}
}

func TestGenerateOrphanPubAlsoRequiresOverwrite(t *testing.T) {
	// 只剩 .pub（私钥先前被删）时也要拦：写出「新私钥 + 旧公钥」这种指纹对不上号的
	// 配对，比报一句"已存在"恶劣得多。
	dir := t.TempDir()
	res := mustGenerate(t, dir, GenerateParams{Name: "id_orphan", Type: "ed25519", Comment: "old"})
	if err := os.Remove(res.PrivatePath); err != nil {
		t.Fatalf("删掉私钥制造孤儿公钥失败: %v", err)
	}
	_, err := Generate(dir, GenerateParams{Name: "id_orphan", Type: "ed25519"})
	if !errors.Is(err, ErrExists) {
		t.Fatalf("只剩孤儿公钥也应报 ErrExists，实得: %v", err)
	}
	if !strings.Contains(err.Error(), ".pub") {
		t.Errorf("拒绝文案应点出是 .pub 挡住的，实得: %v", err)
	}
	if got := listNames(t, dir); len(got) != 1 || got[0] != "id_orphan.pub" {
		t.Errorf("被拒后目录应保持只有那个孤儿公钥，实得 %v", got)
	}
}

func TestGenerateRefusesNonRegularTargets(t *testing.T) {
	// 目标位置站着目录：既不是"覆盖"也不是"清场"能处理的东西，拒。
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "id_dir"), 0o700); err != nil {
		t.Fatalf("造干扰目录失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "id_dir.pub"), []byte("not a key\n"), 0o644); err != nil {
		t.Fatalf("造干扰公钥失败: %v", err)
	}
	res, err := Generate(dir, GenerateParams{Name: "id_dir", Type: "ed25519", Overwrite: true})
	if !errors.Is(err, ErrNotRegularFile) {
		t.Fatalf("目标是目录时必须拒绝（Overwrite 也不放行），实得 res=%+v err=%v", res, err)
	}
	if !strings.Contains(err.Error(), "目录") {
		t.Errorf("拒绝文案应说明那是目录，实得: %v", err)
	}
	// 干扰目录内容未被动过，也没有多出临时件
	if got := listNames(t, dir); len(got) != 2 {
		t.Errorf("拒绝后目录清单应仍是那两样，实得 %v", got)
	}
	body := readFileBytes(t, filepath.Join(dir, "id_dir.pub"))
	if string(body) != "not a key\n" {
		t.Error("被拒的调用改掉了既有 .pub 的内容")
	}
}

func TestGenerateCleansUpWhenSecondWriteFails(t *testing.T) {
	dir := t.TempDir()
	calls := withFailingWrites(t, 2) // 第一次（私钥）成功，第二次（公钥）失败

	res, err := Generate(dir, GenerateParams{Name: "id_late", Type: "ed25519", Passphrase: testPass1})
	if err == nil {
		t.Fatalf("注入的写失败必须让 Generate 失败，实得 %+v", res)
	}
	if !strings.Contains(err.Error(), "回滚") {
		t.Errorf("非覆盖模式下失败应回滚刚写的私钥并说明，实得: %v", err)
	}
	// 不留"私钥已写、公钥缺"的中间态，也不留任何临时件
	if fileExists(filepath.Join(dir, "id_late")) {
		t.Error("失败后残留了半把私钥")
	}
	if fileExists(filepath.Join(dir, "id_late.pub")) {
		t.Error("失败后残留了公钥")
	}
	if got := listNames(t, dir); len(got) != 0 {
		t.Errorf("失败后目录应空，实得 %v", got)
	}
	if len(*calls) != 2 {
		t.Errorf("应恰好尝试过两次写盘，实得 %d 次", len(*calls))
	}
	assertNoSecret(t, "Generate-失败清场", res, err, testPass1)
}

func TestGenerateOverwriteLateFailureIsHonest(t *testing.T) {
	// 覆盖模式 + 第二次写失败：旧私钥已被顶掉，删掉新的也还不回旧的。
	// 这时唯一诚实的处理是把损害如实写进错误，而不是假装"目录回到生成前"。
	dir := t.TempDir()
	first := mustGenerate(t, dir, GenerateParams{Name: "id_ov", Type: "ed25519", Comment: "old"})
	oldPub := readFileBytes(t, filepath.Join(dir, "id_ov.pub"))
	calls := withFailingWrites(t, 2)

	res, err := Generate(dir, GenerateParams{Name: "id_ov", Type: "ed25519", Overwrite: true})
	if err == nil {
		t.Fatalf("注入的写失败必须让 Generate 失败，实得 %+v", res)
	}
	if !strings.Contains(err.Error(), "没有配套公钥") {
		t.Errorf("覆盖模式下的失败文案应明说私钥在、公钥缺，实得: %v", err)
	}
	if !fileExists(filepath.Join(dir, "id_ov")) {
		t.Error("覆盖模式下不该把私钥删掉（删了也没法还回旧的那把）")
	}
	// 公钥那一步失败，盘上留的应当是**旧公钥原文**——新私钥配旧公钥是错误文案里
	// 说清的那个损害，绝不能变成"公钥凭空消失"这种更糟的形态。
	if string(readFileBytes(t, filepath.Join(dir, "id_ov.pub"))) != string(oldPub) {
		t.Error("公钥写失败却动过了旧公钥内容")
	}
	// 前置条件自证：留在盘上那份旧公钥，指纹确实是**旧私钥**的——
	// 于是"新私钥 + 旧公钥"这个错误文案里认了的损害，是能被外部验证的，不是嘴上说说。
	if pub, _, _, _, perr := ssh.ParseAuthorizedKey(oldPub); perr != nil {
		t.Fatalf("旧公钥解不开，测试前置条件就错了: %v", perr)
	} else if ssh.FingerprintSHA256(pub) != first.Fingerprint {
		t.Errorf("旧公钥指纹 %s 与旧私钥 %s 对不上", ssh.FingerprintSHA256(pub), first.Fingerprint)
	}
	if got := listNames(t, dir); len(got) != 2 {
		t.Errorf("除这一对既有文件外不得有任何残留（含临时件），实得 %v", got)
	}
	if len(*calls) != 2 {
		t.Errorf("应恰好尝试过两次写盘，实得 %d 次", len(*calls))
	}
}

func TestGenerateFileModeIntent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 上 os 的 mode 位只是提示性的（真实边界是 ACL，须以 icacls 为准），断言它没有意义")
	}
	dir := t.TempDir()
	res := mustGenerate(t, dir, GenerateParams{Name: "id_modes", Type: "ed25519"})
	privInfo, err := os.Stat(res.PrivatePath)
	if err != nil {
		t.Fatalf("stat 私钥失败: %v", err)
	}
	if privInfo.Mode().Perm() != privFileMode.Perm() {
		t.Errorf("私钥权限应为 %o，实得 %o", privFileMode, privInfo.Mode().Perm())
	}
	pubInfo, err := os.Stat(res.PublicPath)
	if err != nil {
		t.Fatalf("stat 公钥失败: %v", err)
	}
	if pubInfo.Mode().Perm() != pubFileMode.Perm() {
		t.Errorf("公钥权限应为 %o，实得 %o", pubFileMode, pubInfo.Mode().Perm())
	}
}

// assertRSABits 用真解析确认私钥的位数（want=0 表示按默认 3072 期望）。
// passphrase 非空时走带口令的解析分支——加密的钥匙不硬解，这是本层自己的纪律。
func assertRSABits(t *testing.T, privatePath string, want int, passphrase ...string) {
	t.Helper()
	if want == 0 {
		want = defaultRSABits
	}
	raw := readFileBytes(t, privatePath)
	block, _ := pem.Decode(raw)
	if block == nil {
		t.Fatalf("%s 不是 PEM 私钥", privatePath)
	}
	var key any
	var err error
	if len(passphrase) > 0 && passphrase[0] != "" {
		key, err = ssh.ParseRawPrivateKeyWithPassphrase(raw, []byte(passphrase[0]))
	} else {
		key, err = ssh.ParseRawPrivateKey(raw)
	}
	if err != nil {
		t.Fatalf("解私钥失败: %v", err)
	}
	rsaKey, ok := key.(*rsa.PrivateKey)
	if !ok {
		t.Fatalf("期望 *rsa.PrivateKey，实得 %T", key)
	}
	if rsaKey.N.BitLen() != want {
		t.Errorf("RSA 位数应为 %d，实得 %d", want, rsaKey.N.BitLen())
	}
}
