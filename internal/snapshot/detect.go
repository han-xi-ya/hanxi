package snapshot

import (
	"context"
	"log/slog"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"hanxi/internal/platform/windows"
)

// git 可用性探测：快照自带同款短链（LookPath → --version → 正则，PLAN §2.5），
// 不 import envcheck——避免平台底座与业务模块互引；二十行重复换依赖方向干净。

// lookPath / runGitVersion 为包级函数变量 seam，单测可替换模拟 PATH 与执行结果
// （detect.DetectOne 同款可测性设计）。
var (
	lookPath      = exec.LookPath
	runGitVersion = defaultRunGitVersion
)

const gitDetectTimeout = 5 * time.Second

// gitVersionRe 与 envcheck/detect 同谱：`git version 2.46.0.windows.1`。
var gitVersionRe = regexp.MustCompile(`(?i)\bgit version\s+(\d+\.\d+\.\d+(?:\.[0-9A-Za-z]+)*)`)

// gitProbe 探测结论。git 缺失不是错误（PLAN §2.5 静默失败边界）：
// Available=false 时调用方直接走降级影子拷贝。
type gitProbe struct {
	Available bool
	Path      string
	Version   string
}

// detectGit 探测外部 git。Microsoft Store 假存根专属判定：存根执行即报错退出，
// 版本解析自然失败；命中 WindowsApps 路径特征时额外记明日志，免得排查时
// 误以为"装了 git 怎么还降级"。
func detectGit() gitProbe {
	ctx, cancel := context.WithTimeout(context.Background(), gitDetectTimeout)
	defer cancel()

	exe, err := lookPath("git")
	if err != nil {
		return gitProbe{}
	}
	out, rerr := runGitVersion(ctx, exe)
	if rerr == nil {
		if m := gitVersionRe.FindStringSubmatch(out); m != nil {
			return gitProbe{Available: true, Path: exe, Version: m[1]}
		}
		slog.Warn("snapshot: git 版本输出无法识别，按不可用处理（降级影子拷贝）", "path", exe)
		return gitProbe{}
	}
	if isStoreStub(exe) {
		slog.Warn("snapshot: 检测到 Microsoft Store 假 git 存根，按不可用处理（降级影子拷贝）", "path", exe)
		return gitProbe{}
	}
	slog.Warn("snapshot: git 执行失败，按不可用处理（降级影子拷贝）", "path", exe, "err", rerr)
	return gitProbe{}
}

// isStoreStub WindowsApps 商店存根路径特征（大小写不敏感，envcheck python 判据同谱）。
func isStoreStub(exePath string) bool {
	return strings.Contains(strings.ToLower(exePath), "windowsapps")
}

// defaultRunGitVersion 一次性版本探测命令（HideConsole 防黑框，wslcli/detect 同款）。
func defaultRunGitVersion(ctx context.Context, exe string) (string, error) {
	cmd := exec.CommandContext(ctx, exe, "--version")
	windows.HideConsole(cmd)
	out, err := cmd.CombinedOutput()
	return string(out), err
}
