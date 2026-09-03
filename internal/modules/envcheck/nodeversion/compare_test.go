package nodeversion

import "testing"

func TestCompare(t *testing.T) {
	tests := []struct {
		a, b string
		want int
		ok   bool
	}{
		{"v26.8.1", "26.8.1", 0, true},
		{"26.8.2", "26.8.1", 1, true},
		{"26.0.0", "24.99.99", 1, true},
		{"26.0.0-rc.1", "26.0.0", 0, false},
		{"26.0", "26.0.0", 0, false},
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
		"v24.10.0": "24", "26.1.3": "26", "22.20.1": "22",
		"": "", "unknown": "", "24": "",
	} {
		if got := VersionLine(version); got != want {
			t.Fatalf("VersionLine(%q)=%q want %q", version, got, want)
		}
	}
}
