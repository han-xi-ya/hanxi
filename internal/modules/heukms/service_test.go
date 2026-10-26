package heukms

// service_test.go 覆盖 resolveActiveVersion 消费面的版本比较器（兜底目录沉底的
// 第二道闸 importedRank；第一道在 version 包 ListInstalled，外部事实用例见
// version/imported_sink_test.go）。本包无法注入假 PE 接缝造已装夹具
// （version.NewManager 装配 windows 专属实现，假 exe 过不了核账与身份闸），
// 故服务层以比较器单元用例钉住"兜底不当最新、多位数段不跌字典序"两事实，
// 防本路径的闸被静默摘除。

import "testing"

// TestVersionCompare 数值分段比较与 imported- 兜底沉底：
// 字典序会把 9.9.9 排在 63.1.0 前、把 'i' 开头的兜底目录排在一切数字版本前，
// 两者都必须被闸压住（v 前缀与 imported- 兜底形态归一后同判）。
func TestVersionCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"63.1.0", "9.9.9", 1},
		{"9.9.9", "63.1.0", -1},
		{"64.0.0", "v64.0.0", 0},                  // v 前缀归一兼容
		{"9.9.9", "imported-6e449042eec3", 1},     // 兜底目录恒劣后数字版本
		{"imported-20260101-120000", "9.9.9", -1}, // 历史时间戳形态同闸
		{"vimported-6e449042eec3", "63.1.0", -1},  // 带 v 兜底形态归一后同闸
	}
	for _, c := range cases {
		if got := versionCompare(c.a, c.b); got != c.want {
			t.Errorf("versionCompare(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}
