//go:build !windows

package aiagent

import "os/exec"

// hideChildConsole 非 Windows 平台没有"控制台黑框"语义，no-op。
// 平台差异只收在这两个最小文件里，主流程（agent.go）不出现构建标签
// ——与 packages/go/supervisor 的同名分文件姿势一致（supervisor.go 包注释申明了该纪律）。
func hideChildConsole(*exec.Cmd) {}
