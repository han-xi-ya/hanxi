//go:build windows

package windows

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	PurgeHelperMode       = "purge-standby"
	purgeHelperTimeout    = 90 * time.Second
	purgeHelperExitFailed = 1
	purgeHelperExitDenied = 2
)

// RunPurgeHelper 以 UAC 拉起同一个 Hanxi 的一次性 purge helper，等待其写回
// 严格校验的结果文件。helper 不进入 GUI/Wails 生命周期；结果文件读取后立即删除。
func RunPurgeHelper(exe, runtimeDir, requestID string) (PurgeResultFile, error) {
	resultPath, err := NewPurgeRequestFile(runtimeDir, requestID)
	if err != nil {
		return PurgeResultFile{}, err
	}
	defer os.Remove(resultPath)
	CleanupStalePurgeResults(runtimeDir, time.Now())

	args := []string{
		"--mode=" + PurgeHelperMode,
		"--result=" + resultPath,
		"--request-id=" + requestID,
		"--elevated=true",
	}
	quoted := make([]string, 0, len(args))
	for _, arg := range args {
		quoted = append(quoted, PsQuote(arg))
	}
	script := fmt.Sprintf("$p = Start-Process -FilePath %s -ArgumentList %s -Verb RunAs -Wait -WindowStyle Hidden -PassThru; if ($null -eq $p) { exit 1 }; exit [int]$p.ExitCode", PsQuote(exe), strings.Join(quoted, ","))
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script)
	HideConsole(cmd)
	cmd.WaitDelay = purgeHelperTimeout
	out, err := cmd.CombinedOutput()
	if err != nil {
		if errors.Is(err, exec.ErrWaitDelay) {
			return PurgeResultFile{}, fmt.Errorf("purge helper 超时或等待异常: %w", err)
		}
		if isUACCancelled(string(out)) {
			return PurgeResultFile{State: "cancelled", Message: "已取消 UAC 授权，清理未执行"}, nil
		}
		code := ElevatedRunExitCode(err)
		if code == purgeHelperExitDenied {
			return PurgeResultFile{State: "denied", Message: "管理员权限未获分配，清理未执行"}, nil
		}
		return PurgeResultFile{}, fmt.Errorf("purge helper 启动失败（退出码 %d）: %w %s", code, err, strings.TrimSpace(string(out)))
	}
	result, err := ReadPurgeResult(resultPath, requestID)
	if err != nil {
		return PurgeResultFile{}, fmt.Errorf("purge helper 未返回有效结果: %w", err)
	}
	return result, nil
}

func purgeHelperResultPathAllowed(path, runtimeDir string) bool {
	base := filepath.Base(path)
	return filepath.Dir(path) == runtimeDir && strings.HasPrefix(base, purgeResultPrefix) && strings.HasSuffix(base, ".json")
}
