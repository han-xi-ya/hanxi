package instance

import (
	"fmt"
	"os/exec"
	"path/filepath"
)

// spawnMessenger 拉起同路径第二个 MarkerOn 实例充当单实例协议的"信使"，
// 经 WM_COPYDATA 转发由主实例执行 toggle_drawing（Hidden↔Drawing）。
//
// 信使生命周期刻意脱离进程治理（不经 supervisor.Engine）：
//   - 信使进程在健康主实例场 5s 内自退、挂死主实例场 8s 后接管——
//     刻意不 Wait（避免阻塞 RPC 最长 8s）、不进 Job（不属于托管生命周期）、
//     Start 后立即 Release 由操作系统回收句柄。
//     此决策理由请勿在后续维护中"好心"改成 Wait，否则冷启动链路将被拖慢；
//   - 同样刻意不设 HideWindow 等窗口干预：MarkerOn 是 GUI 子系统程序，
//     既不产生控制台窗口，且需保留其原版行为（零 fork 承诺）。
func spawnMessenger(exe string) error {
	cmd := exec.Command(exe)
	cmd.Dir = filepath.Dir(exe)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("拉起切换信使失败: %w", err)
	}
	_ = cmd.Process.Release()
	return nil
}
