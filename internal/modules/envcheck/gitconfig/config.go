package gitconfig

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"hanxi/internal/platform/windows"
)

// readTimeout git 全局配置读取硬上限：纯本机文件读取，比 detect 版本探测的 5s 收紧。
const readTimeout = 3 * time.Second

// errTimeout 读取超时哨兵：超时输出同样为空，须与"git 无全局配置的非零退出"区分开。
var errTimeout = errors.New("读取超时")

// lookPath / runConfigList 为包级函数 seam，单测可替换模拟 PATH 与执行结果（不真跑 git）。
var (
	lookPath      = exec.LookPath
	runConfigList = runConfigListCommand
)

// missingConfigFileRe 匹配 git 全局配置文件不存在时的 fatal 措辞
// （旧版 git 以 128 + "unable to read config file" 报缺文件，新版改为 1 + 空输出）。
var missingConfigFileRe = regexp.MustCompile(`(?i)unable to read (?:global )?config file.{0,120}(?:no such file|cannot access|does not exist)`)

// GlobalOverview 读取并解析 git 全局配置（git config --global --list，只读）：
// 未安装/未配置/读取成功/读取失败四态如实落进 state，失败绝不退化成空列表糊弄；
// 全部条目出参前已按 Redact 词表就地打码。
func GlobalOverview(ctx context.Context) Overview {
	exe, err := lookPath("git")
	if err != nil {
		return Overview{
			State:  StateMissing,
			Detail: "未在 PATH 中找到 git，无法读取全局配置；请先安装 Git 并将安装目录加入系统 PATH",
		}
	}
	out, runErr := runConfigList(ctx, exe)
	if runErr != nil {
		switch {
		case !errors.Is(runErr, errTimeout) && isUnconfiguredOutput(out, runErr):
			return Overview{State: StateUnconfigured}
		default:
			return Overview{State: StateError, Detail: fmt.Sprintf("读取 git 全局配置失败: %v%s", runErr, outputTail(out))}
		}
	}
	items := parseEntries(out)
	if len(items) == 0 {
		return Overview{State: StateUnconfigured}
	}
	return Overview{State: StateConfigured, Items: items}
}

// isUnconfiguredOutput 判定非零退出实为"尚无全局配置"：近期 git 配置文件缺失时以
// 退出码 1 + 空输出返回，旧版则报 fatal: unable to read config file ...。
// 其余非零（如配置内容损坏 exit 128 带报错）由调用侧归入读取失败。
func isUnconfiguredOutput(out string, runErr error) bool {
	if strings.TrimSpace(out) == "" {
		return true
	}
	return missingConfigFileRe.MatchString(out)
}

// runConfigListCommand 执行 git config --global --list 并返回合并输出。
// CombinedOutput 兜底 stderr（缺文件 fatal 措辞版本相关，必须两路都拿）。
func runConfigListCommand(ctx context.Context, exe string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, exe, "config", "--global", "--list")
	windows.HideConsole(cmd) // 防止读取时弹出 cmd 黑框（与 detect 家族同款 CREATE_NO_WINDOW）

	out, err := cmd.CombinedOutput()
	if err != nil {
		if ctx.Err() != nil {
			return string(out), fmt.Errorf("%w（%s）", errTimeout, readTimeout)
		}
		return string(out), fmt.Errorf("git 退出异常: %w", err)
	}
	return string(out), nil
}

// parseEntries 将 --list 输出逐行拆为已脱敏条目；无法拆出键值的行如实跳过（不出界面）。
func parseEntries(out string) []Entry {
	var items []Entry
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line) // 同时消化 \r\n 与首尾空白
		if line == "" {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k, v := Redact(strings.TrimSpace(key), value)
		items = append(items, Entry{Key: k, Value: v})
	}
	return items
}

// outputTail 引用失败输出尾部（rune 安全截断），供 detail 说明真实死因。
func outputTail(out string) string {
	out = strings.TrimSpace(out)
	if out == "" {
		return ""
	}
	runes := []rune(out)
	if len(runes) > 120 {
		runes = runes[:120]
		out = string(runes) + "…"
	}
	return "（git 输出: " + out + "）"
}
