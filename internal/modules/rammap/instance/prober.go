// prober.go 实例探测契约（形状对齐 windterm 家族——Inspect 单一事实源 +
// 窗口级就绪/聚焦）。
//
// RAMMap 上游契约（2026-09-23 侦查实证，方案 PLAN_N4_RAMMAP §0）：
//   - 纯 Win32 GUI 观察工具：无信使/无 CLI 参数协议，窗口关闭即进程退出，
//     无托盘驻留——唤窗/退出全部走"PID 归属 + 可见带标题顶层窗口"直操作；
//   - 进程名按体系结构二选一（RAMMap64.exe / RAMMap64a.exe，与 version 包
//     payloadExeName 同判据）；无命名互斥体可用（非单实例承诺，二次拉起各开
//     一窗），探测一律进程名枚举；
//   - 载荷 manifest requireAdministrator：提权三重契约（#17）适用——未提权
//     Hanxi spawn 直拒 740，elevateHint 特判文案指向"以管理员身份重启
//     Hanxi"（前端 ElevateRestart 一键通道据"管理员"关键词挂载）；提权态
//     Hanxi 拉起后同完整性等级，Job 绑定/WM_CLOSE/Terminate 全部正常可用；
//   - external 态按 N3 终裁 force-free 档治理：RAMMap 是零状态观察工具
//     （无保存概念、打开即重扫），强杀损失≈0，论证同 everything 口径。
package instance

import (
	"context"
	"runtime"
	"time"

	"hanxi/internal/platform"
)

// payloadImageName 本体系结构的 RAMMap 进程名（与 version.payloadExeName 同
// 判据的镜像实现——instance 与 version 包刻意互不依赖，两处注释同规互锁）。
func payloadImageName() string {
	if runtime.GOARCH == "arm64" {
		return "RAMMap64a.exe"
	}
	return "RAMMap64.exe"
}

// Probe RAMMap 实例探测接口（免框架依赖；service 层注入 Windows 实现，
// 单测以 fake 驱动）。
type Probe interface {
	// Inspect 报告是否存在归属非 ownPID 的 RAMMap 进程及其身份
	// （external 判定与退出令牌来源；ownPID=0 时任何进程都算）。
	// 全部候选 Query 失败时返回 (true, nil, nil)：在场但身份不明——
	// 调用方可判 external 存在，但不得据此强杀（保守回指引）。
	Inspect(ctx context.Context, ownPID uint32) (running bool, info *platform.ProcInfo, err error)
	// WaitForReady 轮询等待出现可见带标题的 RAMMap 顶层窗口，超时返回 false。
	WaitForReady(timeout time.Duration) bool
	// FocusMainWindow 将指定 PID 的可见顶层窗口恢复（最小化）并置于前台，
	// 成功返回 true。pid 为 0 时实现方应直接返回 false。
	FocusMainWindow(pid uint32) bool
	// FocusAnyWindow 唤回任一 RAMMap 进程的可见顶层窗口（外部实例聚焦）。
	FocusAnyWindow() bool
}
