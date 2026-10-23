package sshkeys

// 密钥写操作的接线层：把 RPC 请求映射到 keyops / deploy 两个子包，并把它们
// 返回的错误哨兵翻成**枚举码**给前端上色。
//
// 三件在这层落实的事，都不是"转发一下"那么轻：
//
// 一、**围栏由这层补齐**。keyops 的 SetPassphrase 签名里没有 dir（改它要动它的
// 对外契约），它自己只守"存在的普通文件 + 拒重解析点"。所以本层必须先过
// keyops.EnsureInside(密钥目录, 路径) 再调它——漏这一步等于放弃目录归属检查，
// 让"改口令"能指向目录外的任意文件。DeletePair 自带 dir，不需要补。
//
// 二、**路径字面值不可信**。keyops 返回的是**解析后的真身绝对路径**（8.3 短名、
// 大小写规范化、`..` 折叠都可能让它与传入字面不同形）。所以：台账里比对路径
// 一律用 os.SameFile 而不是字符串相等；返回给前端的 Path 就用真身路径，
// 不再回传用户写的那串。
//
// 三、**建目录这件事留在这层，不下沉**。keyops.Generate 刻意不创建 dir（它不替
// 调用方往用户路径落新目录），"~/.ssh 不存在就建一个 0700"是**写用户目录**的
// 决定，属于本模块的 ⚑ 治理项，因此只出现在 GenerateKey 这条显式动作路径上，
// 绝不出现在只读台账或导出对账里。

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"hanxi/internal/modules/sshkeys/deploy"
	"hanxi/internal/modules/sshkeys/keyops"
)

// 错误码枚举：前端按值上色，**不解析中文文案**（文案会改，码不会）。
const (
	codeOK         = "ok"
	codeInvalid    = "invalid-input"
	codeExists     = "exists"
	codeOutsideDir = "outside-dir"
	codeConfirm    = "confirm-mismatch"
	codeUnprotect  = "unprotect-not-allowed"
	codeWrongPass  = "wrong-passphrase"
	codeMissing    = "target-missing"
	codeNotRegular = "not-regular-file"
	codeIO         = "io-error"
)

// GenerateKey 生成一把新密钥对。**显式动作**：这条路径会创建 ~/.ssh（若不存在）。
func (s *SshKeysService) GenerateKey(req GenerateRequest) (GenerateResult, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return GenerateResult{}, gateErr
	}
	defer release()

	dir, err := s.ensureKeyDir()
	if err != nil {
		return GenerateResult{}, err
	}
	res, err := keyops.Generate(dir, keyops.GenerateParams{
		Name:       req.Name,
		Type:       req.Type,
		RSABits:    req.RSABits,
		Comment:    req.Comment,
		Passphrase: req.Passphrase,
		Overwrite:  req.Overwrite,
	})
	if err != nil {
		return GenerateResult{Code: codeOf(err), Message: err.Error()}, err
	}
	return GenerateResult{
		PrivatePath: res.PrivatePath,
		PublicPath:  res.PublicPath,
		Fingerprint: res.Fingerprint,
		Type:        res.Type,
		Guarded:     res.Guarded,
		Code:        codeOK,
		Message:     res.Message,
	}, nil
}

// SetKeyPassphrase 加 / 换 / 去私钥口令。去口令是**降级动作**，必须把文件名回显一遍。
func (s *SshKeysService) SetKeyPassphrase(req SetPassphraseRequest) (KeyOpResult, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return KeyOpResult{}, gateErr
	}
	defer release()

	dir, err := sshHomeDir()
	if err != nil {
		return KeyOpResult{}, err
	}
	// 围栏补位，见文件头第一条。
	target, err := keyops.EnsureInside(dir, req.Path)
	if err != nil {
		return KeyOpResult{Code: codeOf(err), Message: err.Error()}, err
	}

	unprotect := strings.TrimSpace(req.NewPassphrase) == ""
	if unprotect {
		// 去掉口令会让这把钥匙退回"文件被读到即能用"的状态；基名回显相等才允许。
		if strings.TrimSpace(req.Confirm) != filepath.Base(target) {
			msg := fmt.Sprintf("去掉口令保护需要把文件名原样回一遍（期望 %q）", filepath.Base(target))
			return KeyOpResult{Code: codeConfirm, Message: msg}, errors.New(msg)
		}
	}

	res, err := keyops.SetPassphrase(target, req.OldPassphrase, req.NewPassphrase, unprotect)
	if err != nil {
		return KeyOpResult{Path: target, Code: codeOf(err), Message: err.Error()}, err
	}
	return KeyOpResult{
		Path:        res.PrivatePath,
		Fingerprint: res.Fingerprint,
		Guarded:     res.Guarded,
		Code:        codeOK,
		Message:     res.Message,
	}, nil
}

// DeleteKey 成对删除私钥与配套公钥。**清单里还有条目引用这把钥匙时直接拒绝**——
// 删了钥匙而账本还在，后果是"清单上那条永远连不上"，而用户看不出为什么。
func (s *SshKeysService) DeleteKey(req DeleteKeyRequest) (KeyOpResult, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return KeyOpResult{}, gateErr
	}
	defer release()

	dir, err := sshHomeDir()
	if err != nil {
		return KeyOpResult{}, err
	}
	target, err := keyops.EnsureInside(dir, req.Path)
	if err != nil {
		return KeyOpResult{Code: codeOf(err), Message: err.Error()}, err
	}
	if blockers := s.hostsUsingKey(target); len(blockers) > 0 {
		msg := fmt.Sprintf("清单里还有 %d 条在用这把钥匙：%s——请先在清单里改掉或删掉它们，再删钥匙",
			len(blockers), strings.Join(blockers, "、"))
		return KeyOpResult{Path: target, Code: codeExists, Message: msg}, errors.New(msg)
	}

	res, err := keyops.DeletePair(dir, req.Path, req.Confirm)
	if err != nil {
		return KeyOpResult{Path: target, Code: codeOf(err), Message: err.Error()}, err
	}
	return KeyOpResult{
		Path:        res.PrivatePath,
		PublicPath:  res.PublicPath, // 空值有意义：配套公钥本就不存在（回执文案会说清）
		Fingerprint: res.Fingerprint,
		Code:        codeOK,
		Message:     res.Message,
	}, nil
}

// ExportPublicKey 从私钥补出配套 .pub，让这把钥匙变得**可部署**。
//
// 存在的理由：从服务器直接下回来的私钥天生没有 .pub，而"部署公钥"要选公钥文件——
// 没有这个动作，这类钥匙在界面上就是死的。带口令的私钥同样能补：OpenSSH 新格式只
// 加密私钥段，公钥段在外层明文里，所以**本动作不需要也不接触口令**。
func (s *SshKeysService) ExportPublicKey(req ExportPubRequest) (KeyOpResult, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return KeyOpResult{}, gateErr
	}
	defer release()

	dir, err := sshHomeDir()
	if err != nil {
		return KeyOpResult{}, err
	}
	res, err := keyops.ExportPub(dir, req.Path, req.Comment)
	if err != nil {
		return KeyOpResult{Code: codeOf(err), Message: err.Error()}, err
	}
	return KeyOpResult{
		Path:        res.PrivatePath,
		PublicPath:  res.PublicPath, // 新建出来的那份 .pub：界面要能看见、能复制，不能只在散文里
		Fingerprint: res.Fingerprint,
		Guarded:     res.Guarded,
		Code:        codeOK,
		Message:     res.Message,
	}, nil
}

// DeletePublicKey 删除孤儿 .pub。语义与限制见 keyops.DeletePubOnly。
func (s *SshKeysService) DeletePublicKey(req DeletePubRequest) (KeyOpResult, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return KeyOpResult{}, gateErr
	}
	defer release()

	dir, err := sshHomeDir()
	if err != nil {
		return KeyOpResult{}, err
	}
	res, err := keyops.DeletePubOnly(dir, req.Path, req.Confirm)
	if err != nil {
		return KeyOpResult{Code: codeOf(err), Message: err.Error()}, err
	}
	return KeyOpResult{
		Path:        res.PublicPath, // 本动作的对象就是这份公钥
		PublicPath:  res.PublicPath,
		Fingerprint: res.Fingerprint,
		Code:        codeOK,
		Message:     res.Message,
	}, nil
}

// DeployPublicKey 把某把公钥装到目标机器。本模块唯一"对外部机器产生写影响"的 RPC，
// 也是唯一会带口令出网的动作——口令只活在这次调用里，不进结果、不进错误、不落盘。
//
// 与 CheckSSHAuth 一样：**只能是用户显式按下的动作**，绝不作为别的 RPC 的副作用
// 被顺带调用（一次失败的尝试会在对端 sshd 留下记录，也可能触发 fail2ban）。
func (s *SshKeysService) DeployPublicKey(req DeployRequest) (DeployResult, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return DeployResult{}, gateErr
	}
	defer release()

	// **不复用 sshTimeoutOf 的 5s 缺省**：那是给"看一眼横幅"定的，而部署是三段动作
	// （拨号+KEX+认证 / 开 session / 远端执行），deploy 子包按**每段**计时、自带 15s 缺省。
	// 父包替它填 5s 会把子包缺省压掉，形成"两层缺省打架"——这种 bug 不报错，只是偶尔
	// 超时，最难查。所以：给了值就透传并封顶，没给就留 0 让子包按自己缺省走；
	// 外层 ctx 只当保险丝，按三段总和放宽。
	per := time.Duration(0)
	if req.TimeoutMs > 0 {
		per = time.Duration(req.TimeoutMs) * time.Millisecond
		if per > 60*time.Second {
			per = 60 * time.Second
		}
	}
	fuse := per
	if fuse <= 0 {
		fuse = 15 * time.Second // 与 deploy 子包缺省一致，仅用于算保险丝长度
	}
	ctx, cancel := context.WithTimeout(context.Background(), fuse*2+5*time.Second)
	defer cancel()

	res, err := deploy.Deploy(ctx, deploy.Params{
		Host:       req.Host,
		Port:       req.Port,
		User:       req.User,
		Password:   req.Password,
		PubKeyPath: req.PubKeyPath,
		Timeout:    per,
	}, s.pins)
	if err != nil {
		return DeployResult{Verdict: "refused", Reason: err.Error()}, err
	}
	return DeployResult{
		Verdict:            res.Verdict,
		OK:                 res.OK,
		Reason:             res.Reason,
		HostKeyFingerprint: res.HostKeyFingerprint,
		AuthorizedKeysPath: res.AuthorizedKeysPath,
		PubKeyFingerprint:  res.PubKeyFingerprint,
		Code:               deployCode(res),
	}, nil
}

// deployCode 把部署结论翻成前端的成败码。verdict 本身就是有限枚举，
// 直接复用它的值——不另立一套映射，免得两套枚举日后对不上。
func deployCode(res deploy.Result) string {
	if res.OK {
		return codeOK
	}
	return res.Verdict
}

// ensureKeyDir 取密钥目录，不存在则建（0700）。见文件头第三条：这条副作用
// 只在显式写动作里出现。
func (s *SshKeysService) ensureKeyDir() (string, error) {
	dir, err := sshHomeDir()
	if err != nil {
		return "", err
	}
	if _, statErr := os.Stat(dir); errors.Is(statErr, fs.ErrNotExist) {
		if mkErr := os.MkdirAll(dir, 0o700); mkErr != nil {
			return "", fmt.Errorf("创建密钥目录 %s 失败: %w", dir, mkErr)
		}
	}
	return dir, nil
}

// hostsUsingKey 找出清单里所有指向这把钥匙的别名。用 os.SameFile 比，不用字符串相等
// ——真身路径与用户写法的字面差异（短名/大小写/`..`）会让字符串比较漏判，
// 而"删了钥匙清单还指着它"正是要拦的那件事。
func (s *SshKeysService) hostsUsingKey(target string) []string {
	items, err := s.ledger.list()
	if err != nil {
		return nil // 账本读不动时不拦删除——但要说明：这层只是尽力而为的护栏，
		// keyops 的围栏与确认门才是硬边界，护栏失效不该顺手把删除也变成不可能。
	}
	tfi, err := os.Stat(target)
	if err != nil {
		return nil
	}
	var blockers []string
	for _, h := range items {
		p := strings.TrimSpace(h.KeyPath)
		if p == "" {
			continue
		}
		fi, serr := os.Stat(expandHome(p))
		if serr != nil {
			continue
		}
		if os.SameFile(tfi, fi) {
			blockers = append(blockers, h.Alias)
		}
	}
	return blockers
}

// codeOf 把子包错误哨兵翻成枚举码。未识别的一律 io-error——**不假装知道**，
// 也不把中文文案当判据（文案改了前端就跟着错）。
func codeOf(err error) string {
	switch {
	case errors.Is(err, keyops.ErrInvalidInput):
		return codeInvalid
	case errors.Is(err, keyops.ErrExists):
		return codeExists
	case errors.Is(err, keyops.ErrOutsideDir):
		return codeOutsideDir
	case errors.Is(err, keyops.ErrConfirmMismatch):
		return codeConfirm
	case errors.Is(err, keyops.ErrUnprotectNotAllowed):
		return codeUnprotect
	case errors.Is(err, keyops.ErrWrongPassphrase):
		return codeWrongPass
	case errors.Is(err, keyops.ErrTargetMissing):
		return codeMissing
	case errors.Is(err, keyops.ErrNotRegularFile):
		return codeNotRegular
	default:
		return codeIO
	}
}

// 编译期钉子：确保子包契约没被无声改动（签名一变这里就红，而不是到运行时才发现）。
var (
	_ = time.Second
	_ = func(dir string, p keyops.GenerateParams) (keyops.Result, error) { return keyops.Generate(dir, p) }
	_ = func(path, o, n string, allow bool) (keyops.Result, error) {
		return keyops.SetPassphrase(path, o, n, allow)
	}
	_ = func(dir, path, confirm string) (keyops.Result, error) { return keyops.DeletePair(dir, path, confirm) }
	_ = func(dir, path string) (string, error) { return keyops.EnsureInside(dir, path) }
	_ = func(ctx context.Context, p deploy.Params, pins deploy.PinStore) (deploy.Result, error) {
		return deploy.Deploy(ctx, p, pins)
	}
)
