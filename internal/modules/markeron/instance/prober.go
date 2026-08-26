package instance

import "time"

// MutexName MarkerOn Windows 单实例命名互斥体。
// 来源于上游 single_instance_win.rs：`{identifier}-sim`（identifier = com.markeron.app），
// 主实例存活期间持续持有，退出即释放——是"是否已有 MarkerOn 主实例在运行"的权威信号。
const MutexName = "com.markeron.app-sim"

// MarkerProbe 探测 MarkerOn 主实例存在性（接口注入，便于单元测试替换为 fake）。
type MarkerProbe interface {
	// IsMarkerOnRunning 主实例互斥体存在即视为运行中
	IsMarkerOnRunning() bool
	// WaitForMarkerOnReady 阻塞轮询直至互斥体出现或超时（100ms 间隔）
	WaitForMarkerOnReady(timeout time.Duration) bool
}
