// RAMMap 版本链测试：日期版模型（Last-Modified 解析/旧版请求拒装）、
// 单源降级三层关卡（截断/字节数/流式上限/平铺布局）、导入令牌、
// 更新判定与 journal 链。全部真 tmp 目录 + httptest 假源，无网络依赖。
package version

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"hanxi/internal/extapi"
	"hanxi/internal/ops"
	"hanxi/packages/go/artifact"
	"hanxi/packages/go/operation"
)

// ---------- 夹具 ----------

// seedRemote 注入伪"最新版"（date=版本令牌，size=声明字节数）。
func seedRemote(t *testing.T, date string, size int64) {
	t.Helper()
	remoteCache.mu.Lock()
	defer remoteCache.mu.Unlock()
	remoteCache.data = []RammapRelease{{
		Version: date, Published: "Sat, 26 Mar 2026 19:56:22 GMT",
		AssetName: "RAMMap.zip", AssetURL: zipURL, Size: size,
	}}
	remoteCache.size = size
	remoteCache.fetchedAt = time.Now()
}

// zipSource 可编程假源：支持截断与恒定失败。
type zipSource struct {
	mu       sync.Mutex
	body     []byte
	truncate bool
	hits     int
	srv      *httptest.Server
}

func newZipSource(t *testing.T) *zipSource {
	t.Helper()
	z := &zipSource{}
	z.srv = httptest.NewServer(http.HandlerFunc(z.serve))
	t.Cleanup(z.srv.Close)
	return z
}

func (z *zipSource) serve(w http.ResponseWriter, r *http.Request) {
	z.mu.Lock()
	body, truncate := z.body, z.truncate
	z.hits++
	z.mu.Unlock()
	if r.Method == http.MethodHead {
		w.Header().Set("Content-Length", fmt.Sprint(len(body)))
		w.Header().Set("Last-Modified", "Sat, 26 Mar 2026 19:56:22 GMT")
		return
	}
	w.Header().Set("Content-Length", fmt.Sprint(len(body)))
	if truncate {
		_, _ = w.Write(body[:len(body)/2])
		panic(http.ErrAbortHandler)
	}
	_, _ = w.Write(body)
}

func (z *zipSource) set(body []byte, truncate bool) {
	z.mu.Lock()
	z.body, z.truncate = body, truncate
	z.mu.Unlock()
}

func (z *zipSource) count() int {
	z.mu.Lock()
	defer z.mu.Unlock()
	return z.hits
}

// newChainManager 假源 Manager（bespoke 下载段指向回环 http，其余全走生产链）。
func newChainManager(t *testing.T, src *zipSource) *Manager {
	t.Helper()
	m := NewManager(t.TempDir())
	m.client = src.srv.Client()
	return m
}

// buildZip 官方平铺布局：三架构 exe + Eula（无根目录包裹）。
func buildZip(t *testing.T, exeContent string) []byte {
	t.Helper()
	path := filepath.Join(t.TempDir(), "rammap.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for name, content := range map[string]string{
		"RAMMap.exe": "x86", "RAMMap64.exe": exeContent, "RAMMap64a.exe": "arm", "Eula.txt": "license",
	} {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
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

func newTxnID() string { return fmt.Sprintf("txn-%d", time.Now().UnixNano()) }

// redirectDL 把 bespoke 下载段的实际请求指向回环假源（zipURL 为包级常量不改，
// 仅经 dl 接缝替换真实目标地址——下载语义仍走生产 downloadTo 全链）。
func redirectDL(m *Manager, src *zipSource) {
	m.dl = func(ctx context.Context, _ *http.Client, _ string, dest string, maxBytes int64, onProgress func(int64)) error {
		return downloadTo(ctx, src.srv.Client(), src.srv.URL+"/RAMMap.zip", dest, maxBytes, onProgress)
	}
}

// ---------- 日期版模型 ----------

func TestParseLastModified(t *testing.T) {
	for h, want := range map[string]string{
		"Sat, 26 Mar 2026 19:56:22 GMT": "2026-03-26",
		"":                              "",
		"garbage":                       "",
		"Wed, 01 Jan 2025 00:00:00 GMT": "2025-01-01",
	} {
		if got := parseLastModified(h); got != want {
			t.Errorf("parseLastModified(%q)=%q want %q", h, got, want)
		}
	}
	if !dateToken.MatchString("2026-03-26") || dateToken.MatchString("v1.63") {
		t.Fatal("dateToken 形状判定失真")
	}
}

func TestDownloadStaleVersionRefusedBeforeTransfer(t *testing.T) {
	src := newZipSource(t)
	m := newChainManager(t, src)
	zipBytes := buildZip(t, "MZ")
	seedRemote(t, "2026-03-26", int64(len(zipBytes)))
	src.set(zipBytes, false)
	redirectDL(m, src)

	err := m.Download(newTxnID(), "2025-01-01", nil) // 点名旧版：上游无历史资产
	if err == nil || !strings.Contains(err.Error(), "历史资产") {
		t.Fatalf("旧版请求必须如实拒绝而非静默装最新版: %v", err)
	}
	if src.count() != 0 {
		t.Fatalf("拒绝判定必须发生在传输前: hits=%d", src.count())
	}
}

// ---------- 降级链关卡 ----------

func TestDownloadSuccessInstallsCurrentDate(t *testing.T) {
	src := newZipSource(t)
	m := newChainManager(t, src)
	zipBytes := buildZip(t, "MZ-rammap")
	seedRemote(t, "2026-03-26", int64(len(zipBytes)))
	src.set(zipBytes, false)
	redirectDL(m, src)

	rec := &progressRecorder{}
	if err := m.Download(newTxnID(), "", rec.cb()); err != nil {
		t.Fatalf("Download: %v", err)
	}
	st := rec.stages()
	if len(st) != 4 || st[0] != "downloading" || st[3] != "done" {
		t.Fatalf("阶段叙事失真: %v", st)
	}
	list, err := m.ListInstalled()
	if err != nil || len(list) != 1 || list[0].Version != "2026-03-26" {
		t.Fatalf("ListInstalled: %+v err=%v", list, err)
	}
	if list[0].VerifiedHash {
		t.Fatal("无官方摘要必须记 verifiedHash=false")
	}
	if !strings.HasSuffix(list[0].ExePath, payloadExeName()) {
		t.Fatalf("平铺布局解析失真: %s", list[0].ExePath)
	}
	assertNoLeftovers(t, m.versionsDir)
}

func TestDownloadInterruptAllAttemptsFail(t *testing.T) {
	src := newZipSource(t)
	m := newChainManager(t, src)
	zipBytes := buildZip(t, "MZ")
	seedRemote(t, "2026-03-26", int64(len(zipBytes)))
	src.set(zipBytes, true) // 恒截断
	redirectDL(m, src)

	err := m.Download(newTxnID(), "", nil)
	if err == nil {
		t.Fatal("恒截断必须报错")
	}
	if src.count() < downloadAttempts {
		t.Fatalf("单源必须走满重试: hits=%d", src.count())
	}
	assertNoLeftovers(t, m.versionsDir)
	if list, _ := m.ListInstalled(); len(list) != 0 {
		t.Fatalf("失败不得留落位: %+v", list)
	}
}

func TestDownloadOverBudgetStreamAborted(t *testing.T) {
	src := newZipSource(t)
	m := newChainManager(t, src)
	zipBytes := buildZip(t, "MZ")
	pad := append(append([]byte{}, zipBytes...), make([]byte, 4096)...)
	seedRemote(t, "2026-03-26", int64(len(zipBytes))) // 声明小、实发大
	src.set(pad, false)
	redirectDL(m, src)

	err := m.Download(newTxnID(), "", nil)
	if err == nil || !strings.Contains(err.Error(), "超出期望体积") {
		t.Fatalf("流式上限必须断异常放大: %v", err)
	}
	assertNoLeftovers(t, m.versionsDir)
}

func TestDownloadRejectsMissingPayload(t *testing.T) {
	src := newZipSource(t)
	m := newChainManager(t, src)
	path := filepath.Join(t.TempDir(), "odd.zip")
	f, _ := os.Create(path)
	zw := zip.NewWriter(f)
	w, _ := zw.Create("readme.txt")
	w.Write([]byte("没有本架构载荷"))
	zw.Close()
	f.Close()
	body, _ := os.ReadFile(path)
	seedRemote(t, "2026-03-26", int64(len(body)))
	src.set(body, false)
	redirectDL(m, src)

	err := m.Download(newTxnID(), "", nil)
	if err == nil || !strings.Contains(err.Error(), "zip 布局无效") {
		t.Fatalf("布局自检必须拒收: %v", err)
	}
	assertNoLeftovers(t, m.versionsDir)
}

// ---------- 导入令牌与更新判定 ----------

func TestLooksLikeVersionAndImportToken(t *testing.T) {
	for s, want := range map[string]bool{"1.63": true, "2": true, "1.2.3.4": true, "": false, "1.x": false, "1.2.3.4.5": false} {
		if got := looksLikeVersion(s); got != want {
			t.Errorf("looksLikeVersion(%q)=%v want %v", s, got, want)
		}
	}
	junk := filepath.Join(t.TempDir(), "RAMMap64.exe")
	if err := os.WriteFile(junk, []byte("not-pe"), 0644); err != nil {
		t.Fatal(err)
	}
	if tok := importToken(junk); !strings.HasPrefix(tok, "imported-") {
		t.Fatalf("非 PE 必须 imported 兜底: %q", tok)
	}
}

func TestCheckUpdateDateComparison(t *testing.T) {
	// CheckUpdate 走 ListRemote→remoteCache（TTL 内命中种子缓存，不触网）
	remoteCache.mu.Lock()
	remoteCache.data = []RammapRelease{{Version: "2026-03-26"}}
	remoteCache.fetchedAt = time.Now()
	remoteCache.mu.Unlock()

	m := NewManager(t.TempDir())
	installFixture(t, m.versionsDir, "2025-01-01")

	local, remote, has, err := m.CheckUpdate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if local != "2025-01-01" || remote != "2026-03-26" || !has {
		t.Fatalf("日期版更新判定失真: local=%q remote=%q has=%v", local, remote, has)
	}

	// 本机已是同日版 → 无更新
	installFixture(t, m.versionsDir, "2026-03-26")
	_, _, has, _ = m.CheckUpdate(context.Background())
	if has {
		t.Fatal("同日版不得谎报更新")
	}
}

// installFixture 手工摆"已落位"版本目录（本架构 exe + 内核账本）。
func installFixture(t *testing.T, root, token string) {
	t.Helper()
	dir := filepath.Join(root, treeEntryName+"_"+token)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, payloadExeName()), []byte("MZ-fake-bytes"), 0644); err != nil {
		t.Fatal(err)
	}
	meta := artifact.Meta{Schema: artifact.DefaultSchema, Tool: treeEntryName, Version: token,
		Entry: payloadExeName(), InstalledAt: time.Now(), Source: artifact.SourceRemote}
	raw, _ := json.Marshal(meta)
	if err := os.WriteFile(filepath.Join(dir, "meta.json"), raw, 0644); err != nil {
		t.Fatal(err)
	}
}

// ---------- journal 链（2b 交接面） ----------

func withTestKernel(t *testing.T) *operation.Store {
	t.Helper()
	store, err := operation.OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ops.SetKernel(store, operation.NewHub(store))
	t.Cleanup(func() { ops.SetKernel(nil, nil) })
	return store
}

var rammapTestSteps = []string{"download", "unpack", "place"}

func TestJournalChainSuccess(t *testing.T) {
	store := withTestKernel(t)
	src := newZipSource(t)
	m := newChainManager(t, src)
	zipBytes := buildZip(t, "journal-exe")
	seedRemote(t, "2026-02-01", int64(len(zipBytes)))
	src.set(zipBytes, false)
	redirectDL(m, src)

	txnID := newTxnID()
	txn, err := ops.BeginTxn("rammap", extapi.OpInstall, extapi.DeliveryManagedDeclarative, "2026-02-01", txnID, rammapTestSteps)
	if err != nil {
		t.Fatal(err)
	}
	stepIdx := -1
	emit := func(p DownloadProgress) {
		switch p.Stage {
		case "downloading":
			if stepIdx < 0 {
				stepIdx = 0
				txn.Step(stepIdx)
			}
			txn.Progress(p.Done, p.Total)
		case "extract":
			if stepIdx < 1 {
				stepIdx = 1
				txn.Step(stepIdx)
			}
		}
	}
	if err := m.Download(txnID, "2026-02-01", emit); err != nil {
		t.Fatalf("Download: %v", err)
	}
	txn.Done()

	j, err := store.Get(txnID)
	if err != nil {
		t.Fatal(err)
	}
	if j.State != operation.TxnSucceeded || j.ModuleID != "rammap" || len(j.Steps) != 3 {
		t.Fatalf("账本终态异常: %+v", j)
	}
	assertNoLeftovers(t, m.versionsDir)
}

func TestJournalSingleWriteRejectsConcurrent(t *testing.T) {
	withTestKernel(t)
	if _, err := ops.BeginTxn("rammap", extapi.OpInstall, extapi.DeliveryManagedDeclarative, "a", newTxnID(), rammapTestSteps); err != nil {
		t.Fatal(err)
	}
	if _, err := ops.BeginTxn("rammap", extapi.OpInstall, extapi.DeliveryManagedDeclarative, "b", newTxnID(), rammapTestSteps); !errors.Is(err, operation.ErrTxnActive) {
		t.Fatalf("并发第二笔应被单写纪律拒绝, got %v", err)
	}
}
