package version

import (
	"os"
	"path/filepath"
	"testing"
)

func TestVersionBoundaryRejectsInvalidIDs(t *testing.T) {
	m := NewManager(t.TempDir())
	for _, version := range []string{"", "..", "../outside", `..\outside`, "v0.61.1/../..", "imported-../../outside", "imported-", "v0.61.1:stream", " v0.61.1", "v0.61.1.", "NUL"} {
		t.Run(version, func(t *testing.T) {
			if err := m.Remove(version); err == nil {
				t.Fatal("remove accepted invalid ID")
			}
			if _, err := m.ResolveExe(version); err == nil {
				t.Fatal("resolve accepted invalid ID")
			}
			if err := m.Download(version, nil); err == nil {
				t.Fatal("download accepted invalid ID")
			}
		})
	}
}

func TestVersionBoundaryPreservesHistoricalIDs(t *testing.T) {
	for _, version := range []string{"v0.61.1", "0.61.1", "imported-20260821-1", "imported-20260908-120000"} {
		t.Run(version, func(t *testing.T) {
			dir := t.TempDir()
			installed := filepath.Join(dir, "frp_"+version)
			if err := os.Mkdir(installed, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(installed, "frpc.exe"), []byte("fake executable never run"), 0600); err != nil {
				t.Fatal(err)
			}
			m := NewManager(dir)
			if _, err := m.ResolveExe(version); err != nil {
				t.Fatal(err)
			}
			list, err := m.ListInstalled()
			if err != nil || len(list) != 1 {
				t.Fatalf("list = %v, %v", list, err)
			}
			if err := m.Remove(version); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestVersionBoundaryRejectsLinkedDirectory(t *testing.T) {
	outside := t.TempDir()
	marker := filepath.Join(outside, "frpc.exe")
	if err := os.WriteFile(marker, []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(dir, "frp_v0.61.1")); err != nil {
		t.Skipf("link permission unavailable: %v", err)
	}
	m := NewManager(dir)
	if err := m.Remove("v0.61.1"); err == nil {
		t.Fatal("removed linked installation")
	}
	if _, err := m.ResolveExe("v0.61.1"); err == nil {
		t.Fatal("resolved linked installation")
	}
	got, err := os.ReadFile(marker)
	if err != nil || string(got) != "outside" {
		t.Fatal("outside changed")
	}
}
