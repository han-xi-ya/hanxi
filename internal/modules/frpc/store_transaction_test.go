package frpc

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"hanxi/internal/domain"
)

func TestStoreCorruptBlocksMutation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "projects.json")
	original := []byte(`{"projects": broken}`)
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	s := newFrpcStore(dir)
	if _, err := s.List(); err == nil {
		t.Fatal("missing load error")
	}
	p := domain.Project{Name: "new"}
	if err := s.Save(&p); err == nil {
		t.Fatal("save accepted corrupt store")
	}
	if p.ID != "" || p.UpdatedAt != "" {
		t.Fatal("failed save mutated caller")
	}
	if err := s.Delete("missing"); err == nil {
		t.Fatal("delete accepted corrupt store")
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != string(original) {
		t.Fatalf("original changed: %s, %v", got, err)
	}
}

func TestStoreTransactionalFailureAndCopies(t *testing.T) {
	s := newFrpcStore(t.TempDir())
	p := domain.Project{Name: "before", Proxies: []domain.ProxyRule{{Name: "proxy", CustomDomains: []string{"before.example"}}}}
	if err := s.Save(&p); err != nil {
		t.Fatal(err)
	}
	p.Proxies[0].CustomDomains[0] = "caller.example"
	saved, _ := s.Get(p.ID)
	if saved.Proxies[0].CustomDomains[0] != "before.example" {
		t.Fatal("save retained caller slice")
	}
	saved.Proxies[0].CustomDomains[0] = "getter.example"
	list, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if list[0].Proxies[0].CustomDomains[0] != "before.example" {
		t.Fatal("get returned internal slice")
	}
	list[0].Proxies[0].Name = "list mutation"
	before, _ := s.Get(p.ID)
	diskBefore, err := os.ReadFile(s.filePath)
	if err != nil {
		t.Fatal(err)
	}
	originalPath := s.filePath
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, []byte("block"), 0600); err != nil {
		t.Fatal(err)
	}
	s.filePath = filepath.Join(blocker, "projects.json")
	p.Name = "after"
	inputBefore := cloneProject(p)
	if err := s.Save(&p); err == nil {
		t.Fatal("expected write failure")
	}
	if !reflect.DeepEqual(p, inputBefore) {
		t.Fatal("failed save mutated input")
	}
	if err := s.Delete(p.ID); err == nil {
		t.Fatal("expected delete write failure")
	}
	after, ok := s.Get(p.ID)
	if !ok || !reflect.DeepEqual(before, after) {
		t.Fatal("write failure changed memory")
	}
	diskAfter, err := os.ReadFile(originalPath)
	if err != nil || string(diskBefore) != string(diskAfter) {
		t.Fatal("write failure changed original")
	}
	fresh := domain.Project{Name: "fresh"}
	if err := s.Save(&fresh); err == nil || fresh.ID != "" {
		t.Fatal("failed create assigned ID")
	}
}
