package clipboard

import (
	"strings"
	"testing"
)

// TestClipOverlayPlacement 摆位纯函数（契约 §6 跟随光标显示器的几何部分）：
// 水平居中、垂直 18% 落点且不出工作区；窗大于工作区时贴左上绝不负偏移出屏。
func TestClipOverlayPlacement(t *testing.T) {
	x, y := planOverlayPlacement(0, 0, 1920, 1080, overlayWidth, overlayHeight)
	if x != (1920-overlayWidth)/2 || y != 1080*overlayTopPercent/100 {
		t.Fatalf("常规摆位异常: (%d,%d)", x, y)
	}

	// 多显示器：工作区原点非零，摆位随原点平移
	x, y = planOverlayPlacement(2560, 100, 1920, 1080, overlayWidth, overlayHeight)
	if x != 2560+(1920-overlayWidth)/2 || y != 100+1080*overlayTopPercent/100 {
		t.Fatalf("副屏摆位未随工作区原点: (%d,%d)", x, y)
	}

	// 18% 落点会溢出下缘时钳到底（小高屏）
	x, y = planOverlayPlacement(0, 0, 800, 600, 400, 500)
	if y != 600-500 {
		t.Fatalf("下缘钳制失效: (%d,%d)", x, y)
	}

	// 窗口大于工作区：贴左上，不产生负偏移
	x, y = planOverlayPlacement(0, 0, 500, 400, overlayWidth, overlayHeight)
	if x != 0 || y != 0 {
		t.Fatalf("超尺寸应贴左上: (%d,%d)", x, y)
	}
}

// TestClipOverlayHeadlessNoop 无头态浮层全家桶：show/toggle 返回可读错误、
// hide/destroy 幂等不 panic，绝不静默建"幽灵账本"。
func TestClipOverlayHeadlessNoop(t *testing.T) {
	h := newSvcHarness(t)
	defer h.stop()

	if err := h.svc.showOverlay(); err == nil || !strings.Contains(err.Error(), "应用运行") {
		t.Fatalf("无头 showOverlay 应返回可读错误: %v", err)
	}
	if err := h.svc.toggleOverlay(); err == nil || !strings.Contains(err.Error(), "应用运行") {
		t.Fatalf("无头 toggleOverlay 应返回可读错误: %v", err)
	}
	h.svc.hideOverlay() // 无窗：静默武装计时不炸
	h.svc.destroyOverlay(true)
	h.svc.destroyOverlay(false)
	if h.svc.ovWin != nil || h.svc.ovShown {
		t.Fatal("无头路径不应留下浮层账本")
	}

	// CollapseOverlay（§4 十法之 v1.4 H1 收窗 RPC）：内部 hide 路径不依赖
	// Wails 运行态，无头幂等成功且不产生账本/事件
	if err := h.svc.CollapseOverlay(); err != nil {
		t.Fatalf("无头 CollapseOverlay 应幂等成功: %v", err)
	}
	if err := h.svc.CollapseOverlay(); err != nil { // 二次调用幂等
		t.Fatal(err)
	}
	if h.svc.ovWin != nil || h.svc.ovShown {
		t.Fatal("CollapseOverlay 不应产生浮层账本")
	}

	// stop 之后再唤出：无头态应用判定先行（可读错误优先于静默早退），
	// 且绝不留浮层账本；started 门控的"迟到回调不复活"分支属有头态通路，
	// 由提交前复核代码路径保证（本包无头单测不可达，真机冒烟覆盖）。
	if err := h.svc.stop(); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.showOverlay(); err == nil {
		t.Fatal("无头态 showOverlay 不应静默成功")
	}
	if h.svc.ovShown || h.svc.ovWin != nil {
		t.Fatal("stop 后唤出不得留下任何浮层账本")
	}
}
