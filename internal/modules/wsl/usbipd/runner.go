package usbipd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"unicode/utf16"

	platformwin "hanxi/internal/platform/windows"
)

// ErrNotInstalled usbipd-win 未安装（或不在 PATH）的哨兵：上层据此渲染引导卡，
// 不当作运行时故障上报。
var ErrNotInstalled = errors.New("usbipd-win 未安装")

// RunFunc usbipd.exe 调用面的最小注入点：返回 stdout、stderr 与原始错误。
// 单测替换它即可全离线断言命令构造与错误归因。
type RunFunc func(ctx context.Context, args ...string) (stdout string, stderr string, err error)

// Runner usbipd.exe 的 CLI 胶水（非提权通道；bind/unbind 提权执行在 wsl 模块侧）。
type Runner struct {
	run RunFunc
}

// NewRunner 以真实 exec 通道构造 Runner。
func NewRunner() *Runner { return &Runner{run: runUSBIPD} }

// NewRunnerWith 注入自定义调用通道（单测/替身用）。
func NewRunnerWith(run RunFunc) *Runner { return &Runner{run: run} }

// runUSBIPD 用户态隐藏控制台执行 usbipd（.NET 系 CLI，重定向下输出 UTF-8，
// 与 wsl.exe 的 UTF-16 坑无关；BOM 由解析端兜掉）。
func runUSBIPD(ctx context.Context, args ...string) (string, string, error) {
	cmd := exec.CommandContext(ctx, "usbipd", args...)
	platformwin.HideConsole(cmd)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return decodeMaybeUTF16(stdout.Bytes()), decodeMaybeUTF16(stderr.Bytes()), err
}

// decodeMaybeUTF16 防呆解码：偶数长度且含 NUL 视为 UTF-16LE（wsl.exe 同款判据，
// #46 教训外溢），否则按 UTF-8 原文。
func decodeMaybeUTF16(b []byte) string {
	if len(b) > 1 && len(b)%2 == 0 && bytes.IndexByte(b, 0) >= 0 {
		if len(b) >= 2 && b[0] == 0xFF && b[1] == 0xFE {
			b = b[2:]
		}
		units := make([]uint16, 0, len(b)/2)
		for i := 0; i+1 < len(b); i += 2 {
			units = append(units, uint16(b[i])|uint16(b[i+1])<<8)
		}
		return strings.TrimSpace(string(utf16.Decode(units)))
	}
	return strings.TrimSpace(string(b))
}

// notInstalled 判"程序不存在"：exec 找不到文件才算——超时被杀走 ExitError 路径，
// 不补哨兵就会把"没装"的帽子扣到慢启动头上。
func notInstalled(err error) bool {
	return errors.Is(err, exec.ErrNotFound)
}

// exitCoder 退出码访问器（*exec.ExitError 与单测轻量替身同型）。
type exitCoder interface {
	error
	ExitCode() int
}

// exitCode 提取退出码；无退出码语义的错误返回 -1。
func exitCode(err error) int {
	var ec exitCoder
	if errors.As(err, &ec) {
		return ec.ExitCode()
	}
	return -1
}

// withErrText 输出为空时把错误本体并进输出（fake 通道常只给 err 不给文案）。
func withErrText(out string, err error) string {
	if strings.TrimSpace(out) != "" {
		return out
	}
	return err.Error()
}

// ExecError usbipd 非零退出的类型化凭证：保留退出码与原文输出，
// 上层（重放引擎、UI 回执）按中文化 Message 呈现、按 Code 归因。
type ExecError struct {
	Code   int
	Args   []string
	Output string // stdout+stderr 合并原文
}

func (e *ExecError) Error() string {
	return fmt.Sprintf("usbipd %s 失败: %s", strings.Join(e.Args, " "), ZhError(e.Output, e.Code))
}

// ZhError usbipd-win 英文报错 → 中文化说明（识别不出的原文照抄，绝不吞信息）。
// 常见词条对上游 master 源码逐句核对（CommandHandlersCli.cs）。
func ZhError(out string, code int) string {
	low := strings.ToLower(out)
	switch {
	case strings.Contains(low, "already attached"):
		return "设备已附加到某个客户端（先「卸下」再附加）"
	case strings.Contains(low, "not shared; run"), strings.Contains(low, "device is not shared"):
		return "设备尚未共享（bind，需管理员）——先点「共享」"
	case strings.Contains(low, "no device with busid"):
		return "该总线号上此刻没有设备（可能已拔出或换了插口）"
	case strings.Contains(low, "usbipd service is not running"), strings.Contains(low, "server is not running"):
		return "usbipd 系统服务未在运行——到「服务」里启动 usbipd service（或重装 usbipd-win）"
	case strings.Contains(low, "not installed"):
		return "usbipd-win 未安装，或安装后 PATH 未刷新（新开一个窗口即可生效）"
	case strings.Contains(low, "no wsl distributions are running"), strings.Contains(low, "wsl is not running"):
		return "WSL 没有在运行的发行版——先把目标发行版启动起来"
	case strings.Contains(low, "attach failed"):
		return "附加失败（发行版可能不支持 usbipd 集成，或 WSL 需更新）"
	case code == 3: // 上游 ExitCode.AccessDenied
		return "权限不足：该操作需要管理员（UAC 授权）"
	}
	if line := lastLine(out); line != "" {
		return line
	}
	return fmt.Sprintf("命令以退出码 %d 失败", code)
}

func lastLine(out string) string {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" {
			return l
		}
	}
	return ""
}

// Version 探测 usbipd-win 版本（同时充当安装探测）；未安装返回包装 ErrNotInstalled。
func (r *Runner) Version(ctx context.Context) (string, error) {
	stdout, stderr, err := r.run(ctx, VersionArgs()...)
	if err != nil {
		if notInstalled(err) {
			return "", fmt.Errorf("%w: %v", ErrNotInstalled, err)
		}
		return "", &ExecError{Code: exitCode(err), Args: VersionArgs(), Output: withErrText(stdout+stderr, err)}
	}
	if v := versionRe.FindString(normalizeBOM(stdout)); v != "" {
		return v, nil
	}
	// 命令在跑但拿不出版本号（异常输出形态）：如实报错而非谎报已装。
	return "", fmt.Errorf("%w: usbipd --version 输出异常（%s）", ErrNotInstalled, firstToken(stdout))
}

var versionRe = regexp.MustCompile(`\d+\.\d+\.\d+`)

// State 拉取并解析设备表。
func (r *Runner) State(ctx context.Context) ([]Device, error) {
	stdout, stderr, err := r.run(ctx, StateArgs()...)
	if err != nil {
		if notInstalled(err) {
			return nil, fmt.Errorf("%w: %v", ErrNotInstalled, err)
		}
		return nil, &ExecError{Code: exitCode(err), Args: StateArgs(), Output: withErrText(stdout+stderr, err)}
	}
	return ParseState(normalizeBOM(stdout))
}

// Attach 把设备附加到 WSL 发行版（distro 空=默认发行版；distro 白名单校验在调用方）。
func (r *Runner) Attach(ctx context.Context, distro, busID string) error {
	args, err := AttachArgs(distro, busID)
	if err != nil {
		return err
	}
	return r.simple(ctx, args...)
}

// Detach 把设备从客户端卸下（用户态）。
func (r *Runner) Detach(ctx context.Context, busID string) error {
	args, err := DetachArgs(busID)
	if err != nil {
		return err
	}
	return r.simple(ctx, args...)
}

func (r *Runner) simple(ctx context.Context, args ...string) error {
	stdout, stderr, err := r.run(ctx, args...)
	if err == nil {
		return nil
	}
	if notInstalled(err) {
		return fmt.Errorf("%w: %v", ErrNotInstalled, err)
	}
	return &ExecError{Code: exitCode(err), Args: args, Output: withErrText(stdout+stderr, err)}
}

func normalizeBOM(s string) string {
	return strings.TrimPrefix(strings.TrimSpace(s), "\xEF\xBB\xBF")
}

// firstToken 取输出首个非空词（错误消息里只放短样本，防整屏噪音进回执）。
func firstToken(s string) string {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return "空输出"
	}
	if len(fields[0]) > 40 {
		return fields[0][:40] + "…"
	}
	return fields[0]
}
