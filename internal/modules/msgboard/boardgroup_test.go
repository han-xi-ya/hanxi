package msgboard

import (
	"path/filepath"
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func scr(name string, primary bool) *application.Screen {
	return &application.Screen{Name: name, IsPrimary: primary}
}

func names(targets []*application.Screen) []string {
	var out []string
	for _, t := range targets {
		out = append(out, t.Name)
	}
	return out
}

func TestPlanScreenTargetsEveryScreen(t *testing.T) {
	// 副屏打头枚举：主屏仍须置首（窗组序稳，防对账抖动）
	got := names(planScreenTargets([]*application.Screen{scr(`\.\DISPLAY2`, false), scr(`\.\DISPLAY1`, true), nil}, Config{EveryScreen: true}))
	if len(got) != 2 || got[0] != `\.\DISPLAY1` || got[1] != `\.\DISPLAY2` {
		t.Fatalf("多屏排窗异常（主屏须打头、nil 剔除）: %v", got)
	}
}

func TestPlanScreenTargetsSingleScreen(t *testing.T) {
	all := []*application.Screen{scr(`\.\DISPLAY1`, true), scr(`\.\DISPLAY2`, false)}
	if got := names(planScreenTargets(all, Config{Screen: `\.\DISPLAY2`})); len(got) != 1 || got[0] != `\.\DISPLAY2` {
		t.Fatalf("单屏指定设备应精确命中: %v", got)
	}
	// 拔屏/改名失配：回落主屏（F8 旧口径"永远有落点"）
	if got := names(planScreenTargets(all, Config{Screen: `\.\GONE`})); got[0] != `\.\DISPLAY1` {
		t.Fatalf("失配应回主屏: %v", got)
	}
	if got := planScreenTargets(nil, Config{}); got != nil {
		t.Fatalf("无屏必须回 nil 让调用层报错: %v", got)
	}
}

func TestSameDeviceSet(t *testing.T) {
	if !sameDeviceSet([]string{"a", "b"}, []string{"b", "a"}) {
		t.Fatal("乱序同集合应判等")
	}
	if sameDeviceSet([]string{"a", "b"}, []string{"a"}) {
		t.Fatal("拔屏（少员）必须判不等")
	}
	if sameDeviceSet([]string{"a", "a"}, []string{"a", "b"}) {
		t.Fatal("重数差异判不等")
	}
}

func TestStoreEveryScreenDefaultsTrueAndPersists(t *testing.T) {
	s := newMsgBoardStore(t.TempDir())
	if !s.Get().EveryScreen {
		t.Fatal("出厂默认必须多屏同挂（N29 主诉求）")
	}
	if _, err := s.Set(Config{Text: "x", FontSize: 64, EveryScreen: false}); err != nil {
		t.Fatal(err)
	}
	if s.Get().EveryScreen {
		t.Fatal("显式关闭后不得被默认值顶回")
	}
	dir := filepath.Dir(s.filePath)
	if newMsgBoardStore(dir).Get().EveryScreen {
		t.Fatal("重启加载应保持关闭态（显式 false 不得被默认值顶回）")
	}
}
