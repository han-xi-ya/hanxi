package vscode

import (
	"hanxi/internal/modules/vscode/instance"
	"hanxi/internal/modules/vscode/version"
)

// ControlOutcome 打开窗口/安装操作的执行结果说明。
type ControlOutcome struct {
	Action   string `json:"action"` // started / opened / external-opened / confirm-required / installing
	External bool   `json:"external"`
	Message  string `json:"message"` // 面向用户的执行说明
}

// QuitOutcome 退出执行结果。
type QuitOutcome struct {
	Stopped  bool   `json:"stopped"`  // 是否真正终止了自有实例
	External bool   `json:"external"` // true = 当前为外部实例，未越权终止
	Message  string `json:"message"`  // 面向用户的执行说明
}

// Status 前端一次性拉取的组合状态（两形态引擎快照 + 安装版注册表感知 + 联动开关）。
type Status struct {
	Portable     instance.Snapshot     `json:"portable"`  // 便携版引擎（form=portable）
	Installer    instance.Snapshot     `json:"installer"` // 安装版引擎（form=installer）
	Installed    version.InstalledInfo `json:"installed"` // 安装版本机探测（含用户自行安装的）
	FollowOnExit bool                  `json:"followOnExit"`
}
