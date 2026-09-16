// hanxi 是 Hanxi 的可执行入口。
// 包含正常 GUI 模式与 UAC 提权 Helper 模式。
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"golang.org/x/sys/windows"

	embedassets "hanxi" // 根目录包：承载 //go:embed all:frontend/dist
	"hanxi/internal/app"
)

func main() {
	modeFlag := flag.String("mode", "", "run mode: empty for GUI, 'killhelper' for elevated process terminator")
	pidFlag := flag.Uint("pid", 0, "target PID for killhelper mode")
	minimizedFlag := flag.Bool("minimized", false, "start with the main window hidden in the system tray")
	takeoverFlag := flag.Uint("takeover", 0, "elevated restart handoff: wait for this PID (the old instance) to release the single-instance lock")
	routeFlag := flag.String("route", "", "elevated restart handoff: frontend route to open after start (e.g. /ext/bcu)")
	killExeFlag := flag.String("exe", "", "killhelper identity recheck: expected executable path of the target (empty = skip path check)")
	killStartFlag := flag.Int64("start", 0, "killhelper identity recheck: expected target creation time in UNIX nanoseconds (0 = skip)")
	flag.Parse()

	// 1. 如果是 UAC 提权 Helper 模式，以极简逻辑执行并退出
	if *modeFlag == "killhelper" {
		runKillHelper(uint32(*pidFlag), *killExeFlag, *killStartFlag)
		return
	}

	// 2. 正常 GUI 主程序模式
	// 软内存上限: 堆占用超过 256MB 时 GC 更激进地把空闲页归还给操作系统。
	// Go 默认行为是堆页几乎不主动归还, 长时间运行 (frpc 联调/大文件快传等)
	// 后任务管理器中的内存占用会只涨不降; 该上限为软限制, 超限仅提高 GC 频率不 OOM。
	debug.SetMemoryLimit(256 << 20)

	app.RegisterEvents()

	a, cleanup := app.New(application.AssetOptions{
		Handler: application.AssetFileServerFS(embedassets.FS),
	}, app.Options{
		StartMinimized: *minimizedFlag,
		TakeoverPID:    uint32(*takeoverFlag),
		InitialRoute:   app.SanitizeRoute(*routeFlag),
	})
	defer cleanup()

	if err := a.Run(); err != nil {
		log.Fatal(err)
	}
}

// killHelperExitXxx 是提权查杀 helper 与 portkill 主进程约定好的退出码契约：
// 主进程经 Start-Process -PassThru 取回 ExitCode 并映射为用户可读文案。
// 1=通用失败（打不开/查不了/终止失败），2=命中系统保护红线拒绝，3=身份复核不匹配（PID 复用/进程已换）。
const (
	killHelperExitFailed    = 1
	killHelperExitProtected = 2
	killHelperExitMismatch  = 3
)

// criticalSystemProcs 系统关键进程名红线（与 platform/windows.ProcessImpl.IsProtected 同名同表，
// 修改必须两处同步）：提权 helper 拥有最高权限，即便上游参数被伪造或 PID 已被复用，
// 也绝不把刀口对准认证/会话子系统。
var criticalSystemProcs = map[string]bool{
	"csrss.exe": true, "wininit.exe": true, "services.exe": true,
	"lsass.exe": true, "smss.exe": true, "svchost.exe": true,
}

// runKillHelper 以管理员提权权限终止目标进程。
// 除 PID 红线外执行两道身份复核：①实时镜像路径命中关键进程名即拒杀；
// ②若调用方传入 -exe/-start 期望指纹（UAC 弹窗前的快照），逐一比对防"等待授权期间
// 原进程退出、PID 被复用"造成的误杀——比对不过宁可拒杀（退出码 3），由主进程提示重试。
func runKillHelper(pid uint32, expectExe string, expectStart int64) {
	if pid == 0 || pid == 4 || pid == uint32(os.Getpid()) {
		fmt.Fprintf(os.Stderr, "Protected system process PID %d cannot be killed\n", pid)
		os.Exit(killHelperExitProtected)
	}

	hProc, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.PROCESS_TERMINATE, false, pid)
	if err != nil {
		fmt.Fprintf(os.Stderr, "OpenProcess for PID %d failed: %v\n", pid, err)
		os.Exit(killHelperExitFailed)
	}
	defer windows.CloseHandle(hProc)

	// 复核一：镜像路径查询失败（受限句柄读不到路径的往往是受保护进程）同样拒杀，不盲杀
	var buf [windows.MAX_LONG_PATH]uint16
	size := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(hProc, 0, &buf[0], &size); err != nil {
		fmt.Fprintf(os.Stderr, "QueryFullProcessImageName for PID %d failed, refusing blind kill: %v\n", pid, err)
		os.Exit(killHelperExitFailed)
	}
	imagePath := windows.UTF16ToString(buf[:size])
	if criticalSystemProcs[strings.ToLower(filepath.Base(imagePath))] {
		fmt.Fprintf(os.Stderr, "Protected system process %s (PID %d) cannot be killed\n", imagePath, pid)
		os.Exit(killHelperExitProtected)
	}

	// 复核二：与调用方 UAC 前捕获的指纹比对（容忍 1 秒创建时间精度差，与 KillVerified 同标准）
	if expectExe != "" && !strings.EqualFold(expectExe, imagePath) {
		fmt.Fprintf(os.Stderr, "PID %d identity mismatch: expected %s, now %s (PID reused?)\n", pid, expectExe, imagePath)
		os.Exit(killHelperExitMismatch)
	}
	if expectStart > 0 {
		var creation, exit, kernel, user windows.Filetime
		if err := windows.GetProcessTimes(hProc, &creation, &exit, &kernel, &user); err != nil {
			fmt.Fprintf(os.Stderr, "GetProcessTimes for PID %d failed: %v\n", pid, err)
			os.Exit(killHelperExitFailed)
		}
		const winEpochDiff = 116444736000000000 // 1601→1970 的 100ns 刻度差
		ticks := uint64(creation.HighDateTime)<<32 | uint64(creation.LowDateTime)
		actualStart := (int64(ticks) - winEpochDiff) * 100
		if diff := actualStart - expectStart; diff > int64(time.Second) || diff < -int64(time.Second) {
			fmt.Fprintf(os.Stderr, "PID %d start time mismatch (expected %d, actual %d): process was recycled, refusing\n", pid, expectStart, actualStart)
			os.Exit(killHelperExitMismatch)
		}
	}

	if err := windows.TerminateProcess(hProc, 1); err != nil {
		fmt.Fprintf(os.Stderr, "TerminateProcess for PID %d failed: %v\n", pid, err)
		os.Exit(killHelperExitFailed)
	}

	os.Exit(0)
}
