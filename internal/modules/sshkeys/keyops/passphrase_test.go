package keyops

// SetPassphrase 的用例：加/换/去三条路径、降级必须显式允许、旧口令错不得毁文件，
// 以及全包最有产品意义的一条——**指纹跨口令变更恒不变**。

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

// TestSetPassphraseFingerprintInvariantAcrossChanges 是安全设计第 4 条的正题：
// 无→有、有→换、有→去 三步走完，指纹始终是同一把。
//
// 这条断言的依据写在这儿：改的只是 OpenSSH 私钥文件的**封装层**（cipher + KDF + 口令），
// 密钥本体一个比特都没动；所以已经贴到服务端 authorized_keys 的那行公钥照常生效，
// 用户改完口令不需要重新部署任何东西。哪天实现改成"重新生成一对"，这条就会红。
func TestSetPassphraseFingerprintInvariantAcrossChanges(t *testing.T) {
	dir := t.TempDir()
	res := mustGenerate(t, dir, GenerateParams{Name: "id_inv", Type: "ed25519", Comment: "invariant@test"})
	fp0 := res.Fingerprint
	pubBefore := readFileBytes(t, res.PublicPath)

	steps := []struct {
		why        string
		old, new   string
		unprotect  bool
		wantGuard  bool
		wantInText string
	}{
		{"无口令→加口令", "", testPass1, false, true, "已加上口令保护"},
		{"有口令→换口令", testPass1, testPass2, false, true, "已更换口令"},
		{"有口令→去口令（显式允许）", testPass2, "", true, false, "已去掉口令保护"},
	}
	for _, s := range steps {
		t.Run(s.why, func(t *testing.T) {
			got, err := SetPassphrase(res.PrivatePath, s.old, s.new, s.unprotect)
			if err != nil {
				t.Fatalf("%s 失败: %v", s.why, err)
			}
			if got.Fingerprint != fp0 {
				t.Errorf("指纹变了：%s → %s（改口令绝不该换钥匙）", fp0, got.Fingerprint)
			}
			if got.Guarded != s.wantGuard {
				t.Errorf("Guarded 应为 %v，实得 %v", s.wantGuard, got.Guarded)
			}
			if !strings.Contains(got.Message, s.wantInText) {
				t.Errorf("结论文案应含 %q，实得 %q", s.wantInText, got.Message)
			}
			if !strings.Contains(got.Message, "未变") {
				t.Errorf("结论文案应把「指纹未变」说明白，实得 %q", got.Message)
			}
			// 盘上形态与预期一致
			if s.wantGuard {
				mustPassphraseMissing(t, res.PrivatePath)
				if s.new != "" {
					if err := parsePriv(t, res.PrivatePath, s.new); err != nil {
						t.Errorf("新口令解不开: %v", err)
					}
				}
			} else if err := parsePriv(t, res.PrivatePath, ""); err != nil {
				t.Errorf("去掉口令后应能直接解开: %v", err)
			}
			// 配套 .pub **一个字节都不必改**：公钥没变，改它就是把"改口令"做成了"换钥匙"
			if string(readFileBytes(t, res.PublicPath)) != string(pubBefore) {
				t.Error("改口令动了 .pub 文件")
			}
			assertNoSecret(t, "SetPassphrase:"+s.why, got, err, s.old, s.new)
		})
	}
	if got := listNames(t, dir); len(got) != 2 {
		t.Errorf("三步改口令不得多出任何文件：%v", got)
	}
}

func TestSetPassphraseUnprotectRequiresExplicitAllow(t *testing.T) {
	dir := t.TempDir()
	res := mustGenerate(t, dir, GenerateParams{Name: "id_un", Type: "ed25519", Passphrase: testPass1, Comment: "un@test"})
	before := readFileBytes(t, res.PrivatePath)

	got, err := SetPassphrase(res.PrivatePath, testPass1, "", false)
	if !errors.Is(err, ErrUnprotectNotAllowed) {
		t.Fatalf("allowUnprotect=false 时去口令必须被拒，实得 got=%+v err=%v", got, err)
	}
	if !strings.Contains(err.Error(), "降级") {
		t.Errorf("拒绝文案应说明这是降级动作，实得: %v", err)
	}
	// 拒绝即一字节未动，旧口令照常解得开
	if string(readFileBytes(t, res.PrivatePath)) != string(before) {
		t.Fatal("被拒的去口令把文件改了")
	}
	if err := parsePriv(t, res.PrivatePath, testPass1); err != nil {
		t.Errorf("被拒后原口令应仍能解开: %v", err)
	}
	assertNoSecret(t, "去口令拒绝", got, err, testPass1)

	// 同一条路径，给了允许就走通
	again, err := SetPassphrase(res.PrivatePath, testPass1, "", true)
	if err != nil {
		t.Fatalf("allowUnprotect=true 时应放行，实得: %v", err)
	}
	if again.Guarded {
		t.Error("去掉口令后 Guarded 应为 false")
	}
	if err := parsePriv(t, res.PrivatePath, ""); err != nil {
		t.Errorf("去掉口令后应能直接解开: %v", err)
	}
}

func TestSetPassphraseWrongOldKeepsFileUsable(t *testing.T) {
	dir := t.TempDir()
	res := mustGenerate(t, dir, GenerateParams{Name: "id_wrong", Type: "ed25519", Passphrase: testPass1, Comment: "w@test"})
	before := readFileBytes(t, res.PrivatePath)

	got, err := SetPassphrase(res.PrivatePath, "Not-The-Passphrase-42", testPass2, false)
	if !errors.Is(err, ErrWrongPassphrase) {
		t.Fatalf("旧口令错必须报 ErrWrongPassphrase，实得 got=%+v err=%v", got, err)
	}
	if string(readFileBytes(t, res.PrivatePath)) != string(before) {
		t.Fatal("失败的改口令毁掉了原文件")
	}
	// 原口令仍解得开——"文件未被破坏"的硬证据
	if err := parsePriv(t, res.PrivatePath, testPass1); err != nil {
		t.Errorf("失败后原口令应仍能解开: %v", err)
	}
	if len(listNames(t, dir)) != 2 {
		t.Errorf("失败不得留下临时件：%v", listNames(t, dir))
	}
	// 泄露断言：错的口令与对的口令都不许出现在错误文本里
	assertNoSecret(t, "旧口令错", got, err, testPass1, "Not-The-Passphrase-42")
}

func TestSetPassphraseStateMismatches(t *testing.T) {
	t.Run("有口令却不给旧口令", func(t *testing.T) {
		dir := t.TempDir()
		res := mustGenerate(t, dir, GenerateParams{Name: "id_need", Type: "ed25519", Passphrase: testPass1})
		got, err := SetPassphrase(res.PrivatePath, "", testPass2, false)
		if !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("应报参数不合法（缺旧口令），实得 %+v/%v", got, err)
		}
		if !strings.Contains(err.Error(), "重新生成") {
			t.Errorf("文案应给出出路（忘了旧口令只能重生成并重新部署公钥），实得: %v", err)
		}
		assertNoSecret(t, "缺旧口令", got, err, testPass1, testPass2)
	})

	t.Run("无口令却给了旧口令", func(t *testing.T) {
		dir := t.TempDir()
		res := mustGenerate(t, dir, GenerateParams{Name: "id_none", Type: "ed25519"})
		got, err := SetPassphrase(res.PrivatePath, testPass1, testPass2, false)
		if !errors.Is(err, ErrWrongPassphrase) {
			t.Fatalf("应报口令不正确，实得 %+v/%v", got, err)
		}
		if !strings.Contains(err.Error(), "没有口令保护") {
			t.Errorf("文案应说明这把本来就没保护，实得: %v", err)
		}
		assertNoSecret(t, "多余旧口令", got, err, testPass1, testPass2)
	})

	t.Run("无口令再去口令是无事可做", func(t *testing.T) {
		dir := t.TempDir()
		res := mustGenerate(t, dir, GenerateParams{Name: "id_noop", Type: "ed25519"})
		before := readFileBytes(t, res.PrivatePath)
		got, err := SetPassphrase(res.PrivatePath, "", "", false)
		if err != nil {
			t.Fatalf("本来无口令、目标也无口令：应成功返回现状而不是报错，实得: %v", err)
		}
		if !strings.Contains(got.Message, "未做任何改动") {
			t.Errorf("文案应实话实说没改动，实得 %q", got.Message)
		}
		if got.Fingerprint == "" {
			t.Error("即便无改动也应给出指纹")
		}
		if string(readFileBytes(t, res.PrivatePath)) != string(before) {
			t.Error("无事可做却重写了文件")
		}
		// 连 allowUnprotect 都没给，也不该因此报错——什么都没发生，谈不上降级
		if _, err = SetPassphrase(res.PrivatePath, "", "", true); err != nil {
			t.Errorf("allowUnprotect=true 的同一条无操作路径也应成功: %v", err)
		}
	})
}

func TestSetPassphraseRejectsBadTargets(t *testing.T) {
	dir := t.TempDir()
	res := mustGenerate(t, dir, GenerateParams{Name: "id_ok", Type: "ed25519"})

	cases := []struct {
		why  string
		path string
		want error
	}{
		{"公钥路径", res.PublicPath, ErrInvalidInput},
		{"不存在的私钥", filepath.Join(dir, "id_ghost"), ErrTargetMissing},
		{"空路径", "", ErrInvalidInput},
		{"只有空格", "   ", ErrInvalidInput},
		{"NUL 注入", filepath.Join(dir, "id_ok") + "\x00", ErrInvalidInput},
		{"目录本身", dir, ErrNotRegularFile},
	}
	for _, c := range cases {
		t.Run(c.why, func(t *testing.T) {
			got, err := SetPassphrase(c.path, "", testPass1, false)
			if err == nil || !errors.Is(err, c.want) {
				t.Fatalf("应报 %v，实得 got=%+v err=%v", c.want, got, err)
			}
			// 每条拒绝之后，正当那把钥匙必须完好
			if err := parsePriv(t, res.PrivatePath, ""); err != nil {
				t.Errorf("无关的拒绝动作把既有私钥弄坏了: %v", err)
			}
			assertNoSecret(t, "拒绝目标 "+c.why, got, err, testPass1)
		})
	}
}

func TestSetPassphraseOnNonKeyFile(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config")
	body := "Host example\n  HostName 1.2.3.4\n"
	if err := os.WriteFile(cfg, []byte(body), 0o644); err != nil {
		t.Fatalf("写干扰文件失败: %v", err)
	}
	got, err := SetPassphrase(cfg, "", testPass1, false)
	if err == nil {
		t.Fatalf("给配置文件加口令应被拒，实得 %+v", got)
	}
	if strings.Contains(err.Error(), testPass1) {
		t.Error("错误文本里出现了口令明文")
	}
	if string(readFileBytes(t, cfg)) != body {
		t.Error("被拒的动作改动了别人的配置文件")
	}
	if len(listNames(t, dir)) != 1 {
		t.Errorf("被拒后不得留下临时件：%v", listNames(t, dir))
	}
}

func TestSetPassphraseRejectsSymlinkTarget(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "keys")
	outside := filepath.Join(root, "outside")
	for _, d := range []string{dir, outside} {
		if err := os.Mkdir(d, 0o700); err != nil {
			t.Fatalf("建目录失败: %v", err)
		}
	}
	realKey := filepath.Join(outside, "id_real")
	writeKeyInto(t, realKey, "")

	link := filepath.Join(dir, "id_link")
	tryDirLink(t, link, realKey)

	got, err := SetPassphrase(link, "", testPass1, false)
	if err == nil {
		t.Fatalf("目标是链接时必须拒绝，实得 %+v", got)
	}
	if !errors.Is(err, ErrNotRegularFile) {
		t.Errorf("应报目标不是普通文件，实得: %v", err)
	}
	// 链接对面的真私钥没被动过，链接本身也还在
	if err := parsePrivBytes(t, readFileBytes(t, realKey), ""); err != nil {
		t.Errorf("链接对面的私钥被改了: %v", err)
	}
	if !fileExists(link) {
		t.Error("拒绝之后链接不该消失")
	}
	assertNoSecret(t, "链接目标拒绝", got, err, testPass1)
}

// TestSetPassphrasePreservesComment 验注释在封装层改写中被带回去。
// 注释是"日后在 authorized_keys 里认钥匙全靠它"的那段信息，丢了就得人工重贴。
func TestSetPassphrasePreservesComment(t *testing.T) {
	dir := t.TempDir()
	res := mustGenerate(t, dir, GenerateParams{Name: "id_cmt", Type: "ed25519", Comment: "deploy@prod"})
	withPass, err := SetPassphrase(res.PrivatePath, "", testPass1, false)
	if err != nil {
		t.Fatalf("加口令失败: %v", err)
	}
	if !samePath(withPass.PrivatePath, res.PrivatePath) {
		t.Errorf("返回路径应保持指向同一把钥匙：%q vs %q", withPass.PrivatePath, res.PrivatePath)
	}
	// 加了口令之后注释在加密段里，本层读不到（读不到 ≠ 丢了）。
	if got := commentFromPrivateKey(readFileBytes(t, res.PrivatePath)); got != "" {
		t.Errorf("加密私钥不该被读出注释，实得 %q", got)
	}
	// 再去口令：文件回到明文封装，注释应当仍在——这条才是"改口令把注释带回去了"的证据。
	if got, err := SetPassphrase(res.PrivatePath, testPass1, "", true); err != nil {
		t.Fatalf("去口令失败: %v", err)
	} else if c := commentFromPrivateKey(readFileBytes(t, res.PrivatePath)); c != "deploy@prod" {
		t.Errorf("来回改口令后注释丢失，实得 %q（got=%+v）", c, got)
	} else if got.Fingerprint != res.Fingerprint {
		t.Errorf("来回改口令指纹变了：%s → %s", res.Fingerprint, got.Fingerprint)
	}
	// .pub 里那行注释同样还在（本包压根没动它）
	pub, comment, _, _, perr := ssh.ParseAuthorizedKey(readFileBytes(t, res.PublicPath))
	if perr != nil {
		t.Fatalf("解 .pub 失败: %v", perr)
	}
	if comment != "deploy@prod" {
		t.Errorf(".pub 注释应为 deploy@prod，实得 %q", comment)
	}
	if ssh.FingerprintSHA256(pub) != res.Fingerprint {
		t.Error(".pub 与返回值指纹不一致")
	}
}

// TestSetPassphraseCommentsWhenProvenanceLost 验"注释带不回来"这条路径的实话：
// 带口令的私钥，注释在加密段里读不到；又没有配套 .pub 兜底 —— 此时新文件的注释
// 字段就是空的。包不假装无事发生，也不拿文件名编一条注释出来。
func TestSetPassphraseCommentsWhenProvenanceLost(t *testing.T) {
	dir := t.TempDir()
	// 只有私钥、没有 .pub（手工摆一把加密钥匙，绕开 Generate 的成对写入）
	fp := writeKeyFile(t, dir, "id_nopub", testPass1)
	if fileExists(filepath.Join(dir, "id_nopub.pub")) {
		t.Fatal("writeKeyFile 不该写 .pub，测试前置条件错了")
	}
	res, err := SetPassphrase(filepath.Join(dir, "id_nopub"), testPass1, testPass2, false)
	if err != nil {
		t.Fatalf("换口令失败: %v", err)
	}
	if res.Fingerprint != fp {
		t.Errorf("指纹应不变：%s → %s", fp, res.Fingerprint)
	}
	if !strings.Contains(res.Message, "未取回原注释") {
		t.Errorf("文案应把注释没带回来这件事说明白，实得 %q", res.Message)
	}
	assertNoSecret(t, "注释来源丢失", res, err, testPass1, testPass2)

	// 对照实验：同一把钥匙配上 .pub 之后注释就有来源了——证明上面那句实话来自"确实没处取"，
	// 不是文案写死。这里的 .pub 是**另一把钥匙**的（内容故意不配对）：本用例只关心
	// 注释字段从哪来，不校验配对关系，所以取别的钥匙的 .pub 反而更能说明"只读注释"。
	otherDir := t.TempDir()
	other := mustGenerate(t, otherDir, GenerateParams{Name: "id_helper", Type: "ed25519", Comment: "recovered@note"})
	pubPath := filepath.Join(dir, "id_nopub.pub")
	if err := os.WriteFile(pubPath, readFileBytes(t, other.PublicPath), pubFileMode); err != nil {
		t.Fatalf("写 .pub 失败: %v", err)
	}
	body := readFileBytes(t, filepath.Join(dir, "id_nopub"))
	if c := commentFromPrivateKey(body); c != "" {
		t.Errorf("加密私钥不该被读出注释，实得 %q", c)
	}
	res2, err := SetPassphrase(filepath.Join(dir, "id_nopub"), testPass2, "", true)
	if err != nil {
		t.Fatalf("去口令失败: %v", err)
	}
	if strings.Contains(res2.Message, "未取回原注释") {
		t.Errorf("有 .pub 兜底时不该再报注释丢失，实得 %q", res2.Message)
	}
	// 注释真的从 .pub 被带进了新私钥段（去口令后就是明文，读得到）
	if c := commentFromPrivateKey(readFileBytes(t, filepath.Join(dir, "id_nopub"))); c != "recovered@note" {
		t.Errorf(".pub 的注释应被带进新私钥，实得 %q", c)
	}
	if res2.Fingerprint != fp {
		t.Errorf("借 .pub 取注释不该影响指纹：%s → %s", fp, res2.Fingerprint)
	}
}

// TestCommentFromPrivateKeyCoverage 单独钉住注释提取的四种形态。
// 这条函数是"改口令不丢注释"的唯一实现，认不出来必须回空串而不是报错——
// 注释是尽力而为的元数据，为它把整个改口令动作失败掉是本末倒置。
func TestCommentFromPrivateKeyCoverage(t *testing.T) {
	plain, _ := genKeyPEM(t, "ed25519", "", "plain@note")
	guarded, _ := genKeyPEM(t, "ed25519", testPass1, "guarded@note")

	rsaKey, kerr := rsa.GenerateKey(rand.Reader, 2048)
	if kerr != nil {
		t.Fatalf("生成 rsa 失败: %v", kerr)
	}
	legacyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY", // 老式 PKCS#1：x/crypto 能解，但注释不在本层认得的位置
		Bytes: x509.MarshalPKCS1PrivateKey(rsaKey),
	})

	cases := []struct {
		why  string
		body []byte
		want string
	}{
		{"新格式未加密（注释在私钥段里，此时是明文）", plain, "plain@note"},
		// 这条是**实测更正**换来的：注释与密钥本体一起被加密封装，
		// 所以带口令的文件在这里读不到注释——这是设计上的限制，不是解析没写完。
		{"新格式已加密（注释在加密段内，本层不解它，交给 .pub 兜底）", guarded, ""},
		{"老式 PKCS#1（本层认不出注释，回空串交给 .pub 兜底）", legacyPEM, ""},
		{"不是 PEM", []byte("Host example\n"), ""},
		{"空字节", nil, ""},
		{"PEM 头对但内容不是 openssh 格式", pem.EncodeToMemory(&pem.Block{Type: "OPENSSH PRIVATE KEY", Bytes: []byte("乱来")}), ""},
	}
	for _, c := range cases {
		t.Run(c.why, func(t *testing.T) {
			if got := commentFromPrivateKey(c.body); got != c.want {
				t.Errorf("应得 %q，实得 %q", c.want, got)
			}
		})
	}
}

// parsePrivBytes 直接对字节判可否解开（不改盘，专给"链接对面那份没被动"这类断言用）。
func parsePrivBytes(t *testing.T, raw []byte, passphrase string) error {
	t.Helper()
	if passphrase == "" {
		_, err := ssh.ParseRawPrivateKey(raw)
		return err
	}
	_, err := ssh.ParseRawPrivateKeyWithPassphrase(raw, []byte(passphrase))
	return err
}
