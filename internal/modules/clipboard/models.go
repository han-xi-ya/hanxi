package clipboard

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

// 数据模型与入库判定纯函数。结构与 JSON tag 逐字冻结于
// docs/plans/2026-09-26-clipboard-contract.md §3（wire 与落盘共用同一套 tag，
// text 落盘为明文——契约 §12 v1.6 机主裁决取消 DPAPI），改动即前后端断链，禁止擅改。

// Kind 条目内容类型。
type Kind string

const (
	KindText  Kind = "text"
	KindImage Kind = "image"
	KindFile  Kind = "file"
)

// Entry 一份剪贴板历史。JSON tag 即 wire/落盘字段名，与前端 types/clipboard.ts 逐字对齐。
type Entry struct {
	ID         string   `json:"id"` // 16 hex 随机
	Hash       string   `json:"hash"`
	Kind       Kind     `json:"kind"`
	Text       string   `json:"text,omitempty"`  // wire 与落盘同为明文（v1.6）
	Preview    string   `json:"preview"`         // ≤120 rune 首行摘要，列表页用
	Files      []string `json:"files,omitempty"` // CF_HDROP
	Blob       string   `json:"blob,omitempty"`  // "blobs/<sha>.png"
	Thumb      string   `json:"thumb,omitempty"` // R-G2：64px 等比 JPEG(q60 起) dataURL，内嵌 index，List 天然带图
	Width      int      `json:"width,omitempty"`
	Height     int      `json:"height,omitempty"`
	ByteSize   int64    `json:"byteSize"`
	SourceApp  string   `json:"sourceApp,omitempty"` // 复制时前台窗口标题
	SourceExe  string   `json:"sourceExe,omitempty"` // 小写进程名，无路径
	AutoTags   []string `json:"autoTags,omitempty"`  // url/email/phone/color/code/cjk
	Sensitive  bool     `json:"sensitive,omitempty"`
	Pinned     bool     `json:"pinned,omitempty"`
	Manual     bool     `json:"manual,omitempty"` // 固定片段（手建，不淘汰）
	CreatedAt  int64    `json:"createdAt"`        // ms
	LastUsedAt int64    `json:"lastUsedAt,omitempty"`
	UseCount   int      `json:"useCount,omitempty"`
	BlobData   []byte   `json:"blobData,omitempty"` // 仅 Get 回填（PNG 原始字节）
}

// Status 服务运行态（GetStatus 返回面，前端状态区/浮层页脚用）。
type Status struct {
	Paused       bool     `json:"paused"`
	EntryCount   int      `json:"entryCount"`
	BlobBytes    int64    `json:"blobBytes"`
	MaxEntries   int      `json:"maxEntries"`   // 500
	MaxBlobBytes int64    `json:"maxBlobBytes"` // 100<<20
	ExcludedExes []string `json:"excludedExes"`
}

// ---------- 入库判定（纯函数，可单测） ----------

// sensitiveDefaultUsers 敏感排除表**出厂默认**（契约 §2，小写进程名；原文
// 1password.exe 出现两次，此处去重，7 项全量不得裁撤——v1.3.4 裁决：
// "不记密码管理器的复制"是功能存在理由级的硬约束）。
//
// 本表只是默认值：运行态活表由 service 侧持有（可被收口阶段的设置面更新），
// 判定一律 matchExcluded + 活表，禁止任何调用方绕过活表直读默认数组。
var sensitiveDefaultUsers = []string{
	"1password.exe", "bitwarden.exe", "keepass.exe", "keepassxc.exe",
	"lastpass.exe", "veracrypt.exe", "truecrypt.exe",
}

// DefaultExcludedExes 排除表出厂默认（返回副本，改副本不影响任何人；
// 契约 §12 v1.3.4——GUI 默认表/设置页初值/MCP 审计回显共用此单一来源）。
func DefaultExcludedExes() []string {
	return append([]string(nil), sensitiveDefaultUsers...)
}

// matchExcluded 判定复制来源进程名是否命中排除表（纯函数，口径与 A3 的
// matchExe 三分支对位）：空名不排除；子串含 "password" 排除（契约 §2 尾注）；
// 否则对活表大小写不敏感精确命中。
func matchExcluded(exe string, table []string) bool {
	if exe == "" {
		return false
	}
	if strings.Contains(strings.ToLower(exe), "password") {
		return true
	}
	for _, e := range table {
		if strings.EqualFold(e, exe) {
			return true
		}
	}
	return false
}

// secretPatterns secret 启发式（契约 §2）：命中者**入库**但 sensitive=true，
// MCP 通道整条不下发（memo IsMasked 同口径）。
var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`sk-[A-Za-z0-9_-]{16,}`),
	regexp.MustCompile(`ghp_[A-Za-z0-9]{20,}`),
	regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
	regexp.MustCompile(`xox[abprs]-`),
	regexp.MustCompile(`BEGIN [A-Z ]*PRIVATE KEY`),
}

// looksSecret 文本是否命中 secret 启发式（任一即算）。
func looksSecret(text string) bool {
	if text == "" {
		return false
	}
	for _, re := range secretPatterns {
		if re.MatchString(text) {
			return true
		}
	}
	return false
}

// AutoTags 按契约 §3 判定文本语义标签，纯函数：多标签可共存，
// 按 url/email/phone/color/code/cjk 固定序输出去重。
func AutoTags(text string) []string {
	t := strings.TrimSpace(text)
	if t == "" {
		return nil
	}
	var tags []string
	add := func(tag string) {
		for _, have := range tags {
			if have == tag {
				return
			}
		}
		tags = append(tags, tag)
	}

	lower := strings.ToLower(t)
	if strings.Contains(lower, "http://") || strings.Contains(lower, "https://") {
		add("url")
	}
	if emailRe.MatchString(t) {
		add("email")
	}
	if phoneRe.MatchString(t) {
		add("phone")
	}
	if colorRe.MatchString(t) {
		add("color")
	}
	// code：含 ``` 围栏，或 ≥3 行且命中关键字（func/class/def/import/SELECT/{/}/;）
	if strings.Contains(t, "```") || (strings.Count(t, "\n") >= 2 && codeRe.MatchString(t)) {
		add("code")
	}
	if containsCJK(t) {
		add("cjk")
	}
	return tags
}

var (
	emailRe = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9-]+(\.[A-Za-z0-9-]+)+`)
	phoneRe = regexp.MustCompile(`1[3-9]\d{9}`)
	colorRe = regexp.MustCompile(`^#[0-9a-fA-F]{6}$|^#[0-9a-fA-F]{3}$`)
	codeRe  = regexp.MustCompile(`func|class|def|import|SELECT|[{};]`)
)

// containsCJK 含汉字/假名/谚文即算 cjk（契约 §3 "含 CJK"）。
func containsCJK(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Han, r) ||
			unicode.Is(unicode.Hiragana, r) ||
			unicode.Is(unicode.Katakana, r) ||
			unicode.Is(unicode.Hangul, r) {
			return true
		}
	}
	return false
}

// ---------- 入库装配辅助（纯函数） ----------

// newEntryID 16 hex 随机 ID。crypto/rand 失败理论上不可发生，发生即如实报错，
// 不退回时间戳（ID 撞车会让前端 keyed 列表错乱）。
func newEntryID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("生成条目 ID 失败: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

// hashOf 去重哈希：sha256(语义字节)（契约 §2——text 取原文 utf8；image 取
// CF_DIB 原始字节；file 取路径列表以 \n join）。
func hashOf(kind Kind, text string, files []string, dib []byte) string {
	var sum []byte
	switch kind {
	case KindImage:
		sum = dib
	case KindFile:
		sum = []byte(strings.Join(files, "\n"))
	default:
		sum = []byte(text)
	}
	h := sha256.Sum256(sum)
	return hex.EncodeToString(h[:])
}

// previewOf 列表摘要（≤120 rune）：text 取首个非空行；file 取路径串（空格连接）；
// image 无文本面，给尺寸占位。
func previewOf(kind Kind, text string, files []string, width, height int) string {
	switch kind {
	case KindImage:
		return fmt.Sprintf("图片 %d×%d", width, height)
	case KindFile:
		return truncateRunes(strings.Join(files, " "), previewMaxRunes)
	default:
		for _, line := range strings.Split(text, "\n") {
			if l := strings.TrimSpace(line); l != "" {
				return truncateRunes(l, previewMaxRunes)
			}
		}
		return "(空文本)"
	}
}

const (
	previewMaxRunes = 120 // 落盘摘要上限（契约 §3）
	wirePreviewMax  = 200 // clipboard:updated 事件载荷 Preview 截断上限（契约 §5）
)

// truncateRunes 按 rune 截断（多字节安全），不追加省略号（前端自绘）。
func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}

// cloneEntry 深拷贝可变切片，对外出口一律经它，杜绝调用方改中内存账本。
// stripWire=true 时清 Text/BlobData 并截事件级 Preview（clipboard:updated 载荷）。
func cloneEntry(e Entry, stripWire bool) Entry {
	c := e
	c.Files = append([]string(nil), e.Files...)
	c.AutoTags = append([]string(nil), e.AutoTags...)
	if stripWire {
		c.Text = ""
		c.BlobData = nil
		c.Preview = truncateRunes(c.Preview, wirePreviewMax)
	} else {
		c.BlobData = append([]byte(nil), e.BlobData...)
	}
	return c
}
