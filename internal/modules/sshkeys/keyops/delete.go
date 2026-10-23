package keyops

// 成对删除。
//
// 为什么这个函数要三重复核（回显名 + 字面围栏 + 真实形态围栏）：它的 path 来自前端
// 与主机账本，而杀伤半径是**任意文件删除**。围栏失效一次，这个模块就从"钥匙管理"
// 变成"删库入口"。所以这里的原则与其它两处不同——不是"尽力做对"，而是
// "有任何一点判不准就什么都不做"。
//
// 成对删除的理由：只删私钥会留下孤儿公钥（台账里凭空多一把没主的钥匙），
// 只删公钥会让台账显示"这把私钥缺配套公钥"（下一个人就会怀疑钥匙坏了）。
// 两者一起删才符合"一把钥匙"这个用户概念。

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// DeletePair 删除 dir 内的私钥 path 及其配套 .pub。
//
// confirm 必须与文件基名**逐字相等**，不等就拒绝（回显确认，不接受"一键删除"）。
// 配套 .pub 本就不存在不算错误，如实写进 Message。
//
// 拒绝条件：
//   - confirm 与基名不符或为空 → ErrConfirmMismatch；
//   - path 字面或解析后（软链接/junction）落在 dir 外 → ErrOutsideDir；
//   - 目标不存在 → ErrTargetMissing；不是普通文件（目录/链接/设备）→ ErrNotRegularFile；
//   - path 指向的是 .pub → ErrInvalidInput（要删的是私钥，公钥由本函数配套处理）。
//
// 删除顺序是**先公钥后私钥**：公钥没了，私钥照常能用（顶多台账提示缺配套公钥）；
// 反过来先删私钥就只剩一把无法自证的公钥。且任一步失败都在这一步之前完成检查，
// 不会出现"删了一半、另一半因为权限失败"的意外——那类失败照样如实报。
func DeletePair(dir, path, confirm string) (Result, error) {
	root, target, err := guardExistingPath(dir, path)
	if err != nil {
		return Result{}, err
	}

	base := filepath.Base(target)
	if base == "." || base == ".." || base == string(filepath.Separator) {
		return Result{}, fmt.Errorf("%w: %s 指向目录，不删", ErrInvalidInput, path)
	}
	if len(base) >= 4 && strings.EqualFold(base[len(base)-4:], ".pub") {
		return Result{}, fmt.Errorf("%w: %s 是公钥文件；请传私钥路径，配套公钥由本函数一起处理", ErrInvalidInput, path)
	}

	// 回显确认：拿**目标基名**比，而不是拿前端传来的整串——用户确认的是"这个文件"。
	if strings.TrimSpace(confirm) == "" {
		return Result{}, fmt.Errorf("%w: 删除密钥必须把文件名 %s 回显一遍再提交，空确认不算确认", ErrConfirmMismatch, base)
	}
	if confirm != base {
		return Result{}, fmt.Errorf("%w: 期望回显 %q，实得 %q——两者不一致就什么都不删", ErrConfirmMismatch, base, confirm)
	}

	if _, err = requireRegularFile(target); err != nil {
		return Result{}, err
	}

	// 配套公钥也走一遍围栏：它由私钥路径拼出来，正常情况下必然在 dir 内，
	// 但"正常情况下"不是删文件的理由——x.pub 若是条通往外面的链接，同样不删。
	pubPath := target + ".pub"
	if _, err = guardAgainstRoot(root, pubPath); err != nil {
		return Result{}, fmt.Errorf("配套公钥 %s 无法安全定位，本次不删任何文件: %w", pubPath, err)
	}

	// 删之前尽力取一次公钥指纹：公钥本就可公开，删掉之后再想要就没了。
	// 取不到不拦删除（台账里"缺配套公钥"的钥匙本来就该能删）。
	fp := fingerprintOfPubFile(pubPath)

	// pubRemoved 记下"这次真的删掉了一个公钥"，回执文案靠它，不靠删完之后再 Lstat
	// （那时它必然不存在，拿存在性判断会永远报成"本就不存在"）。
	pubRemoved := false
	if fi, lerr := os.Lstat(pubPath); lerr == nil {
		if !fi.Mode().IsRegular() {
			return Result{}, fmt.Errorf("%w: 配套公钥 %s 不是普通文件（%s），本次不删任何文件",
				ErrNotRegularFile, pubPath, fi.Mode())
		}
		if rerr := os.Remove(pubPath); rerr != nil {
			// 私钥一个字节都还没动，失败面是干净的。
			return Result{}, fmt.Errorf("删除配套公钥 %s 失败，私钥 %s 保持原样未删: %w", pubPath, target, rerr)
		}
		pubRemoved = true
	} else if !errors.Is(lerr, os.ErrNotExist) {
		return Result{}, fmt.Errorf("检查配套公钥 %s 失败，本次不删任何文件: %w", pubPath, lerr)
	}

	if rerr := os.Remove(target); rerr != nil {
		if errors.Is(rerr, os.ErrNotExist) {
			return Result{}, fmt.Errorf("%w: %s（公钥已删，私钥在这之前已经不在了）", ErrTargetMissing, target)
		}
		return Result{}, fmt.Errorf("删除私钥 %s 失败，但配套公钥 %s 已被删除——台账会显示这把私钥缺配套公钥: %w",
			target, pubPath, rerr)
	}

	msg := fmt.Sprintf("已删除私钥 %s", base)
	if fp != "" {
		msg += fmt.Sprintf("（指纹 %s）", fp)
	}
	switch {
	case pubRemoved:
		msg += "，并一并删除配套公钥 " + filepath.Base(pubPath)
	default:
		msg += "；配套公钥本就不存在，未做删除"
	}

	return Result{
		PrivatePath: target,
		PublicPath:  pubPathOfResult(pubRemoved, pubPath),
		Fingerprint: fp,
		Message:     msg,
	}, nil
}

// pubPathOfResult 只在真的删过公钥时回填路径：Result 里的 PublicPath 语义是
// "本次动过的公钥"，没动过就不写，免得前端照着它去找一个本就不存在的文件。
func pubPathOfResult(removed bool, pubPath string) string {
	if !removed {
		return ""
	}
	return pubPath
}

// fingerprintOfPubFile 读公钥文件算指纹；任何失败都回空串。
// 公钥是可公开的信息，这里不需要谨慎到报错的程度——它是"删除回执"里的加分项。
func fingerprintOfPubFile(pubPath string) string {
	raw, err := os.ReadFile(pubPath)
	if err != nil || len(raw) > maxKeyFileBytes {
		return ""
	}
	pub, _, perr := publicKeyOfBytes(raw)
	if perr != nil {
		return ""
	}
	return fingerprintOf(pub)
}
