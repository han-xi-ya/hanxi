package instance

import (
	"fmt"
	"os/exec"
	"path/filepath"
)

// spawnMessenger 拉起同路径第二个 CC Switch 实例充当单实例协议的"信使"：
// tauri-plugin-single-instance 捕获第二实例后，经 WM_COPYDATA 把首次启动参数
// 转发给主实例，其回调无条件 show+focus 主窗口——这就是"打开窗口"的可靠实现。
//
// 信使生命周期刻意脱离进程治理（不经 supervisor.Engine）：
//   - 信使转发完毕即自退（主实例挂死场会接管成为新主实例，与上游单实例语义一致）——
//     刻意不 Wait（避免阻塞 RPC）、不进 Job（不属于托管生命周期）、
//     Start 后立即 Release 由操作系统回收句柄。
//     此决策理由请勿在后续维护中"好心"改成 Wait，否则唤窗链路将被拖慢；
//   - 同样刻意不设 HideWindow 等窗口干预：CC Switch 是 GUI 子系统程序，
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
