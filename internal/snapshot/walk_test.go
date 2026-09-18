package snapshot

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func makeDirLink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("当前 Windows 环境不允许创建目录符号链接: %v", err)
		}
		t.Fatal(err)
	}
}

func TestSafeEnumerationRejectsWhitelistLinkEscape(t *testing.T) {
	dataDir := t.TempDir()
	outside := t.TempDir()
	write(t, outside, "secret.json", `{"token":"outside"}`)
	makeDirLink(t, outside, filepath.Join(dataDir, rootState))

	if roots := WhitelistRoots(dataDir); len(roots) != 0 {
		t.Fatalf("链接根不得进入白名单根: %v", roots)
	}
	if _, found := ScanMtime(dataDir); found {
		t.Fatal("ScanMtime 不得跟随链接根")
	}
	if _, err := fingerprint(dataDir); err == nil {
		t.Fatal("备份指纹应拒绝链接根，而非读取数据根外内容")
	}
}

func TestSafeEnumerationRejectsNestedLinkEscape(t *testing.T) {
	dataDir := t.TempDir()
	outside := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dataDir, rootState), 0755); err != nil {
		t.Fatal(err)
	}
	write(t, outside, "secret.json", `{"token":"outside"}`)
	makeDirLink(t, outside, filepath.Join(dataDir, rootState, "linked"))

	if _, found := ScanMtime(dataDir); found {
		t.Fatal("ScanMtime 不得跟随嵌套链接")
	}
	if _, err := fingerprint(dataDir); err == nil {
		t.Fatal("备份指纹应拒绝嵌套链接")
	}
}

func TestValidateWhitelistPathRejectsLinkAncestor(t *testing.T) {
	dataDir := t.TempDir()
	outside := t.TempDir()
	makeDirLink(t, outside, filepath.Join(dataDir, rootMemo))
	if err := validateWhitelistPathOnDisk(dataDir, "memo/a.md", true); err == nil {
		t.Fatal("git 路径门卫应拒绝链接祖先")
	}
}
