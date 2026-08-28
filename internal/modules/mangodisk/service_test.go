package mangodisk

import "testing"

func TestNormalizeVersion(t *testing.T) {
	if got := normalizeVersion("1.0.7"); got != "v1.0.7" {
		t.Fatalf("got %q", got)
	}
	if got := normalizeVersion("v1.0.7"); got != "v1.0.7" {
		t.Fatalf("got %q", got)
	}
}
