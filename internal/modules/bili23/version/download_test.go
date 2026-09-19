// 下载→校验→解包→落位全链的失败注入测试：以回环 httptest 服务器为"源"，
// 真实驱动内核 artifact.Fetch（镜像接缝改指回环）+ artifact.UnpackZip +
// 本包顶层目录收割 + Tree 落位链（真 tmp 目录，无外网依赖），验证中断、
// 坏摘要、无摘要拒装、ZipSlip、三锚点缺件、半件残留、双版本共存、
// 同版本幂等/防漂移等收口行为（ccswitch/markeron 同构黄金样本；官方
// GitHub digest 全量可得——事实核查 2026-09-19，弱摘要薄适配器路线不适用）。
package version

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"hanxi/packages/go/artifact"
)

// ---------- 测试夹具：远程列表注入与假资产源 ----------

const testAssetName = "Bili23-Downloader_2.15.0_windows_x64_portable.zip"

// seedRemote 注入伪远程列表。remoteCache 是包内全局缓存（生产语义即进程级
// 单例），本包测试串行执行（不使用 t.Parallel），互相隔离靠 setup 重灌。
// Bili23 的官方摘要直接携带在 Bili23Release.SHA256（远程解析层已剥前缀）。
func seedRemote(t *testing.T, version, assetName string, size int64, sha string) {
	t.Helper()
	remoteCache.mu.Lock()
	defer remoteCache.mu.Unlock()
	remoteCache.data = []Bili23Release{{
		Version:   version,
		AssetName: assetName,
		AssetURL:  "http://127.0.0.1:0/placeholder",
		Size:      size,
		SHA256:    sha,
	}}
	remoteCache.fetchedAt = time.Now() // 处于 TTL 内，Download 不会再走网络拉列表
}

// zipSource 可编程假资产源：按当前 body 提供下载，支持截断（模拟中断）与命中计数。
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

	w.Header().Set("Content-Length", fmt.Sprint(len(body)))
	if truncate {
		_, _ = w.Write(body[:len(body)/2])
		panic(http.ErrAbortHandler) // 掐断连接：客户端收到短读（传输中断）
	}
	_, _ = w.Write(body)
}

func (z *zipSource) set(body []byte, truncate bool) {
	z.mu.Lock()
	defer z.mu.Unlock()
	z.body = body
	z.truncate = truncate
}

func (z *zipSource) count() int {
	z.mu.Lock()
	defer z.mu.Unlock()
	return z.hits
}

// newChainManager 构造指向假源的 Manager（仅镜像 URL 接缝改指回环 http，
// 下载/校验/解包/落位全链仍走生产实现 artifact.Fetch/UnpackZip/Tree +
// 本包收割）。
func newChainManager(t *testing.T, src *zipSource) *Manager {
	t.Helper()
	m := NewManager(t.TempDir())
	m.mirrors = func(_, _ string) []string {
		return []string{src.srv.URL + "/" + testAssetName}
	}
	return m
}

// buildPortableZip 官方便携 zip 同构形状：顶层 Bili23-Downloader/ 单目录 +
// 三锚点 + 运行时子树（exeContent 决定 Bili23.exe 内容，用于构造异摘要包）。
func buildPortableZip(t *testing.T, exeContent string) []byte {
	t.Helper()
	entries := bili23ZipEntries()
	entries[topDirName+"/"+exeName] = exeContent
	path := makeTestZip(t, entries)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func shaHex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// progressRecorder 收集 DownloadProgress（附并发安全）。
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

// newTxnID 生成唯一且过 ValidateVersionToken 白名单的事务 ID：Download 的
// staging 目录名 .tmp-<txnID> 由它派生（journal 背书恢复定位现场的交接面）。
func newTxnID() string {
	return fmt.Sprintf("txn-%d", time.Now().UnixNano())
}

// installViaChain 走真实下载链装一个版本（正例夹具复用）。
func installViaChain(t *testing.T, m *Manager, src *zipSource, version, exeContent string) {
	t.Helper()
	zipBytes := buildPortableZip(t, exeContent)
	seedRemote(t, version, "Bili23-Downloader_"+strings.TrimPrefix(version, "v")+"_windows_x64_portable.zip",
		int64(len(zipBytes)), shaHex(zipBytes))
	src.set(zipBytes, false)
	if err := m.Download(newTxnID(), version, nil); err != nil {
		t.Fatalf("Download(%s): %v", version, err)
	}
}

func assertNoTransactionLeftovers(t *testing.T, root string) {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") || strings.Contains(name, ".part") || strings.Contains(name, ".removing") {
			t.Errorf("版本树残留事务/临时件: %s", name)
		}
	}
}

// ---------- 全链正例与失败注入用例 ----------

// TestDownloadChainSuccess 正例全链：Fetch（官方摘要双核）→ UnpackZip →
// 收割顶层目录 → 三锚点自检 → Tree.Commit。断言布局、账本、进度词表零漂移。
func TestDownloadChainSuccess(t *testing.T) {
	src := newZipSource(t)
	m := newChainManager(t, src)
	zipBytes := buildPortableZip(t, "fake-exe")
	seedRemote(t, "v2.15.0", testAssetName, int64(len(zipBytes)), shaHex(zipBytes))
	src.set(zipBytes, false)

	rec := &progressRecorder{}
	if err := m.Download(newTxnID(), "v2.15.0", rec.cb()); err != nil {
		t.Fatalf("Download: %v", err)
	}

	// 进度词表零漂移：既有词汇 downloading → verify → extract → done
	// （verify 由内核摘要双核完成后映射发出，前端展示段保留）
	for _, e := range rec.events {
		switch e.Stage {
		case "downloading", "verify", "extract", "done":
		default:
			t.Fatalf("出现了词表之外的进度阶段 %q", e.Stage)
		}
	}
	if got := strings.Join(rec.stages(), ","); got != "downloading,verify,extract,done" {
		t.Errorf("进度阶段序列 = %q", got)
	}

	dir := filepath.Join(m.versionsDir, "bili23_2.15.0")
	for _, rel := range []string{exeName, bootstrapName, filepath.FromSlash(scriptMainRel),
		filepath.Join("runtime", "python313.dll"), filepath.Join("bundle", "ffmpeg.exe"), "LICENSE"} {
		if _, err := os.Stat(filepath.Join(dir, rel)); err != nil {
			t.Errorf("缺少 %s: %v", rel, err)
		}
	}
	// 顶层包装目录残影绝不允许出现
	if _, err := os.Stat(filepath.Join(dir, topDirName)); !os.IsNotExist(err) {
		t.Error("顶层目录未被收割")
	}

	// 落位账本由内核统一形状写入
	metaRaw, err := os.ReadFile(filepath.Join(dir, "meta.json"))
	if err != nil {
		t.Fatal(err)
	}
	var meta artifact.Meta
	if err := json.Unmarshal(metaRaw, &meta); err != nil {
		t.Fatal(err)
	}
	if meta.Entry != exeName || meta.ZipSHA256 != shaHex(zipBytes) || meta.Source != artifact.SourceRemote || meta.Schema == 0 {
		t.Errorf("账本内容异常: %+v", meta)
	}
	if meta.AssetSHA256 != shaHex([]byte("fake-exe")) {
		t.Errorf("exe 诊断摘要入账异常: %q", meta.AssetSHA256)
	}

	list, lerr := m.ListInstalled()
	if lerr != nil || len(list) != 1 || list[0].Version != "v2.15.0" || list[0].IsImport || list[0].InstalledAt == "" {
		t.Fatalf("列表账目异常: %+v %v", list, lerr)
	}
	if list[0].Size <= 0 {
		t.Errorf("目录总大小应为正数: %+v", list[0])
	}
	assertNoTransactionLeftovers(t, m.versionsDir)
}

// TestDownloadFlatLayoutChain 上游若改为扁平布局（无顶层包装目录），同样能装。
func TestDownloadFlatLayoutChain(t *testing.T) {
	src := newZipSource(t)
	m := newChainManager(t, src)
	zipPath := makeTestZip(t, map[string]string{
		exeName:                         "fake-exe",
		bootstrapName:                   "bootstrap",
		filepath.ToSlash(scriptMainRel): "def _main(): pass",
	})
	zipBytes, err := os.ReadFile(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	seedRemote(t, "v2.15.0", testAssetName, int64(len(zipBytes)), shaHex(zipBytes))
	src.set(zipBytes, false)

	if err := m.Download(newTxnID(), "v2.15.0", nil); err != nil {
		t.Fatalf("扁平布局 Download: %v", err)
	}
	if _, err := m.ResolveExe("v2.15.0"); err != nil {
		t.Errorf("安装应可解析: %v", err)
	}
}

// TestDownloadInterruptKeepsOldVersion 下载中断（传输截断，全部候选源失败）→
// 报 error 进度、旧版本完好、版本树无新目录与半件残留。
func TestDownloadInterruptKeepsOldVersion(t *testing.T) {
	src := newZipSource(t)
	m := newChainManager(t, src)
	installViaChain(t, m, src, "v2.14.0", "old-exe-bytes")

	zipBytes := buildPortableZip(t, "brand-new-exe")
	seedRemote(t, "v2.15.0", testAssetName, int64(len(zipBytes)), shaHex(zipBytes))
	src.set(zipBytes, true) // 半路掐线

	rec := &progressRecorder{}
	err := m.Download(newTxnID(), "v2.15.0", rec.cb())
	if err == nil {
		t.Fatal("下载中断应报错")
	}
	stages := strings.Join(rec.stages(), ",")
	if !strings.Contains(stages, "error") || strings.Contains(stages, "done") {
		t.Fatalf("进度事件应含 error 且不得含 done: %v", stages)
	}
	if _, statErr := os.Stat(filepath.Join(m.versionsDir, "bili23_2.15.0")); !os.IsNotExist(statErr) {
		t.Error("新半件目录不得出现")
	}
	assertNoTransactionLeftovers(t, m.versionsDir)
	installed, lerr := m.ListInstalled()
	if lerr != nil || len(installed) != 1 || installed[0].Version != "v2.14.0" {
		t.Fatalf("旧版本应完好可用, got %+v err %v", installed, lerr)
	}
}

// TestDownloadBadDigestRejects 官方摘要不符（源内容与 digest 对不上）→ 不落位、
// 旧版完好；verify 事件不会发出（双核未过，错误如实落在下载步内）。
func TestDownloadBadDigestRejects(t *testing.T) {
	src := newZipSource(t)
	m := newChainManager(t, src)
	installViaChain(t, m, src, "v2.14.0", "old-exe-bytes")

	zipBytes := buildPortableZip(t, "tampered-payload")
	seedRemote(t, "v2.15.0", testAssetName, int64(len(zipBytes)), shaHex([]byte("NOT-THE-REAL-DIGEST")))
	src.set(zipBytes, false)

	rec := &progressRecorder{}
	err := m.Download(newTxnID(), "v2.15.0", rec.cb())
	if err == nil {
		t.Fatal("摘要不符应拒收")
	}
	if !strings.Contains(err.Error(), "SHA256") {
		t.Errorf("错误应指明摘要校验失败: %v", err)
	}
	stages := strings.Join(rec.stages(), ",")
	if !strings.Contains(stages, "error") || strings.Contains(stages, "verify") {
		t.Errorf("摘要未过不得出现 verify 事件: %v", stages)
	}
	if _, statErr := os.Stat(filepath.Join(m.versionsDir, "bili23_2.15.0")); !os.IsNotExist(statErr) {
		t.Error("坏包不得落位")
	}
	assertNoTransactionLeftovers(t, m.versionsDir)
}

// TestDownloadMissingDigestRefusesUnverified 上游无官方摘要 → 拒无校验安装，
// 且一个下载请求都不发出（先审配置，再碰网络）。
func TestDownloadMissingDigestRefusesUnverified(t *testing.T) {
	src := newZipSource(t)
	m := newChainManager(t, src)
	zipBytes := buildPortableZip(t, "x")
	seedRemote(t, "v2.15.0", testAssetName, int64(len(zipBytes)), "")
	src.set(zipBytes, false)

	err := m.Download(newTxnID(), "v2.15.0", nil)
	if err == nil || !strings.Contains(err.Error(), "拒绝无校验安装") {
		t.Fatalf("应拒绝无校验安装, got %v", err)
	}
	if hits := src.count(); hits != 0 {
		t.Errorf("摘要缺失时不得发起下载请求, hits = %d", hits)
	}
}

// TestDownloadZipSlipRejected 逃逸路径 entry（摘要真实、传输完好）：安全闸门
// 收口内核 UnpackZip，拒收报错、半件不落位、逃逸文件不落盘。
func TestDownloadZipSlipRejected(t *testing.T) {
	src := newZipSource(t)
	m := newChainManager(t, src)

	evil := makeTestZip(t, map[string]string{
		topDirName + "/" + exeName:       "fake-exe",
		topDirName + "/" + bootstrapName: "bootstrap",
		topDirName + "/script/main.py":   "x",
		"../../evil.py":                  "x",
	})
	zipBytes, err := os.ReadFile(evil)
	if err != nil {
		t.Fatal(err)
	}
	seedRemote(t, "v2.15.0", testAssetName, int64(len(zipBytes)), shaHex(zipBytes))
	src.set(zipBytes, false)

	err = m.Download(newTxnID(), "v2.15.0", nil)
	if err == nil {
		t.Fatal("ZipSlip entry 应报错")
	}
	if _, statErr := os.Stat(filepath.Join(m.versionsDir, "bili23_2.15.0")); !os.IsNotExist(statErr) {
		t.Error("恶意包不得落位")
	}
	if _, statErr := os.Stat(filepath.Join(filepath.Dir(m.versionsDir), "evil.py")); statErr == nil {
		t.Error("逃逸文件不应落盘")
	}
	assertNoTransactionLeftovers(t, m.versionsDir)
}

// TestDownloadMissingAnchorsRejected 收割后三锚点缺件（无 script/main.py）：
// 布局自检拒装，半件不残留。
func TestDownloadMissingAnchorsRejected(t *testing.T) {
	src := newZipSource(t)
	m := newChainManager(t, src)

	noMain := map[string]string{
		topDirName + "/" + exeName:       "fake-exe",
		topDirName + "/" + bootstrapName: "bootstrap",
		topDirName + "/readme.txt":       "x",
	}
	zipPath := makeTestZip(t, noMain)
	zipBytes, err := os.ReadFile(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	seedRemote(t, "v2.15.0", testAssetName, int64(len(zipBytes)), shaHex(zipBytes))
	src.set(zipBytes, false)

	err = m.Download(newTxnID(), "v2.15.0", nil)
	if err == nil || !strings.Contains(err.Error(), "三锚点") {
		t.Fatalf("缺主模块锚点应拒装, got %v", err)
	}
	assertNoTransactionLeftovers(t, m.versionsDir)
}

// TestReinstallSamePackageIdempotent 同版本同包摘要重复落位 → 内核幂等复用不报错；
// 同版本异摘要 → 拒绝覆盖防漂移。
func TestReinstallSamePackageIdempotent(t *testing.T) {
	src := newZipSource(t)
	m := newChainManager(t, src)
	zipBytes := buildPortableZip(t, "same-exe")
	seedRemote(t, "v2.15.0", testAssetName, int64(len(zipBytes)), shaHex(zipBytes))
	src.set(zipBytes, false)

	if err := m.Download(newTxnID(), "v2.15.0", nil); err != nil {
		t.Fatalf("首装: %v", err)
	}
	if err := m.Download(newTxnID(), "v2.15.0", nil); err != nil {
		t.Fatalf("同摘要重装应幂等: %v", err)
	}

	// 同版本号换内容（模拟上游同 tag 重传）：必须拒绝
	tampered := buildPortableZip(t, "same-exe-tampered")
	seedRemote(t, "v2.15.0", testAssetName, int64(len(tampered)), shaHex(tampered))
	src.set(tampered, false)
	err := m.Download(newTxnID(), "v2.15.0", nil)
	if err == nil || !strings.Contains(err.Error(), "漂移") {
		t.Fatalf("同版本异摘要应拒绝覆盖, got %v", err)
	}
}

// TestUpdateChainCoexist 更新成功链：2.14.0 → 2.15.0 双版本共存、列表最新在前、
// 逐版本 Resolve 稳定；卸载新版不动旧版目录。
func TestUpdateChainCoexist(t *testing.T) {
	src := newZipSource(t)
	m := newChainManager(t, src)
	installViaChain(t, m, src, "v2.14.0", "exe-2.14.0")

	zipBytes := buildPortableZip(t, "exe-2.15.0")
	seedRemote(t, "v2.15.0", testAssetName, int64(len(zipBytes)), shaHex(zipBytes))
	src.set(zipBytes, false)
	rec := &progressRecorder{}
	if err := m.Download(newTxnID(), "v2.15.0", rec.cb()); err != nil {
		t.Fatalf("Download(v2.15.0): %v", err)
	}

	installed, err := m.ListInstalled()
	if err != nil {
		t.Fatal(err)
	}
	if len(installed) != 2 {
		t.Fatalf("应双版本共存, got %+v", installed)
	}
	if installed[0].Version != "v2.15.0" || installed[1].Version != "v2.14.0" {
		t.Errorf("列表应最新在前, got %s, %s", installed[0].Version, installed[1].Version)
	}
	if !strings.HasPrefix(installed[0].InstalledAt, "20") || len(installed[0].InstalledAt) != len("2006-01-02 15:04:05") {
		t.Errorf("installedAt 应保持 yyyy-MM-dd HH:mm:ss 展示口径: %q", installed[0].InstalledAt)
	}

	exeOld, err := m.ResolveExe("v2.14.0")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Remove("v2.15.0"); err != nil {
		t.Fatalf("Remove(v2.15.0): %v", err)
	}
	if fi, serr := os.Stat(exeOld); serr != nil || fi.Size() == 0 {
		t.Error("卸载新版不得动旧版目录")
	}
	if _, err := m.ResolveExe("v2.15.0"); err == nil {
		t.Error("已卸载版本应不可解析")
	}
}

// TestStagedResidueAbandoned 落位中途强杀等价模拟：staging 半件残留 →
// ListInstalled 不显示半件；CleanupAbandoned 收尸后目录干净。
func TestStagedResidueAbandoned(t *testing.T) {
	m := NewManager(t.TempDir())
	staging, discard, err := m.tree.StageDir("inst-kill-sim")
	if err != nil {
		t.Fatal(err)
	}
	_ = discard // 故意不调用 discard：模拟进程被强杀、中转目录留在盘上
	if err := os.WriteFile(filepath.Join(staging, exeName), []byte("half"), 0644); err != nil {
		t.Fatal(err)
	}

	if list, err := m.ListInstalled(); err != nil || len(list) != 0 {
		t.Fatalf("半件 staging 不得出现在版本列表: %+v err %v", list, err)
	}
	if leftovers := m.tree.CleanupAbandoned(); len(leftovers) != 0 {
		t.Fatalf("CleanupAbandoned 应收尸, leftovers = %v", leftovers)
	}
	if _, err := os.Stat(staging); !os.IsNotExist(err) {
		t.Error("残留中转目录应被清理")
	}
}
