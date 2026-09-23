// Package diskusage envcheck 空间家底（W3/N14）：从已探测的开发工具链推导
// "本体安装目录 + 依赖/缓存目录"，交 packages/go/dirstats 在共享预算下限流度量。
//
// 目录推导三档回退：环境变量 → 工具自述命令 → 平台默认路径；每档都尽力而为——
// 命令失败/超时降到下一档，最终路径不存在则该行 Exists=false，绝不臆造。
// 依赖目录按"存在才列"原则呈现（mvn/gradle 仓库只在磁盘上真实在场时出现），
// 未装过的缓存不给整页零值噪音。
//
// 依赖注入面（单测替换）：envLookup（环境变量）、runOutput（工具命令执行）；
// 命令执行统一带 5s 子超时并在 ctx 取消时放弃。模型沿用 detect（其本体经
// platform/windows 传递为 Windows-only，故本包测试在 Windows CI/真机执行，
// 云侧以 GOOS=windows build/vet 把关）；AppData 路径族真值差异列验收项。
package diskusage

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"hanxi/internal/modules/envcheck/detect"
	"hanxi/internal/platform/windows"
	"hanxi/packages/go/dirstats"
)

// Kind 目录类别（前端分组徽标）。
const (
	KindInstall = "install" // 本体安装目录
	KindCache   = "cache"   // 依赖/缓存目录
)

// Verdict 删留判定（W3-c）：静态建议徽章，只回答"能不能删、删了会怎样"，
// 不是删除执行入口——清理动作留在包外（go clean、npm cache clean 等归机主）。
const (
	VerdictKeep        = "keep"        // 保留：本体安装/数据目录，删了=拆软件
	VerdictRecommended = "recommended" // 推荐删：纯构建缓存，删后自动重建，代价只是下次构建变慢
	VerdictRedownload  = "redownload"  // 可删·会重下：下次用到时自动重新下载，付网络与等待
	VerdictCaution     = "caution"     // 慎删：有连带后果（硬链断链、全局 CLI 即卸），先确认再动
)

// dirBudgetPerEntry 单目录兜底预算（整体预算由调用方 ctx 控制；
// 这里只防"一个卡死目录吃光全场"的极端，取 8s）。
const dirBudgetPerEntry = 8 * time.Second

// DirUsage 一个推导出的目录及其度量结果（Partial=true 时 Bytes 为下限估算）。
type DirUsage struct {
	Kind       string `json:"kind"`
	Label      string `json:"label"`
	Path       string `json:"path"`
	Exists     bool   `json:"exists"`
	Bytes      int64  `json:"bytes"`
	Files      int64  `json:"files"`
	Partial    bool   `json:"partial"`
	ErrorCount int64  `json:"errorCount"`
	Note       string `json:"note,omitempty"` // 度量失败原因（存在但扫不动时如实呈现）
	Verdict    string `json:"verdict"`        // 删留判定（Verdict* 常量；空串=未判定）
}

// ToolUsage 一个工具的家底清单（Dirs 首项恒为本体，若可推导）。
type ToolUsage struct {
	Tool    string     `json:"tool"`
	Display string     `json:"display"`
	Dirs    []DirUsage `json:"dirs"`
}

type envLookupFunc func(key string) string
type runOutputFunc func(ctx context.Context, exe string, args ...string) (string, error)

// resolver 由已探测的 ToolInfo 推导该工具的目录清单（未去重、未度量）。
type resolver func(ctx context.Context, info detect.ToolInfo, env envLookupFunc, run runOutputFunc) []DirUsage

var resolvers = map[string]resolver{
	"go":     resolveGo,
	"node":   resolveNode,
	"npm":    resolveNpm,
	"pnpm":   resolvePnpm,
	"python": resolvePython,
	"java":   resolveJava,
	"dotnet": resolveDotnet,
	"git":    resolveGit,
}

// Collect 对已安装且能推导目录的工具逐一度量家底。ctx 控制整场预算
// （服务层传 20s 级超时上下文）；顺序执行保持内存与磁盘 IO 友好
// （缓存目录动辄百万小文件，并发扫描只会互相拖慢）。
func Collect(ctx context.Context, infos []detect.ToolInfo) []ToolUsage {
	return collectWith(ctx, infos, os.Getenv, defaultRun)
}

func collectWith(ctx context.Context, infos []detect.ToolInfo, env envLookupFunc, run runOutputFunc) []ToolUsage {
	var out []ToolUsage
	for _, info := range infos {
		if info.Status != detect.StatusInstalled || info.Path == "" {
			continue
		}
		res, ok := resolvers[info.Name]
		if !ok {
			continue
		}
		dirs := res(ctx, info, env, run)
		measured := make([]DirUsage, 0, len(dirs))
		for _, d := range dirs {
			d.Path = normalizePath(d.Path) // 尾分隔符/盘符大小写等命令回显风格统一归一
			measureDir(ctx, &d)
			measured = append(measured, d)
		}
		if len(measured) > 0 {
			out = append(out, ToolUsage{Tool: info.Name, Display: info.Display, Dirs: measured})
		}
	}
	return out
}

// measureDir 存在性判定 + 预算内度量；根级失败（权限/竞态消失）落 Note 不中断。
func measureDir(ctx context.Context, d *DirUsage) {
	if d.Path == "" {
		return
	}
	st, err := os.Stat(d.Path)
	if err != nil || !st.IsDir() {
		return // Exists=false：路径未落地（未用过该缓存/推导失败），前端不列或灰列
	}
	d.Exists = true
	budgeted, cancel := context.WithTimeout(ctx, dirBudgetPerEntry)
	defer cancel()
	s := dirstats.MeasureWith(budgeted, d.Path, dirstats.Options{})
	d.Bytes, d.Files, d.Partial, d.ErrorCount = s.Bytes, s.Files, s.Partial, s.ErrorCount
	if s.Err != nil {
		d.Note = s.Err.Error()
	}
}

// ---------- 各工具推导 ----------

func resolveGo(ctx context.Context, info detect.ToolInfo, env envLookupFunc, run runOutputFunc) []DirUsage {
	var dirs []DirUsage
	// 本体 GOROOT：env → go env GOROOT → exe 上跳（.../bin/go[.exe]）。
	root := env("GOROOT")
	if root == "" {
		root = cmdFirstLine(ctx, info.Path, run, "env", "GOROOT")
	}
	if root == "" {
		root = parentOf(info.Path) // bin 目录（go.exe 同级即 bin/）
	}
	dirs = append(dirs, DirUsage{Kind: KindInstall, Label: "Go SDK (GOROOT)", Path: root, Verdict: VerdictKeep})
	// 模块缓存：GOMODCACHE → go env → GOPATH/pkg/mod → HOME/go/pkg/mod。
	mod := env("GOMODCACHE")
	if mod == "" {
		mod = cmdFirstLine(ctx, info.Path, run, "env", "GOMODCACHE")
	}
	if mod == "" {
		gopath := firstNonEmpty(env("GOPATH"), filepath.Join(homeOf(env), "go"))
		mod = filepath.Join(strings.Split(gopath, string(os.PathListSeparator))[0], "pkg", "mod")
	}
	dirs = append(dirs, DirUsage{Kind: KindCache, Label: "模块缓存 (GOMODCACHE)", Path: mod, Verdict: VerdictRedownload})
	// 构建缓存：GOCACHE → go env → 平台默认。
	build := env("GOCACHE")
	if build == "" {
		build = cmdFirstLine(ctx, info.Path, run, "env", "GOCACHE")
	}
	if build == "" {
		build = filepath.Join(localAppDataOf(env), "go-build")
	}
	dirs = append(dirs, DirUsage{Kind: KindCache, Label: "构建缓存 (GOCACHE)", Path: build, Verdict: VerdictRecommended})
	return dirs
}

func resolveNode(ctx context.Context, info detect.ToolInfo, env envLookupFunc, run runOutputFunc) []DirUsage {
	// node.exe 同级即安装目录（Windows 官方 zip/installer 形态）。
	return []DirUsage{{Kind: KindInstall, Label: "Node.js 安装目录", Path: parentOf(info.Path), Verdict: VerdictKeep}}
}

func resolveNpm(ctx context.Context, info detect.ToolInfo, env envLookupFunc, run runOutputFunc) []DirUsage {
	var dirs []DirUsage
	cache := env("npm_config_cache")
	if cache == "" {
		cache = cmdFirstLine(ctx, info.Path, run, "config", "get", "cache")
	}
	if cache == "" {
		cache = filepath.Join(localAppDataOf(env), "npm-cache")
	}
	dirs = append(dirs, DirUsage{Kind: KindCache, Label: "npm 缓存", Path: cache, Verdict: VerdictRedownload})
	global := cmdFirstLine(ctx, info.Path, run, "root", "-g")
	if global == "" {
		global = filepath.Join(parentOf(info.Path), "node_modules")
	}
	dirs = append(dirs, DirUsage{Kind: KindCache, Label: "全局包目录", Path: global, Verdict: VerdictCaution}) // 删=卸掉全部全局 CLI（含 claude 等）
	return dirs
}

func resolvePnpm(ctx context.Context, info detect.ToolInfo, env envLookupFunc, run runOutputFunc) []DirUsage {
	store := cmdFirstLine(ctx, info.Path, run, "store", "path")
	if store == "" {
		store = filepath.Join(localAppDataOf(env), "pnpm", "store")
	}
	// 现存工程的 node_modules 硬链指向内容库，直接删会坏依赖——要清走 pnpm store prune。
	return []DirUsage{{Kind: KindCache, Label: "pnpm 内容库", Path: store, Verdict: VerdictCaution}}
}

func resolvePython(ctx context.Context, info detect.ToolInfo, env envLookupFunc, run runOutputFunc) []DirUsage {
	dirs := []DirUsage{{Kind: KindInstall, Label: "Python 安装目录", Path: parentOf(info.Path), Verdict: VerdictKeep}}
	pip := cmdFirstLine(ctx, info.Path, run, "-m", "pip", "cache", "dir")
	if pip == "" {
		pip = filepath.Join(localAppDataOf(env), "pip", "Cache")
	}
	dirs = append(dirs, DirUsage{Kind: KindCache, Label: "pip 缓存", Path: pip, Verdict: VerdictRedownload})
	return dirs
}

func resolveJava(ctx context.Context, info detect.ToolInfo, env envLookupFunc, run runOutputFunc) []DirUsage {
	// java.exe 位于 <jdk>/bin/，本体取上两级。
	jdk := filepath.Dir(parentOf(info.Path))
	if base := filepath.Base(info.Path); !strings.EqualFold(base, "java.exe") && !strings.EqualFold(base, "java") {
		jdk = parentOf(info.Path) // 非 bin/java 形态（如 shim），退回 exe 同级
	}
	dirs := []DirUsage{{Kind: KindInstall, Label: "JDK/JRE 安装目录", Path: jdk, Verdict: VerdictKeep}}
	// Maven/Gradle 家底归属 JVM 生态：仅磁盘真实存在时列出（存在性判定在 measureDir）。
	if home := homeOf(env); home != "" {
		dirs = append(dirs, DirUsage{Kind: KindCache, Label: "Maven 仓库", Path: filepath.Join(home, ".m2", "repository"), Verdict: VerdictRedownload})
		dirs = append(dirs, DirUsage{Kind: KindCache, Label: "Gradle 缓存", Path: filepath.Join(home, ".gradle", "caches"), Verdict: VerdictRedownload})
	}
	return dirs
}

func resolveDotnet(ctx context.Context, info detect.ToolInfo, env envLookupFunc, run runOutputFunc) []DirUsage {
	dirs := []DirUsage{{Kind: KindInstall, Label: ".NET 安装目录", Path: parentOf(info.Path), Verdict: VerdictKeep}}
	nuget := env("NUGET_PACKAGES")
	if nuget == "" {
		out := cmdRaw(ctx, info.Path, run, "nuget", "locals", "global-packages", "--list")
		if _, after, found := strings.Cut(out, ":"); found {
			nuget = strings.TrimSpace(after)
		}
	}
	if nuget == "" {
		nuget = filepath.Join(homeOf(env), ".nuget", "packages")
	}
	dirs = append(dirs, DirUsage{Kind: KindCache, Label: "NuGet 全局包", Path: nuget, Verdict: VerdictRedownload})
	return dirs
}

func resolveGit(ctx context.Context, info detect.ToolInfo, env envLookupFunc, run runOutputFunc) []DirUsage {
	// Windows 安装形态 git.exe 在 <root>/cmd/；便携形态在 <root>/bin/；
	// 上两级命中根目录，取不到再退回 exe 同级。
	twoUp := filepath.Dir(parentOf(info.Path))
	if isGitRoot(twoUp) {
		return []DirUsage{{Kind: KindInstall, Label: "Git 安装目录", Path: twoUp, Verdict: VerdictKeep}}
	}
	return []DirUsage{{Kind: KindInstall, Label: "Git 安装目录", Path: parentOf(info.Path), Verdict: VerdictKeep}}
}

func isGitRoot(dir string) bool {
	if dir == "" || dir == "." {
		return false
	}
	for _, marker := range []string{"LICENSE.txt", "LICENSE.md", "git-bash.exe", "mingw64"} {
		if _, err := os.Stat(filepath.Join(dir, marker)); err == nil {
			return true
		}
	}
	return false
}

// ---------- 小工具 ----------

func cmdFirstLine(ctx context.Context, exe string, run runOutputFunc, args ...string) string {
	return firstLine(cmdRaw(ctx, exe, run, args...))
}

func cmdRaw(ctx context.Context, exe string, run runOutputFunc, args ...string) string {
	out, err := run(ctx, exe, args...)
	if err != nil {
		return ""
	}
	return out
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	// 拦截明显的失败话术（npm/pnpm 命令报错时输出非路径文本）。
	if strings.Contains(s, "not found") || strings.Contains(s, "Usage:") || strings.HasPrefix(s, "Unknown") {
		return ""
	}
	return s
}

func parentOf(exe string) string { return filepath.Dir(filepath.Clean(exe)) }

// normalizePath 推导路径归一：去尾分隔符（filepath.Clean）+ 盘符大写
// （pip 等命令回显整串小写，与 os 默认路径行风格不一）。
func normalizePath(p string) string {
	if p == "" {
		return ""
	}
	p = filepath.Clean(p)
	if len(p) >= 2 && p[1] == ':' && p[0] >= 'a' && p[0] <= 'z' {
		p = string(p[0]-32) + p[1:]
	}
	return p
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func homeOf(env envLookupFunc) string { return firstNonEmpty(env("HOME"), env("USERPROFILE")) }

func localAppDataOf(env envLookupFunc) string {
	return firstNonEmpty(env("LocalAppData"), env("LOCALAPPDATA"), filepath.Join(homeOf(env), "AppData", "Local"))
}

// defaultRun 真实命令执行：5s 子超时、去首尾空白；错误聚合（exit + stderr 摘要）。
var defaultRun runOutputFunc = func(ctx context.Context, exe string, args ...string) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	// npm/pnpm 等 .cmd 分发器经 CreateProcess 无法直启批处理，须 cmd /C 包装
	// （detect.runVersionCommand 同款处理；此前漏包导致命令实际全走默认路径回退）。
	cmd := exec.CommandContext(cctx, exe, args...)
	if ext := strings.ToLower(filepath.Ext(exe)); ext == ".cmd" || ext == ".bat" {
		cmd = exec.CommandContext(cctx, "cmd", append([]string{"/C", exe}, args...)...)
	}
	windows.HideConsole(cmd) // 藏控制台子进程：推导命令不再逐条闪黑框（W3-a）
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s %v: %w（%s）", filepath.Base(exe), args, err, trunc(strings.TrimSpace(stderr.String()), 120))
	}
	return stdout.String(), nil
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// ErrNothingToScan 供服务层话术（无任何可推导家底时）。
var ErrNothingToScan = errors.New("envcheck/diskusage: 没有已安装且可推导空间家底的工具")
