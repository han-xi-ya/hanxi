package readiness

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// probeTimeoutSecs 覆盖 CIM/WMI/Appx 读取的余量（网络探测已拆到 Go 侧并行，不再受脚本捆绑）。
const probeTimeoutSecs = 30 * time.Second

// probeScript 是只读环境探针：注册表、WMI、Appx，输出单行紧凑 JSON。
// 全程不写任何系统状态、无需管理员权限。GitHub 通道探测不放进来——
// 改由 Go 原生 net 并行执行（netprobe.go）：省 PS 超时耦合、体检得以分相流式推送。
const probeScript = `$ProgressPreference = 'SilentlyContinue'
$cv = Get-ItemProperty 'HKLM:\SOFTWARE\Microsoft\Windows NT\CurrentVersion'
$cs = Get-CimInstance Win32_ComputerSystem
$cpu = Get-CimInstance Win32_Processor | Select-Object -First 1
$dg = $null
try { $dg = (Get-CimInstance -ClassName Win32_DeviceGuard -Namespace root\Microsoft\Windows\DeviceGuard -ErrorAction Stop).VirtualizationBasedSecurityStatus } catch {}
$store = [bool](Get-AppxPackage *WindowsStore* -ErrorAction SilentlyContinue)
$msix = ''
$p = Get-AppxPackage *WindowsSubsystemForLinux* -ErrorAction SilentlyContinue | Select-Object -First 1
if ($p) { $msix = [string]$p.Version }
$msi = ''
$u = Get-ItemProperty 'HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\*','HKLM:\SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall\*','HKCU:\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall\*' -ErrorAction SilentlyContinue | Where-Object { $_.DisplayName -match 'Windows Subsystem for Linux' } | Select-Object -First 1
if ($u) { $msi = [string]$u.DisplayVersion }
$exeFile = ''
$e = Get-Item "$env:windir\System32\wsl.exe" -ErrorAction SilentlyContinue
if ($e) { $exeFile = (Get-Command $e.FullName).FileVersionInfo.FileVersion }
$msiCode = ''
if ($u) { $msiCode = [string]$u.PSChildName }
# 可选功能开关状态：Win32_OptionalFeature WMI 类免管理员可读，
# InstallState 1=启用 2=禁用（与 DISM 真值逐项对照校准；服务存在性启发已被证伪——
# vmcompute 属 Hyper-V 全套件，家庭版启用虚拟机平台/旧版 WSL 均不产生对应服务）。
# 组件变更待重启官方台账（免管理员可读，重启后自动消失）
$rebootPending = [bool](Test-Path 'HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion\Component Based Servicing\RebootPending')
$vmPlat = $false
$wslFeat = $false
try {
  $of = Get-CimInstance -ClassName Win32_OptionalFeature -Filter "Name='VirtualMachinePlatform' or Name='Microsoft-Windows-Subsystem-Linux'" -ErrorAction Stop
  foreach ($f in $of) {
    if ($f.InstallState -eq 1) {
      if ($f.Name -eq 'VirtualMachinePlatform') { $vmPlat = $true } else { $wslFeat = $true }
    }
  }
} catch {}
[ordered]@{
  osBuild    = [int]$cv.CurrentBuild
  osUBR      = [int]$cv.UBR
  releaseId  = [string]$cv.ReleaseID
  arch       = $env:PROCESSOR_ARCHITECTURE
  hypervisor = [bool]$cs.HypervisorPresent
  vtFirmware = $cpu.VirtualizationFirmwareEnabled
  vbs        = $dg
  store      = $store
  wslMsix    = $msix
  wslMsi     = $msi
  wslMsiCode = $msiCode
  wslExeFile = $exeFile
  vmPlatform = $vmPlat
  wslFeature = $wslFeat
  rebootPending = $rebootPending
} | ConvertTo-Json -Compress`

// Probe 在隐藏窗口的 PowerShell 中执行只读探针并解析 JSON。
func Probe(ctx context.Context) (ProbeResult, error) {
	ctx, cancel := context.WithTimeout(ctx, probeTimeoutSecs)
	defer cancel()

	cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", probeScript)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.Output()
	if err != nil {
		return ProbeResult{}, fmt.Errorf("执行 WSL 就绪探针失败: %w", err)
	}
	var p ProbeResult
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(out))), &p); err != nil {
		return ProbeResult{}, fmt.Errorf("解析 WSL 就绪探针输出失败: %w", err)
	}
	return p, nil
}

// netStatus 把探针回传的通道状态（JSON number 或 "net-fail"）归一为字符串。
func netStatus(v any) string {
	switch t := v.(type) {
	case nil:
		return "未知"
	case float64:
		return fmt.Sprintf("%d", int(t))
	case string:
		return t
	default:
		return fmt.Sprint(t)
	}
}
