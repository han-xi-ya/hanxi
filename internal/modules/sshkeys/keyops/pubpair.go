package keyops

// 公钥侧的两个补位动作。都是 2026-10-23 从界面的**操作死角**里长出来的：
//
//   - ExportPub：只有私钥、没有 `.pub` 的钥匙，界面上**部署不出去**（部署动作要选
//     公钥文件），也拿不到注释旁的公钥侧信息。典型来源就是"从服务器直接下回来的
//     私钥"——那种文件天生没有配套公钥。
//   - DeletePubOnly：只剩 `.pub`（私钥早已删除）的孤儿公钥，界面上**没有任何删除口**
//     ——DeletePair 刻意拒绝 `.pub` 路径（防"只删一半"）。于是这类垃圾只能人工去
//     文件夹里删，与本模块"看得见就管得动"的承诺不符。
//
// 两个动作合起来把"半把钥匙"的两种形态各自收口：**成对的走成对删，落单的走落单删**，
// 谁都不用绕过界面。

import (
	"encoding/binary"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/ssh"
)

const pubSuffix = ".pub"

// ExportPub 从私钥算出公钥并写出 `<私钥>.pub`。
//
// **带口令的私钥也能用**，这不是疏忽而是 OpenSSH 的格式事实：新格式私钥里被加密的
// 只有私钥段，`pubblob` 是明文摆在外层的（x/crypto 的 ParsePrivateKey 一律先要口令，
// 所以它"读不动"不等于"拿不到"）。这里直接走外层结构取 pubblob，**全程不接触口令、
// 也不解任何私钥字节**。
//
// comment 由调用方给（界面上让用户填或沿用台账里的）：加密私钥的注释在**被加密的
// 私钥段里**，拿不到就是拿不到——不拿文件名编一个注释出来，那会在别人的
// authorized_keys 里留下一个认不出的标签。
func ExportPub(dir, privatePath, comment string) (Result, error) {
	_, real, err := guardExistingPath(dir, privatePath)
	if err != nil {
		return Result{}, err
	}
	if strings.HasSuffix(strings.ToLower(real), pubSuffix) {
		return Result{}, fmt.Errorf("%w: 这里要的是私钥文件；若只想删公钥请用删除公钥的入口", ErrInvalidInput)
	}
	if _, err := requireRegularFile(real); err != nil {
		return Result{}, err
	}

	raw, err := readKeyFile(real)
	if err != nil {
		return Result{}, err
	}
	pub, err := publicFromPrivatePEM(raw)
	if err != nil {
		return Result{}, err
	}

	pubPath := real + pubSuffix
	if _, statErr := os.Stat(pubPath); statErr == nil {
		// 已存在就不覆盖：这一颗按钮的语义是"补出缺失的公钥"，不是"重写公钥"。
		// 真要保持一致应走一次显式覆盖确认，而那是另一个动作。
		return Result{}, fmt.Errorf("%w: 配套公钥 %s 已存在，无需补出", ErrExists, filepath.Base(pubPath))
	}

	line := authorizedLine(pub, strings.TrimSpace(comment))
	// 走包内唯一落盘接缝 writeNewFile（= atomicWrite 的可注入变量），**不直调 atomicWrite**：
	// 直调会绕过故障注入、也让"本包只有一处落盘"这条审计前提失效（generate/passphrase 都走它）。
	if err := writeNewFile(pubPath, line, pubFileMode); err != nil {
		return Result{}, fmt.Errorf("写公钥文件失败: %w", err)
	}
	// 写失败不留半截由 atomicWrite 保证；这里再确认落盘内容能读回，
	// 读回失败就把刚写的文件删掉——留一个 ssh 认不出的 .pub 比没有更糟。
	if back, rErr := os.ReadFile(pubPath); rErr != nil || !sameAuthorizedLine(back, line) {
		_ = os.Remove(pubPath)
		return Result{}, errors.New("公钥写后复核不一致，已回退未留文件")
	}

	return Result{
		PrivatePath: real,
		PublicPath:  pubPath,
		Fingerprint: fingerprintOf(pub),
		Type:        pub.Type(),
		Message: fmt.Sprintf("已从私钥算出公钥并写入 %s（指纹 %s）；私钥未作任何修改",
			filepath.Base(pubPath), fingerprintOf(pub)),
	}, nil
}

// DeletePubOnly 删除**没有配套私钥**的孤儿 `.pub`。
//
// 有配套私钥时坚决拒绝：只删公钥会让那把钥匙在界面上变成"缺配套公钥"的半截态，
// 而那正是 ExportPub 要解决的问题——该走成对删除就走成对删除，别从这里绕。
func DeletePubOnly(dir, path, confirm string) (Result, error) {
	_, real, err := guardExistingPath(dir, path)
	if err != nil {
		return Result{}, err
	}
	if !strings.HasSuffix(strings.ToLower(real), pubSuffix) {
		return Result{}, fmt.Errorf("%w: 这里只删公钥文件；私钥请用成对删除（会一并带走它的 .pub）", ErrInvalidInput)
	}
	if _, err := requireRegularFile(real); err != nil {
		return Result{}, err
	}
	sibling := strings.TrimSuffix(real, pubSuffix)
	if _, statErr := os.Stat(sibling); statErr == nil {
		return Result{}, fmt.Errorf("%w: %s 还在，这把不是孤儿公钥——请改用成对删除，别留下半把钥匙",
			ErrInvalidInput, filepath.Base(sibling))
	}
	// 与 DeletePair 同一道确认门：回显必须等于文件名，防手滑。
	if strings.TrimSpace(confirm) != filepath.Base(real) {
		return Result{}, fmt.Errorf("%w: 要把文件名原样输入一遍（%s）", ErrConfirmMismatch, filepath.Base(real))
	}
	fp := fingerprintOfPubFile(real)
	if err := os.Remove(real); err != nil {
		return Result{}, fmt.Errorf("删除公钥失败: %w", err)
	}
	return Result{
		PublicPath:  real,
		Fingerprint: fp,
		Message: fmt.Sprintf("已删除孤儿公钥 %s（配套私钥本就不存在）；远端机器上若装过它，那行授权不会被回收",
			filepath.Base(real)),
	}, nil
}

// publicFromPrivatePEM 从私钥文件取公钥：先试明文路径（x/crypto），失败再走外层
// pubblob（加密私钥的公开面本就是明文）。两条都不通才报错，不猜。
func publicFromPrivatePEM(raw []byte) (ssh.PublicKey, error) {
	if signer, err := ssh.ParsePrivateKey(raw); err == nil {
		return signer.PublicKey(), nil
	}
	block, _ := pem.Decode(raw)
	if block == nil || !strings.Contains(block.Headers["Type"]+block.Type, "OPENSSH PRIVATE KEY") {
		return nil, fmt.Errorf("%w: 不是可识别的 OpenSSH 私钥形态，未从该文件取公钥", ErrInvalidInput)
	}
	// **pem.Decode 已经把 base64 解过了**，block.Bytes 就是 openssh-key-v1 的原始字节；
	// 再解一次必然失败（这条是测试逼出来的：明文钥匙走 x/crypto 那条捷径，碰不到这里，
	// 所以只有"加密私钥补公钥"的用例能暴露它）。
	pubBytes, ok := publicBlobFromOpenSSHWire(block.Bytes)
	if !ok {
		return nil, fmt.Errorf("%w: 取不到私钥外层的公钥段，未做猜测", ErrInvalidInput)
	}
	pub, err := ssh.ParsePublicKey(pubBytes)
	if err != nil {
		return nil, fmt.Errorf("%w: 私钥外层的公钥段解析失败: %v", ErrInvalidInput, err)
	}
	return pub, nil
}

// publicBlobFromOpenSSHWire 走 openssh-key-v1 外层结构拿 pubblob：
// magic / cipherName / kdfname / kdfopts / nkeys / **pubblob** / privkeys(加密)。
//
// 关键点是**这里不要求 cipherName 为 "none"**——与 commentFromPrivateKey 那条路径不同：
// 注释在被加密的私钥段里所以要看 cipherName，而公钥段本身从来不在加密范围内。
// 多钥文件（nkeys≠1）不猜：OpenSSH 自己也只支持单钥，硬取第一把可能拿到错的那把。
func publicBlobFromOpenSSHWire(blob []byte) ([]byte, bool) {
	if len(blob) < len(opensshMagic) || string(blob[:len(opensshMagic)]) != opensshMagic {
		return nil, false
	}
	i := len(opensshMagic)
	var ok bool
	for range 3 { // cipherName / kdfname / kdfopts
		if _, i, ok = wireNextString(blob, i); !ok {
			return nil, false
		}
	}
	if i+4 > len(blob) {
		return nil, false
	}
	if n := int(binary.BigEndian.Uint32(blob[i : i+4])); n != 1 {
		return nil, false // 多钥文件不猜：OpenSSH 自己也只支持单钥
	}
	i += 4
	pub, _, ok := wireNextString(blob, i)
	return pub, ok
}

func sameAuthorizedLine(a, b []byte) bool {
	return strings.TrimSpace(string(a)) == strings.TrimSpace(string(b))
}
