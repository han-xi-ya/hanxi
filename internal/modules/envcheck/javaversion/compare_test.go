package javaversion

import "testing"

func TestCompare(t *testing.T) {
	tests := []struct {
		a, b string
		want int
		ok   bool
	}{
		{"1.8.0_402", "8.0.401", 1, true},
		{"1.8.0_402-b06", "1.8.0_402-b05", 1, true},
		{"17.0.12", "17.0.12+7", -1, true},
		{"21.0.12.1+1-LTS", "21.0.12.1+1", 0, true},
		{"26", "25.0.9", 1, true},
		{"21-ea", "21", 0, false},
		{"garbage", "21", 0, false},
	}
	for _, tt := range tests {
		got, ok := Compare(tt.a, tt.b)
		if got != tt.want || ok != tt.ok {
			t.Errorf("Compare(%q,%q)=(%d,%v), want (%d,%v)", tt.a, tt.b, got, ok, tt.want, tt.ok)
		}
	}
}
