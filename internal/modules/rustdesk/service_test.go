package rustdesk

import "testing"

// TestVersionCompare 数值分段比较（1.10.0 > 1.9.0 是字典序陷阱回归锚）。
func TestVersionCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"v1.10.0", "v1.9.0", 1},
		{"v1.4.9", "v1.4.9", 0},
		{"v1.4.8", "v1.4.9", -1},
		{"v1.5.0", "1.4.9", 1},
	}
	for _, c := range cases {
		if got := versionCompare(c.a, c.b); got != c.want {
			t.Errorf("versionCompare(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}
