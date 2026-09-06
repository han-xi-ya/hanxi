package translucenttb

import (
	"strings"
	"testing"
)

// TestVersionCompare 年份.序号版本数值分段比较：2026.10 必须大于 2026.2
// （字典序会得出反果，目录名排序依赖本函数），imported- 兜底版本退化为字典序不 panic。
func TestVersionCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"2026.2", "2026.1", 1},
		{"2026.1", "2026.2", -1},
		{"2026.2", "2026.2", 0},
		{"2026.10", "2026.9", 1},
		{"2027.1", "2026.99", 1},
		// imported- 兜底段非数值 → 退化为字典序（与 ccswitch 同构取舍，只求排序确定不求语义）
		{"imported-20260906-150405", "2026.2", 1},
	}
	for _, c := range cases {
		if got := versionCompare(c.a, c.b); got != c.want {
			t.Errorf("versionCompare(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

// TestQuitOutcomeTextHonest 退出文案契约：external 指引不得承诺代退，
// 正常退出必须告知任务栏还原（透明特效随进程消失，用户需预期）。
func TestQuitOutcomeTextHonest(t *testing.T) {
	ext := QuitOutcome{Stopped: false, External: true, Message: "当前是外部自行启动的实例，请在 TranslucentTB 托盘菜单中退出"}
	if ext.Stopped {
		t.Error("external 语义不得标 Stopped")
	}
	ok := QuitOutcome{Stopped: true, Message: "TranslucentTB 已退出，任务栏已还原默认外观"}
	if !strings.Contains(ok.Message, "还原") {
		t.Error("正常退出文案必须预告任务栏还原")
	}
}
