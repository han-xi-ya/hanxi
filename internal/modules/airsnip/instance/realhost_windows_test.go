//go:build windows

// 真机验收夹具（默认 skip，不参与 CI 与日常 go test）。
//
// 存在的理由：本模块的命令通道全部建立在上游源码读到的事实上（消息窗类名、
// 托盘菜单 ID、WM_CLOSE 即退出、无 VERSIONINFO 资源），而这些**在单测里无法证**
// ——Win32 消息投递与 UIPI 的行为只有真 airsnip.exe 在场才算验过。断言一律取
// 外部事实（顶层窗是否真出现），不取 PostMessage 返回值自证。
//
// 跑法（须以管理员权限的终端，因上游 release 版 manifest 是 requireAdministrator；
// 非提权父进程会被 740 直拒，那是另一条已单测覆盖的路径）：
//
//	HANXI_AIRSNIP_REALHOST=1 HANXI_AIRSNIP_EXE='C:\path\to\airsnip.exe' \
//	  go test -count=1 -run RealHost -v ./internal/modules/airsnip/instance/
//
// APPDATA 被重定向到临时目录：上游会把 config.toml 与截图目录写进 APPDATA，
// 不重定向就会污染本机真实配置（本模块自己的接管写入也据此在临时目录里核对）。
package instance

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hanxi/internal/platform/windows"
)

func TestRealHostCommandChannel(t *testing.T) {
	if os.Getenv("HANXI_AIRSNIP_REALHOST") != "1" {
		t.Skip("真机夹具：设 HANXI_AIRSNIP_REALHOST=1 与 HANXI_AIRSNIP_EXE 后执行")
	}
	exe := os.Getenv("HANXI_AIRSNIP_EXE")
	if exe == "" {
		t.Skip("未提供 HANXI_AIRSNIP_EXE（真实 airsnip.exe 路径）")
	}
	if fi, err := os.Stat(exe); err != nil || fi.IsDir() {
		t.Skipf("HANXI_AIRSNIP_EXE 指向的文件不可用: %s", exe)
	}
	t.Setenv("APPDATA", t.TempDir())

	probe := NewAirsnipProbe()
	e := NewEngine(windows.NewJobAPI(), probe, Callbacks{})
	// 兜底收口：断言中途失败也必须把实例连同其覆盖层一起带走，
	// 否则用户屏幕上会留一层冻结的全屏覆盖层。
	defer func() {
		_ = e.Stop()
	}()

	if err := e.Start(StartOptions{Version: "v-realhost", Exe: exe}); err != nil {
		t.Fatalf("真机启动失败（若报 740 请确认本会话已提权）: %v", err)
	}
	if !e.WaitReady(10 * time.Second) {
		t.Fatal("10 秒内进程枚举未见 airsnip.exe")
	}
	snap := e.Snapshot()
	if snap.State != StateRunning || snap.PID == 0 {
		t.Fatalf("引擎应处于 running 自有态: %+v", snap)
	}

	// 1) 常驻隐藏消息窗在场——所有命令的唯一投递目标。
	//    进程在场 ≠ 窗已建（上游 run() 里 spawn 后才注册窗口类与建窗），
	//    本夹具第一次真机运行就是被这条竞态判失败的，故一律走轮询而非瞬时判定。
	if waitForWindowByClass(WindowClassHidden, snap.PID, 10*time.Second) == 0 {
		t.Fatalf("10 秒内未找到 %s 消息窗；该 PID 现有顶层窗类名：%v", WindowClassHidden, dumpWindowClasses(snap.PID))
	}

	// 2) 启动前接管：上游写 config.toml 之前我们已落键，最终文件必须是关闭态
	cfg := filepath.Join(os.Getenv("APPDATA"), configDirName, configFileName)
	if raw, err := os.ReadFile(cfg); err != nil {
		t.Errorf("接管后的配置文件不可读: %v", err)
	} else if !strings.Contains(string(raw), "auto_check_update = false") {
		t.Errorf("接管键被上游覆盖或写错位置:\n%s", raw)
	}

	// 3) 打开设置面板：以 airsnip.settings 顶层窗真出现为送达判据
	posted, focused := e.ShowSettings()
	if !posted {
		t.Fatal("ShowSettings 未送达（面板窗未出现）——命令通道已失效或被 UIPI 拦截")
	}
	if !focused {
		t.Logf("面板在场但未能置前（前台锁拒绝属可接受现状）")
	}

	// 4) 截图：以 airsnip.overlay 出现为送达判据。覆盖层是全屏冻结层，
	//    验完立刻收尾退出，把用户在屏上的停留时间压到最短。
	if !e.Shot() {
		t.Fatal("Shot 未送达（覆盖层窗未出现）——命令通道已失效或被 UIPI 拦截")
	}
	if !WindowPresentByClass(WindowClassOverlay, snap.PID) {
		t.Fatal("Shot 报送达但再判一次覆盖层不在场")
	}

	// 5) 退出：上游托盘「退出」项的实现就是给自己投 WM_CLOSE，Quit 同构
	if err := e.Quit(); err != nil {
		t.Fatalf("Quit 失败: %v", err)
	}
	if err := waitProbeGone(probe); err != nil {
		t.Errorf("Quit 后进程未收口: %v", err)
	}
	if got := e.Snapshot().State; got != StateStopped {
		t.Errorf("收口后状态应为 stopped，实际 %s", got)
	}
}

// waitProbeGone 轮询直至进程枚举未见 airsnip.exe。
func waitProbeGone(p AirsnipProbe) error {
	deadline := time.Now().Add(10 * time.Second)
	for {
		if !p.IsRunning() {
			return nil
		}
		if !time.Now().Before(deadline) {
			return errors.New("10 秒后仍见 airsnip.exe 进程")
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// dumpWindowClasses 列出该 PID 的全部顶层窗类名（失败时的现场证据：
// 上游若改类名或改窗口层级结构，报告里要能直接看见实况，而不是只留一句"没找到"）。
func dumpWindowClasses(pid uint32) []string {
	var out []string
	windows.EnumTopWindows(func(hwnd uintptr, wpid uint32) bool {
		if wpid == pid {
			out = append(out, windowClassName(hwnd))
		}
		return true
	})
	return out
}
