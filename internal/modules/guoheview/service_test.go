package guoheview

import (
	"testing"
)

// TestVersionCompare 四段版本号数值分段比较：多位数段不被字典序坑。
func TestVersionCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"v3.2.7.98", "v3.2.7.97", 1},
		{"v3.2.7.98", "v3.2.7.98", 0},
		{"v3.10.0.100", "v3.9.0.99", 1}, // 字典序会判反
		{"v3.2.7.9", "v3.2.7.10", -1},   // 构建号跨位数
		{"v3.2.8.1", "v3.2.7.99", 1},    // 高位段进位
	}
	for _, c := range cases {
		if got := versionCompare(c.a, c.b); got != c.want {
			t.Errorf("versionCompare(%s,%s) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}
