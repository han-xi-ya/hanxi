package instance

import (
	"fmt"
	"os/exec"
	"path/filepath"
)

// spawnMessenger 拉起同路径第二个 BCU 实例充当单实例协议的"信使"：
// 第二实例发现 Global\BCU-singleinstance 被持有后查找主进程并
// SetForegroundWindow(MainWindowHandle) 唤起主窗口（主实例无窗口时才弹错误框）
// ——这就是"打开窗口"的可靠实现（EntryPoint.cs 源码实证，见 prober.go 包注释）。
//
// 信使生命周期刻意脱离进程治理（不经 supervisor.Engine）：
//   - 信使唤起主窗口即自退（上游 ~20ms 级短命语义，正是"外层 bootstrapper
//     秒退不可当锚点"的同款形态）——刻意不 Wait（避免阻塞 RPC）、不进 Job
//     （不属于托管生命周期），Start 后立即 Release 由操作系统回收句柄。
//     此决策理由请勿在后续维护中"好心"改成 Wait，否则唤窗链路将被拖慢；
//   - 同样刻意不设 HideWindow 等窗口干预：BCU 是 GUI 子系统程序，
//     既不产生控制台窗口，且需保留其原版行为（零 fork 承诺）。
func spawnMessenger(exe string) error {
	cmd := exec.Command(exe)
	cmd.Dir = filepath.Dir(exe) // 与托管启动同口径：工作目录锁定 exe 所在目录
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("拉起窗口信使失败: %w", err)
	}
	_ = cmd.Process.Release()
	return nil
}
