package memo

import "testing"

// 速记卡摆位纯函数语义（多显示器/小工作区防出屏）：窗口 API 部分依赖真实
// Wails 实例，不进单测（无头守卫路径已在 hotkey_test.go 钉死）。

func TestPlanSheetPlacement(t *testing.T) {
	// 常规 1920×1080 工作区：水平居中、垂直 18% 处。
	x, y := planSheetPlacement(0, 0, 1920, 1040, sheetWidth, sheetHeight)
	if want := (1920 - sheetWidth) / 2; x != want {
		t.Fatalf("水平居中 x = %d, want %d", x, want)
	}
	if want := 1040 * sheetTopPercent / 100; y != want {
		t.Fatalf("垂直落点 y = %d, want %d", y, want)
	}

	// 负坐标副屏（主屏在右）：以工作区原点为锚，不得落回 (0,0)。
	x2, y2 := planSheetPlacement(-1936, 0, 1920, 1040, sheetWidth, sheetHeight)
	if x2 != -1936+(1920-sheetWidth)/2 || y2 != 1040*sheetTopPercent/100 {
		t.Fatalf("副屏锚点失真: (%d,%d)", x2, y2)
	}

	// 整窗不出工作区下缘：小工作区时贴底钳位。
	x3, y3 := planSheetPlacement(0, 0, 1280, 300, sheetWidth, sheetHeight)
	if y3+sheetHeight > 300 {
		t.Fatalf("窗口越出工作区下缘: y=%d h=%d waH=300", y3, sheetHeight)
	}
	if x3+sheetWidth > 1280 {
		t.Fatalf("窗口越出工作区右缘: x=%d w=%d waW=1280", x3, sheetWidth)
	}

	// 窗口两个方向都大于工作区：钳到工作区原点，绝不产生负偏移。
	x4, y4 := planSheetPlacement(100, 50, 400, 150, sheetWidth, sheetHeight)
	if x4 != 100 || y4 != 50 {
		t.Fatalf("超小工作区应贴原点: (%d,%d)", x4, y4)
	}

	// 仅垂直放不下、水平放得下：水平照常居中，垂直贴顶。
	x5, y5 := planSheetPlacement(0, 0, 1920, 150, sheetWidth, sheetHeight)
	if x5 != (1920-sheetWidth)/2 || y5 != 0 {
		t.Fatalf("单轴钳位失真: (%d,%d)", x5, y5)
	}
}
