package keyops

// 测试共用件。原则只有一条：**全都用真密钥**——真 crypto/ed25519、真 crypto/rand、
// 真 x/crypto/ssh 导出与解析，落真的临时目录。口令也是真串（含符号与数字），
// 因为"断言返回值与错误里搜不到口令"这件事，用假日令糊弄是测不出名堂的。

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"encoding/pem"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

// 测试口令：够独特，任何一处把它抄进返回值或错误文本都会被断言抓到。
const (
	testPass1  = "Sup3r-S3cret-pp!"
	testPass2  = "An0ther-Passphrase#"
	testPubPfx = "SHA256:"
)

// mustGenerate 生成一把钥匙，失败即 t.Fatal。
// 失败文本里只打 name/type/bits 这些无害字段——口令不进日志这条规矩，
// 在测试里也该照着做，否则跑一次失败就顺手把测试口令印出来了。
func mustGenerate(t *testing.T, dir string, p GenerateParams) Result {
	t.Helper()
	res, err := Generate(dir, p)
	if err != nil {
		t.Fatalf("Generate(name=%q type=%q bits=%d) 失败: %v", p.Name, p.Type, p.RSABits, err)
	}
	return res
}

// genKeyPEM 真生成一把密钥并导出 PEM（passphrase 空即不加密）。
// 用于需要"绕过本包直接摆一个文件在盘上"的用例（坏文件、孤儿公钥、伪造私钥等）。
func genKeyPEM(t *testing.T, kind, passphrase, comment string) (privPEM []byte, fp string) {
	t.Helper()
	var raw any
	switch kind {
	case "ed25519":
		// 返回顺序 (PublicKey, PrivateKey, error)：这里只要私钥，公钥用 _ 明确丢掉。
		_, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatalf("生成 ed25519 失败: %v", err)
		}
		raw = priv
	case "rsa":
		k, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatalf("生成 rsa 失败: %v", err)
		}
		raw = k
	default:
		t.Fatalf("测试用例写错了：未知密钥种类 %q", kind)
	}
	signer, err := ssh.NewSignerFromKey(raw)
	if err != nil {
		t.Fatalf("转 signer 失败: %v", err)
	}
	block, err := marshalBlock(raw, comment, passphrase)
	if err != nil {
		t.Fatalf("导出私钥失败: %v", err)
	}
	return pem.EncodeToMemory(block), ssh.FingerprintSHA256(signer.PublicKey())
}

// writeKeyFile 在 dir 下写一把真私钥（不写 .pub），返回其指纹。
func writeKeyFile(t *testing.T, dir, base, passphrase string) string {
	t.Helper()
	path := filepath.Join(dir, base)
	body, fp := genKeyPEM(t, "ed25519", passphrase, base+"@test")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatalf("写测试私钥失败: %v", err)
	}
	return fp
}

// writeKeyInto 把一把真私钥写到**任意指定路径**（不写 .pub），口令空即不加密。
// 与 writeKeyFile 分开的理由：围栏用例需要故意把钥匙放在目录外面。
func writeKeyInto(t *testing.T, path, passphrase string) string {
	t.Helper()
	body, fp := genKeyPEM(t, "ed25519", passphrase, "written-by-test")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatalf("写测试私钥失败: %v", err)
	}
	return fp
}

// listNames 返回目录下的文件名清单（升序）。
func listNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("读目录 %s 失败: %v", dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names
}

// fileExists 判路径存在（不管形态）。
func fileExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// readFileBytes 读文件原文，用于"文件未被破坏"的逐字节断言。
func readFileBytes(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读 %s 失败: %v", path, err)
	}
	return raw
}

// parsePriv 用给定口令解私钥，返回 error（nil 表示解开了）。
func parsePriv(t *testing.T, path, passphrase string) error {
	t.Helper()
	raw := readFileBytes(t, path)
	if passphrase == "" {
		_, err := ssh.ParseRawPrivateKey(raw)
		return err
	}
	_, err := ssh.ParseRawPrivateKeyWithPassphrase(raw, []byte(passphrase))
	return err
}

// mustPassphraseMissing 断言"不带口令解这把私钥"报的是 ssh.PassphraseMissingError，
// 也就是**口令真的生效了**，而不是"看着像加密其实没加密"。
func mustPassphraseMissing(t *testing.T, path string) {
	t.Helper()
	_, err := ssh.ParsePrivateKey(readFileBytes(t, path))
	if err == nil {
		t.Fatalf("%s 不带口令就解开了，说明口令没生效", path)
	}
	var missing *ssh.PassphraseMissingError
	if !errors.As(err, &missing) {
		t.Fatalf("%s 的错误类型不是 PassphraseMissingError，实为 %T: %v", path, err, err)
	}
}

// assertNoSecret 断言：口令明文既不在 Result 的 JSON 里，也不在错误文本里；
// 返回值与错误里也不许出现私钥 PEM 标记。这是安全设计第 3 条的钉子。
func assertNoSecret(t *testing.T, label string, res Result, err error, secrets ...string) {
	t.Helper()
	b, jerr := json.Marshal(res)
	if jerr != nil {
		t.Fatalf("%s: Result 序列化失败: %v", label, jerr)
	}
	hay := string(b)
	texts := []string{"Result JSON"}
	if err != nil {
		hay += "\n" + err.Error()
		texts = append(texts, "error 文本")
	}
	for i, s := range secrets {
		if s == "" {
			continue
		}
		if strings.Contains(hay, s) {
			t.Errorf("%s: 泄露断言失败——在 %s 里搜到了口令明文（第 %d 个）", label, strings.Join(texts, " 与 "), i+1)
		}
	}
	if strings.Contains(hay, "PRIVATE KEY") {
		t.Errorf("%s: 泄露断言失败——返回值或错误文本里出现了私钥 PEM 标记", label)
	}
}

// withFailingWrites 把唯一的落盘接缝换成"第 failAt 次调用即失败"。
//
// 为什么需要它：要验"私钥写成功、公钥写失败"时真的会清场，得能在两次写之间
// 精确掐断一次。真机上造不出这个时机（磁盘满不可控），所以走接缝——
// 这是**故障注入**，不是拿假日令糊弄测试。返回被调用过的路径清单供断言。
func withFailingWrites(t *testing.T, failAt int) *[]string {
	t.Helper()
	orig := writeNewFile
	var calls []string
	writeNewFile = func(path string, data []byte, mode os.FileMode) error {
		calls = append(calls, path)
		if len(calls) == failAt {
			return errors.New("测试注入：这一步写盘失败")
		}
		return orig(path, data, mode)
	}
	t.Cleanup(func() { writeNewFile = orig })
	return &calls
}

// tryDirLink 在 link 处造一条指向 target 的链接，优先软链接，
// Windows 上退而求其次造 junction（mklink /J，普通权限即可，正是本仓踩过的那类雷）。
//
// 造不出来就 t.Skip 并说明原因——**不伪装成"测过了"**。
// 造出来之后只硬断"穿过它确实能读到 target 里的东西"（链接没生效的用例必须 skip，
// 不能假绿）；围栏抓不抓得住由调用方的硬断言（拒绝 + 目标文件完好）说了算，
// **不断言"靠的是哪一种机制"**——EvalSymlinks 与逐段重解析点判定只要有一条在工作，
// 结果就该是对的，把机制写死成断言只会得到假红。
func tryDirLink(t *testing.T, link, target string) {
	t.Helper()
	var symlinkErr error
	if symlinkErr = os.Symlink(target, link); symlinkErr != nil {
		if runtime.GOOS != "windows" {
			t.Skipf("本机无法创建链接（%v），跳过链接逃逸用例", symlinkErr)
		}
		out, jerr := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput()
		if jerr != nil {
			t.Skipf("本机既造不出软链接（%v），也造不出 junction（%v：%s），跳过链接逃逸用例",
				symlinkErr, jerr, strings.TrimSpace(string(out)))
		}
	}
	reportLinkMechanism(t, link, target)
	if !linkReaches(link, target) {
		t.Skipf("链接造出来了但穿过它读不到目标（%s → %s），这条用例在此环境不成立", link, target)
	}
}

// reportLinkMechanism 把 EvalSymlinks 对这个链接的解析结果记进日志（**只记录不断言**）。
// 本机实测到的事实值得留档：Go 的 EvalSymlinks 解析软链接，但**不解析 Windows junction**
// ——这正是围栏必须另加"逐段重解析点判定"的原因（见 fence.go 头部与 reparse_windows.go）。
func reportLinkMechanism(t *testing.T, link, target string) {
	t.Helper()
	real, err := filepath.EvalSymlinks(link)
	want, werr := filepath.EvalSymlinks(target)
	switch {
	case err != nil:
		t.Logf("EvalSymlinks(%s) 报错：%v", link, err)
	case werr == nil && samePath(real, want):
		t.Logf("EvalSymlinks 解析到了目标之外：%s → %s", link, real)
	default:
		t.Logf("EvalSymlinks **没有**解析这条链接（得到 %q，目标真身 %q）——此时靠逐段重解析点判定拦截", real, want)
	}
}

// linkReaches 判穿过 link 是否真能读到 target（target 是目录时探它里的第一个条目）。
func linkReaches(link, target string) bool {
	if fi, err := os.Stat(target); err == nil && !fi.IsDir() {
		// 链接指向单个文件：Stat 会跟着链接走，断链则报错
		_, err := os.Stat(link)
		return err == nil
	}
	entries, err := os.ReadDir(target)
	if err != nil || len(entries) == 0 {
		return false
	}
	_, err = os.Stat(filepath.Join(link, entries[0].Name()))
	return err == nil
}

// samePath 平台感知的路径比较：Windows 大小写不敏感。
func samePath(a, b string) bool {
	return foldVolumeCase(filepath.Clean(a)) == foldVolumeCase(filepath.Clean(b))
}
