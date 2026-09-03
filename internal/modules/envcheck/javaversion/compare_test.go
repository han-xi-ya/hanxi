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

func TestVersionLine(t *testing.T) {
	for version, want := range map[string]string{
		"21.0.5+11": "21", "25.0.1+8": "25", "1.8.0_402-b06": "8", "8.0.422+5": "8",
		"": "", "unknown": "", "openjdk 21": "",
	} {
		if got := VersionLine(version); got != want {
			t.Fatalf("VersionLine(%q)=%q want %q", version, got, want)
		}
	}
}
