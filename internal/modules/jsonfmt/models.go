// Package jsonfmt 内置模块：JSON 美化。为"从日志里复制一段 JSON"这个动作服务——
// 日志场景的三个麻烦都在这里解决：每行带日志前缀、JSON 被转义成字符串嵌在字段里、
// 以及大整数（雪花 ID）在格式化时被静默改掉末位。
//
// 第三条是这个模块必须待在 Go 后端、不能图省事放前端 JS 的全部理由：JS 的
// JSON.parse 会把数字过一遍 float64，19 位 ID 尾数直接被改；本包自始至终把数字
// 当**原始字面量**搬运，不做任何数值转换。
package jsonfmt

// JsonFmtRequest 一次美化请求。
type JsonFmtRequest struct {
	// Text 是原始输入，可能每行都带日志前缀。
	Text string `json:"text"`
	// Indent 每级缩进空格数；0 表示压缩输出（单行、无空白）。
	Indent int `json:"indent"`
	// SortKeys 为 true 时对象键按字典序重排；默认 false 保留原始顺序——
	// 日志排障时"字段顺序即代码里的写入顺序"本身是信息，不能默认打乱。
	SortKeys bool `json:"sortKeys"`
	// Unescape 为 true 时解开被字符串化的嵌套 JSON（见 JsonFmtResult.Unwrapped）。
	Unescape bool `json:"unescape"`
	// ExtractAll 为 true 时：整段与"剥前缀"两道都失败，就在文本里**扫描出每一段
	// 完整 JSON 逐段美化**，而不是只回一个错。默认开——"粘一堆终端输出进来要一次
	// 全美化"才是这个工具的真实用法；让用户手工注释掉别的段是设计失败。
	ExtractAll bool `json:"extractAll"`
	// Query 非空时顺带做查找：命中"路径含该词"或"字符串值含该词"的节点，
	// 返回规范化路径与值摘要。空则只给统计，省下一次全树字符串匹配。
	Query string `json:"query"`
	// Replace 非空时执行一次结构化替换（改值或改键名，产出仍是合法 JSON）。
	// 它**必须显式触发**：格式化/修复/抽取可以自动，替换不行——改动意图得是用户自己按的。
	Replace *Replace `json:"replace,omitempty"`
	// 修复（宽松语法 + 补缺闭合符）**不设开关、恒自动**：它的触发前提是
	// "原样解析与剥前缀都不行"，也就是说输入本来就没法用——修是净收益，
	// 不修只能回一句语法错误。改了什么一律通过 Repairs 摆到明面上。
}

// RepairStat 一类自动修复及其次数。Kind 直接是人话标签，UI 原样显示。
type RepairStat struct {
	Kind  string `json:"kind"`
	Count int    `json:"count"`
}

// Segment 一段被抽取并格式化出来的 JSON。
//
// Line 是它在**实际被解析的那份文本**里的 1 基行号，供用户回对原日志；
// Index 从 1 起——给人看的编号不该从 0 开始。
type Segment struct {
	Index     int          `json:"index"`
	Line      int          `json:"line"`
	Kind      string       `json:"kind"`
	Output    string       `json:"output"`
	Unwrapped int          `json:"unwrapped"`
	Stats     JsonFmtStats `json:"stats"`
}

// JsonFmtResult 一次美化的结果。
//
// 清洗动作（剔了几行前缀、解开几层转义）一律如实回报而不是默默改数据：用户贴进来
// 的东西被改过，他必须看得见改了什么，否则"美化"就成了"悄悄丢数据"的借口。
type JsonFmtResult struct {
	OK     bool   `json:"ok"`
	Output string `json:"output"`
	Error  string `json:"error,omitempty"`
	// Hint 是失败时"下一步该干什么"的可操作判断（尾部截断 / 开头没复制全 / 无）。
	Hint string `json:"hint,omitempty"`

	// 失败定位：Line/Col 均 1 基，0 表示给不出位置；Snippet 是出错附近原文片段。
	Line    int    `json:"line"`
	Col     int    `json:"col"`
	Snippet string `json:"snippet,omitempty"`

	Repaired int `json:"repaired"` // 补了多少个闭合符（0=未补）
	// Repairs 是宽松语法修复的逐类计数（单引号、裸键、尾逗号…）。空表示一个字节都没改。
	Repairs            []RepairStat `json:"repairs,omitempty"`
	DroppedPrefixLines int          `json:"droppedPrefixLines"` // 剔除了多少行的日志前缀
	Unwrapped          int          `json:"unwrapped"`          // 解开了几层被字符串化的 JSON

	Kind string `json:"kind"` // object / array / scalar / ""（未解析成功）
	// Segments 是多段抽取的结果（单段时也只有一项）。顶层 Output/Stats 是它们的
	// 拼接与聚合：复制走顶层、展示走 Segments，两者必须同源，否则汇总与明细会对不上。
	Segments []Segment    `json:"segments,omitempty"`
	Stats    JsonFmtStats `json:"stats"` // 结构概览
	// KeyStats 是键路径频次表（按出现次数降序）。数组下标折叠成 `[]`，
	// 否则"一批记录"会产出几十行同构路径，把真正有用的次数信息淹掉。
	KeyStats []KeyStat `json:"keyStats,omitempty"`
	// Matches 是查找命中（上限 maxMatches 条；超出只报截断标记，不硬塞列表）。
	Replaced       int     `json:"replaced"` // 替换命中并改动了几处（未请求替换时为 0）
	Matches        []Match `json:"matches,omitempty"`
	MatchTruncated bool    `json:"matchTruncated"`
	// Warnings 是清洗与解析过程中的如实说明（重复键、深度触顶等），前端逐条显示。
	Warnings []string `json:"warnings,omitempty"`
}

// JsonFmtStats 结果的结构统计，全部在解析时一次算出，不由前端二次统计。
type JsonFmtStats struct {
	Keys     int `json:"keys"`     // 对象键总数
	Nodes    int `json:"nodes"`    // 值节点总数（含标量）
	MaxDepth int `json:"maxDepth"` // 最大嵌套深度
	BytesIn  int `json:"bytesIn"`  // 输入字节数（清洗前）
	BytesOut int `json:"bytesOut"` // 输出字节数
	LinesOut int `json:"linesOut"` // 输出行数
}

const (
	// maxSegments 一次最多抽出的段数（现值见下行）。终端里贴进几百段的情况极少，
	// 但必须有上限：没有它，一段畸形输入可以让扫描与渲染的开销随段数线性失控。
	// 这里刻意不写死数字：上一版注释写着 500 而常量是 200，数字漂了还看不出来——
	// 注释里的数字没人测，只有常量改的时候有人会改，所以口径统一交给常量本身。
	maxSegments = 200

	// maxScanAttempts 抽取模式允许尝试解析的候选数上限（不是字节数，见 extractSegments）。
	// 没有这道闸，一段畸形文本就能让浏览器和后端一起卡在 O(n²) 的尝试里。
	maxScanAttempts = 20000

	// MaxInputBytes 单次输入上限。这是同步 RPC，且日志片段可达数十 MiB，
	// 不封顶就等于让一次粘贴有机会把整机的内存拖走。超限如实报错并提示改用文件，
	// 不静默截断——截断后的 JSON 必然解析失败，报错位置还会误导人。
	MaxInputBytes = 2 << 20 // 2 MiB

	// maxDepth 嵌套深度上限，防构造畸形输入把 goroutine 栈打爆。
	maxDepth = 200

	// DefaultIndent 默认两空格缩进（与前端 UI 的默认档一致）。
	DefaultIndent = 2

	// maxUnwrapDepth 转义还原最多递归层数。3 层已经覆盖"日志里套 HTTP body
	// 里再套 MQ 消息"这类真实形态；再深就是自我复制的畸形数据，宁可不解。
	maxUnwrapDepth = 3

	// snippetRadius 出错位置前后各取多少字节作为片段。
	snippetRadius = 60
)
