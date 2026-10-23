package keyops

// 落盘与读回的共用小件。本包所有碰盘的动作都从这里过，一处的规矩管住全部：
// 写只走"同目录临时件 + fsync + rename"，读有大小闸门，权限位是**意图声明**。

import (
	"encoding/binary"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/ssh"
)

// 期望权限位：私钥 0600、公钥 0644。
//
// **如实写明它们的分量**：在 Windows 上 os 的 mode 位只是提示性的——NTFS 真正的
// 边界是 ACL（本仓刚实测过一次：Git Bash 的 `ls -l` 显示 `-rw-r--r--`，而 icacls 里
// 实际只有 SYSTEM/管理员/本人三条）。所以 0600 **不是安全边界**，它是尽力而为的意图
// 声明：在 POSIX 上确实生效，在 Windows 上聊胜于无。真要把钥匙守住的是账户与 ACL，
// 不是这里的一个八进制数。
const (
	privFileMode os.FileMode = 0o600
	pubFileMode  os.FileMode = 0o644

	// maxKeyFileBytes 读闸门。私钥文件撑死几十 KB（RSA 16384 位也就十几 KB），
	// 被喂一个 2GB 文件时"整读进来再判断不是私钥"等于自我 DoS，所以先拒。
	maxKeyFileBytes = 1 << 20

	// opensshMagic 是 OpenSSH 新格式私钥解 PEM 后的固定前导（含结尾 NUL）。
	opensshMagic = "openssh-key-v1\x00"

	// tempPrefix 临时件前缀。留个可辨认的名字，万一进程被硬杀留下残骸，
	// 人也看得出是谁家的临时件、该不该删。
	tempPrefix = ".hanxi-keyops"
)

// writeNewFile 是本包唯一的落盘接缝。
//
// 它是**包级变量**，不为性能、不为抽象，只为一件事：单测把它换成"第 N 次调用即失败"，
// 就能真的演一遍"私钥写成功、公钥写失败"这种在真机上几乎造不出来的分支，
// 验证失败清场（安全设计第 7 条）到底有没有生效。同包 sshHomeDir 的测试接缝是同一手法。
var writeNewFile = atomicWrite

// atomicWrite 写一个文件：同目录临时件 → 写完 fsync → rename 顶到目标名。
//
// 为什么不是 os.WriteFile 一把梭：直写目标名时，磁盘满/进程被杀都会留下一个
// **顶着合法名字的上半身残废文件**——台账按内容判形态，会把它报成"判不出口令的钥匙"，
// 用户看到的就是"我的钥匙坏了"。tmp+rename 把这种状态从盘上抹掉：目标名要么是没有，
// 要么是完整的一份。fsync 是配套的一半：不 sync 就 rename，掉电后 rename 可能在数据之前。
//
// 已存在的目标是**被替换**而不是被改写（rename 语义），所以不会跟着软链接写到别处去；
// 但链接本身会被顶掉，因此上层用 requireRegularFile 提前把链接拒了。
func atomicWrite(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, tempPrefix+"-"+sanitizeForTemp(filepath.Base(path))+"-*.tmp")
	if err != nil {
		return fmt.Errorf("在 %s 创建临时文件失败: %w", dir, err)
	}
	tmpName := tmp.Name()
	// 失败路径统一：关句柄、删临时件。成功 rename 后临时件已不存在，Remove 自然落空，
	// 不值得为它编状态标记，也不该因为"删除失败是预期内的"去检查这个返回值。
	defer func() {
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmpName)
		}
	}()

	if _, err = tmp.Write(data); err != nil {
		return fmt.Errorf("写临时文件 %s 失败: %w", tmpName, err)
	}
	if err = tmp.Sync(); err != nil {
		return fmt.Errorf("落盘（fsync）临时文件 %s 失败: %w", tmpName, err)
	}
	// chmod 在 rename 之前：文件以目标名现身的那一刻就该是最终权限。
	if err = tmp.Chmod(mode); err != nil {
		return fmt.Errorf("设定权限位 %o 失败: %w", mode, err)
	}
	if err = tmp.Close(); err != nil {
		return fmt.Errorf("关闭临时文件 %s 失败: %w", tmpName, err)
	}
	if err = os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("改名落到 %s 失败: %w", path, err)
	}
	return nil
}

// sanitizeForTemp 把基名收敛成临时件模板里安全的片段。
// 目标文件的名字是盘上已有的（可能含空格、点、Unicode），照抄进 CreateTemp 的
// pattern 只会撞上手写分隔符；这里只保留字母数字与 `._-`，其余打点，长度封顶。
func sanitizeForTemp(base string) string {
	var b strings.Builder
	for _, r := range base {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
		if b.Len() >= 24 {
			break
		}
	}
	if b.Len() == 0 {
		return "key"
	}
	return b.String()
}

// readKeyFile 读私钥文件原文，带大小闸门与形态闸门（必须是普通文件）。
//
// 读回来的字节只在本次调用里活着：它既不进 Result，也不进错误文本——
// 错误文案里只放路径与一句结论，"把文件内容摘录进错误"是最容易顺手做出的泄露。
func readKeyFile(path string) ([]byte, error) {
	if _, err := requireRegularFile(path); err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("%w: %s", ErrTargetMissing, path)
		}
		return nil, fmt.Errorf("打开 %s 失败: %w", path, err)
	}
	defer f.Close()
	// 多读 1 字节：正好读满闸门即"超限"，不必先 Stat 再开一次（TOCTOU 也少一处）。
	head, err := io.ReadAll(io.LimitReader(f, maxKeyFileBytes+1))
	if err != nil {
		return nil, fmt.Errorf("读取 %s 失败: %w", path, err)
	}
	if len(head) > maxKeyFileBytes {
		return nil, fmt.Errorf("%w: %s 超过 %d 字节，不像私钥文件（可能是别的用途的大文件，拒绝整读进内存）",
			ErrNotRegularFile, path, maxKeyFileBytes)
	}
	if len(head) == 0 {
		return nil, fmt.Errorf("%w: %s 是空文件，不是私钥", ErrTargetMissing, path)
	}
	return head, nil
}

// pubCommentFromFile 尽力从配套 .pub 取注释（公钥本就可公开，读它不需要口令）。
// 取不到就回空串——**不拿文件名冒充注释**，与父包 keyring.go 同一口径。
func pubCommentFromFile(privatePath string) string {
	raw, err := os.ReadFile(privatePath + ".pub")
	if err != nil || len(raw) > maxKeyFileBytes {
		return ""
	}
	_, comment, perr := publicKeyOfBytes(raw)
	if perr != nil {
		return ""
	}
	return comment
}

// authorizedLine 组装 .pub 的整行内容：`算法 base64 注释\n`。
// 注释为空就不带尾随空格——多一个空格在 authorized_keys 里就是一个空注释字段，
// 台账会把它读成"有注释但为空"，不如干脆不写。
func authorizedLine(pub ssh.PublicKey, comment string) []byte {
	line := string(ssh.MarshalAuthorizedKey(pub)) // 自带结尾换行
	line = strings.TrimSuffix(line, "\n")
	if comment != "" {
		line += " " + comment
	}
	return []byte(line + "\n")
}

// commentFromPrivateKey 取私钥文件的注释字段（尽力而为，取不到回空串）。
//
// 这条路径上有一句**实测更正**，值得留着：注释不在外层结构的尾部，而是在
// 私钥段（privblob 那一个 string）**里面**，排在密钥本体字段之后、填充字节之前。
// 我一开始按"外层尾部"读，读到的全是空串，而且更糟的是——照着这个理解往结构尾部
// 补挂一段，真 ssh-keygen 会报 `Load key …: error in libcrypto` 把整把钥匙拒收
// （见 interop_test.go 那组用例）。格式上的事不靠想象。
//
// 由此得到一条真实限制：注释与密钥本体一起被加密封装，所以**只有未加密的私钥**
// 能在这里读回注释。改一把带口令又没有配套 .pub 的钥匙时，注释带不回去
// （x/crypto 只交出解出来的密钥，不交出解密后的私钥段）；这条在 passphrase.go 里
// 有对应的实话文案。
//
// 拿不到就回空串：注释是展示用元数据，为它让整个改口令失败是本末倒置。
func commentFromPrivateKey(pemBytes []byte) string {
	block, _ := pem.Decode(pemBytes)
	if block == nil || !strings.Contains(block.Type, "PRIVATE KEY") {
		return ""
	}
	return openSSHBlobComment(block.Bytes)
}

// openSSHBlobComment 走外层头部，再把未加密的私钥段交给 commentFromPrivateSection。
//
// 外层结构：magic / cipher / kdfname / kdfopts / nkeys(u32) / pubblob×n / privblob
// 其中只有 cipher 与本函数有关："none" 之外一律回空串——那时 privblob 是密文，
// 注释在密文里，本层不去解密（也不该解：解密的活儿 x/crypto 已经做了）。
func openSSHBlobComment(blob []byte) string {
	if len(blob) < len(opensshMagic) || string(blob[:len(opensshMagic)]) != opensshMagic {
		return ""
	}
	i := len(opensshMagic)
	var val []byte
	var ok bool
	if val, i, ok = wireNextString(blob, i); !ok {
		return ""
	}
	if string(val) != "none" {
		return ""
	}
	// kdfname / kdfopts
	for range 2 {
		if _, i, ok = wireNextString(blob, i); !ok {
			return ""
		}
	}
	if i+4 > len(blob) {
		return ""
	}
	n := int(binary.BigEndian.Uint32(blob[i : i+4]))
	i += 4
	if n != 1 { // 单钥文件是常态（OpenSSH 自己也只支持单钥），多钥不猜
		return ""
	}
	for range n { // pubblob：只跳过，注释不在公钥段里
		if _, i, ok = wireNextString(blob, i); !ok {
			return ""
		}
	}
	priv, _, ok := wireNextString(blob, i)
	if !ok {
		return ""
	}
	return commentFromPrivateSection(priv)
}

// privateSectionFields 是每个算法在私钥段里排在注释**之前**的字段段数。
// 出处是 OpenSSH 的 sshkey_private_serialize 与 x/crypto 的 openSSH*PrivateKey 结构：
//
//	ssh-ed25519:              pub, priv                       → 2
//	ssh-rsa:                  n, e, d, iqmp, p, q             → 6
//	ecdsa-sha2-nistp*:        curve, pub, d                   → 3
//	ssh-dss:                  p, q, g, y, x                   → 5
//
// 认不出的算法（sk 系列、未来新算法）一律回空串，不猜——注释丢了可以补，
// 把别人的字节当成注释带进新文件才是脏事。
var privateSectionFields = map[string]int{
	"ssh-ed25519":         2,
	"ssh-rsa":             6,
	"ecdsa-sha2-nistp256": 3,
	"ecdsa-sha2-nistp384": 3,
	"ecdsa-sha2-nistp521": 3,
	"ssh-dss":             5,
}

// commentFromPrivateSection 走未加密私钥段：
// check1 / check2 / keytype / 密钥本体若干段 / comment / 填充。
func commentFromPrivateSection(sec []byte) string {
	if len(sec) < 8 { // 两个 4 字节的校验码
		return ""
	}
	i := 8
	var val []byte
	var ok bool
	if val, i, ok = wireNextString(sec, i); !ok {
		return ""
	}
	fields, known := privateSectionFields[string(val)]
	if !known {
		return ""
	}
	for range fields {
		if _, i, ok = wireNextString(sec, i); !ok {
			return ""
		}
	}
	if val, _, ok = wireNextString(sec, i); !ok {
		return ""
	}
	s := strings.TrimRight(string(val), "\x00")
	if HasControlChar(s) || len(s) > maxCommentLen {
		return ""
	}
	return s
}

// wireNextString 读 OpenSSH wire 格式里一个 4 字节大端长度前缀的字符串，
// 返回内容与下一个读取位置。长度不可信（<=0 或越过缓冲区尾）即 ok=false。
func wireNextString(b []byte, i int) ([]byte, int, bool) {
	if i < 0 || i+4 > len(b) {
		return nil, i, false
	}
	n := int(binary.BigEndian.Uint32(b[i : i+4]))
	if n < 0 || i+4+n > len(b) {
		return nil, i, false
	}
	return b[i+4 : i+4+n], i + 4 + n, true
}

// privatePEM 导出私钥正文（OpenSSH 新格式，PEM 包裹）。
//
// **全包只有这一处把私钥摊成字节**，所以"它有没有被顺手塞进 Result 或错误文本"
// 这件事只需要审一个函数。口令在这里交给 x/crypto 做封装，本层不复述、不加工。
//
// 注释字段就交给 x/crypto 写进私钥段里，本层**不再手工补尾字段**：
// 实测（见 interop_test.go）在结构尾部多挂一段，真 ssh-keygen 会直接
// `Load key ...: error in libcrypto` 拒收整把钥匙。格式上的对不上一处，代价是
// 产物在系统 ssh 手里变成废物，所以这类"我以为规范是这样"的补丁一律不打。
func privatePEM(rawKey any, comment, passphrase string) ([]byte, error) {
	block, err := marshalBlock(rawKey, comment, passphrase)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(block), nil
}

// marshalBlock 是 x/crypto 两个导出函数的合流点：口令为空走不加密分支。
// 传进去的是 crypto.PrivateKey（值或指针都认，*ed25519.PrivateKey 由 x/crypto 内部
// 收敛成值），本层不重复那套类型判断。
func marshalBlock(rawKey any, comment, passphrase string) (*pem.Block, error) {
	if passphrase == "" {
		return ssh.MarshalPrivateKey(rawKey, comment)
	}
	return ssh.MarshalPrivateKeyWithPassphrase(rawKey, comment, []byte(passphrase))
}

// signerOf 把解出来的私钥转成 ssh signer，顺带当一道形态校验：
// 支持不了的密钥类型在这里就报错，不会带着它去写盘。
//
// 注意传进去的必须是**私钥**（*ed25519.PrivateKey / *rsa.PrivateKey）。
// 接到 ed25519 那两个返回值的顺序（先公后私）传成了公钥，报的就是
// `ssh: unsupported key type ed25519.PublicKey`——本仓踩过，故在此留字。
func signerOf(rawKey any) (ssh.Signer, error) {
	signer, err := ssh.NewSignerFromKey(rawKey)
	if err != nil {
		return nil, fmt.Errorf("这把私钥的算法类型本层不支持（%T）: %w", rawKey, err)
	}
	return signer, nil
}

// fingerprintOf 取 SHA256 指纹；nil 公钥回空串（调用方都是"尽力展示"的场合）。
func fingerprintOf(pub ssh.PublicKey) string {
	if pub == nil {
		return ""
	}
	return ssh.FingerprintSHA256(pub)
}

// publicKeyOfBytes 从 .pub 文件字节解出公钥与注释。
//
// 这一版本的 ssh.ParseAuthorizedKey 签名是
// `(out PublicKey, comment string, options []string, rest []byte, err error)`——
// comment 是**第二个**返回值，与部分上游新版顺序不同。多返回值在多个调用点各数一遍
// 极易数错，所以只在这里数一次、只暴露需要的两个。
func publicKeyOfBytes(raw []byte) (ssh.PublicKey, string, error) {
	pub, comment, _, _, err := ssh.ParseAuthorizedKey(raw)
	if err != nil {
		return nil, "", err
	}
	if pub == nil {
		return nil, "", errors.New("公钥解析返回空对象")
	}
	return pub, strings.TrimSpace(comment), nil
}
