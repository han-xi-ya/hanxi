package instance

import (
	"fmt"
	"os/exec"
	"path/filepath"
)

// spawnMessenger 拉起同路径第二个 VS Code 实例充当单实例协议的"信使"：
// Electron requestSingleInstanceLock 捕获第二实例后，把首次启动参数转发给
// 主实例并触发其唤/开窗口——这就是"打开窗口"的可靠实现（markeron/ccswitch 先例）。
// 信使归属哪个实例组由其 user-data（便携版 data\ / 安装版 %APPDATA%\Code）决定，
// 与具体版本号无关。
//
// 信使生命周期刻意脱离进程治理（不经 supervisor.Engine）：
//   - 信使转发完毕即自退（主实例挂死场会接管成为新主实例，与上游单实例语义一致）——
//     刻意不 Wait（避免阻塞 RPC）、不进 Job（不属于托管生命周期）、
//     Start 后立即 Release 由操作系统回收句柄。
//     此决策理由请勿在后续维护中"好心"改成 Wait，否则唤窗链路将被拖慢；
//   - 同样刻意不设 HideWindow 等窗口干预：VS Code 是 GUI 子系统程序，
//     既不产生控制台窗口，且需保留其原版行为（零 fork 承诺）。
func spawnMessenger(exe string) error {
	cmd := exec.Command(exe)
	cmd.Dir = filepath.Dir(exe) // 工作目录锁定 exe 目录（便携版 data\ 按 exe 位置解析）
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("拉起窗口信使失败: %w", err)
	}
	_ = cmd.Process.Release()
	return nil
}
