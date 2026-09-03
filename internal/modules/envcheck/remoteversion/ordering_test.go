package remoteversion

import (
	"strings"
	"testing"
)

func testLineOf(raw string) string {
	if idx := strings.IndexByte(raw, '.'); idx <= 0 {
		return ""
	}
	return raw[:strings.IndexByte(raw, '.')]
}

func TestPrioritizeLocalLine(t *testing.T) {
	channels := []Channel{
		{Key: "lts", Releases: []Release{{Version: "24.10.0"}}},
		{Key: "maintenance", Releases: []Release{{Version: "22.20.0"}}},
		{Key: "current", Releases: []Release{{Version: "26.1.3"}}},
	}
	PrioritizeLocalLine(channels, "26.1.2", testLineOf)
	if channels[0].Key != "current" || channels[1].Key != "lts" || channels[2].Key != "maintenance" {
		t.Fatalf("order=%v %v %v", channels[0].Key, channels[1].Key, channels[2].Key)
	}

	// 本机命中当前首位通道：保持原顺序。
	PrioritizeLocalLine(channels, "26.9.0", testLineOf)
	if channels[0].Key != "current" || channels[1].Key != "lts" || channels[2].Key != "maintenance" {
		t.Fatalf("already-front reorder changed order: %v %v %v", channels[0].Key, channels[1].Key, channels[2].Key)
	}
	// 中间通道命中：移动到最前，其余保持相对顺序。
	PrioritizeLocalLine(channels, "22.0.0", testLineOf)
	if channels[0].Key != "maintenance" || channels[1].Key != "current" || channels[2].Key != "lts" {
		t.Fatalf("mid move=%v %v %v", channels[0].Key, channels[1].Key, channels[2].Key)
	}

	// 无法解析、无匹配通道或 release 为空时维持原顺序。
	before := []Channel{
		{Key: "a", Releases: []Release{{Version: "24.10.0"}}},
		{Key: "b", Releases: []Release{{Version: "26.1.3"}}},
		{Key: "empty"},
	}
	for _, local := range []string{"", "unknown", "30.0.0"} {
		got := append([]Channel(nil), before...)
		PrioritizeLocalLine(got, local, testLineOf)
		if got[0].Key != "a" || got[1].Key != "b" {
			t.Fatalf("local=%q should keep order, got %v %v", local, got[0].Key, got[1].Key)
		}
	}
}
