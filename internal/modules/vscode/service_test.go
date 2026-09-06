package vscode

import (
	"testing"
)

// TestVersionCompare 数值分段比较：多位数段是字典序的经典陷阱。
func TestVersionCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.136.1", "1.136.0", 1},
		{"1.100.0", "1.99.0", 1}, // 字典序会判负
		{"1.9.0", "1.10.0", -1},  // 字典序会判正
		{"1.2.3", "1.2.3", 0},
		{"v1.136.1", "1.136.0", 1}, // v 前缀容错
	}
	for _, c := range cases {
		if got := versionCompare(c.a, c.b); got != c.want {
			t.Errorf("versionCompare(%q,%q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestNormalizeVersion(t *testing.T) {
	for _, in := range []string{" 1.136.1 ", "v1.136.1", "V1.136.1"} {
		if got := normalizeVersion(in); got != "1.136.1" {
			t.Errorf("normalizeVersion(%q) = %q", in, got)
		}
	}
}

func TestLabelForm(t *testing.T) {
	if labelForm("installer") != "安装版" || labelForm("PORTABLE") != "便携版" || labelForm("") != "便携版" {
		t.Error("labelForm 归一错误")
	}
}
