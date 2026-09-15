package paseo

import (
	"testing"

	"hanxi/internal/modules/paseo/version"
)

// TestSortInstalledOrdering 冷启动"自动最新已装"回退依赖此排序：
// stable 恒大于同名预发布，beta 间按序号，imported 时间戳退化字典序排最后。
func TestSortInstalledOrdering(t *testing.T) {
	list := []version.PaseoVersionInfo{
		{Version: "imported-20260915-010203"},
		{Version: "0.8.0-beta.2"},
		{Version: "0.7.2"},
		{Version: "0.8.0"},
		{Version: "0.8.0-beta.1"},
	}
	sortInstalled(list)
	want := []string{"0.8.0", "0.8.0-beta.2", "0.8.0-beta.1", "0.7.2", "imported-20260915-010203"}
	for i := range want {
		if list[i].Version != want[i] {
			t.Fatalf("排序异常: %d = %s, want %s (%v)", i, list[i].Version, want[i], versionsOf(list))
		}
	}
}

func versionsOf(list []version.PaseoVersionInfo) []string {
	out := make([]string, len(list))
	for i, v := range list {
		out[i] = v.Version
	}
	return out
}
