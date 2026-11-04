package ompversion

import "testing"

func TestCompare(t *testing.T) {
	tests := []struct {
		a, b string
		want int
		ok   bool
	}{
		{"18.16.0", "18.16.0", 0, true},
		{"18.16.1", "18.16.0", 1, true},
		{"18.15.9", "18.16.0", -1, true},
		{"9.9.9", "18.16.0", -1, true}, // 数值序非字典序
		{"1.10.0", "1.9.9", 1, true},
		{"v18.16.0", "18.16.0", 0, true}, // 可选 v 前缀
		{"18.16", "18.16.0", 0, false},   // 两段不是 omp 稳定版形状
		{"18.16.0-rc.1", "18.16.0", 0, false},
	}
	for _, tt := range tests {
		got, ok := Compare(tt.a, tt.b)
		if got != tt.want || ok != tt.ok {
			t.Fatalf("Compare(%q,%q)=%d,%v want %d,%v", tt.a, tt.b, got, ok, tt.want, tt.ok)
		}
	}
}

func TestVersionLine(t *testing.T) {
	if got := VersionLine("v18.16.0"); got != "18.16" {
		t.Fatalf("VersionLine=%q", got)
	}
	if got := VersionLine("nightly"); got != "" {
		t.Fatalf("VersionLine(invalid)=%q", got)
	}
}
