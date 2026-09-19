package supervisor

import (
	"io"
	"os"
	"os/exec"
)

// processHandle 对"已创建进程"的最小抽象：默认实现包装 *exec.Cmd，
// 单测注入 fake 后全部治理路径不依赖真实进程（本仓库 CI 环境约束）。
// 并发纪律：Wait 的唯一调用者是引擎的 wait 协程（单一 cmd.Wait 所有者）。
type processHandle interface {
	// Start 拉起进程（不阻塞）。
	Start() error
	// Wait 阻塞至进程退出；返回 exec 风格错误（正常退出为 nil）。
	Wait() error
	// Kill 强制终止（兜底路径；经持有句柄，不受 PID 复用影响）。
	Kill() error
	// PID 进程 ID（Start 成功后有效）。
	PID() uint32
	// ExitCode 退出码（Wait 返回后有效；不可得时返回 -1）。
	ExitCode() int
	// StdoutPipe/StderrPipe 获取子进程输出读取端（须在 Start 前调用，同
	// exec.Cmd.StdoutPipe 语义）；不转发输出的实现可返回 (nil, nil)。
	StdoutPipe() (io.Reader, error)
	// StderrPipe 见 StdoutPipe 说明。
	StderrPipe() (io.Reader, error)
}

// opener 进程创建器：由 Spec 产出尚未 Start 的句柄。引擎字段可替换供测试注入。
type opener func(spec Spec) (processHandle, error)

// execHandle processHandle 的默认实现（跨平台主流程；HideWindow 等平台特异注入
// 经 Spec 声明、由 child_windows.go / child_other.go 的 applyWindowFlags 落地，
// 本文件不出现平台分支常量）。
type execHandle struct {
	cmd *exec.Cmd
}

func defaultOpener(spec Spec) (processHandle, error) {
	cmd := exec.Command(spec.Exe, spec.Args...)
	cmd.Dir = spec.WorkingDir
	if len(spec.Env) > 0 {
		// 显式继承当前进程环境再追加（cmd.Env 非 nil 即不再自动继承）。
		cmd.Env = append(os.Environ(), spec.Env...)
	}
	if spec.HideWindow {
		applyWindowFlags(cmd)
	}
	return &execHandle{cmd: cmd}, nil
}

func (h *execHandle) Start() error { return h.cmd.Start() }

func (h *execHandle) Wait() error { return h.cmd.Wait() }

func (h *execHandle) Kill() error {
	if h.cmd.Process == nil {
		return nil
	}
	return h.cmd.Process.Kill()
}

func (h *execHandle) PID() uint32 {
	if h.cmd.Process == nil {
		return 0
	}
	return uint32(h.cmd.Process.Pid)
}

func (h *execHandle) ExitCode() int {
	if h.cmd.ProcessState == nil {
		return -1
	}
	return h.cmd.ProcessState.ExitCode()
}

func (h *execHandle) StdoutPipe() (io.Reader, error) { return h.cmd.StdoutPipe() }

func (h *execHandle) StderrPipe() (io.Reader, error) { return h.cmd.StderrPipe() }
