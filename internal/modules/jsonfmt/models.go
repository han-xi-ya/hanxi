// Package jsonfmt 内置模块：JSON 美化——**基础美化器**。
//
// 2026-10-31 机主裁决：本模块只保留主干（标准 JSON 解析 → 格式化/压缩/排序），
// 曾经长出来的规则族（日志前缀剥离、宽松语法修复、扫描抽取、转义展开、查找/
// 替换/统计、AI 提取）整体删除——不是隐藏：多开关就多一层"它刚才对我做了什么"
// 的疑虑，工具要诚实就要小。脏文本的出路统一是 AI 聊天（视图里的"带去 AI 聊天"
// 一键转交，提示词纪律见 aichat 模块）。历史与教训都还在 git 与踩坑册里
// （#125–#133），要回来从 2ff5 之后的提交里捡，不许悄悄再长回去。
//
// 留在 Go 后端的唯一硬理由是数字保真：JS 的 JSON.parse 会把数字过一遍
// float64，19 位雪花 ID 尾数直接被改；本包自始至终把数字当**原始字面量**
// 搬运，不做任何数值转换。
package jsonfmt

// JsonFmtRequest 一次美化请求。
type JsonFmtRequest struct {
	// Text 是待格式化的 JSON 原文。必须已是合法 JSON——本模块不再猜、不再修。
	Text string `json:"text"`
	// Indent 每级缩进空格数；0 表示压缩输出（单行、无空白）。
	Indent int `json:"indent"`
	// SortKeys 为 true 时对象键按字典序重排；默认 false 保留原始顺序——
	// "字段顺序即写入顺序"本身是信息，不能默认打乱。
	SortKeys bool `json:"sortKeys"`
}

// JsonFmtResult 一次美化的结果。
type JsonFmtResult struct {
	OK     bool   `json:"ok"`
	Output string `json:"output"`
	Error  string `json:"error,omitempty"`
	// Hint 是失败时"下一步该干什么"的可操作判断（尾部截断 / 开头没复制全 /
	// 开头混入说明文字——指路 AI 聊天）。判不出来就留空，宁可少说不说反。
	Hint string `json:"hint,omitempty"`

	// 失败定位：Line/Col 均 1 基，0 表示给不出位置；Snippet 是出错附近原文片段。
	Line    int    `json:"line"`
	Col     int    `json:"col"`
	Snippet string `json:"snippet,omitempty"`

	Kind string `json:"kind"` // object / array / scalar / ""（未解析成功）
	// Warnings 是解析过程中的如实说明（重复键等），前端逐条显示。
	Warnings []string `json:"warnings,omitempty"`
}

const (
	// MaxInputBytes 单次输入上限。这是同步 RPC，日志片段可达数十 MiB，
	// 不封顶就等于让一次粘贴有机会把整机的内存拖走。超限如实报错并提示改用文件，
	// 不静默截断——截断后的 JSON 必然解析失败，报错位置还会误导人。
	MaxInputBytes = 2 << 20 // 2 MiB

	// maxDepth 嵌套深度上限，防构造畸形输入把 goroutine 栈打爆。
	maxDepth = 200

	// DefaultIndent 默认两空格缩进（与前端 UI 的默认档一致）。
	DefaultIndent = 2

	// snippetRadius 出错位置前后各取多少字节作为片段。
	snippetRadius = 60
)
