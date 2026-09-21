package instance

import (
	"fmt"
	"os/exec"
	"path/filepath"
)

// SpawnCommandMessenger 拉起同路径第二实例充当命令信使：Snipaste 是 Qt 单实例
// 应用，二次启动会把命令转发给已在运行的实例后立即自退（官方 CLI 契约，
// docs.snipaste.com/command-line-options："commands will only be effective if
// Snipaste is already running"）——这是官方提供的外部控制通道（W1 调研 §2），
// 对自有/外部实例同样有效，且基础命令免费。
//
// 生命周期纪律同 everything/ccswitch 信使惯例：转发即退，刻意不 Wait、不进
// Job、不设 HideWindow（Snipaste 为 GUI 子系统程序，不产生控制台窗口）；
// 目标以管理员运行时转发会被 UIPI 静默拦截，调用方以状态复探为准、不承诺必达。
func SpawnCommandMessenger(exe, command string) error {
	cmd := exec.Command(exe, command)
	cmd.Dir = filepath.Dir(exe)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("拉起 Snipaste 命令信使失败: %w", err)
	}
	_ = cmd.Process.Release()
	return nil
}
