package wsl

// 正规卸载与可选功能还原：MSI 系统版交官方卸载器自收编、MSIX 用户包走
// Remove-AppxPackage，Lxss 历史登记只巡查报告不暗中删（红线：不写删注册表）。

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"hanxi/internal/modules/wsl/readiness"
)

// msiGuidRe MSI ProductCode 键名形态：{8-4-4-4-12}。
var msiGuidRe = regexp.MustCompile(`^\{[0-9A-Fa-f]{8}(-[0-9A-Fa-f]{4}){3}-[0-9A-Fa-f]{12}\}$`)

// 卸载与残留巡查的固定脚本（只读查询 + 正规 Remove-AppxPackage；绝不写删注册表）。
// UninstallWsl 用 uninstallMSIXScript，msiProductCode 的补查用 msiCodeScript。
const (
	// msiCodeScript 巡查脚本在 readiness.MsiWslLookupPS（探针同一份）之后取 ProductCode。
	msiCodeScript = readiness.MsiWslLookupPS + `
if ($u) { [string]$u.PSChildName }`

	uninstallMSIXScript = `$ProgressPreference = 'SilentlyContinue'
$p = Get-AppxPackage *WindowsSubsystemForLinux* -ErrorAction SilentlyContinue
if ($p) { try { $p | Remove-AppxPackage -ErrorAction Stop; $r = 'removed' } catch { $r = 'failed' } } else { $r = 'none' }
$n = @(Get-ChildItem 'HKCU:\SOFTWARE\Microsoft\Windows\CurrentVersion\Lxss' -ErrorAction SilentlyContinue).Count
"$r|$n"`
)

// ---- 正规卸载与组件还原 ----

// UninstallWsl 双形态正规卸载：MSI 系统版走 msiexec /X ProductCode（提权、官方卸载器
// 自收编其注册表登记），MSIX 用户包走 Remove-AppxPackage（当前用户、无需提权）。
// 边界如实申明：不强删注册表、不触碰可选功能（另见 DisableWslFeatures）；
// Lxss 历史发行版注册键属正规卸载器不管辖的残留，巡查后如实报告而非暗中删除。
func (s *WslService) UninstallWsl() (OperationOutcome, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	var steps []string

	// 1) MSI 系统版：优先复用探针缓存的 ProductCode，缺失时只读补查一次。
	if code := s.msiProductCode(ctx); code != "" {
		out, err := s.elevProc(ctx, "MsiExec.exe", "/X"+code, "/qb")
		if err != nil {
			return OperationOutcome{}, fmt.Errorf("MSI 系统版卸载失败: %w", err)
		}
		if !out.Success {
			return out, nil // UAC 取消：整体中止，如实回执
		}
		steps = append(steps, "MSI 系统版已通过官方卸载器移除")
	} else {
		steps = append(steps, "未检出 MSI 系统版（跳过）")
	}

	// 2) MSIX 用户包 + Lxss 残留巡查（同一脚本一次执行）。
	outStr, err := s.localPS(ctx, uninstallMSIXScript)
	if err != nil {
		return OperationOutcome{}, fmt.Errorf("MSIX 用户包清理执行失败: %w", err)
	}
	verdict, lxssCount := parseUninstallReport(outStr)
	msixOK := true
	switch verdict {
	case "removed":
		steps = append(steps, "MSIX 用户包已移除")
	case "none":
		steps = append(steps, "未检出 MSIX 用户包（跳过）")
		msixOK = true
	default:
		steps = append(steps, "MSIX 包移除失败（可在「设置→应用」重试）")
		msixOK = false
	}
	if lxssCount > 0 {
		steps = append(steps, fmt.Sprintf("检测到 %d 个历史发行版注册残留（HKCU\\…\\Lxss 键，正规卸载器不负责回收，无害；介意可手动删除）", lxssCount))
	} else {
		steps = append(steps, "Lxss 发行版注册无残留")
	}
	steps = append(steps, "可选功能（虚拟机平台/WSL）未触碰——如需彻底还原系统，可执行「关闭虚拟机平台组件」")

	return OperationOutcome{
		Success: msixOK,
		Message: strings.Join(steps, " · "),
	}, nil
}

// EnableWslFeatures 提权经 DISM 启用虚拟机平台与旧版 WSL 两个可选功能
// （与「关闭」对称的状态还原通道；「🚀 一键开启」会在装 WSL 时顺带完成同样动作）。
// 两条 dism 之间显式传播 $LASTEXITCODE：`;` 链的退出码只反映最后一条命令，
// 曾因"第一条失败、第二条成功"被整体误报为成功（实机 UAC 窗口一闪而过的漏报事故）。
func (s *WslService) EnableWslFeatures() (OperationOutcome, error) {
	inner := "dism /online /enable-feature /featurename:VirtualMachinePlatform /norestart; if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }; " +
		"dism /online /enable-feature /featurename:Microsoft-Windows-Subsystem-Linux /norestart; exit $LASTEXITCODE"
	out, err := s.elevProc(context.Background(), "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", inner)
	if err != nil {
		return OperationOutcome{}, fmt.Errorf("启用虚拟机平台组件失败: %w", err)
	}
	if out.Success {
		out.Message = "可选功能启用指令已执行完毕，重启后生效（重启前发行版无法拉起）"
	}
	return out, nil
}

// DisableWslFeatures 提权经 DISM 关闭两个可选功能（虚拟机平台 + WSL 旧功能开关），
// 这是"把 Windows 自身被 WSL 改变的开关还原回去"的正道；/norestart 交由用户择机重启。
func (s *WslService) DisableWslFeatures() (OperationOutcome, error) {
	inner := "dism /online /disable-feature /featurename:VirtualMachinePlatform /norestart; if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }; " +
		"dism /online /disable-feature /featurename:Microsoft-Windows-Subsystem-Linux /norestart; exit $LASTEXITCODE"
	out, err := s.elevProc(context.Background(), "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", inner)
	if err != nil {
		return OperationOutcome{}, fmt.Errorf("关闭虚拟机平台组件失败: %w", err)
	}
	if out.Success {
		out.Message = "可选功能关闭指令已执行完毕，重启后生效（点「🔁 稍后重启」直达系统电源设置）"
	}
	return out, nil
}

// msiProductCode 优先取探针缓存（体检顺带采集），缺失时只读补查注册表。
func (s *WslService) msiProductCode(ctx context.Context) string {
	s.mu.Lock()
	cached := ""
	if s.lastProbe != nil {
		cached = s.lastProbe.WslMSICode
	}
	s.mu.Unlock()
	if msiGuidRe.MatchString(cached) {
		return cached
	}
	out, err := s.localPS(ctx, msiCodeScript)
	code := strings.TrimSpace(out)
	if err != nil || !msiGuidRe.MatchString(code) {
		return ""
	}
	return code
}

// parseUninstallReport 解析 "removed|3" 形态脚本输出；畸形输出按 failed/-1 保守处理。
func parseUninstallReport(s string) (verdict string, lxssCount int) {
	verdict, countPart, _ := strings.Cut(strings.TrimSpace(s), "|")
	if _, err := fmt.Sscanf(strings.TrimSpace(countPart), "%d", &lxssCount); err != nil {
		lxssCount = -1
	}
	switch verdict {
	case "removed", "none", "failed":
		return verdict, lxssCount
	default:
		return "failed", lxssCount
	}
}
