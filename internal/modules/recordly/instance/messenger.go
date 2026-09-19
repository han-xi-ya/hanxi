package instance

import (
	"fmt"
	"os/exec"
	"path/filepath"
)

// spawnMessenger 拉起"信使"二次进程：Recordly（Electron requestSingleInstanceLock，
// electron/main.ts 实证）检测单实例锁被持有后向主实例投递 second-instance 事件，
// 主实例 restoreWindowSafely（show + moveTop + focus）唤起窗口，信使自身
// app.quit() 即退——二次拉起不产生新实例，Recordly 唯一启动语义即开窗。
//
// 信使生命周期刻意脱离进程治理（不经 supervisor.Engine，markeron/ccswitch 同族纪律）：
//   - 转发完毕即自退——刻意不 Wait（避免阻塞 RPC）、不进 Job（不属于托管生命周期）、
//     Start 后立即 Release 由操作系统回收句柄。
//     此决策理由请勿在后续维护中"好心"改成 Wait，否则唤窗链路将被拖慢
//     （markeron 先例：改了就拖慢冷启动）；
//   - 同样刻意不设 HideWindow 等窗口干预：Recordly 是 GUI 子系统程序（Electron），
//     既不产生控制台窗口，且需保留其原版行为（零 fork 承诺）；
//   - 信使不注入 RECORDLY_DISABLE_AUTO_UPDATES：它不进入主流程（拿不到锁即退），
//     托管主实例的更新抑制由 service 层 Start 注入承担。
func spawnMessenger(exe string) error {
	cmd := exec.Command(exe)
	cmd.Dir = filepath.Dir(exe)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("拉起窗口信使失败: %w", err)
	}
	_ = cmd.Process.Release()
	return nil
}
