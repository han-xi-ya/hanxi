// Package artifact 是 Wave 4 规划中的共享托管内核（ArtifactManager）：
// 把各托管模块重复实现的"下载 → 校验 → 安全解包 → 版本树落位"能力一次收口。
//
// 设计蓝本与防护语义来源：
//   - internal/modules/ocr/hosted.go（权威实现仍在模块侧）：zip 契约校验、
//     ZipSlip/炸弹上限、原子落位、同版本异摘要防漂移、版本令牌白名单；
//   - internal/modules/litemonitor/version（迁移前形态；"样板反迁内核"样本——
//     staging + 原子 rename 落位链、.removing- 隔离卸载、官方摘要必检的下载
//     完整性四层兜底即取样于此，该模块现已改为消费本包）；
//   - docs/plans/PLAN_OFFICIAL_MODULE_DISTRIBUTION.md §8.2 通用原则、
//     §10.4 安全解包、§10.5 下载安全。
//
// 纪律：
//   - 不信任传输来源，官方 SHA-256 必检（镜像只是搬运工）；
//   - 落位原子化、已安装版本目录不可原地覆盖；
//   - staging 独占创建、不覆盖、不执行任何内容；
//   - 错误信息中文可读、可诊断，无裸 panic。
package artifact

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
)

// Progress 是托管交付链各阶段的通用进度。各托管模块负责把这里的通用形状
// 映射为自己的事件形状（如 Wails 事件 payload）。
type Progress struct {
	Stage   string // resolve|download|verify|unpack|place|done|error
	Version string // 关联版本（未知时为空）
	Done    int64  // 已完成字节（不适用时为 0）
	Total   int64  // 总字节（未知时为 0）
	Message string // 人话说明（error 阶段携带错误文本）
}

// 进度阶段常量（Progress.Stage 取值域）。
const (
	StageResolve  = "resolve"  // 解析候选源 / 目标版本
	StageDownload = "download" // 流式下载
	StageVerify   = "verify"   // 摘要 / 大小核验
	StageUnpack   = "unpack"   // 安全解包
	StagePlace    = "place"    // 原子落位
	StageDone     = "done"     // 全链完成
	StageError    = "error"    // 任一阶段失败
)

// versionTokenRe 版本令牌白名单（防目录名注入）：字母数字起头，允许 . _ -，
// 总长 ≤64。与 ocr hosted.go 的 validateVersionToken 语义一致。
var versionTokenRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// sha256HexRe 官方 SHA-256 摘要形状（64 位十六进制，大小写均可）。
var sha256HexRe = regexp.MustCompile(`^[0-9a-fA-F]{64}$`)

// ValidateVersionToken 版本令牌白名单校验：白名单字符、拒路径分隔与 ..、
// 拒尾点/尾连字符（Windows 目录名限制）、长度上限。中文报错。
func ValidateVersionToken(v string) error {
	v = strings.TrimSpace(v)
	switch {
	case v == "":
		return fmt.Errorf("版本令牌为空")
	case !versionTokenRe.MatchString(v):
		return fmt.Errorf("版本号 %q 含非法字符（仅限字母数字与 . _ -，不得含 / \\ : 等路径分隔，不得以 . 或 - 结尾，长度 ≤64）", v)
	case strings.HasSuffix(v, ".") || strings.HasSuffix(v, "-"):
		return fmt.Errorf("版本号 %q 不得以 . 或 - 结尾（Windows 目录名限制）", v)
	case strings.Contains(v, ".."):
		return fmt.Errorf("版本号 %q 不得包含连续的点 ..（目录名逃逸防护）", v)
	}
	return nil
}

// isSHA256Hex 判断字符串是否为合法的 SHA-256 摘要形状。
func isSHA256Hex(s string) bool {
	return sha256HexRe.MatchString(strings.TrimSpace(s))
}

// fileSHA256 计算文件全量 SHA-256（十六进制小写）。
func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("无法打开文件 %s: %w", path, err)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("读取文件 %s 失败: %w", path, err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// writeJSONFile 以缩进 JSON 落盘元信息文件（0644）。
func writeJSONFile(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// sanitizeFilePart 把任意名字清洗为可安全用于临时文件模式的后缀：
// 只保留字母数字与 . _ -，截断到 64 字符；清洗后为空则回退 "artifact"。
func sanitizeFilePart(raw string) string {
	raw = filepathBase(raw)
	var b strings.Builder
	for _, r := range raw {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '_' || r == '-' {
			b.WriteRune(r)
		}
	}
	out := b.String()
	if len(out) > 64 {
		out = out[:64]
	}
	if out == "" {
		out = "artifact"
	}
	return out
}

// filepathBase 对跨平台路径取基名（正/反斜杠都视为分隔，兼容 zip 反斜杠名）。
func filepathBase(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[i+1:]
	}
	return p
}
