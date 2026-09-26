//go:build windows

package windows

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	PurgeHelperMode            = "purge-standby"
	EmptyWorkingSetsHelperMode = "empty-workingsets"
	purgeHelperTimeout         = 90 * time.Second
)

// RunPurgeHelper 以 UAC 拉起同一个 Hanxi 的一次性内存 helper，等待其写回
// 严格校验的结果文件。helper 不进入 GUI/Wails 生命周期；结果文件读取后立即
// 删除。mode 只接受 PurgeHelperMode / EmptyWorkingSetsHelperMode 两值（两模式
// 共用同一握手与结果路径白名单；调用方持锁保证同一时刻至多一个 helper 在飞）。
func RunPurgeHelper(exe, runtimeDir, requestID, mode string) (PurgeResultFile, error) {
	if mode != PurgeHelperMode && mode != EmptyWorkingSetsHelperMode {
		return PurgeResultFile{}, fmt.Errorf("未知 helper mode %q", mode)
	}
	resultPath, err := NewPurgeRequestFile(runtimeDir, requestID)
	if err != nil {
		return PurgeResultFile{}, err
	}
	defer os.Remove(resultPath)
	CleanupStalePurgeResults(runtimeDir, time.Now())

	args := []string{
		"--mode=" + mode,
		"--result=" + resultPath,
		"--request-id=" + requestID,
		"--elevated=true",
	}
	quoted := make([]string, 0, len(args))
	for _, arg := range args {
		quoted = append(quoted, PsQuote(arg))
	}
	script := fmt.Sprintf("$p = Start-Process -FilePath %s -ArgumentList %s -Verb RunAs -Wait -WindowStyle Hidden -PassThru; if ($null -eq $p) { exit 1 }; exit [int]$p.ExitCode", PsQuote(exe), strings.Join(quoted, ","))
	// 审查 P1#4：WaitDelay 无 Context 时只约束"进程退出后的 I/O 收尾"，
	// 防不住"Start-Process -Wait 卡在无人应答的 UAC 上"——必须 CommandContext
	// + WithTimeout，到点真杀 powershell，purgeMu 才有封顶可言。
	ctx, cancel := context.WithTimeout(context.Background(), purgeHelperTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", script)
	HideConsole(cmd)
	cmd.WaitDelay = 5 * time.Second
	out, err := cmd.CombinedOutput()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return PurgeResultFile{}, fmt.Errorf("purge helper 超时（%s，含 UAC 等待），已终止", purgeHelperTimeout)
		}
		if isUACCancelled(string(out)) {
			return PurgeResultFile{State: "cancelled", Message: "已取消 UAC 授权，清理未执行"}, nil
		}
		// 审查 P1#5：helper 失败只有 exit 1（denied 走结果文件），曾据的
		// exit 2 协议位从无生成方且 Go panic 退 2 会谎报"权限未分配"——
		// 死分支删除，未知退出码统一归启动失败附原始输出。
		code := ElevatedRunExitCode(err)
		return PurgeResultFile{}, fmt.Errorf("purge helper 启动失败（退出码 %d）: %w %s", code, err, strings.TrimSpace(string(out)))
	}
	result, err := ReadPurgeResult(resultPath, requestID)
	if err != nil {
		return PurgeResultFile{}, fmt.Errorf("purge helper 未返回有效结果: %w", err)
	}
	return result, nil
}

// PurgeResultPathAllowed 钉死结果文件必须位于主进程 runtime 目录且形状合规
// （审查 P1#6：只验形状不验目录=提权 helper 可被诱导往任意可写目录写合规
// JSON）。由 sysinfo.RunPurgeStandbyHelper 以自身解析的 runtime 目录接线。
func PurgeResultPathAllowed(path, runtimeDir string) bool {
	if path == "" || runtimeDir == "" {
		return false
	}
	base := filepath.Base(path)
	return filepath.Dir(filepath.Clean(path)) == filepath.Clean(runtimeDir) &&
		strings.HasPrefix(base, purgeResultPrefix) && strings.HasSuffix(base, ".json")
}
