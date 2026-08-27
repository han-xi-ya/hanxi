package detect

import (
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"sync"
)

// lookPath / runVersionOutput 为包级函数变量，单测可替换模拟 PATH 与执行结果。
var (
	lookPath         = exec.LookPath
	runVersionOutput = runVersionCommand
)

// DetectOne 按统一流程探测单个工具：
// LookPath 找可执行文件 → 执行版本命令 → 正则解析版本。
// 除探测结果本身（含 env PATH 信息）外不依赖任何外部状态。
func DetectOne(ctx context.Context, d Detector) ToolInfo {
	info := ToolInfo{
		Name:    d.Name(),
		Display: d.Display(),
		Status:  StatusMissing,
		Hint:    fmt.Sprintf("未在 PATH 中找到 %s，请安装后将可执行文件所在目录加入系统 PATH", d.Display()),
	}
	if mh, ok := d.(MissingHintAware); ok {
		info.Hint = mh.MissingHint()
	}

	exe, err := lookPath(d.Name())
	if err != nil {
		return info
	}
	info.Path = exe

	raw, runErr := runVersionOutput(ctx, exe, d.VersionArgs())
	if runErr != nil || strings.TrimSpace(raw) == "" {
		if sa, ok := d.(StubAware); ok && sa.IsStoreStub(exe) {
			info.Status = StatusStoreStub
			info.Hint = fmt.Sprintf("检测到 Microsoft Store 存根（假 %s），请到官网安装正式版本，并确保其安装目录位于 PATH 中 WindowsApps 之前", d.Display())
			return info
		}
		info.Status = StatusError
		if runErr != nil {
			info.Hint = fmt.Sprintf("执行 %s %s 失败: %v", d.Name(), strings.Join(d.VersionArgs(), " "), runErr)
		} else {
			info.Hint = "版本命令无输出"
		}
		return info
	}

	if v := d.Parse(raw); v != "" {
		info.Version = v
		info.Status = StatusInstalled
		info.Hint = ""
		return info
	}
	info.Status = StatusError
	info.Hint = fmt.Sprintf("版本输出无法识别: %s", firstLine(raw))
	return info
}

// RunAll 并发探测全部工具：每工具独立 goroutine（单工具 5s 超时兜底），
// WaitGroup 汇总后同步返回完整列表（总耗时 ≈ 最慢工具，上限 5s）。
// 失败落在工具级 status 上，不整体报错。
func RunAll(ctx context.Context) []ToolInfo {
	ds := Detectors()
	results := make([]ToolInfo, len(ds))
	var wg sync.WaitGroup
	for i, d := range ds {
		wg.Add(1)
		go func(i int, d Detector) {
			defer wg.Done()
			results[i] = DetectOne(ctx, d)
		}(i, d)
	}
	wg.Wait()
	sort.SliceStable(results, func(i, j int) bool {
		return statusRank(results[i].Status) < statusRank(results[j].Status)
	})
	return results
}

// statusRank 将可正常使用的环境排在前面，未安装和异常环境统一放到末尾。
// 同一状态内保持注册表原有的 Name 字典序。
func statusRank(status Status) int {
	switch status {
	case StatusInstalled:
		return 0
	case StatusError:
		return 1
	case StatusStoreStub:
		return 2
	case StatusMissing:
		return 3
	default:
		return 4
	}
}

// RunOne 按注册名探测单个工具，未知名返回错误。
func RunOne(ctx context.Context, name string) (ToolInfo, error) {
	for _, d := range Detectors() {
		if d.Name() == name {
			return DetectOne(ctx, d), nil
		}
	}
	return ToolInfo{}, fmt.Errorf("未知工具: %s", name)
}

// firstLine 取输出首行（剔除 \r），供"无法识别"提示引用原文。
func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	if len(s) > 120 {
		s = s[:120] + "…"
	}
	return s
}
