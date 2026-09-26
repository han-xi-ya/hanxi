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

// validateHelperHandshake 两个一次性 helper 入口共用的握手红线：
// request ID 必填、结果路径必须落在本数据根 runtime 目录（PurgeResultPathAllowed，
// 审查 P1#6）、文件名前缀必须与 request ID 一致（同 runtime 目录多请求防串写）。
func validateHelperHandshake(resultPath, requestID string) error {
	if resultPath == "" || requestID == "" {
		return errors.New("helper: 缺少结果文件或 request ID")
	}
	if !windows.PurgeResultPathAllowed(resultPath, settings.GetPaths().RuntimeDir()) {
		return errors.New("helper: 结果路径不在本数据根 runtime 目录或形状不符，拒写")
	}
	base := filepath.Base(resultPath)
	if !strings.HasPrefix(base, "hanxi-purge-"+requestID) {
		return errors.New("helper: 结果路径与 request ID 不匹配")
	}
	return nil
}

// RunPurgeStandbyHelper 供 cmd/hanxi 一次性提权子命令调用（mode=purge-standby）：
// 不装配 GUI、不读写用户数据，只在当前（已提权）进程内完成一次待机列表清理并
// 写回主进程预创建的结果文件（含前后逐项链账）。
//
// 审查 E1：本函数只在 --elevated=true 的 helper 进程里跑，拒绝态同样发生在已提升
// 环境——曾恒写 elevated:false 与直连路径账目相反，UI 会"清理被拒+未提权"双误导；
// elevated 原样透传。
func RunPurgeStandbyHelper(resultPath, requestID string, elevated bool) error {
	if err := validateHelperHandshake(resultPath, requestID); err != nil {
		return err
	}
	out, err := windows.PurgeStandbyList(windows.CaptureMemoryLedger)
	if err != nil {
		return writeDenied(resultPath, requestID, windows.PurgeHelperMode, elevated, out.Before, err)
	}
	payload := successPayload(resultPath, requestID, windows.PurgeHelperMode, elevated, out.Before, out.After)
	return windows.WritePurgeResult(resultPath, payload)
}

// RunEmptyWorkingSetsHelper 供 cmd/hanxi 一次性提权子命令调用
// （mode=empty-workingsets）：遍历进程清空工作集（RAMMap "Empty Working Sets"
// 同款谨慎语义——页被压回待机而非释放），回执含成功/跳过计数与前后账目。
func RunEmptyWorkingSetsHelper(resultPath, requestID string, elevated bool) error {
	if err := validateHelperHandshake(resultPath, requestID); err != nil {
		return err
	}
	out, err := windows.EmptyWorkingSets(windows.CaptureMemoryLedger)
	if err != nil {
		return writeDenied(resultPath, requestID, windows.EmptyWorkingSetsHelperMode, elevated, out.Before, err)
	}
	payload := successPayload(resultPath, requestID, windows.EmptyWorkingSetsHelperMode, elevated, out.Before, out.After)
	payload.EmptiedProcessCount = out.Emptied
	payload.SkippedProcessCount = out.Skipped
	return windows.WritePurgeResult(resultPath, payload)
}

func successPayload(resultPath, requestID, mode string, elevated bool, before, after windows.MemoryLedger) windows.PurgeResultFile {
	return windows.PurgeResultFile{
		RequestID:            requestID,
		Mode:                 mode,
		State:                "success",
		Message:              "清理完成",
		BeforeAvailableBytes: before.AvailableBytes,
		AfterAvailableBytes:  after.AvailableBytes,
		BeforeLedger:         before,
		AfterLedger:          after,
		Elevated:             elevated,
	}
}

func writeDenied(path, requestID, mode string, elevated bool, before windows.MemoryLedger, cause error) error {
	payload := windows.PurgeResultFile{
		RequestID:            requestID,
		Mode:                 mode,
		State:                "denied",
		Message:              cause.Error(),
		ErrorCode:            "privilege-or-action-failed",
		BeforeAvailableBytes: before.AvailableBytes,
		BeforeLedger:         before,
		Elevated:             elevated,
	}
	if err := windows.WritePurgeResult(path, payload); err != nil {
		return fmt.Errorf("写 helper 拒绝态失败: %w（原始原因: %v）", err, cause)
	}
	return nil
}

// LaunchPurgeHelperElevated 由普通权限主进程调用：UAC 拉起同一 Hanxi 的一次性
// 待机清理 helper 并把严格校验后的结果带回。runtimeDir 必须是主进程自身已初始化
// 的数据根 runtime 目录——helper 不能从管理员用户目录推导它。
func LaunchPurgeHelperElevated(runtimeDir, exe, requestID string) (windows.PurgeResultFile, error) {
	if runtimeDir == "" || exe == "" || requestID == "" {
		return windows.PurgeResultFile{}, errors.New("purge helper 参数不完整")
	}
	return windows.RunPurgeHelper(exe, runtimeDir, requestID, windows.PurgeHelperMode)
}

// LaunchEmptyWorkingSetsHelperElevated 同 LaunchPurgeHelperElevated，走
// empty-workingsets 模式：握手协议、结果路径白名单与单飞约束两模式共用。
func LaunchEmptyWorkingSetsHelperElevated(runtimeDir, exe, requestID string) (windows.PurgeResultFile, error) {
	if runtimeDir == "" || exe == "" || requestID == "" {
		return windows.PurgeResultFile{}, errors.New("working-sets helper 参数不完整")
	}
	return windows.RunPurgeHelper(exe, runtimeDir, requestID, windows.EmptyWorkingSetsHelperMode)
}
