package instance

import (
	"fmt"
	"os/exec"
	"path/filepath"
)

// spawnMessenger 拉起同路径第二个 TranslucentTB 实例充当单实例协议的"状态信使"：
// 上游检测互斥体被持有后，向主实例托盘消息窗口投递 TTB_NewInstanceStarted，
// 主实例即重放当前配置到任务栏（等价托盘菜单 "Reset dynamic state"）并弹
// "已在运行"气泡——信使转发完毕即自退，不产生任何窗口。
//
// 信使生命周期刻意脱离进程治理（不经 supervisor.Engine）：
//   - 刻意不 Wait（避免阻塞 RPC）、不进 Job（不属于托管生命周期）、
//     Start 后立即 Release 由操作系统回收句柄。
//     此决策理由请勿在后续维护中"好心"改成 Wait，否则重设链路将被拖慢；
//   - 同样刻意不设窗口干预：TranslucentTB 是 GUI 子系统程序（零 fork 承诺）；
//   - 主实例挂死场信使会接管成为新主实例，与上游单实例语义一致（与 ccswitch
//     唤窗信使同款兜底行为）。
func spawnMessenger(exe string) error {
	cmd := exec.Command(exe)
	cmd.Dir = filepath.Dir(exe)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("拉起状态信使失败: %w", err)
	}
	_ = cmd.Process.Release()
	return nil
}
