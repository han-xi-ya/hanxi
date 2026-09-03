package pythonversion

import "testing"

func TestCompare(t *testing.T) {
	tests := []struct {
		a, b string
		want int
		ok   bool
	}{
		{"Python 3.14.7", "3.14.7", 0, true},
		{"3.14", "3.14.0", 0, true},
		{"3.14.1", "3.13.99", 1, true},
		{"3.12.14", "3.12.9", 1, true},
		{"3.15.0rc1", "3.14.7", 0, false},
		{"3.15.0a1", "3.14.7", 0, false},
		{"3.14.7+vendor", "3.14.7", 0, false},
		{"v3.14.7", "3.14.7", 0, false},
		{"18446744073709551616.1.0", "3.14.7", 0, false},
	}
	for _, tt := range tests {
		got, ok := Compare(tt.a, tt.b)
		if got != tt.want || ok != tt.ok {
			t.Errorf("Compare(%q,%q)=(%d,%v), want (%d,%v)", tt.a, tt.b, got, ok, tt.want, tt.ok)
		}
	}
}

func TestParseFinalNameStrict(t *testing.T) {
	for _, raw := range []string{"Python 3.15.0a1", "Python 3.15.0b2", "Python 3.15.0rc1", "Python 3.14", "3.14.7"} {
		if _, ok := parseFinalName(raw); ok {
			t.Fatalf("parseFinalName(%q) unexpectedly accepted", raw)
		}
	}
	got, ok := parseFinalName("Python 3.14.7")
	if !ok || canonicalVersion(got) != "3.14.7" {
		t.Fatalf("got=%#v ok=%v", got, ok)
	}
}

func TestVersionLine(t *testing.T) {
	for version, want := range map[string]string{
		"3.12.4": "3.12", "Python 3.14.7": "3.14", "3.13": "3.13",
		"": "", "unknown": "", "3.15.0rc1": "",
	} {
		if got := VersionLine(version); got != want {
			t.Fatalf("VersionLine(%q)=%q want %q", version, got, want)
		}
	}
}
