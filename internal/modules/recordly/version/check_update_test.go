// check_update_test.go 覆盖 NSIS 单目录托管族代表（recordly：ListRemote 带
// includePre 通道参数、已装版本读 hanxi-meta tag）的 UpdateChecker shim。
package version

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// seedRemoteList 多通道条目注入全局 remoteCache（TTL 内 ListRemote(false)
// 免网络并按 IsPre 裁剪 beta 通道）。
func seedRemoteList(t *testing.T, releases ...RecordlyRelease) {
	t.Helper()
	remoteCache.mu.Lock()
	remoteCache.data = releases
	remoteCache.fetchedAt = time.Now()
	remoteCache.mu.Unlock()
	t.Cleanup(func() {
		remoteCache.mu.Lock()
		remoteCache.data = nil
		remoteCache.fetchedAt = time.Time{}
		remoteCache.mu.Unlock()
	})
}

// installRecordly 伪造托管目录现态：Recordly.exe 非空 + resources/app.asar
// 双锚点 + hanxi-meta tag 权威版本（PE 读取链不属于 shim 关注面）。
func installRecordly(t *testing.T, versionsDir, tag string) {
	t.Helper()
	dir := filepath.Join(versionsDir, installDirName)
	if err := os.MkdirAll(filepath.Join(dir, "resources"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, exeName), []byte("exe"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, asarRelPath), []byte("asar"), 0644); err != nil {
		t.Fatal(err)
	}
	meta := `{"tag":"` + tag + `"}`
	if err := os.WriteFile(filepath.Join(dir, "hanxi-meta.json"), []byte(meta), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestCheckUpdateRecordlyFamily(t *testing.T) {
	ctx := context.Background()

	t.Run("稳定通道有新版", func(t *testing.T) {
		seedRemoteList(t,
			RecordlyRelease{Version: "v1.5.0-beta.2", IsPre: true},
			RecordlyRelease{Version: "v1.4.0"},
		)
		dir := t.TempDir()
		installRecordly(t, dir, "v1.3.3")
		m := NewManager(dir)
		local, remote, up, err := m.CheckUpdate(ctx)
		if err != nil || !up || local != "v1.3.3" || remote != "v1.4.0" {
			t.Fatalf("got (%q,%q,%v,%v), want (v1.3.3,v1.4.0,true,nil)", local, remote, up, err)
		}
	})

	t.Run("beta 领先 stable 落后本机=无更新", func(t *testing.T) {
		seedRemoteList(t,
			RecordlyRelease{Version: "v1.5.0-beta.2", IsPre: true},
			RecordlyRelease{Version: "v1.3.3"},
		)
		dir := t.TempDir()
		installRecordly(t, dir, "v1.3.3")
		m := NewManager(dir)
		if _, _, up, err := m.CheckUpdate(ctx); err != nil || up {
			t.Fatalf("up=%v err=%v, want 无更新", up, err)
		}
	})

	t.Run("未安装托管副本不误报", func(t *testing.T) {
		seedRemoteList(t, RecordlyRelease{Version: "v1.4.0"})
		m := NewManager(t.TempDir())
		local, remote, up, err := m.CheckUpdate(ctx)
		if err != nil || up || local != "" || remote != "v1.4.0" {
			t.Fatalf("got (%q,%q,%v,%v)", local, remote, up, err)
		}
	})
}
