package goversion

import "testing"

func TestCompare(t *testing.T) {
	tests := []struct {
		a, b string
		want int
		ok   bool
	}{
		{"1.27", "1.27.0", 0, true},
		{"go1.27.1", "1.27", 1, true},
		{"1.27", "1.26.99", 1, true},
		{"1.28rc1", "1.27.1", 0, false},
		{"1.28-devel", "1.27.1", 0, false},
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
		"go1.26.8": "1.26", "1.25": "1.25", "1.24.3": "1.24",
		"": "", "unknown": "", "devel branch": "",
	} {
		if got := VersionLine(version); got != want {
			t.Fatalf("VersionLine(%q)=%q want %q", version, got, want)
		}
	}
}
