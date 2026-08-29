package snipaste

import (
	"os"
	"path/filepath"
	"testing"

	"hanxi/internal/modules/snipaste/version"
)

func TestSnipasteStoreRoundTripAndDamageTolerance(t *testing.T) {
	dir := t.TempDir()
	s := newSnipasteStore(dir)
	if err := s.SetActive("2.11.3"); err != nil {
		t.Fatal(err)
	}
	if got := newSnipasteStore(dir).GetActive(); got != "2.11.3" {
		t.Fatalf("active=%q", got)
	}
	if err := os.WriteFile(filepath.Join(dir, "snipaste.json"), []byte("{"), 0644); err != nil {
		t.Fatal(err)
	}
	if got := newSnipasteStore(dir).GetActive(); got != "" {
		t.Fatalf("damaged active=%q", got)
	}
}

func TestResolveActiveVersion(t *testing.T) {
	versionsDir := t.TempDir()
	dataDir := t.TempDir()
	dir := filepath.Join(versionsDir, "snipaste_v2.11.3")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(dir, "Snipaste.exe")
	if err := os.WriteFile(exe, []byte("exe"), 0644); err != nil {
		t.Fatal(err)
	}
	manager := version.NewManager(versionsDir)
	store := newSnipasteStore(dataDir)
	if err := store.SetActive("2.11.3"); err != nil {
		t.Fatal(err)
	}
	svc := &SnipasteService{manager: manager, store: store, downloads: map[string]struct{}{}}
	selected, gotExe, err := svc.resolveActiveVersion()
	if err != nil {
		t.Fatal(err)
	}
	if selected != "2.11.3" || gotExe != exe {
		t.Fatalf("selected=%q exe=%q", selected, gotExe)
	}
}

func TestRemoveActiveVersionRejected(t *testing.T) {
	store := newSnipasteStore(t.TempDir())
	if err := store.SetActive("2.11.3"); err != nil {
		t.Fatal(err)
	}
	svc := &SnipasteService{manager: version.NewManager(t.TempDir()), store: store, downloads: map[string]struct{}{}}
	if err := svc.RemoveVersion("2.11.3"); err == nil {
		t.Fatal("active version removal should fail")
	}
}
