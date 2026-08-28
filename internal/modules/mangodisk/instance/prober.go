// Package instance 实现 MangoDisk Tauri 单实例探测和 JobObject 生命周期托管。
package instance

import "time"

// MangoDiskProbe 抽象单实例互斥体和 signal window PID 探测。
type MangoDiskProbe interface {
	IsRunning() bool
	WaitForReady(timeout time.Duration) bool
	SignalPID() (uint32, bool)
}
