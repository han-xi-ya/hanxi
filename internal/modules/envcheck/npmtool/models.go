// Package npmtool 在开发环境检测内提供配置驱动的 npm 全局 CLI 工具管理框架。
// 目录（catalog）是唯一真源：每个条目同时驱动 detect 卡片探测、npm registry
// 最新版对比，以及经 npm 的一键安装/升级/卸载操作。新增工具 = 目录加一条配置，
// 检测、卡片、service、前端全链路零改动。
package npmtool

import (
	"hanxi/internal/modules/envcheck/detect"
	"hanxi/internal/modules/envcheck/remoteversion"
)

// ToolBrief 目录工具的最小展示信息（随 Overview 下发，前端不再硬编码包名）。
type ToolBrief struct {
	Command string `json:"command"` // 目录 ID = 可执行文件名，如 "claude"
	Display string `json:"display"` // 前端展示名，如 "Claude Code"
	Package string `json:"package"` // npm 包名，如 "@anthropic-ai/claude-code"
}

// ToolOverview 单个目录工具卡：本机探测 × registry 最新版 × 关系与安全提醒。
// Local 恒填充（即便未安装也是 status=missing 的 ToolInfo），registry 查询失败
// 时降级为 LatestError + RelationUnknown，前端仍需渲染本机状态与操作按钮。
type ToolOverview struct {
	Tool           ToolBrief              `json:"tool"`
	Local          detect.ToolInfo        `json:"local"`
	Latest         remoteversion.Release  `json:"latest"`
	LatestError    string                 `json:"latestError,omitempty"`
	Relation       remoteversion.Relation `json:"relation"`
	RelationDetail string                 `json:"relationDetail,omitempty"` // prefix 不一致等安全提醒
	IsStale        bool                   `json:"isStale"`
	FetchedAt      string                 `json:"fetchedAt"`
}

// Overview 目录整体视图。ActiveOperation 为 npm 全局树互斥锁快照，页面重挂载
// （如操作中途切走再回）也能恢复按钮忙碌态，避免前端凭内存态误判空闲。
type Overview struct {
	Tools           []ToolOverview     `json:"tools"`
	ActiveOperation *OperationProgress `json:"activeOperation,omitempty"`
}

// OperationProgress 操作阶段事件（仿 nanazip.OperationProgress 裁剪，无下载字节进度）。
type OperationProgress struct {
	OperationID string `json:"operationId"`
	ToolID      string `json:"toolId"`
	Kind        string `json:"kind"`  // install/upgrade/uninstall
	Stage       string `json:"stage"` // started/running/done/error
	Message     string `json:"message"`
	Terminal    bool   `json:"terminal"`
	Success     bool   `json:"success"`
}

// OperationLog npm 命令输出流式行事件（逐行，仿 frpc/ddnsgo instance-log）。
type OperationLog struct {
	OperationID string `json:"operationId"`
	ToolID      string `json:"toolId"`
	Line        string `json:"line"`
}

// OperationAccepted 操作受理回执：同步校验通过后立即返回，真实进度经事件推送。
type OperationAccepted struct {
	OperationID string `json:"operationId"`
	Kind        string `json:"kind"`
	Message     string `json:"message"`
}
