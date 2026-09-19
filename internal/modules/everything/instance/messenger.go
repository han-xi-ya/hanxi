package instance

import (
	"fmt"
	"os/exec"
	"path/filepath"
)

// spawnMessenger 拉起同路径第二个 Everything 实例充当单实例协议的"信使"：
// 无参二次启动被主实例的 single-instance 协议捕获并转发，语义即"唤起搜索窗口"
// （主实例显示/前置其窗口，信使自身立即退出）。
//
// 信使生命周期刻意脱离进程治理（不经 supervisor.Engine）：
//   - 转发完毕即自退——刻意不 Wait（避免阻塞 RPC）、不进 Job（不属于托管
//     生命周期）、Start 后立即 Release 由操作系统回收句柄。
//     此决策理由请勿在后续维护中"好心"改成 Wait，否则唤窗链路将被拖慢；
//   - 同样刻意不设 HideWindow 等窗口干预：Everything 是 GUI 子系统程序，
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

// spawnQuitMessenger 拉起 -quit 信使：Everything 官方 CLI 契约，经 IPC 转发给
// 主实例请求退出（先落盘索引库再收口，避免下次冷启动重建全量索引）。
// 与窗口信使同款生命周期纪律：转发后立即退出，刻意不 Wait、不进 Job；
// 主实例挂死/不识别 -quit 时收不到效果，由内核 Quit 的宽限强杀兜底
// （索引库有 Everything 自身的定期落盘保障，强杀代价可接受）。
func spawnQuitMessenger(exe string) error {
	cmd := exec.Command(exe, "-quit")
	cmd.Dir = filepath.Dir(exe)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("拉起退出信使失败: %w", err)
	}
	_ = cmd.Process.Release()
	return nil
}
