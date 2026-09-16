// Package instance 实现 hanxi-ocr 单实例运行引擎：
//
// hanxi-ocr 是用户自备的本地 OCR HTTP 服务（微信 4.0 引擎封装，私有分发件，
// 不随 Hanxi 打包）。本引擎负责：端口契约探测（外部实例感知）、JobObject
// 托管启停（JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE，Hanxi 退出内核连带终止，
// 杜绝孤儿占端口）、进程输出环形缓冲（排障）。
//
// 上游契约（hanxi-ocr v0.2.0 源码实证）：
//   - 控制台子系统 exe，裸跑即 serve 监听 127.0.0.1:53120，仅回环 + Host 头校验；
//   - 自定义端口参数：`serve -port <n>`；
//   - GET /api/status 返回 {"ok":true,"name":"hanxi-ocr",...}——外部实例
//     判别以该契约探测为准（端口开 + 契约匹配），进程名扫描仅作崩溃分类辅助；
//   - 有优雅退出通道 POST /api/shutdown（service 层优先走它，引擎只留强杀兜底）。
//
// 本包零框架依赖，便于单元测试。
package instance

// Probe 实例存活与端口探测（免框架依赖；service 层注入平台实现）。
type Probe interface {
	// FindPIDs 返回全部 hanxi-ocr.exe 进程 PID（空 = 未运行；含外部实例）。
	FindPIDs() []uint32
	// IsRunning hanxi-ocr.exe 进程存在性探测（进程名扫描，覆盖任意端口实例）。
	IsRunning() bool
	// PortOpen 指定监听地址 TCP 可连通。
	PortOpen(listenAddr string) bool
	// IsOCRService 契约探测：GET /api/status 且 name 匹配 hanxi-ocr。
	// 与 PortOpen 联合使用可区分"hanxi-ocr 服务"与"任意占端口程序"。
	IsOCRService(listenAddr string) bool
}
