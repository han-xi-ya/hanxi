//go:build live_e2e

package version

import (
	"os"
	"path/filepath"
	"testing"
)

// 一次性真机全链路校验：真实 GitHub API + 镜像下载 + 官方 sha256 + 顶层目录展平解压。
// 默认不参与常规 go test ./...（受 build tag 门控），手动 `go test -tags live_e2e` 触发。
func TestLiveDownloadLatest(t *testing.T) {
	rels, err := (&Manager{versionsDir: t.TempDir()}).ListRemote()
	if err != nil {
		t.Fatalf("ListRemote: %v", err)
	}
	if len(rels) == 0 {
		t.Fatal("远程列表为空")
	}
	latest := rels[0]
	t.Logf("最新可安装: %s (%s, %d bytes, sha=%s…)", latest.Version, latest.AssetName, latest.Size, latest.SHA256[:12])

	dir := t.TempDir()
	m := NewManager(dir)
	if err := m.Download(latest.Version, func(p DownloadProgress) {
		t.Logf("progress: %s done=%d total=%d %s", p.Stage, p.Done, p.Total, p.Message)
	}); err != nil {
		t.Fatalf("Download: %v", err)
	}
	// 三锚点必须落在隔离目录根（顶层 Bili23-Downloader/ 已剥离）
	install := filepath.Join(dir, "bili23_"+trimV(latest.Version))
	for _, rel := range []string{exeName, bootstrapName, filepath.FromSlash(scriptMainRel)} {
		if fi, err := os.Stat(filepath.Join(install, rel)); err != nil || fi.Size() == 0 {
			t.Errorf("锚点缺失或空: %s (err=%v)", rel, err)
		}
	}
	if _, err := os.Stat(filepath.Join(install, topDirName)); !os.IsNotExist(err) {
		t.Error("顶层目录未剥离")
	}
	info, err := m.ListInstalled()
	if err != nil || len(info) != 1 {
		t.Fatalf("ListInstalled = %+v, %v", info, err)
	}
	if got := detectAppVersion(install); got != trimV(latest.Version) {
		t.Errorf("导入版本探测与 tag 不一致: config app_version=%q tag=%q", got, latest.Version)
	}
}

func trimV(s string) string {
	if len(s) > 0 && s[0] == 'v' {
		return s[1:]
	}
	return s
}
