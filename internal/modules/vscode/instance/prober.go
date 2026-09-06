// Package instance 实现 VS Code 实例运行引擎（便携版 / 安装版各一台 Engine）：
//
// VS Code 由本引擎启动后绑定 Windows Job Object（JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE），
// Hanxi 无论以何种方式退出（托盘退出/崩溃/强杀），内核都会连带终止 Code.exe
// 及其全部 Electron 子进程（GPU/扩展宿主等），杜绝孤儿驻留。
// 托管默认 Detached（全托管统一口径）：不随 Hanxi 关闭，由 store 开关联动。
//
// 上游实例模型（Electron，src/vs/code/electron-main/app.ts 实证）：
//   - 单实例锁 requestSingleInstanceLock 按 user-data 目录分实例组：二次无参拉起
//     即"信使"——转发唤醒既有实例窗口后自退（markeron 先例，信使不 Wait 不进 Job）；
//   - 便携版（data\ 自包含）与安装版（%APPDATA%\Code）user-data 不同 → 实例组
//     天然隔离，两形态可同时运行、信使各唤各的；
//   - 命名互斥体 "vscode" 仅 isInnoSetupInstall()（安装版）时创建——探测通道
//     据此分治：安装版 OpenMutex("vscode")（与用户日常实例同组语义一致），
//     便携版无互斥体可用，走 Code.exe 进程镜像路径前缀匹配托管目录。
//   - 关窗即退（无托盘驻留），因此不设空闲自动退出：用户关窗进程自退，
//     窗口开着说明用户正在编辑，3 分钟强退编辑器是反需求。
//
// 本包零框架依赖，便于单元测试。
package instance

import "time"

// Probe 实例存活探测（免框架依赖；service 层注入 Windows 实现）。
type Probe interface {
	// IsRunning 该形态实例是否存活（安装版=互斥体；便携版=托管目录内 Code.exe 进程）。
	IsRunning() bool
	// WaitForReady 轮询等待实例就绪信号，超时返回 false。
	WaitForReady(timeout time.Duration) bool
}
