// Package history 统一历史记录公共包：各功能工具"最近用过什么"的留档底座。
//
// 设计选型（论证详见 docs/plans/PLAN_HISTORY.md §1.1/§2.1，开放问题 Q1-Q6 已拍板）：
//   - 存储：单文件 <StateDir>/history.json，格式 map[funcType][]Record（桶头插，新→旧）。
//     桶数首批 3、全接后 ≲15，每桶 ≤200 条且字段有截断上界，单文件体量有硬上限；
//     每桶一文件要多文件生命周期与目录扫描，收益为零，不取。
//   - 落盘：复用 internal/jsonstore 公共核（MarshalIndent→tmp→fsync→rename 原子写），
//     与全仓"每次变更立即落盘"惯例一致，不做 debounce（全仓无先例，不做第一个特例）。
//   - 写失败语义：内存不回滚、仅记日志返回错误——历史是尽力而为的副作用，
//     不能让"记历史失败"阻塞或回滚业务操作（与 memo TogglePin 同档）。
//   - 损坏策略：取 frpc 严格档——loadErr 驻留、禁一切 Save 防空库覆写存量，
//     读侧返回错误让面板可见提示；不做隔离改名（那是启动关键路径 config 的待遇，
//     历史损坏降级为"只读不可新增"即可，需手工删文件恢复写）。
//   - 脱敏：构造入口 newRecord 统一过 logging.Redact（与日志出口同一口径），
//     模块侧零脱敏责任。注意 Redact 只认 token/password=xxx 形态，
//     自然语言裸文本凭据拦不住——OCR 全文入库档位由设置页开关裁决（Q1）。
//   - Extra 刻意做成纯字符串而非 map：防被塞任意大对象撑爆单文件。
//
// 依赖纪律：本包不 import 任何业务模块（防环）；funcType 取值由各模块自己传入
// （约定用模块注册 ID 字符串，如 "ocr"/"portkill"/"envcheck"）。
package history

// Record 一条历史记录。四文本字段（Summary/Input/Output/Extra）在 Save 入口
// 统一过脱敏与截断，调用方直接传原文即可。
type Record struct {
	// ID 全局单调递增：时间戳毫秒*1000 起步，与桶内已见最大值取严格递增，
	// 重启后天然续接（时钟回拨也单调）。
	ID int64 `json:"id"`
	// FuncType 分桶键："ocr" / "portkill" / "envcheck" …（对齐模块注册 ID）。
	FuncType string `json:"funcType"`
	// Summary 一行摘要；留空则 Save 时取 Input 前 40 字（对齐 MooTool 口径）。
	Summary string `json:"summary"`
	// Input 输入侧留档（如图片路径、端口号、工具 ID），截断上限 4000 rune。
	Input string `json:"input"`
	// Output 输出侧留档（如识别全文、错误信息），截断上限 4000 rune。
	Output string `json:"output"`
	// Extra 元信息标记，如 "snip"、"npm-install|fail"、"elevated|denied"。
	Extra string `json:"extra"`
	// CreatedAt RFC3339；留空则 Save 时补当前时间。
	CreatedAt string `json:"createdAt"`
}
