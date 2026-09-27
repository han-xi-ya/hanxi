package instance

// 打包实例归属收编与外部强制结束（P1-8 + 机主裁决"即使判 external 也要允许
// 强制退出"，2026-09-27）：
//
// 病根——supervisor 内核的 OwnManaged 只认自家进程句柄，MSIX 打包版经
// Add-AppxPackage 部署后由激活通道（shell:AppsFolder）拉起、永不入 JobObject，
// 句柄恒缺席；旧探针又只回互斥体布尔（"有实例活着"），既给不出身份也无从
// 判别来源——凡非本页 spawn 的实例一律落 external，"外部用户自启"的话术与
// "不可结束"的义务被一起强加给经 hanxi 部署/唤起的打包实例。
//
// 修复口径分两层：
//   - 归属判定改用独立事实源（镜像路径 + 进程身份），不再依赖句柄形态：
//     探针 FindInstances 枚举带 PID/全路径/启动时刻的实例，镜像落在
//     <…>\WindowsApps\<包名前缀>_…\TranslucentTB.exe 者判为 hanxi 打包线
//     部署资产，快照收编显示托管（running），退出走既有三段链（优雅 WM_CLOSE
//     + 宽限 + 强杀兜底）；
//   - 真 external（便携双击/自启等）保留 external，但 service 层提供
//     ForceQuitExternal 强制结束通道（身份复核后 OpenProcess+TerminateProcess，
//     被系统权限拒绝如实报错不吞）。

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"hanxi/internal/platform"
	sup "hanxi/packages/go/supervisor"
)

// adoptedPollInterval 收编/终结链等待实例落位的观察间隔（内核 wait 句柄不可用，
// 互斥体释放是唯一跨进程可靠事实）。包级变量仅为单测压缩等待，生产值 100ms。
var adoptedPollInterval = 100 * time.Millisecond

// SetProcessAPI 接线系统进程通道：打包收编实例的强杀兜底与真外部实例的强制
// 终结都经 platform.ProcessAPI.KillVerified 的身份复核链（PID+镜像路径+启动
// 时刻，PID 复用即拒杀）；同时启用内核侧的启动身份令牌。
func (e *Engine) SetProcessAPI(api platform.ProcessAPI) {
	e.procAPI = api
	e.sup.WithProcessAPI(api)
}

// SetPackagedPackagePrefix 接线打包实例归属判据的输入：包全名前缀
// （service 层传 MsixPackageName+"_"）。空串 = 判据停用，external 语义回旧口径。
func (e *Engine) SetPackagedPackagePrefix(prefix string) {
	e.packagedPrefix = strings.TrimSpace(prefix)
}

// SetPackagedMessenger 接线打包实例的"信使"通道：WindowsApps 镜像受包目录
// ACL 限制不可被非打包进程直接 spawn，再激活（shell:AppsFolder 派发）即上游
// 单实例协议的信使等价物。nil = 退回直接 spawn（注定 access denied，如实报错）。
func (e *Engine) SetPackagedMessenger(fn func(exe string) error) {
	e.packagedMessenger = fn
}

// isPackagedImage 当前接线下的打包实例判据（前缀未接线恒假——宁缺毋滥）。
func (e *Engine) isPackagedImage(exePath string) bool {
	return isPackagedInstanceImage(exePath, e.packagedPrefix)
}

// isPackagedInstanceImage 纯判据：镜像路径落 <…>\WindowsApps\<包名前缀>… 且
// 基名 = TranslucentTB.exe（大小写不敏感）。三锚缺一不可，仅进程名不算、
// 仅"在跑"也不算——真外部便携实例同名同互斥体，必须出局。
func isPackagedInstanceImage(exePath, prefix string) bool {
	if exePath == "" || prefix == "" {
		return false
	}
	lower := strings.ToLower(exePath)
	if !strings.Contains(lower, `\windowsapps\`+strings.ToLower(prefix)) {
		return false
	}
	return strings.EqualFold(filepath.Base(exePath), exeName)
}

// adoptedInstance 当前内核快照处于"打包收编实例"态（external + 判据命中 +
// 探针交回 PID）时交回其进程身份；否则 false（真外部/自有/静止态）。
func (e *Engine) adoptedInstance() (platform.ProcInfo, bool) {
	s := e.sup.Snapshot()
	if s.State != sup.StateExternal || s.PID == 0 || !e.isPackagedImage(s.Exe) {
		return platform.ProcInfo{}, false
	}
	return platform.ProcInfo{PID: s.PID, Name: exeName, ExePath: s.Exe, StartedAt: s.Since}, true
}

// Adopted 报告当前快照是否呈"打包收编实例当托管"态（service 指引文案取词用）。
func (e *Engine) Adopted() bool {
	_, ok := e.adoptedInstance()
	return ok
}

// quitAdopted 打包收编实例的三段退出链（对齐托管 Quit 语义）：
//  1. 优雅：postClose 向托盘消息窗口投 WM_CLOSE（上游 Exit() 保存 settings.json
//     后自退；属主校验链与便携同款，见 close_windows.go）；
//  2. 宽限：closeGracePeriod 内观察互斥体释放（无自有句柄可 Wait）；
//  3. 兜底：KillVerified 身份复核后 TerminateProcess——被系统权限拒绝如实
//     报错（打包实例不在 JobObject 内，终结受系统权限约束），绝不假装已退。
func (e *Engine) quitAdopted(info platform.ProcInfo) error {
	postClose()
	if e.waitGone(closeGracePeriod) {
		e.RefreshExternal()
		return nil
	}
	if e.procAPI == nil {
		return errors.New("优雅退出超时且系统进程通道未接线，无法结束打包实例")
	}
	token := platform.VerifyToken{PID: info.PID, ExePath: info.ExePath, StartedAt: info.StartedAt}
	if err := e.procAPI.KillVerified(context.Background(), token, true); err != nil &&
		!errors.Is(err, platform.ErrProcessNotFound) {
		if errors.Is(err, platform.ErrAccessDenied) {
			return fmt.Errorf("优雅退出超时且结束被系统拒绝（access denied）：打包实例不在 JobObject 内，结束受系统权限约束——可在其托盘菜单手动退出后重试: %w", err)
		}
		return fmt.Errorf("优雅退出超时，强制结束亦失败: %w", err)
	}
	if e.waitGone(closeGracePeriod) {
		e.RefreshExternal()
		return nil
	}
	return errors.New("已向打包实例发出终止信号但其仍在场，稍后重试退出")
}

// waitGone 在窗口内轮询"互斥体释放 = 主实例落位"；到期前最后一次复核防边界竞态。
func (e *Engine) waitGone(within time.Duration) bool {
	deadline := time.Now().Add(within)
	for {
		if !e.probe.IsRunning() {
			return true
		}
		if !time.Now().Before(deadline) {
			return !e.probe.IsRunning()
		}
		time.Sleep(adoptedPollInterval)
	}
}

// ExternalQuitReport 强制结束外部实例的计数字（成败/被拒分列，不吞任何一支）。
type ExternalQuitReport struct {
	Killed int // 已结束（复核时进程本就已消失也算达成目标态）
	Denied int // OpenProcess 被系统权限拒绝（UIPI/提权边界——能否结束由系统权限决定）
}

// ForceQuitExternal 真外部判定实例的强制结束执行面（机主裁决通道）：按探针
// 枚举逐个 KillVerified（身份复核防 PID 复用误杀）。三类排除——hanxi 自有 Job
// 托管进程（归 JobObject 管辖）、打包收编实例（归 Quit 退出链）、非
// TranslucentTB 映像（探针按名枚举天然出局）。access denied 如实计数不吞；
// 其余失败并入返回 error；收尾刷新一次静止态校正。
func (e *Engine) ForceQuitExternal() (ExternalQuitReport, error) {
	var rep ExternalQuitReport
	if e.procAPI == nil {
		return rep, errors.New("系统进程通道未接线，无法执行强制结束")
	}
	own := e.sup.Snapshot()
	var errs []error
	for _, in := range e.probe.FindInstances() {
		if in.PID == 0 {
			continue
		}
		if (own.State == sup.StateRunning || own.State == sup.StateStarting) && in.PID == own.PID {
			continue // 自有托管进程：句柄与 Job 在手，绝不按"外部"名义误杀
		}
		if e.isPackagedImage(in.ExePath) {
			continue // 打包收编实例：归"退出"三段链管辖，不在强制结束射程
		}
		token := platform.VerifyToken{PID: in.PID, ExePath: in.ExePath, StartedAt: in.StartedAt}
		err := e.procAPI.KillVerified(context.Background(), token, true)
		switch {
		case err == nil || errors.Is(err, platform.ErrProcessNotFound):
			rep.Killed++
		case errors.Is(err, platform.ErrAccessDenied):
			rep.Denied++
		default:
			errs = append(errs, fmt.Errorf("结束实例 PID %d 失败: %w", in.PID, err))
		}
	}
	e.RefreshExternal()
	return rep, errors.Join(errs...)
}
