// Package keyops 是 sshkeys 的**写操作纯逻辑层**：生成密钥对、加/换/去口令、成对删除。
//
// 为什么单开一个子包而不是接着往父包加文件：父包 sshkeys 的台账层（keyring.go）
// 刻意只读，"不往用户目录静默落东西"是它的立身之本。写操作混进同一个文件族之后，
// "这个包到底会不会碰盘"就得靠逐个函数读才说得清；分家后边界直接写在目录名上——
// 看清现状找 sshkeys，要改盘找 sshkeys/keyops。先例是 frpc/docgen：模块子包只放纯逻辑。
//
// **本包不 import 任何 hanxi 包**（父包、settings、extapi 都不引）。理由有两条，
// 第二条比第一条重要：
//  1. keyops 是 sshkeys 的子目录，引父包就是 import 循环；
//  2. 目录围栏与"目录从哪来"（settings/paths、RPC 上下文）一旦耦合进本包，这三个
//     函数就再也无法脱离程序环境被穷举测一遍——而围栏恰恰是全包最需要穷举测的部分。
//     所以 dir 由调用方给，本包只负责一件事：**给了 dir 之后绝不越出去**。
//
// 三条贯穿全包的硬规矩：
//  1. **口令只在入参里活一次**：不进 Result、不进 error 文本、不落日志。包内错误文案
//     一律是"说清哪儿不对"的可诊断中文，但绝不复述口令——x/crypto 自己不回显口令，
//     真正会犯规的是我们写的文案（"口令不对：hunter2" 这种）；
//  2. **写/删先过围栏**（fence.go）：解析后的真实路径必须仍在 dir 内。`..`、软链接、
//     Windows junction 都算逃逸通道——本仓有过 junction 跟随删除的事故（构建 worktree
//     那条），所以这里的处理原则是"判不出来就拒"，不按字面放行；
//  3. **改盘要么全成，要么自己收拾**：所有落盘走"同目录临时件 + fsync + rename"
//     （keyio.go），中途失败即清掉自己刚写的东西，绝不留"私钥已写、公钥缺"的静默中间态。
//
// 与真 OpenSSH 的互操作事实（由 interop_test.go 用本机 ssh-keygen 实测，不是读规范推的）：
//   - 本包生成的私钥，未加密与带口令两种形态**都能被 ssh-keygen -y / -l 正常读取**，
//     指纹与本包返回值同源；错误口令被系统侧拒绝，说明口令是真管用的。
//   - OpenSSH 新格式里的 comment 字段在**私钥段内部**（密钥本体字段之后、填充之前），
//     不在外层结构的尾部。因此：未加密的私钥读得到注释，带口令的读不到（x/crypto
//     只交出解出的密钥，不交出解密后的私钥段）。往外层尾部手工补挂一段注释会让
//     ssh-keygen 报 `Load key …: error in libcrypto` 整把拒收——这条弯路踩过并已在测试里钉住。
//   - `ssh-keygen -c` 只改注释，不会用 -N 重新封装口令；改口令要用 `-p -P 旧 -N 新`。
//   - 改口令只动封装层，**指纹恒不变**，所以已部署到服务端的授权不受影响（passphrase.go）。
package keyops

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// 失败类型哨兵。父包按 errors.Is 映射成有限枚举给前端上色，
// 不该去 parse 中文文案——文案会改，枚举不会。
var (
	// ErrInvalidInput 参数本身不合法（名字带路径分隔符、类型不认识、注释含控制字符……）。
	ErrInvalidInput = errors.New("参数不合法")
	// ErrExists 目标已存在。覆盖必须显式确认，故这是**可预期的拒绝**而非 IO 故障。
	ErrExists = errors.New("目标文件已存在")
	// ErrOutsideDir 围栏拒绝：路径解析后不在给定目录内。这是安全闸门，命中即当异常对待。
	ErrOutsideDir = errors.New("路径越出指定目录")
	// ErrConfirmMismatch 删除用的回显名与文件基名不符（防手滑，也防前端把名字传错）。
	ErrConfirmMismatch = errors.New("确认名与文件名不符")
	// ErrUnprotectNotAllowed 去掉口令属降级动作，调用方没给 allowUnprotect 就被拒。
	ErrUnprotectNotAllowed = errors.New("去掉口令保护需显式允许")
	// ErrWrongPassphrase 用给定口令解不开这把私钥（口令不对，或文件已损坏——二者在
	// OpenSSH 格式下不可区分，如实并列，不假装知道是哪种）。
	ErrWrongPassphrase = errors.New("口令不正确")
	// ErrTargetMissing 目标文件不存在。
	ErrTargetMissing = errors.New("目标文件不存在")
	// ErrNotRegularFile 目标是目录、符号链接/junction 之类的重解析点或设备——
	// 写与删都不跟着它走。
	ErrNotRegularFile = errors.New("目标不是普通文件")
)

// 支持的密钥类型与默认参数。刻意只支持两种：与对外契约（父包 types.go 的
// GenerateRequest.Type 注释）一致，前端枚举就是这两个值。
// 要加 ecdsa/ed25519-sk 是好事，但必须与前端枚举同批改，不在这里单方面放开。
const (
	typeEd25519 = "ed25519"
	typeRSA     = "rsa"

	// defaultRSABits 取 3072 而非 2048：RSA 只在用户明确选了 rsa 时才走到，
	// 既然已经付了慢的代价，就把安全边际买足（3072 ≈ 128bit 强度）。
	defaultRSABits = 3072
	minRSABits     = 2048
	maxRSABits     = 16384 // 与 OpenSSH 自己的上限一致，再大只会写出没人能用的钥匙

	maxNameLen    = 64
	maxCommentLen = 200
)

// Result 写操作的结果。字段就四类可安全展示的东西：**路径、算法名、指纹、结论**
// （外加"当前是否有口令保护"这一态）。没有私钥正文，没有公钥正文，没有口令。
// 各函数按动作填相关字段，用不到的留零值（json 上 omitempty 让前端拿到干净结构）。
type Result struct {
	// PrivatePath 私钥路径（Generate/SetPassphrase 为落盘结果，DeletePair 为已删除回执）。
	// 一律是**解析后的真身绝对路径**：Windows 的 8.3 短名、大小写与 `..` 都已收敛，
	// 因此它可能与调用方传进来的字面写法不同形——要与盘上比对时请比这个返回值。
	PrivatePath string `json:"privatePath,omitempty"`
	// PublicPath 配套 .pub 路径；删除动作里"本就没有公钥"时为空。
	PublicPath string `json:"publicPath,omitempty"`
	// Fingerprint SHA256:… 形式。**跨口令变更保持不变**，是"改口令不影响已部署授权"
	// 的唯一凭据，故每个写操作都回填它。
	Fingerprint string `json:"fingerprint,omitempty"`
	// Type ssh 算法名，如 ssh-ed25519 / ssh-rsa。
	Type string `json:"type,omitempty"`
	// Guarded 操作完成后这把私钥是否受口令保护。
	Guarded bool `json:"guarded"`
	// Message 给用户看的一句结论，不含术语堆砌，也不含任何口令。
	Message string `json:"message,omitempty"`
}

// GenerateParams 生成一把新密钥对的全部输入。自持结构，不引用父包类型——
// 父包把自己的 GenerateRequest 逐字段映射过来即可。
type GenerateParams struct {
	// Name 落盘文件名（如 "id_deploy"），**只是名字**：不许带目录分隔符、
	// 盘符、控制字符，理由见 validateKeyName。
	Name string
	// Type "ed25519"（默认）| "rsa"。空串按 ed25519 处理。
	Type string
	// RSABits 仅 rsa 生效；0 → defaultRSABits。
	RSABits int
	// Comment 公钥注释（写进 .pub 行尾，也写进私钥的明文头部）。见 sanitizeComment。
	Comment string
	// Passphrase 私钥口令。空串 = 不加口令。**只进加密函数，不进任何返回值与错误文本。**
	Passphrase string
	// Overwrite 目标（私钥或 .pub）已存在时必须显式置 true，否则拒绝。
	Overwrite bool
}

// validateKeyName 校验落盘文件名。Name 来自前端，按**注入面**对待：凡是能改变
// "写到哪儿"、或"在不同文件系统上变成另一个名字"的输入都在这里挡掉，而不是指望
// 后面的围栏兜住——围栏管路径，这里管名字。
func validateKeyName(name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("%w: 密钥文件名不能为空", ErrInvalidInput)
	}
	if name != strings.TrimSpace(name) {
		return "", fmt.Errorf("%w: 文件名 %q 首尾有空格（Windows 会静默去掉，台账与盘上名字就会不一致）", ErrInvalidInput, name)
	}
	if len(name) > maxNameLen {
		return "", fmt.Errorf("%w: 文件名长度 %d 超过上限 %d", ErrInvalidInput, len(name), maxNameLen)
	}
	// 名字里出现分隔符就等于把"写到哪"交给了前端，围栏再严也不该走这一步。
	if strings.ContainsAny(name, `/\`) {
		return "", fmt.Errorf("%w: 文件名 %q 不能包含路径分隔符（目录由调用方单独给定）", ErrInvalidInput, name)
	}
	if name == "." || name == ".." || strings.HasPrefix(name, "..") {
		return "", fmt.Errorf("%w: 文件名 %q 不是合法名字", ErrInvalidInput, name)
	}
	if filepath.VolumeName(name) != "" || strings.Contains(name, ":") {
		return "", fmt.Errorf("%w: 文件名 %q 含盘符或冒号（NTFS 下冒号是备用数据流分隔符，写出来的会是「文件名看着正常、内容在流里」的假钥匙）", ErrInvalidInput, name)
	}
	if strings.ContainsAny(name, `<>|?"*`) {
		return "", fmt.Errorf("%w: 文件名 %q 含 Windows 保留字符", ErrInvalidInput, name)
	}
	// 控制字符（含换行）会污染台账、日志与任何把文件名拼进文本的地方。
	if HasControlChar(name) {
		return "", fmt.Errorf("%w: 文件名 %q 含控制字符或换行", ErrInvalidInput, name)
	}
	if last := name[len(name)-1]; last == '.' || last == ' ' {
		return "", fmt.Errorf("%w: 文件名 %q 不能以点或空格结尾（Windows 会静默截掉）", ErrInvalidInput, name)
	}
	// 成对逻辑靠"私钥名 + .pub"推导，把私钥起名 x.pub 会推出 x.pub.pub 这种没人要的伴儿。
	if strings.EqualFold(name[len(name)-min(4, len(name)):], ".pub") {
		return "", fmt.Errorf("%w: 文件名 %q 以 .pub 结尾，这里是私钥名，不该带公钥后缀", ErrInvalidInput, name)
	}
	if reserved, ok := windowsReservedName(name); ok {
		return "", fmt.Errorf("%w: 文件名 %q 与 Windows 保留设备名 %s 冲突（NTFS 上会写出删不掉也列不全的文件）", ErrInvalidInput, name, reserved)
	}
	return name, nil
}

// windowsReservedName 判 CON/NUL/PRN/AUX/COM1-9/LPT1-9 这类保留名。
// 它们**不带扩展名也生效**且大小写无关，所以 "nul"、"COM1.pub" 都得拒。
func windowsReservedName(name string) (string, bool) {
	base := name
	if i := strings.IndexByte(base, '.'); i >= 0 {
		base = base[:i]
	}
	up := strings.ToUpper(base)
	switch up {
	case "CON", "PRN", "AUX", "NUL", "CLOCK$":
		return up, true
	}
	for _, set := range [2]string{"COM", "LPT"} {
		if len(up) == len(set)+1 && strings.HasPrefix(up, set) && up[len(set)] >= '1' && up[len(set)] <= '9' {
			return up, true
		}
	}
	return "", false
}

// normalizeKeyType 收敛密钥类型。空串走默认；大小写宽容（前端传值本该规范，
// 但这一层宽容的代价为零，拒掉反而把人挡在"类型不支持"的假错误上）。
func normalizeKeyType(t string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(t)) {
	case "", typeEd25519:
		return typeEd25519, nil
	case typeRSA:
		return typeRSA, nil
	default:
		return "", fmt.Errorf("%w: 不支持的密钥类型 %q，本层只支持 ed25519 与 rsa", ErrInvalidInput, t)
	}
}

// normalizeRSABits 校验 RSA 位数。非 rsa 类型时忽略该值（不报错：前端表单会带着
// 上一次留下的 RSABits 一起提交，为这种无害的残留报错是自找麻烦）。
func normalizeRSABits(keyType string, bits int) (int, error) {
	if keyType != typeRSA {
		return 0, nil
	}
	if bits == 0 {
		return defaultRSABits, nil
	}
	if bits < minRSABits || bits > maxRSABits {
		return 0, fmt.Errorf("%w: RSA 位数 %d 不在 %d–%d 之间", ErrInvalidInput, bits, minRSABits, maxRSABits)
	}
	return bits, nil
}

// sanitizeComment 处理公钥注释。注释被写进 .pub 的**同一行**，
// 所以换行等于往 authorized_keys 里追加一整行——与"别名字段是注入面"
// （docs/plans/PLAN_SSH_KEYS.md §②-2）是同一条道理：控制字符一律拒，不做转义糊弄。
func sanitizeComment(c string) (string, error) {
	c = strings.TrimSpace(c)
	if c == "" {
		return "", nil // 空注释是合法的：.pub 就只有算法与 base64 两段
	}
	if len(c) > maxCommentLen {
		return "", fmt.Errorf("%w: 公钥注释长度 %d 超过上限 %d", ErrInvalidInput, len(c), maxCommentLen)
	}
	if HasControlChar(c) {
		return "", fmt.Errorf("%w: 公钥注释含控制字符或换行（会被写进 .pub 同一行，属于注入面）", ErrInvalidInput)
	}
	return c, nil
}

// HasControlChar 判是否含 C0 控制字符、DEL(0x7f) 或 NUL。
// 导出是给父包校验同类字段用的——同一条道理不该每个包重新想一遍。
func HasControlChar(s string) bool {
	for i := 0; i < len(s); i++ {
		if b := s[i]; b < 0x20 || b == 0x7f {
			return true
		}
	}
	return false
}
