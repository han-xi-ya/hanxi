package keyops

// 生成密钥对。
//
// 顺序上刻意的设计：**所有会失败的纯计算全部排在第一次碰盘之前**。
// 密钥材料生成、私钥导出、公钥行组装都在内存里先做完，失败就直接返回——此时盘上
// 一个字节都没动，不存在"清场"这件事。只有剩下两步写盘可能失败，那时靠
// tmp+rename 与显式回滚保证不留成对状态错乱的中间态（安全设计第 7 条）。

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"fmt"
	"os"

	"golang.org/x/crypto/ssh"
)

// Generate 在 dir 下生成一把新密钥对，返回私钥路径、公钥路径与公钥指纹。
//
// 产物是 OpenSSH 新格式（`openssh-key-v1`，PEM 包裹）：私钥 0600、公钥 0644，
// 公钥行是标准 authorized_keys 形态 `算法 base64 注释`，可直接贴到对端。
//
// 拒绝条件（都以哨兵错误返回，父包可映射成枚举）：
//   - Name / Type / RSABits / Comment 不合法 → ErrInvalidInput；
//   - dir 不存在、不是目录，或 Name 解析后落在 dir 外 → ErrOutsideDir 等；
//   - 私钥或 .pub 任一已存在且 Overwrite=false → ErrExists；
//   - 已存在的目标不是普通文件（目录、软链接、junction）→ ErrNotRegularFile，
//     **这条与 Overwrite 无关**：覆盖一个重解析点不是"覆盖一个文件"。
func Generate(dir string, p GenerateParams) (Result, error) {
	name, err := validateKeyName(p.Name)
	if err != nil {
		return Result{}, err
	}
	keyType, err := normalizeKeyType(p.Type)
	if err != nil {
		return Result{}, err
	}
	bits, err := normalizeRSABits(keyType, p.RSABits)
	if err != nil {
		return Result{}, err
	}
	comment, err := sanitizeComment(p.Comment)
	if err != nil {
		return Result{}, err
	}
	// p.Passphrase 到这里为止没被读过第二次：它只会被交给加密函数。
	// 不加校验也是有意的——口令的"强弱"属机主判断，本层无权替他决定，
	// 更不该把它抄进任何错误文案里"提示"一遍。

	_, privPath, err := guardNewPath(dir, name)
	if err != nil {
		return Result{}, err
	}
	pubPath := privPath + ".pub"

	privPreExisted, pubPreExisted, err := checkOverwrite(privPath, pubPath, p.Overwrite)
	if err != nil {
		return Result{}, err
	}

	// —— 以下全是纯计算，失败时盘上无痕 ——
	rawKey, sshPub, err := newKeyMaterial(keyType, bits)
	if err != nil {
		return Result{}, err
	}
	privPEM, err := privatePEM(rawKey, comment, p.Passphrase)
	if err != nil {
		return Result{}, fmt.Errorf("导出私钥失败: %w", err)
	}
	pubLine := authorizedLine(sshPub, comment)
	fp := fingerprintOf(sshPub)

	// —— 到这里才开始碰盘 ——
	if err = writeNewFile(privPath, privPEM, privFileMode); err != nil {
		return Result{}, fmt.Errorf("写私钥 %s 失败: %w", privPath, err)
	}
	if err = writeNewFile(pubPath, pubLine, pubFileMode); err != nil {
		// 私钥写成了、公钥没写成：这就是安全设计第 7 条要点名清除的中间态。
		// 只有"这把私钥是本次新建的"才谈得上回滚；覆盖模式下旧私钥已经被顶掉了，
		// 删掉新的并不能把旧的还回来，如实说明比假装干净更负责。
		if !privPreExisted {
			if rerr := os.Remove(privPath); rerr != nil && !errors.Is(rerr, os.ErrNotExist) {
				return Result{}, fmt.Errorf("写公钥 %s 失败，且回滚刚写的私钥也失败：%s 需手动处理: %w；原始错误: %v",
					pubPath, privPath, rerr, err)
			}
			return Result{}, fmt.Errorf("写公钥 %s 失败，已回滚刚写的私钥，目录回到生成前: %v", pubPath, err)
		}
		return Result{}, fmt.Errorf("写公钥 %s 失败，私钥 %s 已被新钥匙覆盖且没有配套公钥（可再跑一次生成，或删掉私钥重来）: %v",
			pubPath, privPath, err)
	}

	msg := fmt.Sprintf("已生成 %s 密钥 %s", sshPub.Type(), name)
	if p.Passphrase != "" {
		msg += "，并已开启口令保护"
	} else {
		msg += "，未设置口令（私钥在磁盘上是明文可读的）"
	}
	if pubPreExisted {
		msg += "；原有配套公钥已被替换"
	}

	return Result{
		PrivatePath: privPath,
		PublicPath:  pubPath,
		Fingerprint: fp,
		Type:        sshPub.Type(),
		Guarded:     p.Passphrase != "",
		Message:     msg,
	}, nil
}

// checkOverwrite 覆盖闸门 + "已存在目标必须是普通文件"判定。
//
// 为什么要连 .pub 一起判：只剩一把孤儿公钥时直接生成会写出"新私钥 + 旧公钥"这种
// **指纹对不上**的配对，比报一句"已存在"恶劣得多——用户会拿着对不上号的公钥去部署。
func checkOverwrite(privPath, pubPath string, overwrite bool) (privExisted, pubExisted bool, err error) {
	if fi, lerr := os.Lstat(privPath); lerr == nil {
		privExisted = true
		if err = ensureWritableTarget(privPath, fi); err != nil {
			return
		}
	} else if !errors.Is(lerr, os.ErrNotExist) {
		return false, false, fmt.Errorf("检查 %s 失败: %w", privPath, lerr)
	}
	if fi, lerr := os.Lstat(pubPath); lerr == nil {
		pubExisted = true
		if err = ensureWritableTarget(pubPath, fi); err != nil {
			return
		}
	} else if !errors.Is(lerr, os.ErrNotExist) {
		return false, false, fmt.Errorf("检查 %s 失败: %w", pubPath, lerr)
	}

	if (privExisted || pubExisted) && !overwrite {
		who := privPath
		if !privExisted {
			who = pubPath
		}
		return privExisted, pubExisted, fmt.Errorf(
			"%w: %s 已经存在（配套文件也一并检查过）。覆盖会**永久毁掉**原来那把钥匙，"+
				"必须显式确认（Overwrite=true）才动手", ErrExists, who)
	}
	return privExisted, pubExisted, nil
}

// newKeyMaterial 生成密钥材料，返回「可直接交给导出函数的私钥本体」与「它的 ssh 公钥」。
//
// 为什么不返回 ssh.Signer：这一层的两个用途（导出私钥、算指纹）分别需要
// crypto.PrivateKey 与 ssh.PublicKey，而 ssh.Signer 上**没有** PrivateKey 方法，
// 拿 signer 反而要在调用方再绕一圈。顺带说清 ed25519 那条：GenerateKey 的返回顺序是
// **(PublicKey, PrivateKey, error)**——先公后私，与直觉相反。接反了就是把公钥当私钥去
// 导出，报 `ssh: unsupported key type ed25519.PublicKey`（本仓今天刚踩过，故留字）。
func newKeyMaterial(keyType string, bits int) (any, ssh.PublicKey, error) {
	var raw any
	switch keyType {
	case typeEd25519:
		_, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return nil, nil, fmt.Errorf("生成 ed25519 密钥失败: %w", err)
		}
		raw = priv
	case typeRSA:
		// 显式写 rand.Reader 而不是传 nil：熵源这件事不该靠"默认值恰好是对的"。
		priv, err := rsa.GenerateKey(rand.Reader, bits)
		if err != nil {
			return nil, nil, fmt.Errorf("生成 %d 位 RSA 密钥失败: %w", bits, err)
		}
		raw = priv
	default:
		return nil, nil, fmt.Errorf("%w: 内部错误——密钥类型 %s 未进入生成分支", ErrInvalidInput, keyType)
	}
	// signerOf 在这里既是形态校验也是公钥来源：不支持的类型此刻现形，
	// 而不是等写进盘上再被下一个人发现打不开。
	signer, err := signerOf(raw)
	if err != nil {
		return nil, nil, err
	}
	return raw, signer.PublicKey(), nil
}
