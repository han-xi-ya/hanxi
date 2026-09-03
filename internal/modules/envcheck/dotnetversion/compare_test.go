package dotnetversion

import (
	"testing"

	"hanxi/internal/modules/envcheck/remoteversion"
)

func TestVersionLine(t *testing.T) {
	for version, want := range map[string]string{
		"9.0": "9.0", "9.0.8": "9.0", "10.0.100": "10.0", "Python 3.12": "",
		"": "", "unknown": "", "9.0.0-rc.1": "",
	} {
		if got := VersionLine(version); got != want {
			t.Fatalf("VersionLine(%q)=%q want %q", version, got, want)
		}
	}
}

func TestCompareStrictGA(t *testing.T) {
	if result, ok := Compare("8.0.13", "8.0.11"); !ok || result != 1 {
		t.Fatalf("8.0.13 vs 8.0.11 = %d %v", result, ok)
	}
	if result, ok := Compare("9.0.0", "10.0.0"); !ok || result != -1 {
		t.Fatalf("9.0.0 vs 10.0.0 = %d %v", result, ok)
	}
	for _, pair := range [][2]string{{"9.0.0-rc.1", "9.0.0"}, {"8.0", "8.0.0"}, {"", "9.0.0"}, {"v9.0.0", "9.0.0"}} {
		if _, ok := Compare(pair[0], pair[1]); ok {
			t.Fatalf("Compare(%q,%q) should reject", pair[0], pair[1])
		}
	}
}

func TestSelectChannels(t *testing.T) {
	lines := []remoteversion.Channel{
		{Key: "dotnet-10.0", Releases: []remoteversion.Release{{Version: "10.0.1"}}},
		{Key: "dotnet-9.0", Releases: []remoteversion.Release{{Version: "9.0.8"}}},
		{Key: "dotnet-8.0", Releases: []remoteversion.Release{{Version: "8.0.16"}}},
	}
	got, supported := SelectChannels(lines, "8.0.13")
	if !supported || len(got) != 2 || got[0].Key != "dotnet-8.0" || got[1].Key != "dotnet-10.0" {
		t.Fatalf("local 8.0: %v supported=%v", keys(got), supported)
	}
	got, supported = SelectChannels(lines, "10.0.1")
	if !supported || len(got) != 1 || got[0].Key != "dotnet-10.0" {
		t.Fatalf("local on latest line: %v supported=%v", keys(got), supported)
	}
	got, supported = SelectChannels(lines, "6.0.36")
	if supported || len(got) != 1 || got[0].Key != "dotnet-10.0" {
		t.Fatalf("EOL local line: %v supported=%v", keys(got), supported)
	}
	got, supported = SelectChannels(lines, "")
	if supported || len(got) != 1 || got[0].Key != "dotnet-10.0" {
		t.Fatalf("unknown local: %v supported=%v", keys(got), supported)
	}
	if got, _ := SelectChannels(nil, "8.0.1"); got != nil {
		t.Fatalf("empty lines=%v", keys(got))
	}
}

func keys(channels []remoteversion.Channel) []string {
	out := make([]string, 0, len(channels))
	for _, channel := range channels {
		out = append(out, channel.Key)
	}
	return out
}
