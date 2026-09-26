package dbx

import "hanxi/internal/modules/dbx/instance"

// ControlOutcome 打开窗口操作的执行结果说明。
type ControlOutcome struct {
	Action   string `json:"action"` // started（冷启动）/ opened（自有实例信使唤窗）/ external-opened（外部实例信使唤窗）/ starting（启动临界区）
	External bool   `json:"external"`
	Message  string `json:"message"` // 面向用户的执行说明
}

// QuitOutcome 退出执行结果。
// 如实口径：DBX 关窗语义为 CloseRequested → 驻托盘或弹询问确认框，WM_CLOSE
// 可能不退进程或被模态框挂住——托管退出在宽限后以 JobObject 强杀兜底；
// 强杀仅覆盖自有实例。若退出后仍检见同名进程（用户自启外部实例，或应用
// 自带"托管备份"计划任务以 --managed-backup-worker 再拉起的残留），状态
// 如实翻为 external 并在 Message 给出指引——Hanxi 绝不越权强杀托管外进程。
type QuitOutcome struct {
	Stopped  bool   `json:"stopped"`  // 是否真正终止了自有实例
	External bool   `json:"external"` // true = 当前为外部实例，未越权终止
	Message  string `json:"message"`  // 面向用户的执行说明
}

// DBXStatus GetStatus 复合投影：实例快照 + 使用版本的账本漂移信号
// （version 线 VerifyLedger 只读复查结果；mtime 闸控成本纪律见其注释）+
// 数据改道账目。内嵌 Snapshot 经 JSON 展平，前端消费形状 = instance-state
// 事件载荷 + drifted/driftNote/dataDir/dataDirInjected 四字段。
type DBXStatus struct {
	instance.Snapshot
	Drifted   bool   `json:"drifted"`   // 使用版本 exe 发生账外漂移（只报告不处置）
	DriftNote string `json:"driftNote"` // 漂移/无法比对的如实明细（空 = 未复查）
	// DataDir 托管注入的受控数据目录（Hanxi 数据根下稳定路径，恒有值投影；
	// DBX_DATA_DIR 即指向此处，跨版本共享、删版本不删数据）。
	DataDir string `json:"dataDir"`
	// DataDirInjected 自有实例是否至少完成过一次带注入的托管启动（历史账目，
	// metaHints 披露"数据已在 Hanxi 数据根落位"的依据）。
	DataDirInjected bool `json:"dataDirInjected"`
}
