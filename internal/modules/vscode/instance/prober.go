package instance

import "time"

// Probe 实例存活探测（免框架依赖；service 层注入 Windows 实现，经 supProbe
// 适配为内核统一探针契约）。两形态探测通道分治：
//   - 安装版：命名互斥体 "vscode" 存在性（仅 Inno 安装版创建，与用户日常
//     实例同组语义一致）；
//   - 便携版：托管目录镜像路径前缀内的 Code.exe 进程枚举（无互斥体可用）。
type Probe interface {
	// IsRunning 该形态实例是否存活（安装版=互斥体；便携版=托管目录内 Code.exe 进程）。
	IsRunning() bool
	// WaitForReady 轮询等待实例就绪信号，超时返回 false。
	WaitForReady(timeout time.Duration) bool
}
