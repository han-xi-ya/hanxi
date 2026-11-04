package version

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hanxi/packages/go/artifact"
)

// fakeExe 生成带 MZ 头的伪 exe 字节（内核链只验哈希与大小，MZ 断言验前 2 字节）。
func fakeExe(body string) []byte { return append([]byte("MZ"), []byte(body)...) }

func shaHex(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }

// withRemoteCache 把样例 release 注入进程级缓存（测试期间禁用真实 GitHub 访问）。
func withRemoteCache(t *testing.T, releases []OmpRelease) {
	t.Helper()
	old := remoteCache.data
	oldAt := remoteCache.fetchedAt
	remoteCache.data = releases
	remoteCache.fetchedAt = time.Now()
	t.Cleanup(func() { remoteCache.data, remoteCache.fetchedAt = old, oldAt })
}

// newTestManager 版本树落在 t.TempDir，mirrors 指向给定 URL（回环靶，恒直连——
// LoopbackAwareProxyFunc 内核纪律，无需额外拨闸）。
func newTestManager(t *testing.T, url string) *Manager {
	t.Helper()
	m := NewManager(filepath.Join(t.TempDir(), "versions"))
	m.mirrors = func(_, _ string) []string { return []string{url} }
	return m
}

func TestDownloadContextSuccess(t *testing.T) {
	payload := fakeExe("omp-test-payload-0123456789")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	sha := shaHex(payload)
	withRemoteCache(t, []OmpRelease{{Version: "v18.16.0", Tag: "v18.16.0", AssetName: "omp-windows-x64.exe", Size: int64(len(payload)), SHA256: sha}})

	m := newTestManager(t, srv.URL)
	var stages []string
	err := m.DownloadContext(context.Background(), "txn-ok", "v18.16.0", func(p DownloadProgress) {
		stages = append(stages, p.Stage)
	})
	if err != nil {
		t.Fatalf("DownloadContext 期望成功: %v", err)
	}
	joined := strings.Join(stages, ",")
	if !strings.HasPrefix(joined, "downloading") || !strings.HasSuffix(joined, "done") {
		t.Errorf("阶段序列应以 downloading 开、done 收，实际：%v", stages)
	}
	exe, err := m.ResolveExe("18.16.0")
	if err != nil {
		t.Fatalf("ResolveExe: %v", err)
	}
	got, _ := os.ReadFile(exe)
	if shaHex(got) != sha {
		t.Errorf("落位内容哈希不符")
	}
	list, err := m.ListInstalled()
	if err != nil || len(list) != 1 {
		t.Fatalf("ListInstalled 期望 1 条，实际 %+v err=%v", list, err)
	}
	if list[0].Version != "v18.16.0" || list[0].Source != artifact.SourceRemote || list[0].SHA256 != sha {
		t.Errorf("账本字段不符：%+v", list[0])
	}
	if fi, _ := os.Stat(exe); fi == nil || fi.Size() != int64(len(payload)) {
		t.Errorf("落位大小与上游声明不一致")
	}
}

func TestDownloadContextTamperedPayload(t *testing.T) {
	payload := fakeExe("original-bytes")
	tampered := fakeExe("tampered-bytes!")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(tampered) // 尺寸一致、哈希不同：内核流式 SHA 必炸
	}))
	defer srv.Close()

	withRemoteCache(t, []OmpRelease{{Version: "v1.2.3", Tag: "v1.2.3", AssetName: "omp-windows-x64.exe", Size: int64(len(payload)), SHA256: shaHex(payload)}})
	m := newTestManager(t, srv.URL)

	var errStage string
	err := m.DownloadContext(context.Background(), "txn-bad", "v1.2.3", func(p DownloadProgress) {
		if p.Stage == "error" {
			errStage = p.Message
		}
	})
	if err == nil {
		t.Fatalf("篡改载荷必须失败")
	}
	if errStage == "" {
		t.Errorf("错误阶段未上报告警事件")
	}
	if _, rerr := m.ResolveExe("1.2.3"); rerr == nil {
		t.Errorf("校验失败不得留下已落位版本")
	}
}

func TestImportLocalFallbackToken(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "omp.exe")
	payload := fakeExe("imported-local-binary-not-executable")
	if err := os.WriteFile(src, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	withRemoteCache(t, nil) // 空远程：哈希嗅探与 --version 探测都注定失败 → imported-<sha12>

	m := NewManager(filepath.Join(dir, "versions"))
	info, err := m.ImportLocal(context.Background(), "txn-imp", src)
	if err != nil {
		t.Fatalf("ImportLocal: %v", err)
	}
	wantTok := "imported-" + shaHex(payload)[:12]
	if info.Version != "v"+wantTok || info.Source != artifact.SourceImported || info.SHA256 != shaHex(payload) {
		t.Errorf("兜底令牌/账本不符：%+v", info)
	}
	if _, rerr := m.ResolveExe(wantTok); rerr != nil {
		t.Errorf("导入后 ResolveExe: %v", rerr)
	}
}

func TestImportLocalHashMatchesRemoteAdoptsOfficialVersion(t *testing.T) {
	dir := t.TempDir()
	payload := fakeExe("official-build-bytes")
	src := filepath.Join(dir, "omp.exe")
	if err := os.WriteFile(src, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	sha := shaHex(payload)
	withRemoteCache(t, []OmpRelease{{Version: "v9.9.9", Tag: "v9.9.9", AssetName: "omp-windows-x64.exe", Size: int64(len(payload)), SHA256: sha}})

	m := NewManager(filepath.Join(dir, "versions"))
	info, err := m.ImportLocal(context.Background(), "txn-hash", src)
	if err != nil {
		t.Fatalf("ImportLocal: %v", err)
	}
	if info.Version != "v9.9.9" {
		t.Errorf("哈希命中官方摘要应采纳官方版本令牌，实际 %q", info.Version)
	}
	if info.Source != artifact.SourceImported {
		t.Errorf("来源必须诚实标 imported，实际 %q", info.Source)
	}
}

func TestImportLocalRejectsNonPE(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "not-exe.exe")
	if err := os.WriteFile(src, []byte("HTML error page pretending"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := NewManager(filepath.Join(dir, "versions"))
	if _, err := m.ImportLocal(context.Background(), "txn-junk", src); err == nil {
		t.Fatalf("非 MZ 载荷必须拒收")
	}
}

func TestRemoveActiveAndCheckUpdateLocalOnly(t *testing.T) {
	dir := t.TempDir()
	payload := fakeExe("installed-bytes")
	sha := shaHex(payload)
	withRemoteCache(t, []OmpRelease{{Version: "v18.16.0", Tag: "v18.16.0", AssetName: "omp-windows-x64.exe", Size: int64(len(payload)), SHA256: sha}})

	m := NewManager(filepath.Join(dir, "versions"))
	staging, discard, err := m.tree.StageDir("txn-seed")
	if err != nil {
		t.Fatal(err)
	}
	defer discard()
	if err := os.WriteFile(filepath.Join(staging, exeName), payload, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := m.tree.Commit(staging, "18.16.0", artifact.Meta{Entry: exeName, ZipSHA256: sha, AssetSHA256: sha, Source: artifact.SourceRemote, InstalledAt: time.Now()}); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// 远程最新 == 本机已装：无更新
	local, remote, hasUpdate, err := m.CheckUpdate(context.Background())
	if err != nil || local != "v18.16.0" || remote != "v18.16.0" || hasUpdate {
		t.Errorf("同版本不应报更新：%q/%q/%v/%v", local, remote, hasUpdate, err)
	}

	// 远程前进一位：报更新
	withRemoteCache(t, []OmpRelease{{Version: "v18.17.0", Tag: "v18.17.0", AssetName: "omp-windows-x64.exe", Size: 1, SHA256: strings.Repeat("0a", 32)}})
	if _, _, hasUpdate, _ = m.CheckUpdate(context.Background()); !hasUpdate {
		t.Errorf("远程更新应点亮 hasUpdate")
	}

	// imported 兜底目录不参与更新判定（本机视作无稳定资产）
	if err := m.Remove("18.16.0"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if list, _ := m.ListInstalled(); len(list) != 0 {
		t.Errorf("Remove 后仍可见：%+v", list)
	}
	local, _, hasUpdate, err = m.CheckUpdate(context.Background())
	if err != nil || local != "" || hasUpdate {
		t.Errorf("无已装资产应无法判定：%q/%v/%v", local, hasUpdate, err)
	}
}
