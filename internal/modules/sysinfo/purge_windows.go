//go:build windows

package sysinfo

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"hanxi/internal/platform/windows"
)

// RunPurgeStandbyHelper 供 cmd/hanxi 一次性提权子命令调用：不装配 GUI、
// 不读写用户数据，只在当前（已提权）进程内完成一次待机列表清理并写回
// 主进程预创建的结果文件。写回严格校验 request ID 与结果路径形状。
func RunPurgeStandbyHelper(resultPath, requestID string, elevated bool) error {
	if resultPath == "" || requestID == "" {
		return errors.New("purge helper: 缺少结果文件或 request ID")
	}
	base := filepath.Base(resultPath)
	if !strings.HasPrefix(base, "hanxi-purge-"+requestID) || !strings.HasSuffix(base, ".json") {
		return errors.New("purge helper: 结果路径与 request ID 不匹配")
	}
	before, err := availablePhysicalBytes()
	if err != nil {
		return writeDenied(resultPath, requestID, err)
	}
	out, err := windows.PurgeStandbyList(func() (uint64, error) { return before, nil }, availablePhysicalBytes)
	if err != nil {
		return writeDenied(resultPath, requestID, err)
	}
	return windows.WritePurgeResult(resultPath, requestID, "success", "清理完成", "", out.BeforeAvailableBytes, out.AfterAvailableBytes, elevated)
}

func writeDenied(path, requestID string, cause error) error {
	if err := windows.WritePurgeResult(path, requestID, "denied", cause.Error(), "privilege-or-action-failed", 0, 0, false); err != nil {
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
