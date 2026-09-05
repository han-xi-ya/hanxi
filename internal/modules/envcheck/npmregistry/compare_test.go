package npmregistry

import "testing"

func TestCompare(t *testing.T) {
	tests := []struct {
		a, b string
		want int
		ok   bool
	}{
		{"2.1.260", "2.1.261", -1, true},
		{"2.1.261", "2.1.260", 1, true},
		{"2.1.260", "2.1.260", 0, true},
		{"1.10.0", "1.9.0", 1, true},
		{"v2.1.260", "2.1.260", 0, true},
		{"2.1.260", "2.1.260-beta.1", 1, true},  // 正式版 > 预发布
		{"2.1.260-beta.1", "2.1.260", -1, true}, // 预发布 < 正式版
		{"2.1.260-beta.1", "2.1.260-beta.2", -1, true},
		{"2.1.260-beta.2", "2.1.260-beta.10", -1, true}, // 数字标识按数值比较
		{"2.1.260-alpha", "2.1.260-beta", -1, true},     // 字母标识按字典序
		{"2.1.260+build.5", "2.1.260+build.9", 0, true}, // 构建元数据忽略
		{"2.1.260", "garbage", 0, false},
		{"garbage", "2.1.260", 0, false},
		{"", "2.1.260", 0, false},
	}
	for _, tt := range tests {
		got, ok := Compare(tt.a, tt.b)
		if ok != tt.ok {
			t.Errorf("Compare(%q,%q) ok=%v want %v", tt.a, tt.b, ok, tt.ok)
			continue
		}
		if ok && got != tt.want {
			t.Errorf("Compare(%q,%q)=%d want %d", tt.a, tt.b, got, tt.want)
		}
	}
}
