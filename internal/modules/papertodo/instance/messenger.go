package instance

import (
	"os/exec"
	"path/filepath"
)

// spawnMessenger 拉起同路径第二个 PaperTodo 实例充当单实例协议的"信使"：
// WPF SingleInstanceHelper 捕获第二实例后，把其命令行参数（show/hide/exit
// 命令词表，见包注释）经命名管道转发给主实例，信使随即自退。
//
// 信使生命周期刻意脱离进程治理（不经 supervisor.Engine）：
//   - 转发完毕即自退（主实例挂死场会接管成为新主实例，与上游单实例语义一致）——
//     刻意不 Wait（避免阻塞 RPC）、不进 Job（不属于托管生命周期）、
//     Start 后立即 Release 由操作系统回收句柄。
//     此决策理由请勿在后续维护中"好心"改成 Wait，否则唤窗/退出链路将被拖慢；
//   - 同样刻意不设 HideWindow 等窗口干预：PaperTodo 是 GUI 子系统程序，
//     既不产生控制台窗口，且需保留其原版行为（零 fork 承诺）。
//
// 错误原样返回（不加工文案）：调用方按各自操作既有口径包装
// （"拉起窗口信使失败"/"拉起收拢信使失败"）。
func spawnMessenger(exe string, args ...string) error {
	cmd := exec.Command(exe, args...)
	cmd.Dir = filepath.Dir(exe)
	if err := cmd.Start(); err != nil {
		return err
	}
	_ = cmd.Process.Release()
	return nil
}
