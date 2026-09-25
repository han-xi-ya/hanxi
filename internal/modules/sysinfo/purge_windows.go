//go:build windows

package sysinfo

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"hanxi/internal/platform/windows"
	"hanxi/internal/settings"
)

// RunPurgeStandbyHelper 供 cmd/hanxi 一次性提权子命令调用：不装配 GUI、
// 不读写用户数据，只在当前（已提权）进程内完成一次待机列表清理并写回
// 主进程预创建的结果文件。写回严格校验 request ID 与结果路径形状。
func RunPurgeStandbyHelper(resultPath, requestID string, elevated bool) error {
	if resultPath == "" || requestID == "" {
		return errors.New("purge helper: 缺少结果文件或 request ID")
	}
	if !windows.PurgeResultPathAllowed(resultPath, settings.GetPaths().RuntimeDir()) {
		return errors.New("purge helper: 结果路径不在本数据根 runtime 目录或形状不符，拒写")
	}
	// 形状双保险：request ID 必须与文件前缀一致（同 runtime 目录多请求防串写）
	base := filepath.Base(resultPath)
	if !strings.HasPrefix(base, "hanxi-purge-"+requestID) {
		return errors.New("purge helper: 结果路径与 request ID 不匹配")
	}
	// 审查 E1：本函数只在 --elevated=true 的 helper 进程里跑，拒绝态同样
	// 发生在已提升环境——曾恒写 elevated:false 与直连路径账目相反，
	// UI 会"清理被拒+未提权"双误导；elevated 原样透传。
	before, err := availablePhysicalBytes()
	if err != nil {
		return writeDenied(resultPath, requestID, elevated, err)
	}
	out, err := windows.PurgeStandbyList(func() (uint64, error) { return before, nil }, availablePhysicalBytes)
	if err != nil {
		return writeDenied(resultPath, requestID, elevated, err)
	}
	return windows.WritePurgeResult(resultPath, requestID, "success", "清理完成", "", out.BeforeAvailableBytes, out.AfterAvailableBytes, elevated)
}

func writeDenied(path, requestID string, elevated bool, cause error) error {
	if err := windows.WritePurgeResult(path, requestID, "denied", cause.Error(), "privilege-or-action-failed", 0, 0, elevated); err != nil {
		return fmt.Errorf("写 purge 拒绝态失败: %w（原始原因: %v）", err, cause)
	}
	return nil
}

// LaunchPurgeHelperElevated 由普通权限主进程调用：UAC 拉起同一 Hanxi 的一次性
// helper 并把严格校验后的结果带回。runtimeDir 必须是主进程自身已初始化的数据根
// runtime 目录——helper 不能从管理员用户目录推导它。
func LaunchPurgeHelperElevated(runtimeDir, exe, requestID string) (windows.PurgeResultFile, error) {
	if runtimeDir == "" || exe == "" || requestID == "" {
		return windows.PurgeResultFile{}, errors.New("purge helper 参数不完整")
	}
	return windows.RunPurgeHelper(exe, runtimeDir, requestID)
}
