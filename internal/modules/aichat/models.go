package aichat

// 数据形状两套：RPC 返回的行（SessionRow）与三类事件载荷。
// 事件载荷必须在 internal/app/app.go 的 RegisterEvents() 登记类型（Wails v3
// 的严格类型闸会**静默丢弃**未注册载荷——与 Void 的口径见 app.go:157-159 注释），
// 且名字进 scripts/fixture/composition_contract.json，三处同改。

// SessionRow 会话索引行（hanxi 侧账本）。对话正文不在这里——它归 claude 自己
// 的 transcript（~/.claude/projects/<cwd 编码>/<sessionId>.jsonl，实测 init
// .memory_paths 实证）；hanxi 只起进程、认路径、记账目。
type SessionRow struct {
	ID        string `json:"id"`        // claude 会话 uuid4（--session-id 我方起名）
	Title     string `json:"title"`     // 首条消息前为"新会话"
	CreatedAt int64  `json:"createdAt"` // unix ms
	// Archived 封存位：CloseSession 只杀进程（transcript 不动，SendMessage
	// 会自动 --resume 复进）；DeleteSession 只从账本除名——CLI 的 transcript
	// **不由 hanxi 删除**（那是它自己的数据根，越界删它等于制造第二种归档语义）。
	Archived bool `json:"archived"`
}

// TimelineItem 消息流条目事件（aichat:timeline）。
// Kind 只放 UI 能直接消费的三档；内核里 thinking_tokens/hook/lifecycle 等
// 实测噪音帧**到不了这里**（在 aiagent 层就按设计丢弃）。
type TimelineItem struct {
	SessionID string `json:"sessionId"`
	TurnID    string `json:"turnId"`
	Kind      string `json:"kind"` // assistant | error | note
	Text      string `json:"text"`
	Ts        int64  `json:"ts"`
}

// SessionState 会话进程状态事件（aichat:session-state）。
// State: spawning | ready | working | idle | dead。
// "停止"在 v1 没有秒级 control 帧（未实测不进主链）——它等于杀进程再 resume，
// 所以 UI 的停止钮语义按"结束本轮并回收进程"标注，不假称即刻打断。
type SessionState struct {
	SessionID string `json:"sessionId"`
	State     string `json:"state"`
	Note      string `json:"note"` // 死亡原因/诊断尾巴（StderrNote 等），仅展示
}

// TurnStats 回合统计事件（aichat:turn-stats）。数字全部来自 result 帧实测字段
// （duration_ms/ttft_ms/usage/total_cost_usd），hanxi 不自己掐表造第二口径。
type TurnStats struct {
	SessionID    string  `json:"sessionId"`
	TurnID       string  `json:"turnId"`
	ElapsedMs    int64   `json:"elapsedMs"`
	TTFTMs       int64   `json:"ttftMs"`
	InBytes      int     `json:"inBytes"`
	OutBytes     int64   `json:"outBytes"`
	TotalCostUSD float64 `json:"totalCostUsd"` // 用户通道口径回包，展示可以，推断不行
}
