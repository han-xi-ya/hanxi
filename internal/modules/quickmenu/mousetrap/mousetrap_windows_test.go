//go:build windows

// 快捷菜单钩子自测。
//
// 分两档：
//   - 默认档（CI/日常 go test ./... 必跑）：纯逻辑冒烟，不触碰真实输入链；
//   - 实机档（opt-in，设 HANXI_LIVE_MOUSE_TEST=1 开启）：用 SendInput 注入右键
//     走完整"钩子链→吞按→阈值→触发/回放"闭环。**会在当前桌面移动光标、
//     给光标下应用闪一次右键菜单**——请在机器空闲时于普通终端手动运行，并先
//     退出正在运行的 Hanxi 实例（它的钩子会抢先吞事件导致误报）。
package mousetrap

import (
	"os"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

var (
	procSetCursorPos   = syscall.NewLazyDLL("user32.dll").NewProc("SetCursorPos")
	procGetSystemMetrc = syscall.NewLazyDLL("user32.dll").NewProc("GetSystemMetrics")
)

func requireLiveDesktop(t *testing.T) {
	t.Helper()
	if os.Getenv("HANXI_LIVE_MOUSE_TEST") != "1" {
		t.Skip("实机手势测试默认关闭（会操作真实桌面）：设 HANXI_LIVE_MOUSE_TEST=1 并空闲机器后运行")
	}
}

func systemMetrics(idx uintptr) int {
	r, _, _ := procGetSystemMetrc.Call(idx)
	return int(r)
}

// injectMouse 注入一次不带魔术 extraInfo 的鼠标事件（等同真实物理输入路径）。
// 无交互桌面注入权限（沙箱宿主）时自动 Skip。
func injectMouse(t *testing.T, flags uint32) {
	t.Helper()
	var ev cinput
	ev.typ = mouseInput
	ev.mi.flags = flags
	r, _, errno := procSendInput.Call(1, uintptr(unsafe.Pointer(&ev)), uintptr(unsafe.Sizeof(ev)))
	if r != 1 {
		t.Skipf("SendInput(0x%x) 被拒绝: %v（非交互桌面环境）", flags, errno)
	}
}

func injectRightDownUp(t *testing.T, down bool) {
	t.Helper()
	if down {
		injectMouse(t, mouseeventfRightDo)
	} else {
		injectMouse(t, mouseeventfRightUp)
	}
}

// sendEsc 注入 Esc，关闭回放顺带弹出的上下文菜单。
func sendEsc() {
	const (
		inputKeyboard  = 1
		keyeventfKeyup = 0x0002
		vkEscape       = 0x1B
	)
	type kbd struct {
		typ uint32
		_   uint32
		ki  struct {
			vk, scan uint16
			flags    uint32
			time     uint32
			_        uint32
			extra    uintptr
		}
	}
	down, up := kbd{}, kbd{}
	down.typ, up.typ = inputKeyboard, inputKeyboard
	down.ki.vk, up.ki.vk = vkEscape, vkEscape
	up.ki.flags = keyeventfKeyup
	evs := [2]kbd{down, up}
	procSendInput.Call(2, uintptr(unsafe.Pointer(&evs[0])), uintptr(unsafe.Sizeof(down)))
}

type observer struct {
	magicDowns atomic.Int32
	magicUps   atomic.Int32
}

// installObserver 在链顶（后装先收）挂一只只读观察者，统计带自家魔术值的回放
// 右键事件是否真正回到输入链。t.Cleanup 自动摘除。
func installObserver(t *testing.T) *observer {
	t.Helper()
	obs := &observer{}
	cb := syscall.NewCallback(func(code int32, w, l uintptr) uintptr {
		if code >= 0 && (w == wmRButtonDown || w == wmRButtonUp) {
			if hookInfo(l).extraInfo == replayMagic {
				if w == wmRButtonDown {
					obs.magicDowns.Add(1)
				} else {
					obs.magicUps.Add(1)
				}
			}
		}
		ret, _, _ := procCallNext.Call(uintptr(code), w, l)
		return ret
	})
	h, _, err := procSetHook.Call(uintptr(whMouseLL), cb, 0, 0)
	if h == 0 {
		t.Fatalf("安装观察者钩子失败: %v", err)
	}
	t.Cleanup(func() { procUnhook.Call(h) })
	return obs
}

func waitFor(cond func() bool, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

// park 把光标钉在主屏左上安全区：注入点若落在任务栏旁路带/其它特殊区域，
// 手势用例的断言前提就不成立。
func park(t *testing.T) {
	t.Helper()
	if r, _, errno := procSetCursorPos.Call(600, 400); r == 0 {
		t.Skipf("SetCursorPos 被拒绝（%v）：无法定位注入点", errno)
	}
}

// TestLongPressFiresBeforeRelease 实机档：按住达到阈值后，抬手之前触发事件应
// 经 AfterFunc→PostThreadMessage→泵线程投递到达（全程无内核 APC）。
func TestLongPressFiresBeforeRelease(t *testing.T) {
	requireLiveDesktop(t)
	trap, err := Start(Config{MinHold: 200 * time.Millisecond, MaxMove: 10000}) // 容差放大：环境鼠标抖动会触发拖拽让位，非本用例目标
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = trap.Stop() })
	park(t)

	injectRightDownUp(t, true)
	defer injectRightDownUp(t, false) // 断言失败也保证按键态收尾

	select {
	case trg := <-trap.C():
		if trg.Hold < 150*time.Millisecond {
			t.Errorf("触发时机过早: hold=%v（应≥阈值 200ms）", trg.Hold)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("长按阈值未触发：注入按下后 3s 内没有收到 Trigger")
	}

	// 抬手必须被吞净：不得产生第二次触发（重放循环/迟到竞态回归锁）。
	time.Sleep(150 * time.Millisecond)
	injectRightDownUp(t, false)
	select {
	case extra := <-trap.C():
		t.Errorf("抬手后产生重复触发: %+v", extra)
	case <-time.After(400 * time.Millisecond):
	}
}

// TestShortPressReplaysPair 实机档：阈值前抬手，泵应回放一组带自家魔术 extraInfo
// 的完整右键，让下游应用照常弹上下文菜单；且不得投递触发。
func TestShortPressReplaysPair(t *testing.T) {
	requireLiveDesktop(t)
	trap, err := Start(Config{MinHold: 600 * time.Millisecond, MaxMove: 10000})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = trap.Stop() })
	obs := installObserver(t) // 后装：链顶优先收到全部事件（含被吞者）
	park(t)

	injectRightDownUp(t, true)
	time.Sleep(80 * time.Millisecond)
	injectRightDownUp(t, false)
	t.Cleanup(sendEsc) // 回放会给光标下应用弹菜单，注入 Esc 收起

	if !waitFor(func() bool { return obs.magicDowns.Load() >= 1 && obs.magicUps.Load() >= 1 }, 2*time.Second) {
		t.Fatalf("未观察到完整回放配对: magicDowns=%d magicUps=%d（若有其它 Hanxi/鼠标钩子程序在运行，先退出后重试）",
			obs.magicDowns.Load(), obs.magicUps.Load())
	}
	select {
	case trg := <-trap.C():
		t.Errorf("短按误触发长按: %+v", trg)
	case <-time.After(200 * time.Millisecond):
	}
}

// TestTaskbarSurfaceNoCrash 默认档：矩形判定冒烟——各坐标不得 panic、结果可解释。
func TestTaskbarSurfaceNoCrash(t *testing.T) {
	if got := isTaskbarSurface(1, 1); got {
		t.Error("主屏左上角不应判为任务栏")
	}
	// 主屏底部横带中心：几何上必在任务栏（本任务栏在底部时）或保守 false，两者皆不崩。
	mw, mh := systemMetrics(0), systemMetrics(1)
	_ = isTaskbarSurface(int32(mw/2), int32(mh-2))
	_ = isTaskbarSurface(-40000, 0) // 越界坐标：保守 false，不得 panic
}
