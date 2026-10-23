package keyops

// 与真 OpenSSH 的互操作取证。
//
// 为什么单开这一组：前面所有用例都是"Go 写、Go 读"，自洽不等于能用——本包的产物
// 最终要交给系统 ssh 与 ssh-agent，本包要改的也常是别人（ssh-keygen）写下的文件。
// 这两条边界只有真二进制能验。这一组用例就是把本包注释里那句"OpenSSH 格式是……"
// 从推断改成实证的地方，而且它确实推翻过一次推断（结构尾部多挂一段注释，
// ssh-keygen 会直接把整把钥匙判成 `error in libcrypto`）。
//
// 找不到 ssh-keygen 就整组跳过，并把"跳过了什么"如实报出来。

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

// sshKeygenPath 定位可用的 ssh-keygen；没有就跳过当前用例。
func sshKeygenPath(t *testing.T) string {
	t.Helper()
	p, err := exec.LookPath("ssh-keygen")
	if err != nil {
		t.Skipf("本机 PATH 里没有 ssh-keygen，跳过与真 OpenSSH 的互操作用例（这条事实需如实上报）")
	}
	return p
}

// runSSHKeygen 跑一次 ssh-keygen 并返回输出；非零退出即把命令与输出一起报出来。
func runSSHKeygen(t *testing.T, args ...string) string {
	t.Helper()
	bin := sshKeygenPath(t)
	out, err := exec.Command(bin, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("ssh-keygen %s 失败: %v\n输出: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out)
}

// trySSHKeygenFails 跑一次**期望失败**的 ssh-keygen，返回其输出。
func trySSHKeygenFails(t *testing.T, args ...string) string {
	t.Helper()
	bin := sshKeygenPath(t)
	out, err := exec.Command(bin, args...).CombinedOutput()
	if err == nil {
		t.Fatalf("ssh-keygen %s 本应失败却成功了，输出: %s", strings.Join(args, " "), strings.TrimSpace(string(out)))
	}
	return string(out)
}

// TestInteropSystemSSHCanReadGeneratedKeys 验本包产物是真 ssh 认的私钥。
// 这一步不过，"生成的钥匙可以直接拿去登录"这句话就不该说出口。
func TestInteropSystemSSHCanReadGeneratedKeys(t *testing.T) {
	cases := []struct {
		why        string
		passphrase string
		comment    string
	}{
		{"未加密产物", "", "hanxi-plain"},
		{"带口令产物", testPass1, "hanxi-guarded"},
	}
	for _, c := range cases {
		t.Run(c.why, func(t *testing.T) {
			dir := t.TempDir()
			res := mustGenerate(t, dir, GenerateParams{
				Name: "id_interop", Type: "ed25519", Comment: c.comment, Passphrase: c.passphrase,
			})

			// 1. 系统 ssh 能把私钥读出公钥，且与本包返回的指纹同源
			args := []string{"-y", "-f", res.PrivatePath}
			if c.passphrase != "" {
				args = append(args, "-P", c.passphrase)
			}
			line := runSSHKeygen(t, args...)
			pub, _, _, _, err := ssh.ParseAuthorizedKey([]byte(line))
			if err != nil {
				t.Fatalf("ssh-keygen -y 打出的公钥行解不开: %v（输出 %q）", err, line)
			}
			if got := ssh.FingerprintSHA256(pub); got != res.Fingerprint {
				t.Errorf("系统 ssh 读出的指纹 %s 与本包返回的 %s 不一致", got, res.Fingerprint)
			}

			// 2. 错误口令必须被系统 ssh 拒掉——口令是真的在管着这把钥匙
			if c.passphrase != "" {
				bad := trySSHKeygenFails(t, "-y", "-f", res.PrivatePath, "-P", "Wrong-Passphrase-99")
				if !strings.Contains(bad, "passphrase") {
					t.Errorf("错误口令的输出应提到 passphrase，实得 %q", bad)
				}
			}

			// 3. .pub 也能被 ssh-keygen 认（它就是要贴到对端的那一行）
			runSSHKeygen(t, "-l", "-f", res.PublicPath)

			// 4. 未加密产物：注释写在私钥段里，ssh-keygen -l 读得回来（对得上 .pub）
			if c.passphrase == "" {
				listing := runSSHKeygen(t, "-l", "-f", res.PrivatePath)
				if !strings.Contains(listing, c.comment) {
					t.Errorf("ssh-keygen -l 应读回注释 %q，实得 %q", c.comment, listing)
				}
			}
		})
	}
}

// TestInteropSetPassphraseOnRealOpenSSHKey 是本包存在意义最强的一条：
// **用户用 ssh-keygen 建的钥匙，hanxi 改得动口令，改完系统 ssh 还认，指纹与注释都不丢。**
func TestInteropSetPassphraseOnRealOpenSSHKey(t *testing.T) {
	cases := []struct {
		why    string
		oldPP  string
		unprot bool // 起始状态是否未加密
	}{
		{"起始带口令", testPass1, false},
		{"起始未加密（加口令这条路）", "", true},
	}
	for _, c := range cases {
		t.Run(c.why, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "id_from_openssh")
			const comment = "note-from-openssh"
			runSSHKeygen(t, "-q", "-t", "ed25519", "-C", comment, "-N", c.oldPP, "-f", path)

			pub, _, _, _, err := ssh.ParseAuthorizedKey(readFileBytes(t, path+".pub"))
			if err != nil {
				t.Fatalf("解 ssh-keygen 的 .pub 失败: %v", err)
			}
			fp0 := ssh.FingerprintSHA256(pub)

			// 前置自证：未加密时注释确实在私钥段里读得到；带口令时读不到（本层不解密）。
			gotComment := commentFromPrivateKey(readFileBytes(t, path))
			if c.unprot && gotComment != comment {
				t.Fatalf("真 OpenSSH 未加密私钥的注释应读得到 %q，实得 %q", comment, gotComment)
			}
			if !c.unprot && gotComment != "" {
				t.Fatalf("带口令的私钥不该被读出注释，实得 %q", gotComment)
			}

			// 加/换口令
			newPP := testPass2
			res, err := SetPassphrase(path, c.oldPP, newPP, false)
			if err != nil {
				t.Fatalf("改 ssh-keygen 产物口令失败: %v", err)
			}
			if res.Fingerprint != fp0 {
				t.Errorf("改口令后指纹变了：%s → %s", fp0, res.Fingerprint)
			}
			// 改写后的文件必须仍被真 ssh 接受，且用的是新口令
			if line := runSSHKeygen(t, "-y", "-f", path, "-P", newPP); !strings.HasPrefix(line, "ssh-ed25519 ") {
				t.Errorf("ssh-keygen 用新口令读回应为 ed25519 公钥行，实得 %q", line)
			}
			// 回执里的路径是**解析后的真身**（本机 t.TempDir 给 8.3 短名时与之不同形），
			// 所以断言只跟它自己比，不跟调用方当初传进来的写法比。
			if !strings.HasSuffix(res.PublicPath, ".pub") || !fileExists(res.PublicPath) {
				t.Errorf("回执应指向那把配套公钥，实得 %q", res.PublicPath)
			}
			if res.PublicPath != res.PrivatePath+".pub" {
				t.Errorf("回执里公私钥路径应同源，实得 %q / %q", res.PrivatePath, res.PublicPath)
			}

			// 去掉口令之后再验注释：注释能读回来 = 它真的被写进了新私钥段（而非只在 .pub 里）
			res2, err := SetPassphrase(path, newPP, "", true)
			if err != nil {
				t.Fatalf("对真格式私钥去口令失败: %v", err)
			}
			if res2.Fingerprint != fp0 {
				t.Errorf("去口令后指纹变了：%s → %s", fp0, res2.Fingerprint)
			}
			runSSHKeygen(t, "-y", "-f", path, "-P", "")
			if c := commentFromPrivateKey(readFileBytes(t, path)); c != comment {
				t.Errorf("来回改口令后注释应为 %q，实得 %q", comment, c)
			}
			if listing := runSSHKeygen(t, "-l", "-f", path); !strings.Contains(listing, comment) {
				t.Errorf("改写后 ssh-keygen -l 应仍读得到注释，实得 %q", listing)
			}
			assertNoSecret(t, "真格式改口令", res2, nil, testPass1, testPass2, c.oldPP)
		})
	}
}

// TestInteropGeneratedKeyRoundTripsThroughSystemSSH 验一条产品闭环上的顺序：
// hanxi 生成 → 用户拿去 ssh-keygen/ssh-add 用 → 回到 hanxi 改口令 → 再给系统 ssh 用。
// 中间那一步本包完全不参与，所以这一步过了才说明产物是"标准钥匙"而不是"hanxi 私有家当"。
func TestInteropGeneratedKeyRoundTripsThroughSystemSSH(t *testing.T) {
	cases := []struct {
		why        string
		passphrase string
	}{
		{"未加密往返", ""},
		{"带口令往返", testPass1},
	}
	for _, c := range cases {
		t.Run(c.why, func(t *testing.T) {
			dir := t.TempDir()
			res := mustGenerate(t, dir, GenerateParams{
				Name: "id_round", Type: "ed25519", Comment: "round-trip", Passphrase: c.passphrase,
			})

			if c.passphrase != "" {
				// 系统侧改一次口令（`ssh-keygen -p -P 旧 -N 新`），再把产物交回本包。
				// 注意是 **-p**（改口令）而不是 -c：实测 -c 只动注释，给了 -N 也不重新封装。
				runSSHKeygen(t, "-p", "-P", c.passphrase, "-N", testPass2, "-f", res.PrivatePath)
			}

			// 本包接手别人（系统）封装过的文件：带口令的那把走"去口令"，
			// 未加密那把走"本来就无口令、无事可做"——两条都不该报错。
			var again Result
			var err error
			if c.passphrase != "" {
				again, err = SetPassphrase(res.PrivatePath, testPass2, "", true)
			} else {
				again, err = SetPassphrase(res.PrivatePath, "", "", false)
			}
			if err != nil {
				t.Fatalf("接手 ssh-keygen 产物失败: %v", err)
			}
			if again.Fingerprint != res.Fingerprint {
				t.Errorf("绕系统一圈回来指纹变了：%s → %s", res.Fingerprint, again.Fingerprint)
			}
			// 注释：未加密时本包能直接读回；带口令时由 .pub 兜住（两者都验）
			if c.passphrase == "" {
				if got := commentFromPrivateKey(readFileBytes(t, res.PrivatePath)); got != "round-trip" {
					t.Errorf("注释应仍为 round-trip，实得 %q", got)
				}
			}
			runSSHKeygen(t, "-y", "-f", res.PrivatePath, "-P", "")
			runSSHKeygen(t, "-l", "-f", res.PublicPath)
			assertNoSecret(t, "闭环往返", again, nil, testPass1, testPass2, c.passphrase)
		})
	}
}

// TestInteropOldFormatKeyRejectedNotCorrupted 验一条诚实的边界：喂给本包它解不动的
// 文件时，必须**报错而不是写出半截文件**。
func TestInteropOldFormatKeyRejectedNotCorrupted(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "id_broken")
	body := "-----BEGIN OPENSSH PRIVATE KEY-----\nYWJjZGVmCg==\n-----END OPENSSH PRIVATE KEY-----\n"
	writeFileBytes(t, path, []byte(body))
	writeFileBytes(t, path+".pub", []byte("ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIHfakefakefakefakefakefakefakefakefakefake broken@test\n"))

	res, err := SetPassphrase(path, "", testPass1, false)
	if err == nil {
		t.Fatalf("解不动的文件必须报错，实得 %+v", res)
	}
	if string(readFileBytes(t, path)) != body {
		t.Error("报错之后原文件被改写了")
	}
	if len(listNames(t, dir)) != 2 {
		t.Errorf("失败不得留下临时件：%v", listNames(t, dir))
	}
	assertNoSecret(t, "坏文件拒绝", res, err, testPass1)
}

// writeFileBytes 测试辅助：往任意路径写字节。
func writeFileBytes(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("写测试文件 %s 失败: %v", path, err)
	}
}
