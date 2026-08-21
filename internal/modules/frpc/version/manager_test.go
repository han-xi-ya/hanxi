package version

import (
	"os"
	"path/filepath"
	"testing"
)

// writeFakeExe 创建假 frpc.exe（含有效内容）
func writeFakeExe(t *testing.T, dir, name string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(dir, name)
	if err := os.WriteFile(exe, []byte("fake frpc binary content"), 0644); err != nil {
		t.Fatal(err)
	}
	return exe
}

func TestListInstalledCompatDirs(t *testing.T) {
	versionsDir := t.TempDir()

	// 规范命名：frp_v0.61.1
	writeFakeExe(t, filepath.Join(versionsDir, "frp_v0.61.1"), "frpc.exe")
	// 旧命名（下载创建）：frp_0.71.0
	writeFakeExe(t, filepath.Join(versionsDir, "frp_0.71.0"), "frpc.exe")
	// 导入探测失败：frp_imported-20260821-120000
	writeFakeExe(t, filepath.Join(versionsDir, "frp_imported-20260821-120000"), "frpc.exe")
	// 无关目录（不应被识别）
	_ = os.MkdirAll(filepath.Join(versionsDir, "other-tool"), 0755)

	m := NewManager(versionsDir)
	list, err := m.ListInstalled()
	if err != nil {
		t.Fatalf("ListInstalled failed: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("expected 3 installed versions, got %d: %+v", len(list), list)
	}

	versions := map[string]bool{}
	for _, v := range list {
		versions[v.Version] = true
		if v.ExePath == "" || v.SHA256 == "" {
			t.Errorf("version %s missing exePath/sha256", v.Version)
		}
	}
	for _, want := range []string{"v0.61.1", "v0.71.0", "imported-20260821-120000"} {
		if !versions[want] {
			t.Errorf("missing version %q in %v", want, versions)
		}
	}

	// ResolveExe 兼容两种命名
	if _, err := m.ResolveExe("v0.61.1"); err != nil {
		t.Errorf("ResolveExe(v0.61.1) failed: %v", err)
	}
	if _, err := m.ResolveExe("v0.71.0"); err != nil {
		t.Errorf("ResolveExe(v0.71.0) failed: %v", err)
	}
	if _, err := m.ResolveExe("v9.9.9"); err == nil {
		t.Error("ResolveExe(v9.9.9) should fail")
	}

	// Remove 兼容旧命名
	if err := m.Remove("v0.71.0"); err != nil {
		t.Errorf("Remove(v0.71.0) failed: %v", err)
	}
	rest, _ := m.ListInstalled()
	if len(rest) != 2 {
		t.Errorf("expected 2 versions after remove, got %d", len(rest))
	}
}

func TestVersionFromDirName(t *testing.T) {
	cases := map[string]string{
		"frp_v0.61.1":             "v0.61.1",
		"frp_0.61.1":              "v0.61.1",
		"frp_imported-20260821-1": "imported-20260821-1",
		"other-tool":              "",
		"frp_v":                   "",
		"frpc.exe":                "",
	}
	for name, want := range cases {
		got, ok := versionFromDirName(name)
		if want == "" {
			if ok {
				t.Errorf("versionFromDirName(%q) should be rejected, got %q", name, got)
			}
			continue
		}
		if !ok || got != want {
			t.Errorf("versionFromDirName(%q) = %q, %v; want %q", name, got, ok, want)
		}
	}
}
