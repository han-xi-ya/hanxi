package keyops

// 加口令 / 换口令 / 去口令。
//
// **指纹跨这三种操作恒不变**，这不是实现巧合，是本函数存在的理由：OpenSSH 私钥文件
// 的"封装层"（cipher + KDF + 口令）与"密钥本身"是两件事，换封装不动密钥。所以
// **改口令不会让你已经部署到服务端的公钥授权失效**——authorized_keys 里那行原封不动，
// 连配套 .pub 文件都不需要重写。反过来说，如果哪天这里的实现改成"重新生成一对钥匙"，
// 用户就会在一次无害的改口令操作之后突然连不上服务器；那条不变性测试（指纹恒等）
// 就是钉住这个区别的。
//
// 危险动作分级：
//   - 加口令、换口令 = 升级，直接放行；
//   - 去口令 = **降级**（私钥从"拿到文件还需口令"变成"拿到文件即可登录"），
//     必须 allowUnprotect=true，否则拒绝。
//
// 口令的生命周期：只在这一次调用的参数与 []byte 转换里活着。做不到彻底擦拭
// （Go 的 string 不可变、[]byte 拷贝点不可控，声称能 zeroize 是骗人），能保证的是
// **不把它写进任何会活得比这次调用更久的地方**——Result、错误文本、日志都不行。

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/ssh"
)

// SetPassphrase 给 path 指向的私钥加/换/去口令，返回新状态与指纹。
//
// allowUnprotect=false 时"去掉口令"（newPass 为空而当前有口令）必定被拒。
//
// 本函数**没有 dir 参数**（对外契约已定），因此它只能守住自己守得住的那部分：
// 目标必须是存在的普通文件，且**拒绝符号链接/junction**——没有围栏根就没法判
// 链接通向哪里，硬猜就是"往一个不知道在哪的文件上改口令"。需要目录围栏的调用方
// 先用 EnsureInside 把路径钉回密钥目录（父包映射 RPC 时应当这么做）。
//
// 返回值里的路径是**解析后的真身**，可能与传入写法不同形（Windows 的 8.3 短名、
// 大小写、`..` 都会被收敛）。父包若把台账里的原字符串与返回值做比较，
// 要比解析后的那一份，别比字面。
func SetPassphrase(path, oldPass, newPass string, allowUnprotect bool) (Result, error) {
	target, err := normalizeTargetPath(path)
	if err != nil {
		return Result{}, err
	}
	raw, err := readKeyFile(target)
	if err != nil {
		return Result{}, err
	}

	key, guarded, err := decryptPrivateKey(target, raw, oldPass)
	if err != nil {
		return Result{}, err
	}

	unprotect := guarded && newPass == ""
	if unprotect && !allowUnprotect {
		// 拒绝时一个字节都不写：这条路径上文件必然保持原样。
		return Result{}, fmt.Errorf(
			"%w: %s 当前受口令保护，把口令去掉会让私钥在磁盘上变成明文可读，"+
				"这是降级动作。确认无误请显式允许（allowUnprotect）", ErrUnprotectNotAllowed, target)
	}
	if !guarded && newPass == "" {
		// 本来就没口令，"去掉"是无事可做——不重写文件（少一次改盘就少一次风险），
		// 但如实把现状说清楚，别让调用方以为发生了变更。
		signer, serr := signerOf(key)
		if serr != nil {
			return Result{}, serr
		}
		return Result{
			PrivatePath: target,
			PublicPath:  existingPubPath(target),
			Fingerprint: fingerprintOf(signer.PublicKey()),
			Type:        signer.PublicKey().Type(),
			Guarded:     false,
			Message:     "这把私钥本来就没有口令保护，未做任何改动",
		}, nil
	}

	// 注释带回两级来源：先取私钥段里的 comment 字段（只有**未加密**时才读得到，
	// 见 keyio.go 的 commentFromPrivateKey），取不到再退回配套 .pub 的尾字段。
	// 两处都没有就留空串——**不拿文件名编造一条注释出来**。
	comment := commentFromPrivateKey(raw)
	if comment == "" {
		comment = pubCommentFromFile(target)
	}

	newPEM, mErr := privatePEM(key, comment, newPass)
	if mErr != nil {
		return Result{}, fmt.Errorf("重新导出私钥失败: %w", mErr)
	}
	signer, sErr := signerOf(key)
	if sErr != nil {
		return Result{}, sErr
	}
	fp := fingerprintOf(signer.PublicKey())

	if err = writeNewFile(target, newPEM, privFileMode); err != nil {
		// tmp+rename 的用处在这里现形：失败时目标名还是原来那份完整文件。
		return Result{}, fmt.Errorf("写回 %s 失败，原文件保持原样: %w", target, err)
	}

	// 读回复核：改盘动作不复核就等于宣称成功。这里验的是"新状态真的落下来了"
	// 与"指纹真的没变"两件事，任一不符即用内存里留着原文把文件还原回去。
	if verr := verifyWrittenKey(target, newPass, fp); verr != nil {
		restoreErr := writeNewFile(target, raw, privFileMode)
		if restoreErr != nil {
			return Result{}, fmt.Errorf("口令改写后复核不通过（%v），且还原原文件也失败，%s 需人工确认: %v", verr, target, restoreErr)
		}
		return Result{}, fmt.Errorf("口令改写后复核不通过，已还原原文件: %v", verr)
	}

	msg := "已加上口令保护"
	switch {
	case unprotect:
		msg = "已去掉口令保护"
	case guarded:
		msg = "已更换口令"
	}
	// 这句是本次操作最有价值的实话：改的只是文件封装层，密钥本体与指纹都没动。
	msg += fmt.Sprintf("；指纹 %s 未变，已部署到服务端的公钥授权不受影响，配套 .pub 无需重新分发", fp)
	if comment == "" && guarded {
		// 实话：加密私钥的注释在加密段里，本层读不到；没有配套 .pub 就没有兜底来源。
		// 于是新文件的注释字段是空的——不假装无事发生，也不拿文件名编一条出来。
		msg += "；未取回原注释（带口令的私钥没有配套 .pub 时读不到注释），需要时用 ssh-keygen -c 补"
	}

	return Result{
		PrivatePath: target,
		PublicPath:  existingPubPath(target),
		Fingerprint: fp,
		Type:        signer.PublicKey().Type(),
		Guarded:     newPass != "",
		Message:     msg,
	}, nil
}

// normalizeTargetPath 收敛目标路径：解析成盘上真身、拒重解析点、判基名、拒 .pub。
//
// 走 resolveForWrite 而不是自己 Abs+Clean：后者挡不住 `keys/link/../id_x` 这种
// "穿过链接再回退"的写法——Clean 会把它折成 `keys/id_x`（看着在目录里），
// 而操作系统真正打开的是链接对面的那份。解析过再动作，字面与真实才是同一件事。
//
// 拒 .pub 是因为这个函数改的是**私钥**的封装层：把公钥文件当私钥传进来，
// 报一句 x/crypto 的英文错误不如提前用明白话拦住。
//
// **注意本函数不含目录围栏**：SetPassphrase 的签名里没有 dir（对外契约已定）。
// 需要围栏的调用方必须先调 EnsureInside。
func normalizeTargetPath(path string) (string, error) {
	resolved, err := resolveForWrite(path)
	if err != nil {
		return "", err
	}
	base := filepath.Base(resolved)
	if base == "." || base == ".." || base == string(filepath.Separator) || base == "" {
		return "", fmt.Errorf("%w: %s 指向的是目录而不是私钥文件", ErrInvalidInput, path)
	}
	if len(base) >= 4 && strings.EqualFold(base[len(base)-4:], ".pub") {
		return "", fmt.Errorf("%w: %s 是公钥文件；口令保护只作用于私钥，请传私钥路径", ErrInvalidInput, path)
	}
	return resolved, nil
}

// decryptPrivateKey 解出私钥本体，并给出"它原本是否有口令保护"。
//
// 三态处理与父包 keyring.go 同一条道理：**不猜**。解不开又不是"缺口令"，
// 那就是文件坏了或格式不支持，如实报错。
func decryptPrivateKey(target string, raw []byte, oldPass string) (any, bool, error) {
	key, err := ssh.ParseRawPrivateKey(raw)
	if err == nil {
		if oldPass != "" {
			return nil, false, fmt.Errorf(
				"%w: %s 没有口令保护，不需要旧口令（要加口令只需填新口令即可）", ErrWrongPassphrase, target)
		}
		return key, false, nil
	}

	var missing *ssh.PassphraseMissingError
	if !errors.As(err, &missing) {
		return nil, false, fmt.Errorf("%s 不是本层能识别的私钥文件（可能是配置、公钥或已损坏）: %w", target, err)
	}
	if oldPass == "" {
		return nil, false, fmt.Errorf("%w: %s 受口令保护，必须先给出旧口令；旧口令忘了就只能重新生成一把并重新部署公钥",
			ErrInvalidInput, target)
	}
	key, perr := ssh.ParseRawPrivateKeyWithPassphrase(raw, []byte(oldPass))
	if perr != nil {
		// 底层对"口令不对"与"文件损坏"给的是同一类失败（MAC 校验不过），不假装分得清。
		// 错误文本里**只有底层那句英文与路径**——x/crypto 不回显口令，我们也不加进去。
		return nil, true, fmt.Errorf("用旧口令解不开 %s（口令不对，或文件已损坏）: %w",
			target, errors.Join(ErrWrongPassphrase, perr))
	}
	return key, true, nil
}

// verifyWrittenKey 读回刚写的文件，确认：形态与预期一致（有口令/无口令）、
// 给定口令确实解得开、指纹与改写前相同。
func verifyWrittenKey(target, passphrase, wantFP string) error {
	raw, err := readKeyFile(target)
	if err != nil {
		return fmt.Errorf("读回失败: %w", err)
	}
	key, err := ssh.ParseRawPrivateKey(raw)
	if err == nil {
		if passphrase != "" {
			return errors.New("写回后文件不含口令保护，与预期不符")
		}
	} else {
		var missing *ssh.PassphraseMissingError
		if !errors.As(err, &missing) {
			return fmt.Errorf("写回后文件形态异常: %w", err)
		}
		if passphrase == "" {
			return errors.New("写回后文件仍受口令保护，与预期不符")
		}
		key, err = ssh.ParseRawPrivateKeyWithPassphrase(raw, []byte(passphrase))
		if err != nil {
			return fmt.Errorf("新口令写回后解不开（不应发生，已按失败处理）: %w", err)
		}
	}
	signer, err := signerOf(key)
	if err != nil {
		return err
	}
	if got := fingerprintOf(signer.PublicKey()); got != wantFP {
		return fmt.Errorf("改写后指纹变成 %s，与改写前的 %s 不符——密钥本体不该因改口令而改变", got, wantFP)
	}
	return nil
}

// existingPubPath 配套 .pub 存在则回其路径，否则空串（Result 里不写不存在的文件）。
func existingPubPath(privatePath string) string {
	if fi, err := os.Lstat(privatePath + ".pub"); err == nil && fi.Mode().IsRegular() {
		return privatePath + ".pub"
	}
	return ""
}
