// conpty-spike: ConPTY 实弹探针（仓外验证，hanxi 内嵌终端生死前提）。
//
// 手写 FFI 路线（不引入任何第三方 conpty 库）：
//   - kernel32!CreatePseudoConsole / ResizePseudoConsole / ClosePseudoConsole（NewLazySystemDLL）
//   - InitializeProcThreadAttributeList + UpdateProcThreadAttribute(PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE)
//   - STARTUPINFOEXW + CreateProcess(EXTENDED_STARTUPINFO_PRESENT [| CREATE_NEW_CONSOLE])
//   - 输入/输出用 os.Pipe；子进程/conhost 侧句柄(inR/outW)置继承位，
//     CreateProcess(bInheritHandles=TRUE) 时 conhost 才能拿到管道两端（MSDN ConsolePTY 样例要点）。
//
// 关键验证点：-H windowsgui（GUI 子系统、无控制台）进程内整套是否仍然跑通。
package main

import (
	"encoding/binary"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ---------- 手写 kernel32 FFI（仅 ConPTY 三件套与属性列表——x/sys 未覆盖的部分） ----------

var (
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")

	procCreatePseudoConsole               = kernel32.NewProc("CreatePseudoConsole")
	procResizePseudoConsole               = kernel32.NewProc("ResizePseudoConsole")
	procClosePseudoConsole                = kernel32.NewProc("ClosePseudoConsole")
	procInitializeProcThreadAttributeList = kernel32.NewProc("InitializeProcThreadAttributeList")
	procUpdateProcThreadAttribute         = kernel32.NewProc("UpdateProcThreadAttribute")
	procDeleteProcThreadAttributeList     = kernel32.NewProc("DeleteProcThreadAttributeList")
	procGetConsoleWindow                  = kernel32.NewProc("GetConsoleWindow")
)

const (
	procThreadAttributePseudoConsole = 0x00020016 // winbase.h: PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE
	extendedStartupInfoPresent       = 0x00080000 // EXTENDED_STARTUPINFO_PRESENT
	createNewConsole                 = 0x00000010
	createUnicodeEnvironment         = 0x00000400
	waitObject0                      = 0x00000000
	waitTimeout                      = 0x00000102
	stillActive                      = 259
)

// packCoord 把 (cols, rows) 打包成 COORD（两个 int16）经 Syscall 传参。
func packCoord(cols, rows int) uintptr {
	return uintptr(uint32(uint16(cols)) | uint32(uint16(rows))<<16)
}

// createPseudoConsole: CreatePseudoConsole(COORD size, HANDLE hInput(读端), HANDLE hOutput(写端),
// DWORD dwFlags, HPCON *phPC)。返回 HRESULT——非 0 即失败。
func createPseudoConsole(cols, rows int, inR, outW windows.Handle, ptyFlags uint32) (uintptr, error) {
	var hpc uintptr
	r, _, err := procCreatePseudoConsole.Call(
		packCoord(cols, rows), uintptr(inR), uintptr(outW), uintptr(ptyFlags),
		uintptr(unsafe.Pointer(&hpc)))
	if r != 0 {
		return 0, fmt.Errorf("CreatePseudoConsole HRESULT=0x%08X (gle=%d)", uint32(r), err)
	}
	if hpc == 0 {
		return 0, fmt.Errorf("CreatePseudoConsole 成功但 hPC=0")
	}
	return hpc, nil
}

func resizePseudoConsole(hpc uintptr, cols, rows int) error {
	r, _, err := procResizePseudoConsole.Call(hpc, packCoord(cols, rows))
	if r != 0 {
		return fmt.Errorf("ResizePseudoConsole HRESULT=0x%08X (gle=%d)", uint32(r), err)
	}
	return nil
}

// attrList 承载手工初始化的 PROC_THREAD_ATTRIBUTE_LIST。
type attrList struct {
	buf  []byte
	list *windows.ProcThreadAttributeList
}

func newAttrList(hpc uintptr) (*attrList, error) {
	var size uintptr
	// 第一次调用传 NULL 仅探测所需字节数（必返回 FALSE，忽略其错误）
	procInitializeProcThreadAttributeList.Call(0, 1, 0, uintptr(unsafe.Pointer(&size)))
	if size == 0 {
		return nil, fmt.Errorf("InitializeProcThreadAttributeList 探测尺寸得到 0")
	}
	a := &attrList{buf: make([]byte, size)}
	r, _, err := procInitializeProcThreadAttributeList.Call(
		uintptr(unsafe.Pointer(&a.buf[0])), 1, 0, uintptr(unsafe.Pointer(&size)))
	if r == 0 {
		return nil, fmt.Errorf("InitializeProcThreadAttributeList 失败 gle=%d", err)
	}
	a.list = (*windows.ProcThreadAttributeList)(unsafe.Pointer(&a.buf[0]))
	// MSDN ConsolePTY 样例要点：PSEUDOCONSOLE 属性值就是 HPCON 句柄本身（不是 &hPC）
	r, _, err = procUpdateProcThreadAttribute.Call(
		uintptr(unsafe.Pointer(&a.buf[0])), 0, procThreadAttributePseudoConsole,
		hpc, unsafe.Sizeof(hpc), 0, 0)
	if r == 0 {
		return nil, fmt.Errorf("UpdateProcThreadAttribute(PSEUDOCONSOLE) 失败 gle=%d", err)
	}
	return a, nil
}

func (a *attrList) destroy() {
	if a != nil && a.list != nil {
		procDeleteProcThreadAttributeList.Call(uintptr(unsafe.Pointer(a.list)))
	}
}

// ---------- 日志 / 判定 / 输出累积 ----------

type logger struct {
	mu     sync.Mutex
	lines  []string
	start  time.Time
	logF   *os.File
	mirror bool
}

func (l *logger) f(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	line := fmt.Sprintf("[%s elapsed=%6.2fs] %s",
		time.Now().Format("15:04:05.000"), time.Since(l.start).Seconds(), msg)
	l.mu.Lock()
	l.lines = append(l.lines, line)
	l.mu.Unlock()
	if l.logF != nil {
		l.logF.WriteString(line + "\n")
	}
	if l.mirror {
		fmt.Fprintln(os.Stdout, line)
	}
}

// collector 累积 ConPTY 输出原始字节（ANSI 原样保存）。
type collector struct {
	mu  sync.Mutex
	buf []byte
}

func (c *collector) append(b []byte) {
	c.mu.Lock()
	c.buf = append(c.buf, b...)
	c.mu.Unlock()
}

func (c *collector) length() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.buf)
}

func (c *collector) slice(from, to int) []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	if to > len(c.buf) {
		to = len(c.buf)
	}
	if from > to {
		from = to
	}
	out := make([]byte, to-from)
	copy(out, c.buf[from:to])
	return out
}

type verdict struct {
	name string
	ok   bool
	msg  string
}

// ---------- 主流程 ----------

func main() {
	childName := flag.String("child", "powershell", "powershell|claude")
	tag := flag.String("tag", "run", "运行标签（决定 spike-<tag>.log 等默认文件名）")
	evidencePath := flag.String("evidence", "guievidence.txt", "证据文件路径")
	rawPath := flag.String("raw", "", "ConPTY 原始输出转储路径（默认 raw-<tag>.log）")
	flagsOverride := flag.Uint("creation-flags", extendedStartupInfoPresent|createUnicodeEnvironment,
		"CreateProcess creationFlags（默认 0x80400=EXTENDED|UNICODE_ENV；0x80010 实测断链）")
	ptyFlags := flag.Uint("pty-flags", 0, "CreatePseudoConsole dwFlags（0=标准；wezterm 用 7=INHERIT_CURSOR|RESIZE_QUIRK|WIN32_INPUT_MODE）")
	claudeWait := flag.Int("claude-wait-sec", 10, "claude 模式观察 TUI 输出的秒数，随后 taskkill /T /F")
	mirror := flag.Bool("mirror-stdout", true, "日志同步镜像到 stdout（GUI build 下无害）")
	flag.Parse()

	if *rawPath == "" {
		*rawPath = "raw-" + *tag + ".log"
	}
	logF, _ := os.OpenFile("spike-"+*tag+".log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	lg := &logger{start: time.Now(), logF: logF, mirror: *mirror}

	exe, _ := os.Executable()
	cw, _, _ := procGetConsoleWindow.Call()
	lg.f("==== conpty-spike 开始 child=%s tag=%s ====", *childName, *tag)
	lg.f("自身 PE 子系统: %s（2=GUI/windowsgui 3=CUI/console）；GetConsoleWindow=0x%X", peSubsystem(exe), cw)
	lg.f("exe=%s pid=%d go=%s GetConsoleWindow=0x%X（注意：harness 基座本身无控制台窗口，此项 0 不足以证明子系统，以 PE 子系统字段为准）",
		exe, os.Getpid(), runtime.Version(), cw)
	lg.f("creationFlags 首选=0x%X", *flagsOverride)

	var subs []verdict
	okStep := func(name, msg string) {
		subs = append(subs, verdict{name, true, msg})
		lg.f("OK   %s: %s", name, msg)
	}
	badStep := func(name, msg string) {
		subs = append(subs, verdict{name, false, msg})
		lg.f("FAIL %s: %s", name, msg)
	}

	finalize := func(code int) {
		overall := "PASS"
		for _, v := range subs {
			if !v.ok {
				overall = "FAIL"
			}
		}
		if code != 0 && overall == "PASS" {
			overall = "FAIL"
		}
		lines := []string{
			fmt.Sprintf("=== conpty-spike evidence tag=%s child=%s start=%s ===",
				*tag, *childName, lg.start.Format(time.RFC3339)),
			fmt.Sprintf("exe=%s pid=%d go=%s subsystem=%s GetConsoleWindow=0x%X",
				exe, os.Getpid(), runtime.Version(), peSubsystem(exe), cw),
			"", "-- 时间戳日志 --",
		}
		lines = append(lines, lg.lines...)
		lines = append(lines, "", "-- 步骤判定 --")
		for _, v := range subs {
			st := "PASS"
			if !v.ok {
				st = "FAIL"
			}
			lines = append(lines, fmt.Sprintf("[%s] %s: %s", st, v.name, v.msg))
		}
		lines = append(lines, fmt.Sprintf("VERDICT=%s EXITCODE=%d", overall, code))
		lines = append(lines, fmt.Sprintf("DONE %s", time.Now().Format(time.RFC3339Nano)))
		if err := os.WriteFile(*evidencePath, []byte(strings.Join(lines, "\n")+"\n"), 0644); err != nil {
			lg.f("写证据文件失败: %v", err)
		}
		if logF != nil {
			logF.Close()
		}
		os.Exit(code)
	}

	// 1) 匿名管道两对：input(父写→conpty 读)、output(conpty 写→父读)
	inR, inW, err := os.Pipe()
	if err != nil {
		badStep("os.Pipe(input)", err.Error())
		finalize(2)
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		badStep("os.Pipe(output)", err.Error())
		finalize(2)
	}
	hInR, hInW := windows.Handle(inR.Fd()), windows.Handle(inW.Fd())
	hOutR, hOutW := windows.Handle(outR.Fd()), windows.Handle(outW.Fd())
	// portable-pty 配方：bInheritHandles=FALSE，句柄继承位一律不用——
	// conhost/conpty 经由 HPCON 属性拿管道，子进程 std 端由伪终端控制台提供。
	okStep("pipes", fmt.Sprintf("inR=%d inW=%d outR=%d outW=%d（不依赖继承位）", hInR, hInW, hOutR, hOutW))

	// 2) CreatePseudoConsole(100x30)：hInput=inR（conpty 从读端取输入），hOutput=outW（conpty 往写端吐渲染）
	hPC, err := createPseudoConsole(100, 30, hInR, hOutW, uint32(*ptyFlags))
	if err != nil {
		badStep("CreatePseudoConsole(100x30)", err.Error())
		finalize(3)
	}
	okStep("CreatePseudoConsole", fmt.Sprintf("hPC=0x%X (100x30)", hPC))

	// 3) PROC_THREAD_ATTRIBUTE_LIST + STARTUPINFOEXW
	al, err := newAttrList(hPC)
	if err != nil {
		badStep("ProcThreadAttributeList", err.Error())
		procClosePseudoConsole.Call(hPC)
		finalize(3)
	}
	siEx := &windows.StartupInfoEx{}
	siEx.Cb = uint32(unsafe.Sizeof(*siEx))
	// portable-pty(wezterm) 生产配方：STARTF_USESTDHANDLES + 三端 INVALID_HANDLE_VALUE。
	// 关键注释（原文）：Explicitly set the stdio handles as invalid handles otherwise
	// we can end up with a weird state where the spawned process can inherit the
	// explicitly redirected output handles from its parent. —— 即防"子进程偷用父进程 stdio
	// 导致 ConPTY 渲染被父 stderr 吞掉/写串"，这正是本探针此前看到 0 字节的根因。
	siEx.Flags = windows.STARTF_USESTDHANDLES
	siEx.StdInput = windows.InvalidHandle
	siEx.StdOutput = windows.InvalidHandle
	siEx.StdErr = windows.InvalidHandle
	siEx.ProcThreadAttributeList = al.list
	lg.f("startupinfo 配方: portable-pty(USESTDHANDLES+三端 INVALID)，ptyFlags=0x%X", *ptyFlags)

	var cmdlineStr string
	switch *childName {
	case "powershell":
		cmdlineStr = `powershell.exe -NoLogo -NoProfile`
	case "cmd":
		cmdlineStr = `cmd.exe /K`
	case "claude":
		// .cmd 批处理必须经 cmd.exe /c 解释
		cmdlineStr = `cmd.exe /c "C:\Users\Administrator\AppData\Roaming\npm\claude.cmd"`
	default:
		badStep("child", "未知 child: "+*childName)
		finalize(1)
	}
	cmdU16 := windows.StringToUTF16(cmdlineStr)

	// 4) CreateProcess。
	//    portable-pty 权威配方：bInheritHandles=FALSE + creationFlags=
	//    EXTENDED_STARTUPINFO_PRESENT|CREATE_UNICODE_ENVIRONMENT。
	//    任务书点名的 CREATE_NEW_CONSOLE(0x10) 实测与 PSEUDOCONSOLE 冲突——
	//    它让子进程另起真控制台、抢走渲染输出，故默认不加；用 --creation-flags 可强行加回复现该坑。
	tryFlags := []uint32{uint32(*flagsOverride)}
	if *flagsOverride&createNewConsole != 0 {
		tryFlags = append(tryFlags, extendedStartupInfoPresent|createUnicodeEnvironment)
	}
	var pi windows.ProcessInformation
	spawnOK := false
	usedFlags := uint32(0)
	for i, fl := range tryFlags {
		// bInheritHandles=false：配合三端 INVALID 的标准句柄设置，避免父 stdio 泄露进子进程
		err := windows.CreateProcess(nil, &cmdU16[0], nil, nil, false, fl, nil, nil, &siEx.StartupInfo, &pi)
		if err == nil {
			usedFlags = fl
			spawnOK = true
			pid, _ := windows.GetProcessId(pi.Process)
			if i > 0 {
				lg.f("PITFALL: 首选 flags=0x%X 失败，回退 0x%X 成功——CREATE_NEW_CONSOLE 与 PSEUDOCONSOLE 冲突", tryFlags[0], fl)
			}
			okStep("CreateProcess", fmt.Sprintf("flags=0x%X child_pid=%d inheritHandles=false cmdline=%q", fl, pid, cmdlineStr))
			break
		}
		lg.f("CreateProcess flags=0x%X 失败: %v", fl, err)
	}
	if !spawnOK {
		badStep("CreateProcess", fmt.Sprintf("含回退共 %d 次尝试均失败，见日志 (GetLastError=%d)", len(tryFlags), windows.GetLastError()))
		procClosePseudoConsole.Call(hPC)
		al.destroy()
		finalize(4)
	}
	childPID, _ := windows.GetProcessId(pi.Process)
	runtime.KeepAlive(al) // 属性列表缓冲须活到 CreateProcess 返回之后

	// 5) 读取协程：outR → collector + 原始字节转储
	col := &collector{}
	rawF, err := os.Create(*rawPath)
	if err != nil {
		lg.f("打开原始日志 %s 失败: %v（继续但不转储）", *rawPath, err)
		rawF = nil
	}
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		buf := make([]byte, 8192)
		for {
			n, rerr := outR.Read(buf)
			if n > 0 {
				col.append(buf[:n])
				if rawF != nil {
					rawF.Write(buf[:n])
				}
			}
			if rerr != nil {
				lg.f("输出读取结束: %v（累计 %d 字节）", rerr, col.length())
				return
			}
		}
	}()

	// 6) 功能序列
	seqOK := true
	switch *childName {
	case "powershell", "cmd":
		seqOK = shellSequence(lg, col, inW, hPC, pi, &subs, usedFlags, *tag, *childName)
	case "claude":
		seqOK = claudeSequence(lg, col, hPC, pi, childPID, *claudeWait, &subs, *tag)
	}
	if !seqOK {
		lg.f("功能序列未全绿（详见判定表）")
	}

	// 7) 拆除：ClosePseudoConsole → DeleteProcThreadAttributeList → 关句柄/文件
	procClosePseudoConsole.Call(hPC)
	lg.f("ClosePseudoConsole(hPC=0x%X) 完成", hPC)
	al.destroy()
	inW.Close()
	inR.Close()
	outW.Close()
	select {
	case <-readerDone:
	case <-time.After(5 * time.Second):
		lg.f("WARN reader 协程 5s 未退出，强关 outR")
		outR.Close()
	}
	if rawF != nil {
		rawF.Close()
	}
	windows.CloseHandle(pi.Thread)
	windows.CloseHandle(pi.Process)
	lg.f("==== 结束 seqOK=%v ====", seqOK)

	code := 0
	if !seqOK {
		code = 5
	}
	finalize(code)
}

// waitQuiet 事件驱动：轮询输出直到“quietNeed 时间内不再增长”或达到 max。返回最终字节数。
func waitQuiet(col *collector, max, quietNeed time.Duration) int {
	deadline := time.Now().Add(max)
	lastLen, lastGrow := col.length(), time.Now()
	for {
		time.Sleep(300 * time.Millisecond)
		if n := col.length(); n != lastLen {
			lastLen, lastGrow = n, time.Now()
		}
		if time.Since(lastGrow) >= quietNeed {
			return lastLen
		}
		if time.Now().After(deadline) {
			return lastLen
		}
	}
}

// shellSequence 任务书第 3 步功能序列（事件驱动取证）。child 支持 powershell/cmd（cmd 作 PSReadLine 对照组）。
func shellSequence(lg *logger, col *collector, inW *os.File, hPC uintptr,
	pi windows.ProcessInformation, subs *[]verdict, usedFlags uint32, tagStr, childName string) bool {
	ok := true
	add := func(name string, pass bool, msg string) {
		*subs = append(*subs, verdict{name, pass, msg})
		if pass {
			lg.f("OK   %s: %s", name, msg)
		} else {
			lg.f("FAIL %s: %s", name, msg)
			ok = false
		}
	}
	cmd1, cmd2, expect1 := "Get-Date", "cmd /c mode", "202"
	if childName == "cmd" {
		cmd1, cmd2, expect1 = "echo SPIKE_ECHO_123", "mode con", "SPIKE_ECHO_123"
	}

	// 等 powershell 起来出 banner/提示符（最多 8s，静默 1.5s 视为就绪）
	n0 := waitQuiet(col, 8*time.Second, 1500*time.Millisecond)
	add("startup-output", n0 > 0, fmt.Sprintf("启动期收到 %d 字节（banner/提示符）；creationFlags=0x%X", n0, usedFlags))
	if n0 == 0 && usedFlags&createNewConsole != 0 {
		lg.f("WARN 0 字节且用了 CREATE_NEW_CONSOLE——疑似子进程挂到了真控制台窗口而非 ConPTY")
	}

	// 输入预热：先打一个空回车，逼出提示符重绘，验证输入通道存活（吞首写竞态的保险）
	warm := writeLineSync(lg, col, inW, "")
	waitQuiet(col, 4*time.Second, 1000*time.Millisecond)
	lg.f("输入预热(空回车) 首写生效=%v", warm)

	// 首条命令
	firstOK := writeLineSync(lg, col, inW, cmd1)
	lg.f("%s 首写生效=%v", cmd1, firstOK)
	n1 := waitQuiet(col, 8*time.Second, 1500*time.Millisecond)
	seg1 := col.slice(n0, n1)
	os.WriteFile("out1-"+tagStr+".log", seg1, 0644)
	vis1 := stripANSI(seg1)
	add("Get-Date→输出", len(seg1) > 0 && strings.Contains(vis1, expect1),
		fmt.Sprintf("out1-%s.log=%dB 含标记%q=%v；可读摘录: %q", tagStr, len(seg1), expect1,
			strings.Contains(vis1, expect1), clip(collapse(vis1), 160)))

	// Resize 120x40 → cmd /c mode 观察不崩
	if err := resizePseudoConsole(hPC, 120, 40); err != nil {
		add("ResizePseudoConsole(120x40)", false, err.Error())
	} else {
		add("ResizePseudoConsole(120x40)", true, "HRESULT=0")
	}
	writeLineSync(lg, col, inW, cmd2)
	n2 := waitQuiet(col, 8*time.Second, 1500*time.Millisecond)
	seg2 := col.slice(n1, n2)
	os.WriteFile("out2-"+tagStr+".log", seg2, 0644)
	vis2 := stripANSI(seg2)
	code := mustExitCode(pi.Process)
	add("resize 后存活且 mode 有输出", code == stillActive && len(seg2) > 0,
		fmt.Sprintf("exitcode=%d(259=运行中) out2-%s.log=%dB 含120列痕迹=%v；摘录: %q",
			code, tagStr, len(seg2), strings.Contains(vis2, "120"), clip(collapse(vis2), 200)))

	// exit → 等待子进程自然退出（可靠写入自带补发）
	exitFirstOK := writeLineSync(lg, col, inW, "exit")
	w, _ := windows.WaitForSingleObject(pi.Process, 8000)
	if w != waitObject0 {
		add("powershell exit", false, fmt.Sprintf("exit(首写生效=%v) 补等 8s 仍未退出 WaitForSingleObject=%d", exitFirstOK, w))
		out, _ := exec.Command("taskkill.exe", "/T", "/F", "/PID", fmt.Sprint(mustPID(pi.Process))).CombinedOutput()
		lg.f("taskkill 兜底: %s", clip(collapse(string(out)), 120))
	} else {
		ec := mustExitCode(pi.Process)
		add("powershell exit", ec == 0, fmt.Sprintf("正常退出 exitcode=%d（首写生效=%v）", ec, exitFirstOK))
	}
	return ok
}

// claudeSequence 尽力项：claude.cmd 裸起交互 TUI，N 秒后 taskkill /T /F 清理。
func claudeSequence(lg *logger, col *collector, hPC uintptr,
	pi windows.ProcessInformation, childPID uint32, waitSec int, subs *[]verdict, tagStr string) bool {
	ok := true
	add := func(name string, pass bool, msg string) {
		*subs = append(*subs, verdict{name, pass, msg})
		if pass {
			lg.f("OK   %s: %s", name, msg)
		} else {
			lg.f("FAIL %s: %s", name, msg)
			ok = false
		}
	}

	lg.f("claude 模式：观察 %ds（不发任何输入）", waitSec)
	time.Sleep(time.Duration(waitSec) * time.Second)
	n := col.length()
	data := col.slice(0, n)
	os.WriteFile("claude-out-"+tagStr+".log", data, 0644)
	escOff, hasESC := findESCLB(data)
	add("claude TUI 字节", n > 0 && hasESC,
		fmt.Sprintf("claude-out-%s.log=%dB 1b5b(ESC[)首现@%d 存在=%v", tagStr, n, escOff, hasESC))
	if hasESC {
		lg.f("ESC 前 64 字节 hex: %s", hex.EncodeToString(head(data, 64)))
	}
	if n == 0 {
		lg.f("WARN claude 无任何输出（cmd 壳未拉起/依赖缺失/启动太慢）")
	}

	out, terr := exec.Command("taskkill.exe", "/T", "/F", "/PID", fmt.Sprint(childPID)).CombinedOutput()
	lg.f("taskkill /T /F /PID %d → err=%v 输出=%q", childPID, terr, clip(collapse(string(out)), 200))
	w, _ := windows.WaitForSingleObject(pi.Process, 10000)
	ec := mustExitCode(pi.Process)
	add("claude 清理后退出", w == waitObject0, fmt.Sprintf("Wait=%d exitcode=%d（1/0xC000013A 等=被强杀属预期）", w, ec))
	time.Sleep(1 * time.Second) // 给 conhost/conpty 收尾
	return ok
}

// ---------- 小助手 ----------

func writeLine(lg *logger, w *os.File, s string) {
	_, err := w.Write([]byte(s + "\r\n"))
	if err != nil {
		lg.f("WARN 写输入 %q 失败: %v", s, err)
		return
	}
	lg.f("已写入: %s\\r\\n", s)
}

// writeLineSync 可靠写入：写后看输出有无动静；3s 无任何新字节则视为被
// ConPTY 输入服务吞掉（实测静默窗口后的首写必丢），补发一次。
// 返回是否首写即生效。
func writeLineSync(lg *logger, col *collector, w *os.File, s string) bool {
	time.Sleep(300 * time.Millisecond) // 稳定化延迟
	mark := col.length()
	writeLine(lg, w, s)
	for i := 0; i < 10; i++ {
		time.Sleep(300 * time.Millisecond)
		if col.length() > mark {
			return true
		}
	}
	lg.f("WARN 首写 %q 后 3s 输出零动静——ConPTY 吞输入，补发一次", s)
	writeLine(lg, w, s)
	return false
}

func mustExitCode(h windows.Handle) uint32 {
	var c uint32
	if err := windows.GetExitCodeProcess(h, &c); err != nil {
		return 0xFFFF
	}
	return c
}

func mustPID(h windows.Handle) uint32 {
	id, err := windows.GetProcessId(h)
	if err != nil {
		return 0
	}
	return id
}

// peSubsystem 读自身 PE 头 OptionalHeader.Subsystem（文件偏移 = e_lfanew+92）。
// 2=IMAGE_SUBSYSTEM_WINDOWS_GUI（-H windowsgui 产物），3=WINDOWS_CUI。
// 这是 GUI 子系统证据的硬指标，不受启动器是否有控制台窗口影响。
func peSubsystem(path string) string {
	b, err := os.ReadFile(path)
	if err != nil || len(b) < 128 {
		return "读取失败:" + fmt.Sprint(err)
	}
	peOff := int(b[0x3C]) | int(b[0x3D])<<8 | int(b[0x3E])<<16 | int(b[0x3F])<<24
	if peOff+94 > len(b) || b[peOff] != 'P' || b[peOff+1] != 'E' {
		return "PE头异常"
	}
	sub := binary.LittleEndian.Uint16(b[peOff+4+20+68:])
	switch sub {
	case 2:
		return "2(GUI)"
	case 3:
		return "3(CUI)"
	default:
		return fmt.Sprintf("%d(其他)", sub)
	}
}

func findESCLB(b []byte) (int, bool) {
	for i := 0; i+1 < len(b); i++ {
		if b[i] == 0x1b && b[i+1] == 0x5b {
			return i, true
		}
	}
	return -1, false
}

// stripANSI 仅用于可读性摘要（证据正文保存原始 ANSI 字节）。
// 规则：CSI(ESC [ ... 最终字节 0x40-0x7E)、OSC(ESC ] ... BEL/ESC)、其余双字符 ESC 序列一律吞掉。
func stripANSI(b []byte) string {
	s := string(b)
	var sb strings.Builder
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == 0x1b:
			i++
			if i < len(s) && s[i] == '[' {
				i++
				for i < len(s) && !(s[i] >= 0x40 && s[i] <= 0x7e) {
					i++
				}
				if i < len(s) {
					i++
				}
			} else if i < len(s) && s[i] == ']' {
				i++
				for i < len(s) && s[i] != 0x07 && s[i] != 0x1b {
					i++
				}
				if i < len(s) {
					i++
				}
			} else if i < len(s) {
				i++
			}
		case c == '\r' || (c < 0x20 && c != '\n' && c != '\t'):
			i++
		default:
			sb.WriteByte(c)
			i++
		}
	}
	return sb.String()
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func collapse(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func head(b []byte, n int) []byte {
	if len(b) < n {
		n = len(b)
	}
	return b[:n]
}
