//go:build windows

// Package conpty 封装 Windows 伪控制台（ConPTY）这一件事：起一个"以为自己在
// 真终端里"的子进程，宿主拿到的只是三条管道 + 一个 resize 按钮。
//
// 本包的每条配方都有 2026-10-31 实弹探针背书（证据与失败复现存仓外
// C:/tmp/conpty-spike/：guievidence.txt / run2c-evidence.txt / run3-evidence.txt）：
//
//   - **CREATE_NEW_CONSOLE 与 PSEUDOCONSOLE 互斥且 CreateProcess 不报错**——
//     子进程会静默挂到一个真控制台，ConPTY 管道全程 0 字节、输入全吞。
//     正确的 creationFlags = EXTENDED_STARTUPINFO_PRESENT|CREATE_UNICODE_ENVIRONMENT
//     （0x80400）。这条是探针用两种配方各复现一次的铁证（run2c）。
//   - **STARTUPINFO 三端必须置 INVALID_HANDLE_VALUE、bInheritHandles=FALSE**
//     （portable-pty 同款姿势）。按 MSDN 样例"继承真实管道句柄"的做法，当宿主
//     stdio 本身是可继承管道（被测试基座/其它程序拉起）时，子进程 std 会抢挂到
//     宿主管道上：字形漏进父 stdout、ConPTY 只见转义噪音、powershell 自杀退出。
//     "首写输入被吞"的假象也是这个竞态的副作用——配方修正后无此现象，
//     宿主侧不需要任何补发逻辑。
//   - HPCON 不是普通句柄：只能 ClosePseudoConsole，绝不能丢进 CloseHandle。
//   - CreatePseudoConsole/ResizePseudoConsole 返回 HRESULT（x/sys 的公开封装
//     已按 error 转换；本包全部走 x/sys ≥v0.15 的公开函数，不手搓 LazyDLL——
//     探针阶段的手搓路线证实可行但属多余，原料 x/sys 早已备齐）。
//   - Win10 1809 以下无这三个导出：x/sys 的 lazy proc 会把"找不到函数"转成
//     error 上抛，本包如实透传，不假装支持。
//   - GUI 子系统（-H windowsgui）宿主实测可整套运行——hanxi 本体正是这种进程。
//
// 与 JobObject 的关系：子进程树清理现走 taskkill /T /F（探针与 aiagent 双先例）；
// 若要接仓内 supervisor 的 JobObject 属后续优化，不在本层语义里。
package conpty

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ErrNoConPTY 系统里没有 ConPTY 导出（Windows 10 1809 之前的形态）。
var ErrNoConPTY = errors.New("系统过旧：缺少 ConPTY 支持（需 Windows 10 1809+）")

// kernel32!UpdateProcThreadAttribute 的入口（x/sys 的容器方法只暴露会触发
// unsafeptr 检查的形态，见 Spawn 内注释）。
var procUpdateProcThreadAttribute = syscall.NewLazyDLL("kernel32.dll").NewProc("UpdateProcThreadAttribute")

// Options 一次 spawn 的可选参数。
type Options struct {
	// Cols/Rows 初始尺寸，零值取 80x24。
	Cols, Rows uint16
	// Dir 子进程工作目录，空=继承。
	Dir string
	// Env 完整环境（KEY=VALUE 列表）；nil 继承当前进程。
	// 重复键取后者——os/exec 语义，不跟随 CreateProcess 的 first-wins 怪癖，
	// 差异在此写明白（探针报告点名的坑 7）。
	Env []string
}

// Pty 一个伪控制台会话。Read/Write 是**原始字节流**（含 VT 转义序列），
// 渲染语义交给前端终端仿真器，本层一概不解析。
type Pty struct {
	hpc windows.Handle

	inW  *os.File // 宿主写、pty 读
	outR *os.File // pty 写、宿主读

	cmdLine string
	pid     uint32

	procHandle windows.Handle // CreateProcess 返回的主进程句柄（子孙不管，归 taskkill）

	closeOnce sync.Once
	exited    chan struct{} // 主进程退出后关闭
	lastErr   error
}

// Spawn 起伪控制台并挂上 command + args。
// command 不含参数；args 逐个按 CreateProcess 命令行转义拼接
// （windows.ComposeCommandLine，x/sys 公开件），没有 os/exec 那种 argv 直传。
func Spawn(command string, args []string, opts Options) (*Pty, error) {
	if command == "" {
		return nil, errors.New("conpty: command 不能为空")
	}
	cols, rows := opts.Cols, opts.Rows
	if cols == 0 {
		cols = 80
	}
	if rows == 0 {
		rows = 24
	}

	cmdLine := strings.TrimSpace(strings.Join(append([]string{command}, args...), " "))
	if cmdLine == "" {
		return nil, errors.New("conpty: 空命令行")
	}

	// 管道成对：宿主留 inW/outR，pty 侧端交给 CreatePseudoConsole 后立即关闭
	// （探针配方；留错方向会让 EOF 永远等不来）。
	inR, inW, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("conpty: 建输入管道失败: %w", err)
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		inR.Close()
		inW.Close()
		return nil, fmt.Errorf("conpty: 建输出管道失败: %w", err)
	}
	fail := func(e error) (*Pty, error) {
		inR.Close()
		inW.Close()
		outR.Close()
		outW.Close()
		return nil, e
	}

	var hpc windows.Handle
	err = windows.CreatePseudoConsole(
		windows.Coord{X: int16(cols), Y: int16(rows)},
		windows.Handle(inR.Fd()), windows.Handle(outW.Fd()), 0, &hpc) // flags=0：不用 INHERIT_CURSOR（探针陷阱 5）
	if err != nil {
		return fail(fmt.Errorf("conpty: CreatePseudoConsole 失败: %w（%w）", err, ErrNoConPTY))
	}
	inR.Close()  // pty 侧端交完即关（EOF 语义的前提）
	outW.Close() // 同上

	attr, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		windows.ClosePseudoConsole(hpc)
		return fail(fmt.Errorf("conpty: 属性表初始化失败: %w", err))
	}
	// PSEUDOCONSOLE 属性传 HPCON 句柄本身而不是 &hpc——探针实证的写法（陷阱清单）。
	// 走原生 proc 调用而非 attr.Update：后者签名逼一个 uintptr→unsafe.Pointer 的
	// 转换，go vet 的 unsafeptr 会按"伪造指针"拦下（句柄本就是整数，直接传
	// uintptr 参数才是诚实写法）。
	ret, errcode, _ := procUpdateProcThreadAttribute.Call(
		uintptr(unsafe.Pointer(attr.List())),
		0,
		uintptr(windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE),
		uintptr(hpc),
		unsafe.Sizeof(hpc),
		0,
	)
	if ret == 0 {
		attr.Delete()
		windows.ClosePseudoConsole(hpc)
		return fail(fmt.Errorf("conpty: 属性表挂 ConPTY 失败 (errcode=%d)", errcode))
	}
	defer attr.Delete() // CreateProcess 成功后列表所有权即完成使命（随进程语义）

	var envBlock *uint16
	if opts.Env != nil {
		envBlock = windows.StringToUTF16Ptr(strings.Join(dedupEnvLastWins(opts.Env), "\x00") + "\x00\x00")
	}
	var dirPtr *uint16
	if opts.Dir != "" {
		dirPtr = windows.StringToUTF16Ptr(opts.Dir)
	}

	si := windows.StartupInfoEx{
		ProcThreadAttributeList: attr.List(),
	}
	si.StartupInfo.Cb = uint32(unsafe.Sizeof(si))
	// 三端全部 INVALID：不借句继承这条路（头注铁证 2）；bInheritHandles=FALSE。
	// USESTDHANDLES 旗标要亮——否则 INVALID 三端根本不生效，子进程会去抢
	// 宿主管道（探针 run1b/run1i 的炸相就是这条）。
	si.StartupInfo.Flags |= windows.STARTF_USESTDHANDLES
	const invalid = ^windows.Handle(0)
	si.StartupInfo.StdInput = invalid
	si.StartupInfo.StdOutput = invalid
	si.StartupInfo.StdErr = invalid

	var pi windows.ProcessInformation
	err = windows.CreateProcess(
		nil, windows.StringToUTF16Ptr(cmdLine), nil, nil,
		false, // 关键：不继承句柄
		windows.EXTENDED_STARTUPINFO_PRESENT|windows.CREATE_UNICODE_ENVIRONMENT,
		envBlock, dirPtr, &si.StartupInfo, &pi)
	if err != nil {
		windows.ClosePseudoConsole(hpc)
		return fail(fmt.Errorf("conpty: CreateProcess 失败: %w", err))
	}
	windows.CloseHandle(pi.Thread)

	p := &Pty{
		hpc: hpc, inW: inW, outR: outR,
		cmdLine: cmdLine, pid: pi.ProcessId,
		procHandle: pi.Process,
		exited:     make(chan struct{}),
	}
	go p.watchExit()
	return p, nil
}

// watchExit 等主进程退场并落盘退出码；退出后 conpty 会关输出端，Read 自然 EOF。
func (p *Pty) watchExit() {
	defer close(p.exited)
	_, _ = windows.WaitForSingleObject(p.procHandle, windows.INFINITE)
	var code uint32
	if err := windows.GetExitCodeProcess(p.procHandle, &code); err == nil && code != 0 {
		p.lastErr = fmt.Errorf("conpty: 子进程退出码 %d", code)
	}
}

// Read 读 pty 输出流（原始字节，含 VT 序列）。子进程退场后返回 io.EOF。
func (p *Pty) Read(b []byte) (int, error) { return p.outR.Read(b) }

// Write 写 pty 输入流。行节流由上层（UI 键位）决定，本层不加工字节。
func (p *Pty) Write(b []byte) (int, error) { return p.inW.Write(b) }

// Resize 调整伪控制台尺寸。resize 紧贴启动会被 conpty 吞（社区实证 quirk），
// 调用方应防抖；紧邻 Close 的 resize 报错属正常。
func (p *Pty) Resize(cols, rows uint16) error {
	if cols == 0 || rows == 0 {
		return errors.New("conpty: 尺寸不能为 0")
	}
	err := windows.ResizePseudoConsole(p.hpc, windows.Coord{X: int16(cols), Y: int16(rows)})
	if err != nil {
		return fmt.Errorf("conpty: ResizePseudoConsole 失败: %w", err)
	}
	return nil
}

// Pid 主进程 PID（子孙进程树不在此列，清理走 Close 的 taskkill）。
func (p *Pty) Pid() uint32 { return p.pid }

// Done 主进程退出的通知通道。
func (p *Pty) Done() <-chan struct{} { return p.exited }

// Err 退出码非 0 的错误（进程仍活着时为 nil；退场后可读）。
func (p *Pty) Err() error {
	select {
	case <-p.exited:
		return p.lastErr
	default:
		return nil
	}
}

// Close 收场：杀进程树 → 等退场 → ClosePseudoConsole → 关宿主管道。
// 幂等。顺序不能换：管道先关会让 conpty 带着未退的子进程悬着。
func (p *Pty) Close() error {
	var err error
	p.closeOnce.Do(func() {
		err = p.killTree()
		select {
		case <-p.exited:
		case <-time.After(3 * time.Second): // 兜底上限：taskkill /F 正常秒回，等满即认卡死
		}
		windows.ClosePseudoConsole(p.hpc)
		p.inW.Close()
		p.outR.Close()
		windows.CloseHandle(p.procHandle)
		runtime.GC() // 无它也行，但探针期管道泄漏排查成本太高，留一道显式收口提醒
	})
	return err
}

// killTree 收掉 ConPTY 子进程的整棵树（cmd→node→…）：taskkill /T /F，
// aiagent 同先例（Process.Kill 只杀头，实测孙进程存活烧网络）。
func (p *Pty) killTree() error {
	select {
	case <-p.exited:
		return nil // 已自然退场，不补刀
	default:
	}
	tk := exec.Command("taskkill", "/PID", fmt.Sprint(p.pid), "/T", "/F")
	tk.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	if out, err := tk.CombinedOutput(); err != nil {
		// taskkill 自己失败不谎报成功；原因拼出来（多半是竞态：刚自然退出）。
		return fmt.Errorf("conpty: 杀进程树失败: %w｜%s", err, firstLine(string(out)))
	}
	return nil
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return strings.TrimSpace(s)
}

// dedupEnvLastWins 重复键后取（os/exec 语义），CreateProcess 原生 first-wins 的
// 怪癖在这道预备工序里抹平。
func dedupEnvLastWins(env []string) []string {
	idx := map[string]int{}
	out := make([]string, 0, len(env))
	for _, kv := range env {
		k := kv
		if i := strings.IndexByte(kv, '='); i >= 0 {
			k = kv[:i]
		}
		if prev, ok := idx[k]; ok {
			out[prev] = kv
			continue
		}
		idx[k] = len(out)
		out = append(out, kv)
	}
	return out
}
