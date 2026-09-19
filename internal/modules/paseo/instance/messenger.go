package instance

import (
	"fmt"
	"os/exec"
	"path/filepath"
)

// spawnMessenger 拉起"信使"二次进程：Paseo 的 second-instance 回调是
// openAdditional（新开窗口而非聚焦，main.ts 实证）——信使仅作无可见窗口时的
// 兜底开新窗通道（有窗唤窗一律走 FocusWindow 直唤）。
//
// 信使生命周期刻意脱离进程治理（不经 supervisor.Engine）：
//   - 信使转发完毕即自退（主实例挂死场会接管成为新主实例，与上游单实例语义
//     一致）——刻意不 Wait（避免阻塞 RPC）、不进 Job（不属于托管生命周期）、
//     Start 后立即 Release 由操作系统回收句柄。
//     此决策理由请勿在后续维护中"好心"改成 Wait，否则唤窗链路将被拖慢
//     （markeron 先例：改了就拖慢冷启动）；
//   - 同样刻意不设 HideWindow 等窗口干预：Paseo 是 GUI 子系统程序（Electron），
//     既不产生控制台窗口，且需保留其原版行为（零 fork 承诺）。
func spawnMessenger(exe string) error {
	cmd := exec.Command(exe)
	cmd.Dir = filepath.Dir(exe)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("拉起窗口信使失败: %w", err)
	}
	_ = cmd.Process.Release()
	return nil
}
