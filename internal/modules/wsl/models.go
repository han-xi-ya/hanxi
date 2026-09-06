// Package wsl 内置模块：Windows Subsystem for Linux 就绪检测与版本管理。
// 与 envcheck 同属"检测/运维"形态（无常驻资源、不托管进程）：
//   - 只读探针体检（系统门槛 / 虚拟化 / GitHub 403 通道诊断 / 发行版现状）；
//   - microsoft/WSL 官方 Releases 版本列表与 MSI 直链；
//   - 白名单化的提权操作（一键开启 / 更新 / 装发行版），命令面固定、绝不拼接任意输入。
package wsl

import "hanxi/internal/modules/wsl/readiness"

// OperationOutcome 提权操作统一回执（UAC 取消不算错误，以文案区分）。
type OperationOutcome struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// ReadinessUpdate 流式体检推送载荷（事件 wsl:readiness）：
// 前端先渲染全量 pending 骨架，各阶段到达后按 key 落位点亮，done 携带终版报告。
// 阶段划分与数据源一一对应，互不等待：system=探针、wsl=本机运行时、net=Go 网络探测。
type ReadinessUpdate struct {
	Stage  string                `json:"stage"` // system | wsl | net | done | error
	Items  []readiness.CheckItem `json:"items,omitempty"`
	Report *readiness.Report     `json:"report,omitempty"`
	Error  string                `json:"error,omitempty"` // stage=error 时的失败说明
}
