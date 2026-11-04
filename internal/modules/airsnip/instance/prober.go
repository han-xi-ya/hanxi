// Package instance 实现 AirSnip 单实例运行引擎（Wave 4 内核委托形态）：
//
// 进程治理主流程（spawn → Job Object 绑定 → 就绪/退出分类 → 手动停止/外部甄别）
// 收口至共享内核 hanxi/packages/go/supervisor；本包只保留 AirSnip 领域适配：
//   - 进程快照枚举探针（CreateToolhelp32Snapshot 命中 airsnip.exe）——上游确有
//     命名互斥体 Local\airsnip.single.instance（src/app/mod.rs 的 MUTEX_NAME），但刻意不用：
//     release 版以 requireAdministrator 运行（ADR 0003），提权进程创建的
//     内核对象默认 ACL 由提权令牌派生，中完整性 Hanxi 的 OpenMutex 是否放行
//     取决于该 DACL 细节，真机不便为证；进程名快照对完整性差异免疫、且天然
//     带出存活 PID，与 litemonitor/snipaste 同策略（探针实现见 probe_windows.go）；
//   - **控制通道是本模块最大的非共识点**：AirSnip 托盘常驻、无主窗口（只有一个
//     隐藏消息窗，类名 airsnip.hidden，标题 "AirSnip"），全局热键 F1 截图 /
//     F3 贴图才是它的操作面。上游"第二实例唤起已有实例"的路径是**死代码**：
//     acquire_single_instance 会向消息窗 PostMessage(WM_APP+2)（src/app/mod.rs），
//     而 wndproc 的 match 分支里没有任何 WM_APP+2 处理（src/app/wndproc.rs 全文
//     实证），二次拉起等于投一颗石子进空气——所以"信使二次拉起"在本模块不可用。
//     可用通道只有上游自己托盘菜单的命令 ID：handle_command 分派
//     40001 截图 / 40002 从剪贴板贴图 / 40003 设置 / 40004 退出（src/app/mod.rs
//     的 IDM_* 常量与 src/app/tray.rs 菜单项一一对应），退出项内部就是给自己投 WM_CLOSE。
//     本包据此用 PostMessage(WM_COMMAND, id) 到 airsnip.hidden 实现动作，
//     与用户手点托盘菜单完全同构。**代价如实记账**：这组 ID 是上游未公开的
//     内部约定，上游改版可能漂移；且 Hanxi 与实例不同完整性时消息被 UIPI
//     静默丢弃（PostMessage 返回成功而目标毫无反应）。因此**送达判据一律取外部
//     事实而非返回值**：截图等 airsnip.overlay 顶层窗在场、设置面板等
//     airsnip.settings 在场（两者类名均为上游源码实证），窗不在场就报失败并
//     由 service 给出提权/复探指引——不自证成功；贴图（40002）无独立窗可对账
//     （剪贴板无位图时上游本就不建贴图窗，属正常语义），维持"尽力投递"口径。
//   - manifest requireAdministrator 特判：未提权 spawn 直得 740（不代弹 UAC），
//     文案改写在 Start wrapper 内完成（见 instance.go holdSpawnFail 的收口窗口）；
//     UIPI 同时限制"已运行实例的命令投递/WM_CLOSE"，因此指引统一指向
//     "以管理员身份重启 Hanxi"（前端 ElevateRestart 一键通道按文案关键词显隐）；
//   - 状态词表映射：内核 stopped/starting/running/external/failed/stopping →
//     本包既有 stopped/starting/running/external/failed（stopping 折并入 running，
//     终止窗口对前端保持运行语义，终态由后续广播给出）；
//   - Snapshot 形状映射：内核快照 + 本包推算的 ExitCode/StoppedAt 拼回既有事件
//     契约（前端与 wails 事件载荷零漂移），"已手动停止"折回本引擎的空文案；
//   - 启动前接管上游内置更新检查（config.go）：AirSnip 的 config.toml 落在
//     %APPDATA%\airsnip（**不随 exe 目录**，与 litemonitor 的便携 seed 语义
//     不同），故只能就地改写单键而非"文件不存在才播种"。
//
// 上游 release 构建未嵌 VERSIONINFO 资源（app.rc 仅 ICON + manifest，实测
// VersionInfo.FileVersion 为空串）：已装版本判定完全依赖版本隔离目录令牌，
// 详见 version 包注释。
//
// AirSnip 由内核启动后绑定 Windows Job Object（JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE），
// Hanxi 无论以何种方式退出（托盘退出/崩溃/强杀），内核都会连带终止进程树；
// "不随 Hanxi 关闭"开关（默认关）走 DetachFromJob 解除联动。贴图/覆盖层与
// 主消息窗同属一个进程，无独立子进程需要额外治理。
//
// 本包零框架依赖，便于单元测试。
package instance

import "time"

// AirsnipProbe 实例存活探测（免框架依赖；service 层注入 Windows 实现）。
type AirsnipProbe interface {
	// FindPIDs 返回全部 airsnip.exe 进程 PID（空 = 未运行；含外部实例）。
	FindPIDs() []uint32
	// IsRunning airsnip.exe 进程存在性探测。
	IsRunning() bool
	// WaitForReady 轮询等待进程出现（实例就绪），超时返回 false。
	WaitForReady(timeout time.Duration) bool
}
