// Package instance 实现 MangoDisk Tauri 单实例探测和 JobObject 生命周期托管。
package instance

import "time"

// MangoDiskProbe 抽象单实例互斥体和 signal window PID 探测。
type MangoDiskProbe interface {
	// IsRunning 通过 Tauri 单实例互斥体判断是否存在任一 MangoDisk 主实例（不区分归属）。
	IsRunning() bool
	// WaitForReady 轮询直至互斥体与信号窗口同时出现（GUI 完全就绪），超时返回 false。
	WaitForReady(timeout time.Duration) bool
	// SignalPID 读取 Tauri 隐藏信号窗口的属主 PID，用于区分外部实例与自有实例。
	SignalPID() (uint32, bool)
}
