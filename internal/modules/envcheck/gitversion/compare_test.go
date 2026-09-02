package gitversion

import "testing"

func TestCompare(t *testing.T) {
	tests := []struct {
		name   string
		a      string
		b      string
		want   int
		wantOK bool
	}{
		{name: "same", a: "2.46.0.windows.1", b: "v2.46.0.windows.1", want: 0, wantOK: true},
		{name: "revision", a: "2.46.0.windows.2", b: "2.46.0.windows.1", want: 1, wantOK: true},
		{name: "minor", a: "2.47.0.windows.1", b: "2.46.9.windows.9", want: 1, wantOK: true},
		{name: "plain is revision zero", a: "2.46.0", b: "2.46.0.windows.1", want: -1, wantOK: true},
		{name: "invalid", a: "2.46", b: "2.46.0.windows.1", want: 0, wantOK: false},
		{name: "preview invalid", a: "2.46.0-rc1", b: "2.46.0.windows.1", want: 0, wantOK: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := Compare(tt.a, tt.b)
			if got != tt.want || ok != tt.wantOK {
				t.Fatalf("Compare(%q, %q) = (%d, %v), want (%d, %v)", tt.a, tt.b, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

func TestRelationForLocal(t *testing.T) {
	tests := []struct {
		version   string
		installed bool
		latest    string
		want      Relation
	}{
		{installed: false, latest: "2.50.0.windows.1", want: RelationNotInstalled},
		{version: "2.50.0.windows.1", installed: true, latest: "2.50.0.windows.1", want: RelationLatest},
		{version: "2.49.0.windows.1", installed: true, latest: "2.50.0.windows.1", want: RelationUpdateAvailable},
		{version: "2.51.0", installed: true, latest: "2.50.0.windows.1", want: RelationAhead},
		{version: "unknown", installed: true, latest: "2.50.0.windows.1", want: RelationUnknown},
		{version: "2.50.0", installed: true, latest: "", want: RelationUnknown},
	}
	for _, tt := range tests {
		if got := RelationForLocal(tt.version, tt.installed, tt.latest); got != tt.want {
			t.Errorf("RelationForLocal(%q, %v, %q) = %q, want %q", tt.version, tt.installed, tt.latest, got, tt.want)
		}
	}
}
