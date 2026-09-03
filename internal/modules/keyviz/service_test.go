package keyviz

import (
	"testing"
)

// TestVersionCompare 数值分段比较：多位数段不被字典序坑。
func TestVersionCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"v2.1.1", "v2.1.0", 1},
		{"v2.9.0", "v2.10.0", -1}, // 字典序会判反
		{"v2.1.1", "v2.1.1", 0},
		{"v2.0.0", "v1.99.99", 1},
	}
	for _, c := range cases {
		if got := versionCompare(c.a, c.b); got != c.want {
			t.Errorf("versionCompare(%s,%s) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}
