package sshkeys

// 台账层的纯工具：形态判定与只读辅助。从 keyring.go 拆出来，是因为台账主体按
// "一把钥匙"归组后已经够长，混在一起两头都读不动。
//
// 注意：`redactSecret` **不在这里**——它随凭据测试住在 prober.go（与 sshauth.go 同批），
// 本文件刻意不重复定义第二份。

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rsa"
	"encoding/base64"
	"errors"
	"os"
	"strings"

	"golang.org/x/crypto/ssh"
)

// keyHeadBytes 是判形态所需的最少字节数。判口令只看头部结构，不需要整把钥匙——
// 少读一点就少一份"整文件内容进过内存"的可能。
const keyHeadBytes = 8192

// opensshKeyMagic 是 OpenSSH 新格式私钥解 base64 后的固定前导（含结尾 NUL）。
const opensshKeyMagic = "openssh-key-v1\x00"

// readHead 只读前 n 字节。读不满也照样返回已读到的部分——判形态看的是头部结构。
func readHead(path string, n int) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	buf := make([]byte, n)
	read, rErr := f.Read(buf)
	if read <= 0 {
		if rErr != nil {
			return nil, rErr
		}
		return nil, errors.New("空文件")
	}
	return buf[:read], nil
}

// parsePubBytes 解析 authorized_key 形态的公钥字节，收敛成"带 ok 的两值"。
//
// 这一版 x/crypto 的签名是 `(out PublicKey, comment string, options []string,
// rest []byte, err error)`——**comment 是第二个值**，与部分上游新版顺序不同；每个调用点
// 各数一遍极易数错（本包就为此连吃两次编译错）。所以只在这里数一次。
func parsePubBytes(raw []byte) (ssh.PublicKey, string, bool) {
	if len(raw) == 0 {
		return nil, "", false
	}
	pub, comment, _, _, err := ssh.ParseAuthorizedKey(raw)
	if err != nil || pub == nil {
		return nil, "", false
	}
	return pub, strings.TrimSpace(comment), true
}

// detectGuard 判私钥是否受口令保护，返回 (是否加密, 判定是否成功)。
//
// 为什么必须有第三态：把"判不出来"报成"无口令"，等于在界面上宣称"这把钥匙谁拿到
// 都能直接登录"，那是**反方向**的错；报成"有口令"同样有害——人会以为自动化用不了它。
//
//   - OpenSSH 新格式：PEM 包 base64，解出字节以 openssh-key-v1\0 开头，紧跟
//     4 字节大端长度 + **cipherName**，值为 "none" 即未加密；
//   - 老式 PEM：头部含 `Proc-Type: 4,ENCRYPTED`。
func detectGuard(head []byte) (guarded bool, known bool) {
	s := string(head)
	if strings.Contains(s, "Proc-Type: 4,ENCRYPTED") {
		return true, true
	}
	body := armorBody(s, "BEGIN OPENSSH PRIVATE KEY")
	if body == "" {
		return false, false
	}
	blob, err := base64.StdEncoding.DecodeString(collapseWS(body))
	if err != nil || len(blob) < len(opensshKeyMagic)+4 {
		return false, false
	}
	if !strings.HasPrefix(string(blob), opensshKeyMagic) {
		return false, false
	}
	cipher, ok := readWireString(blob[len(opensshKeyMagic):])
	if !ok {
		return false, false
	}
	switch cipher {
	case "none":
		return false, true
	case "":
		return false, false
	default:
		return true, true // 如 aes256-gcm@openssh.com
	}
}

// readWireString 取 OpenSSH wire 格式里一个 4 字节大端长度前缀的字符串。
func readWireString(b []byte) (string, bool) {
	if len(b) < 4 {
		return "", false
	}
	n := int(uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3]))
	// 256 的封顶是防御：cipherName/kdfName 都是短 ASCII 名，超长即形态已错位，
	// 继续读只会把一个无意义切片当结论，不如报"判不出来"。
	if n <= 0 || n > 256 || len(b) < 4+n {
		return "", false
	}
	return string(b[4 : 4+n]), true
}

// armorBody 抠出 PEM 两条标记行之间的 base64 正文。
func armorBody(s, marker string) string {
	i := strings.Index(s, marker)
	if i < 0 {
		return ""
	}
	rest := s[i+len(marker):]
	j := strings.IndexByte(rest, '\n')
	if j < 0 {
		return ""
	}
	rest = rest[j+1:]
	if k := strings.Index(rest, "-----END"); k >= 0 {
		rest = rest[:k]
	}
	return rest
}

func collapseWS(s string) string { return strings.Join(strings.Fields(s), "") }

// keyBits 尽力给出位数/曲线宽度，取不到返回 0（纯展示，不参与任何判定）。
func keyBits(pub ssh.PublicKey) int {
	cpk, ok := pub.(ssh.CryptoPublicKey)
	if !ok {
		return 0
	}
	switch k := cpk.CryptoPublicKey().(type) {
	case *rsa.PublicKey:
		return k.N.BitLen()
	case *ecdsa.PublicKey:
		if k.Curve != nil {
			return k.Curve.Params().BitSize
		}
	case ed25519.PublicKey:
		return 256
	}
	return 0
}
