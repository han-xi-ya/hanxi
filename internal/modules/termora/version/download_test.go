// 下载链关卡测试：官方摘要必检（缺摘要拒装——markeron 纪律）、jpackage
// 嵌套布局与 app/ 目录自检、portable data\ 激活器补齐、失败不留半件。
// 传输与摘要双核本体由 artifact 包测试矩阵担保，此处以 fetch  seam 注入。
package version

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"hanxi/packages/go/artifact"
)

func seedRemote(t *testing.T, version, assetName string, size int64, digest string) {
	t.Helper()
	remoteCache.mu.Lock()
	defer remoteCache.mu.Unlock()
	remoteCache.data = []TermoraRelease{{
		Version:   version,
		AssetName: assetName,
		AssetURL:  "http://127.0.0.1:0/placeholder",
		Size:      size,
	}}
	remoteCache.digests = map[string]string{}
	if digest != "" {
		remoteCache.digests[version] = digest
	}
	remoteCache.fetchedAt = time.Now()
}

// buildPortableZip jpackage app-image 形状：Termora/ 固定包根 + exe + app/。
func buildPortableZip(t *testing.T, exeContent string) []byte {
	t.Helper()
	return writeTestZip(t, map[string]string{
		payloadDir + "/" + exeName:           exeContent,
		payloadDir + "/app/Termora.jar":      "jar",
		payloadDir + "/app/Termora.cfg":      "[Application]",
		payloadDir + "/runtime/bin/java.dll": "dll",
	})
}

type progressRecorder struct {
	mu     sync.Mutex
	events []DownloadProgress
}

func (r *progressRecorder) cb() func(DownloadProgress) {
	return func(p DownloadProgress) {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.events = append(r.events, p)
	}
}

func (r *progressRecorder) stages() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for _, e := range r.events {
		if len(out) == 0 || out[len(out)-1] != e.Stage {
			out = append(out, e.Stage)
		}
	}
	return out
}

func assertNoLeftovers(t *testing.T, root string) {
	t.Helper()
	ents, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		t.Fatal(err)
	}
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), ".tmp-") || strings.HasPrefix(e.Name(), ".corrupt-") {
			t.Fatalf("残留半件: %s", e.Name())
		}
	}
}

// fakeFetch 以 zip 字节回放内核 Fetch 落盘行为，并核对 Source 契约字段。
func fakeFetch(m *Manager, zipBytes []byte, wantDigest string, fail error) {
	m.fetch = func(_ context.Context, src artifact.Source, dest string, _ func(artifact.Progress), _ time.Duration) error {
		if fail != nil {
			return fail
		}
		if src.SHA256 != wantDigest {
			return &errorCapture{msg: "fetch 未透传官方摘要: " + src.SHA256}
		}
		if src.MaxBytes <= 0 {
			return &errorCapture{msg: "fetch 未设流式上限"}
		}
		return os.WriteFile(dest, zipBytes, 0644)
	}
}

type errorCapture struct{ msg string }

func (e *errorCapture) Error() string { return e.msg }

func TestDownloadOfficialDigestChainSuccess(t *testing.T) {
	zipBytes := buildPortableZip(t, "MZ-termora")
	seedRemote(t, "v2.0.0-beta.16", "termora-2.0.0-beta.16-windows-x86-64.zip", int64(len(zipBytes)), strings.Repeat("b", 64))
	m := NewManager(t.TempDir())
	fakeFetch(m, zipBytes, strings.Repeat("b", 64), nil)
	m.mirrors = func(tag, asset string) []string { return []string{"https://github.com/x/" + asset} }

	rec := &progressRecorder{}
	if err := m.Download(newTxnID(), "2.0.0-beta.16", rec.cb()); err != nil {
		t.Fatalf("Download: %v", err)
	}
	st := rec.stages()
	if len(st) != 3 || st[0] != "downloading" || st[1] != "extract" || st[2] != "done" {
		t.Fatalf("阶段叙事失真（markeron 口径：verify 折进 download 不造幻影步骤）: %v", st)
	}

	list, err := m.ListInstalled()
	if err != nil || len(list) != 1 {
		t.Fatalf("ListInstalled: %+v err=%v", list, err)
	}
	if !strings.HasSuffix(list[0].ExePath, filepath.Join(payloadDir, exeName)) {
		t.Fatalf("jpackage 嵌套布局应保持: %s", list[0].ExePath)
	}
	// portable 激活器落位
	if fi, serr := os.Stat(filepath.Join(filepath.Dir(list[0].ExePath), dataDirName)); serr != nil || !fi.IsDir() {
		t.Fatalf("下载链必须补齐 data\\ 激活目录: %v", serr)
	}
	assertNoLeftovers(t, m.versionsDir)
}

func TestDownloadMissingDigestRefusesUnverified(t *testing.T) {
	zipBytes := buildPortableZip(t, "MZ")
	seedRemote(t, "v2.0.0-beta.17", "termora-2.0.0-beta.17-windows-x86-64.zip", int64(len(zipBytes)), "") // 上游缺摘要
	m := NewManager(t.TempDir())
	fetched := false
	m.fetch = func(context.Context, artifact.Source, string, func(artifact.Progress), time.Duration) error {
		fetched = true
		return nil
	}
	err := m.Download(newTxnID(), "v2.0.0-beta.17", nil)
	if err == nil || !strings.Contains(err.Error(), "拒绝无校验安装") {
		t.Fatalf("缺官方摘要必须拒装（markeron 纪律）: %v", err)
	}
	if fetched {
		t.Fatal("拒装判定必须发生在任何传输之前")
	}
	assertNoLeftovers(t, m.versionsDir)
}

func TestDownloadRejectsMissingAppDir(t *testing.T) {
	zipBytes := writeTestZip(t, map[string]string{payloadDir + "/" + exeName: "MZ"}) // 有 exe 无 app/
	seedRemote(t, "v1.9.9", "termora-1.9.9-windows-x86-64.zip", int64(len(zipBytes)), strings.Repeat("c", 64))
	m := NewManager(t.TempDir())
	fakeFetch(m, zipBytes, strings.Repeat("c", 64), nil)

	err := m.Download(newTxnID(), "v1.9.9", nil)
	if err == nil || !strings.Contains(err.Error(), "app") {
		t.Fatalf("app/ 载荷目录缺失必须拒收: %v", err)
	}
	assertNoLeftovers(t, m.versionsDir)
}

func TestDownloadRejectsNoPayloadExe(t *testing.T) {
	zipBytes := writeTestZip(t, map[string]string{"readme.txt": "没有主程序的包"})
	seedRemote(t, "v1.9.8", "termora-1.9.8-windows-x86-64.zip", int64(len(zipBytes)), strings.Repeat("d", 64))
	m := NewManager(t.TempDir())
	fakeFetch(m, zipBytes, strings.Repeat("d", 64), nil)

	err := m.Download(newTxnID(), "v1.9.8", nil)
	if err == nil || !strings.Contains(err.Error(), "zip 布局无效") {
		t.Fatalf("布局自检必须拒收: %v", err)
	}
	assertNoLeftovers(t, m.versionsDir)
}

func TestDownloadFetchFailureNoLeftovers(t *testing.T) {
	seedRemote(t, "v2.0.0", "termora-2.0.0-windows-x86-64.zip", 1000, strings.Repeat("e", 64))
	m := NewManager(t.TempDir())
	m.fetch = func(context.Context, artifact.Source, string, func(artifact.Progress), time.Duration) error {
		return &errorCapture{msg: "sha256 校验失败"}
	}
	err := m.Download(newTxnID(), "v2.0.0", nil)
	if err == nil || !strings.Contains(err.Error(), "sha256") {
		t.Fatalf("传输失败必须如实上抛: %v", err)
	}
	assertNoLeftovers(t, m.versionsDir)
	if list, _ := m.ListInstalled(); len(list) != 0 {
		t.Fatalf("失败后不得有落位: %+v", list)
	}
}

func TestDownloadCtxCancelPrecheck(t *testing.T) {
	seedRemote(t, "v2.0.0", "termora-2.0.0-windows-x86-64.zip", 1000, strings.Repeat("f", 64))
	m := NewManager(t.TempDir())
	m.fetch = func(context.Context, artifact.Source, string, func(artifact.Progress), time.Duration) error {
		t.Fatal("已取消 ctx 不得触达传输")
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := m.DownloadContext(ctx, newTxnID(), "v2.0.0", nil); err == nil {
		t.Fatal("预检必须即刻拒")
	}
}

// ---------- 导入链 ----------

func TestImportLocalKeepsUserDataAndCommitsLedger(t *testing.T) {
	m := NewManager(t.TempDir())
	src := filepath.Join(t.TempDir(), "Termora")
	if err := os.MkdirAll(filepath.Join(src, "data", "sessions"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, exeName), []byte("MZ-import"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "data", "sessions", "s1.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	info, err := m.ImportLocal(src)
	if err != nil {
		t.Fatalf("ImportLocal: %v", err)
	}
	if !info.IsImport || info.VerifiedHash {
		t.Fatalf("导入账目失真: %+v", info)
	}
	// 用户会话数据原样收纳（导入即"接着用"）
	if _, err := os.Stat(filepath.Join(filepath.Dir(info.ExePath), "data", "sessions", "s1.json")); err != nil {
		t.Fatalf("data\\sessions 必须随目录收纳: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(info.Dir, "meta.json"))
	if err != nil {
		t.Fatal(err)
	}
	var meta artifact.Meta
	if json.Unmarshal(raw, &meta) != nil || meta.Source != artifact.SourceImported {
		t.Fatalf("内核账本必须记 imported 来源: %s", raw)
	}
	assertNoLeftovers(t, m.versionsDir)
}
