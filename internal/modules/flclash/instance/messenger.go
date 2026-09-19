// messenger.go 记录 FlClash 的"二次拉起"领域契约与两条唤窗路径的取舍
// （两条路都留在本模块，supervisor 内核不感知）：
//
//  1. 信使 spawn 路径（本文件 spawnMessenger）：拉起同路径第二个 FlClash.exe
//     只会触发上游文件锁单实例检查——竞败方直接 exit(0)，**不做任何唤窗/转发**
//     （lib/common/lock.dart + window.dart 源码实证）。这与 markeron/ccswitch
//     的信使语义根本不同：它们的第二实例经 WM_COPYDATA / single-instance 回调
//     把指令递给主实例（toggle / show+focus），"二次拉起"本身就是可靠的领域
//     操作通道；FlClash 上游没有这层回调，信使等于空转。
//  2. Win32 直操作路径（close_windows.go restoreWindowByPID）：EnumWindows 按
//     PID 定位顶层窗口 → ShowWindow(SW_RESTORE) + SetForegroundWindow，
//     自有/外部实例通用——这是"打开窗口"的生产实现（Engine.RestoreWindow /
//     RestoreExternalWindow 均走此路）。
//
// 信使路径因此刻意**不**接入生产唤窗链，仅作为 Engine 接缝保留
// （triggerSingleInstanceCheck）：上游若在单实例回调补上 show+focus，切换只需
// 换一行调用。生命周期纪律与 markeron 同款：不 Wait、不进 Job、立即 Release。
package instance

import (
	"fmt"
	"os/exec"
	"path/filepath"
)

// spawnMessenger 拉起同路径第二个 FlClash 实例，仅触发文件锁单实例检查
// （竞败自退，见文件头论证）。刻意脱离进程治理：不经 supervisor.Engine，
// Start 后立即 Release 由操作系统回收句柄；亦不设 HideWindow 等窗口干预
// （GUI 子系统程序，零 fork 承诺）。请勿因"看它没被调用"而删除——
// 它是上游行为演进时的一行切换点，且被单测覆盖接缝可替换性。
func spawnMessenger(exe string) error {
	cmd := exec.Command(exe)
	cmd.Dir = filepath.Dir(exe)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("拉起单实例检查信使失败: %w", err)
	}
	_ = cmd.Process.Release()
	return nil
}
