//go:build windows

// Package conpty 封装 Windows 伪控制台（ConPTY）这一件事：起一个"以为自己在
// 真终端里"的子进程，宿主拿到的只是三条管道 + 一个 resize 按钮。
//
// 本包的每条配方都有 2026-10-31 实弹探针背书（证据与失败复现存仓外
// C:/tmp/conpty-spike/：guievidence.txt / run2c-evidence.txt / run3-evidence.txt），
// 并经第二双眼睛的只读审计补严（C:/tmp/conpty-audit-check/：Env 块 panic
// 复现 A 组、Read‖Close 并发 2000 轮 B 组、挂等待关句柄 C 组）：
//
//   - **CREATE_NEW_CONSOLE 与 PSEUDOCONSOLE 互斥且 CreateProcess 不报错**——
//     子进程会静默挂到一个真控制台，ConPTY 管道全程 0 字节、输入全吞。
//     正确的 creationFlags = EXTENDED_STARTUPINFO_PRESENT|CREATE_UNICODE_ENVIRONMENT
//     （0x80400）。这条是探针复现的铁证（run2c）。
//   - **STARTUPINFO 三端必须置 INVALID_HANDLE_VALUE、bInheritHandles=FALSE**
//     （portable-pty 同款姿势）。按 MSDN 样例"继承真实管道句柄"的做法，当宿主
//     stdio 本身是可继承管道（被测试基座/其它程序拉起）时，子进程 std 会抢挂到
//     宿主管道上：字形漏进父 stdout、ConPTY 只见转义噪音、powershell 自杀退出。
//     "首写输入被吞"的假象也是这个竞态的副作用——配方修正后无此现象，
//     宿主侧不需要任何补发逻辑。
//   - HPCON 不是普通句柄：只能 ClosePseudoConsole，绝不能丢进 CloseHandle；
//     且**生命周期止于 ClosePseudoConsole**——关闭后 Resize 就是解引用已释放
//     的 conpty 堆对象（UB，可能崩宿主而非返错），Resize‖Close 必须加锁互斥。
//   - Env/命令行一律走会**报错**的 UTF16 转换族：StringToUTF16Ptr 对含内嵌
//     NUL 的串直接 panic（x/sys v0.47.0 源码白纸黑字；审计坐实旧写法对任何
//     非 nil Env 100% panic，且恰是全包唯一零实测路径——侧巷比主路更容易埋雷）。
//   - Win10 1809 以下无这三个导出：找不到入口点时以 ERROR_PROC_NOT_FOUND 命中
//     哨兵 ErrNoConPTY；其余 HRESULT 不借哨兵误导排障方向。
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
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// procUpdateProcThreadAttribute：kernel32 入口。x/sys 的容器方法
// (ProcThreadAttributeListContainer).Update 签名逼一个 uintptr→unsafe.Pointer
// 转换（go vet unsafeptr 按"伪造指针"拦），句柄本就是整数——直走原生 Call
// 传 uintptr 才是诚实写法。NewLazySystemDLL 与探针/头注纪律同源。
var procUpdateProcThreadAttribute = windows.NewLazySystemDLL("kernel32.dll").NewProc("UpdateProcThreadAttribute")

// Options 一次 spawn 的可选参数。
type Options struct {
	// Cols/Rows 初始尺寸，零值取 80x24。
	Cols, Rows uint16
	// Dir 子进程工作目录，空=继承。
	Dir string
	// Env 完整环境（KEY=VALUE 列表）；nil 继承当前进程。
	// 重复键取后者——os/exec 语义，不跟随 CreateProcess 的 first-wins 怪癖；
	// 含 NUL 项报错不 panic；空串项被滤（空项会在块中途画出"双零终结"，
	// 其后所有项静默蒸发——MSDN 块扫描语义，闸门在组装处关死）。
	Env []string
}

// Pty 一个伪控制台会话。Read/Write 是**原始字节流**（含 VT 转义序列），
// 渲染语义交给前端终端仿真器，本层一概不解析。**必须 Close**：本包无
// finalizer 兜底，忘关即全量泄漏（HPCON/conhost 无自动收口）。
type Pty struct {
	hpc windows.Handle

	inW  *os.File // 宿主写、pty 读
	outR *os.File // pty 写、宿主读

	pid uint32

	// procHandle 关闭权**只归 watchExit**：MSDN 禁止在
	// WaitForSingleObject 挂起期间关掉被等待句柄（文档性 UB：等待不提前
	// 返回、不报错，但其后 GetExitCodeProcess 对已关句柄返错——把非零
	// 退出码静默吞成假阴性，还开窗期内句柄值复用唤醒错对象的风险）。
	procHandle windows.Handle

	closeOnce sync.Once
	exited    chan struct{} // 主进程退出后关闭
	lastErr   error         // 仅在 exited 关闭前写入；读方经 channel 建立 happens-before

	// ctlMu 是 Resize 与 Close 的排他护栏（审计 P1）：HPCON 生命周期止于
	// ClosePseudoConsole，关闭后的 Resize 是对已释放 conpty 堆对象的解引用
	// ——MSDN 明示 UB，**可能崩宿主而非返错**。sync.Once 只防双 Close，
	// 防不了 Resize‖Close，所以是锁+closed 位而不是 flag 一把梭。
	ctlMu  sync.RWMutex
	closed bool
	// killed 自家击杀标记：击杀后的退出码 1 是"我们干的"，不冒充子进程异常。
	killed atomic.Bool
}

// Spawn 起伪控制台并挂上 command + args。
// command 不含参数；argv 经 windows.ComposeCommandLine 转义拼接
// （x/sys 公开件，与 MSVCRT 命令行规则同源）——裸 Join 空格在带空格的
// 绝对路径上必断链（claude 档的 LookPath 路径在便携部署里不是假想风险）。
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

	argv := append([]string{command}, args...)
	cmdLine := windows.ComposeCommandLine(argv)
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
		if errors.Is(err, windows.ERROR_PROC_NOT_FOUND) {
			return fail(fmt.Errorf("conpty: CreatePseudoConsole: %w（%w）", err, ErrNoConPTY))
		}
		return fail(fmt.Errorf("conpty: CreatePseudoConsole 失败: %w", err))
	}
	inR.Close()  // pty 侧端交完即关（EOF 语义的前提）
	outW.Close() // 同上

	attr, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		windows.ClosePseudoConsole(hpc)
		return fail(fmt.Errorf("conpty: 属性表初始化失败: %w", err))
	}
	// PSEUDOCONSOLE 属性传 HPCON 句柄本身而不是 &hpc——探针实证的写法（陷阱清单）。
	// 七参对应 UpdateProcThreadAttribute 的七个形参（最后两个 out 参数传 0）。
	// 少传一个时第 7 参寄存器是脏值：平时侥幸、偶发以离奇 errcode 翻车
	// （主会话实测 errcode=2665982758416 就是一个脏寄存器）——syscall 的
	// 参数个数没有编译器兜底，逐个数是纪律。
	ret, errcode, _ := procUpdateProcThreadAttribute.Call(
		uintptr(unsafe.Pointer(attr.List())), // 真实指针→uintptr：unsafeptr 的合法方向
		0,
		uintptr(windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE),
		uintptr(hpc),
		unsafe.Sizeof(hpc),
		0, // lpPreviousValue
		0, // lpSizeReturn
	)
	if ret == 0 {
		attr.Delete()
		windows.ClosePseudoConsole(hpc)
		return fail(fmt.Errorf("conpty: 属性表挂 ConPTY 失败 (errcode=%d)", errcode))
	}
	defer attr.Delete() // CreateProcess 成功后列表所有权即完成使命（随进程语义）

	envBlock, err := buildEnvBlock(opts.Env)
	if err != nil {
		return fail(err)
	}
	var dirPtr *uint16
	if opts.Dir != "" {
		if dirPtr, err = windows.UTF16PtrFromString(opts.Dir); err != nil {
			return fail(fmt.Errorf("conpty: 工作目录名非法: %w", err))
		}
	}
	exePtr, err := windows.UTF16PtrFromString(cmdLine)
	if err != nil {
		return fail(fmt.Errorf("conpty: 命令行含非法字符: %w", err))
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
		nil, exePtr, nil, nil,
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
		pid:        pi.ProcessId,
		procHandle: pi.Process,
		exited:     make(chan struct{}),
	}
	go p.watchExit()
	return p, nil
}

// watchExit 四步同一人：等退场 → 读退出码 → 关进程句柄 → 关 exited。
// CloseHandle 归此处而非 Close 的理由见结构体注释（审计 P2：把"等待中
// 关句柄"的文档性 UB 与退出码假阴性一次根除）。自家击杀后 code=1 属
// 预期形态，由 killed 位甄别，不冒充子进程异常。
func (p *Pty) watchExit() {
	_, _ = windows.WaitForSingleObject(p.procHandle, windows.INFINITE)
	var code uint32
	err := windows.GetExitCodeProcess(p.procHandle, &code)
	windows.CloseHandle(p.procHandle)
	if err == nil && code != 0 && !p.killed.Load() {
		p.lastErr = fmt.Errorf("conpty: 子进程退出码 %d", code)
	}
	close(p.exited)
}

// Read 读 pty 输出流（原始字节，含 VT 序列）。子进程**自然退场**后返回
// io.EOF；经 Close 收场时返回 os.ErrClosed 族（"file already closed"——
// 审计实测 2000/2000 轮确定性行为，不卡死不 panic）。调用方把两者都当
// "流结束了"处理，别只认 EOF。Close 瞬间管内未读尾部字节会被丢（末屏
// 可能缺最后几十字节，v0 已知代价）。
func (p *Pty) Read(b []byte) (int, error) { return p.outR.Read(b) }

// Write 写 pty 输入流。行节流由上层（UI 键位）决定，本层不加工字节。
func (p *Pty) Write(b []byte) (int, error) { return p.inW.Write(b) }

// Resize 调整伪控制台尺寸。两条纪律：
//   - resize 紧贴启动会被 conpty 吞（社区实证 quirk；wezterm 用
//     RESIZE_QUIRK 旗标换服务端容忍，本包 flags=0，**防抖归调用方**）；
//   - 与 Close 互斥（护栏）：ClosePseudoConsole 之后碰 hpc 是宿主级
//     崩溃面，这里拒绝并报明确错，而不是"多半没事"。
func (p *Pty) Resize(cols, rows uint16) error {
	if cols == 0 || rows == 0 {
		return errors.New("conpty: 尺寸不能为 0")
	}
	p.ctlMu.RLock()
	defer p.ctlMu.RUnlock()
	if p.closed {
		return errors.New("conpty: 会话已关闭，resize 拒绝（hpc 用后释放是崩溃面，护栏而非报错）")
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

// Err 子进程**异常**退场的错误（自家击杀不计；进程仍活着时为 nil）。
func (p *Pty) Err() error {
	select {
	case <-p.exited:
		return p.lastErr
	default:
		return nil
	}
}

// Close 收场：立击杀牌 → 杀进程树 → 等退场 → （锁内）置 closed、
// ClosePseudoConsole、关宿主管道。幂等；第二次 Close 返回 nil。
// 顺序不能换：管道先关会让 conpty 带着未退的子进程悬着；procHandle
// 不在这里关（所有权在 watchExit）。
func (p *Pty) Close() error {
	var err error
	p.closeOnce.Do(func() {
		p.killed.Store(true)
		err = p.killTree()
		select {
		case <-p.exited:
		case <-time.After(3 * time.Second): // 兜底上限：taskkill /F 正常秒回，等满即认卡死
			// 卡死路径不抢关 procHandle——watchExit 终会收柄；这里只推进 hpc/管道收口。
		}
		p.ctlMu.Lock()
		p.closed = true
		windows.ClosePseudoConsole(p.hpc)
		p.inW.Close()
		p.outR.Close()
		p.ctlMu.Unlock()
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
		// exit 128 = "找不到进程"：自然退出与击杀的竞态常态，降级 nil；
		// 其余失败不谎报成功，原因拼出来。
		var ee *exec.ExitError
		if errors.As(err, &ee) && ee.ExitCode() == 128 {
			return nil
		}
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

// buildEnvBlock 组 CreateProcess 的 UTF-16 环境块：逐项 UTF16FromString
// （自带终止 0；内嵌 NUL 返 EINVAL 上抛，绝不经 StringToUTF16Ptr 的 panic
// 路径），块尾再补一个 0 画终结空串。nil=继承（返回 nil 指针）；空串项滤除
// ——块中途的空串=提前终结，其后各项静默蒸发（MSDN 块扫描语义）。
func buildEnvBlock(env []string) (*uint16, error) {
	if env == nil {
		return nil, nil
	}
	var total []uint16
	for _, kv := range dedupEnvLastWins(env) {
		if kv == "" {
			continue
		}
		u, err := windows.UTF16FromString(kv)
		if err != nil {
			return nil, fmt.Errorf("conpty: 环境变量含非法 NUL: %q", kv)
		}
		total = append(total, u...)
	}
	total = append(total, 0)
	return &total[0], nil
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
