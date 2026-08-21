// hubkit 是 HubKit 的可执行入口。
// 包含正常 GUI 模式与 UAC 提权 Helper 模式。
package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/wailsapp/wails/v3/pkg/application"
	"golang.org/x/sys/windows"

	embedassets "hubkit" // 根目录包：承载 //go:embed all:frontend/dist
	"hubkit/internal/app"
)

func main() {
	modeFlag := flag.String("mode", "", "run mode: empty for GUI, 'killhelper' for elevated process terminator")
	pidFlag := flag.Uint("pid", 0, "target PID for killhelper mode")
	flag.Parse()

	// 1. 如果是 UAC 提权 Helper 模式，以极简逻辑执行并退出
	if *modeFlag == "killhelper" {
		runKillHelper(uint32(*pidFlag))
		return
	}

	// 2. 正常 GUI 主程序模式
	app.RegisterEvents()

	a, cleanup := app.New(application.AssetOptions{
		Handler: application.AssetFileServerFS(embedassets.FS),
	})
	defer cleanup()

	if err := a.Run(); err != nil {
		log.Fatal(err)
	}
}

// runKillHelper 以管理员提权权限直接终止目标 PID
func runKillHelper(pid uint32) {
	if pid == 0 || pid == 4 || pid == uint32(os.Getpid()) {
		fmt.Fprintf(os.Stderr, "Protected system process PID %d cannot be killed\n", pid)
		os.Exit(2)
	}

	hProc, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, pid)
	if err != nil {
		fmt.Fprintf(os.Stderr, "OpenProcess for PID %d failed: %v\n", pid, err)
		os.Exit(1)
	}
	defer windows.CloseHandle(hProc)

	if err := windows.TerminateProcess(hProc, 1); err != nil {
		fmt.Fprintf(os.Stderr, "TerminateProcess for PID %d failed: %v\n", pid, err)
		os.Exit(1)
	}

	os.Exit(0)
}
