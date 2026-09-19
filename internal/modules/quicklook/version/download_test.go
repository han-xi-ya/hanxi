// 下载→校验→解包→落位全链的失败注入测试：以回环 httptest 服务器为"源"，
// 真实驱动内核 artifact.Fetch（镜像接缝改指回环）+ 本包 bespoke extractAll
// （longpath/反斜杠条目特例）+ 内核 Tree 落位链（真 tmp 目录，无外网依赖），
// 验证中断、坏摘要、无摘要拒装、ZipSlip、布局缺件、半件残留、双版本共存、
// 同版本幂等/防漂移等收口行为（ccswitch/markeron 同构黄金样本；解包段因
// QuickLook 单家特例留模块，闸门纪律在 extractAll 内自持，ADR-0003）。
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

const testAssetName = "QuickLook-4.5.0.zip"

// seedRemote 注入伪远程列表。remoteCache 是包内全局缓存（生产语义即进程级
// 单例），本包测试串行执行（不使用 t.Parallel），互相隔离靠 setup 重灌。
// QuickLook 的官方摘要直接携带在 QuickLookRelease.SHA256（远程解析层已剥前缀）。
func seedRemote(t *testing.T, version, assetName string, size int64, sha string) {
	t.Helper()
	remoteCache.mu.Lock()
	defer remoteCache.mu.Unlock()
	remoteCache.data = []QuickLookRelease{{
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
// 下载/校验全链仍走生产实现 artifact.Fetch；解包/落位走生产 extractAll/Tree）。
func newChainManager(t *testing.T, src *zipSource) *Manager {
	t.Helper()
	m := NewManager(t.TempDir())
	m.mirrors = func(_, _ string) []string {
		return []string{src.srv.URL + "/" + testAssetName}
	}
	return m
}

// buildPortableZip 官方便携 zip 同构形状：exe + portable.lock + 原生 DLL 双锚点，
// 外加反斜杠分隔的插件树与反斜杠结尾的目录条目（实测 v4.5.0 官方包布局，
// 正是 bespoke extractAll 拒绝改走内核 UnpackZip 的场景）。
func buildPortableZip(t *testing.T, exeContent string) []byte {
	t.Helper()
	path := makeTestZip(t, map[string]string{
		exeName:          exeContent,
		portableMarkName: "",
		nativeMarkName:   "fake-native",
		`QuickLook.Plugin\QuickLook.Plugin.TextViewer\dll`:                          "plugin",
		`QuickLook.Plugin\QuickLook.Plugin.FontViewer\runtimes\`:                    "",
		`QuickLook.Plugin\QuickLook.Plugin.FontViewer\runtimes\win\x64\nat\lib.dll`: "dll",
	})
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
	seedRemote(t, version, "QuickLook-"+version+".zip", int64(len(zipBytes)), shaHex(zipBytes))
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

// TestDownloadChainSuccess 正例全链：Fetch（官方摘要双核）→ bespoke extractAll
// （反斜杠条目归一展开 + 双锚点自检）→ Tree.Commit。断言布局、账本、进度词表零漂移。
func TestDownloadChainSuccess(t *testing.T) {
	src := newZipSource(t)
	m := newChainManager(t, src)
	zipBytes := buildPortableZip(t, "fake-exe")
	seedRemote(t, "4.5.0", testAssetName, int64(len(zipBytes)), shaHex(zipBytes))
	src.set(zipBytes, false)

	rec := &progressRecorder{}
	if err := m.Download(newTxnID(), "4.5.0", rec.cb()); err != nil {
		t.Fatalf("Download: %v", err)
	}

	// 进度词表零漂移：既有词汇 downloading → verify → extract → done
	// （verify 由内核摘要双核完成后映射发出，前端"哈希校验…"展示段保留）
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

	dir := filepath.Join(m.versionsDir, "quicklook_4.5.0")
	for _, rel := range []string{exeName, portableMarkName, nativeMarkName,
		filepath.Join("QuickLook.Plugin", "QuickLook.Plugin.TextViewer", "dll"),
		filepath.Join("QuickLook.Plugin", "QuickLook.Plugin.FontViewer", "runtimes", "win", "x64", "nat", "lib.dll")} {
		if _, err := os.Stat(filepath.Join(dir, rel)); err != nil {
			t.Errorf("缺少 %s: %v", rel, err)
		}
	}
	// 反斜杠目录条目不得被误当文件落成 0 字节同名件
	if fi, err := os.Stat(filepath.Join(dir, "QuickLook.Plugin", "QuickLook.Plugin.FontViewer", "runtimes")); err != nil || !fi.IsDir() {
		t.Errorf("runtimes 应为目录: err=%v", err)
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
	if lerr != nil || len(list) != 1 || list[0].Version != "4.5.0" || list[0].IsImport || list[0].InstalledAt == "" {
		t.Fatalf("列表账目异常: %+v %v", list, lerr)
	}
	assertNoTransactionLeftovers(t, m.versionsDir)
}

// TestDownloadInterruptKeepsOldVersion 下载中断（传输截断，全部候选源失败）→
// 报 error 进度、旧版本完好、版本树无新目录与半件残留。
func TestDownloadInterruptKeepsOldVersion(t *testing.T) {
	src := newZipSource(t)
	m := newChainManager(t, src)
	installViaChain(t, m, src, "4.4.0", "old-exe-bytes")

	zipBytes := buildPortableZip(t, "brand-new-exe")
	seedRemote(t, "4.5.0", testAssetName, int64(len(zipBytes)), shaHex(zipBytes))
	src.set(zipBytes, true) // 半路掐线

	rec := &progressRecorder{}
	err := m.Download(newTxnID(), "4.5.0", rec.cb())
	if err == nil {
		t.Fatal("下载中断应报错")
	}
	stages := strings.Join(rec.stages(), ",")
	if !strings.Contains(stages, "error") || strings.Contains(stages, "done") {
		t.Fatalf("进度事件应含 error 且不得含 done: %v", stages)
	}
	if _, statErr := os.Stat(filepath.Join(m.versionsDir, "quicklook_4.5.0")); !os.IsNotExist(statErr) {
		t.Error("新半件目录不得出现")
	}
	assertNoTransactionLeftovers(t, m.versionsDir)
	installed, lerr := m.ListInstalled()
	if lerr != nil || len(installed) != 1 || installed[0].Version != "4.4.0" {
		t.Fatalf("旧版本应完好可用, got %+v err %v", installed, lerr)
	}
}

// TestDownloadBadDigestRejects 官方摘要不符（源内容与 digest 对不上）→ 不落位、
// 旧版完好；verify 事件不会发出（双核未过，错误如实落在下载步内）。
func TestDownloadBadDigestRejects(t *testing.T) {
	src := newZipSource(t)
	m := newChainManager(t, src)
	installViaChain(t, m, src, "4.4.0", "old-exe-bytes")

	zipBytes := buildPortableZip(t, "tampered-payload")
	seedRemote(t, "4.5.0", testAssetName, int64(len(zipBytes)), shaHex([]byte("NOT-THE-REAL-DIGEST")))
	src.set(zipBytes, false)

	rec := &progressRecorder{}
	err := m.Download(newTxnID(), "4.5.0", rec.cb())
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
	if _, statErr := os.Stat(filepath.Join(m.versionsDir, "quicklook_4.5.0")); !os.IsNotExist(statErr) {
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
	seedRemote(t, "4.5.0", testAssetName, int64(len(zipBytes)), "")
	src.set(zipBytes, false)

	err := m.Download(newTxnID(), "4.5.0", nil)
	if err == nil || !strings.Contains(err.Error(), "拒绝无校验安装") {
		t.Fatalf("应拒绝无校验安装, got %v", err)
	}
	if hits := src.count(); hits != 0 {
		t.Errorf("摘要缺失时不得发起下载请求, hits = %d", hits)
	}
}

// TestDownloadZipSlipRejected 逃逸路径 entry（摘要真实、传输完好）：解包段
// bespoke extractAll 的 ZipSlip 闸门拒收，半件不落位、逃逸文件不落盘。
func TestDownloadZipSlipRejected(t *testing.T) {
	src := newZipSource(t)
	m := newChainManager(t, src)

	evil := makeTestZip(t, map[string]string{
		exeName:          "fake-exe",
		portableMarkName: "",
		nativeMarkName:   "n",
		"../evil.dll":    "x",
	})
	zipBytes, err := os.ReadFile(evil)
	if err != nil {
		t.Fatal(err)
	}
	seedRemote(t, "4.5.0", testAssetName, int64(len(zipBytes)), shaHex(zipBytes))
	src.set(zipBytes, false)

	err = m.Download(newTxnID(), "4.5.0", nil)
	if err == nil || !strings.Contains(err.Error(), "非法路径") {
		t.Fatalf("ZipSlip entry 应报错, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(m.versionsDir, "quicklook_4.5.0")); !os.IsNotExist(statErr) {
		t.Error("恶意包不得落位")
	}
	if _, statErr := os.Stat(filepath.Join(m.versionsDir, "evil.dll")); statErr == nil {
		t.Error("逃逸文件不应落盘")
	}
	assertNoTransactionLeftovers(t, m.versionsDir)
}

// TestZipLayoutRejectedKeepsTree 便携双锚点缺件（无原生 DLL）：extractAll
// 布局自检拒装，最终目录不出现、半件不残留。
func TestZipLayoutRejectedKeepsTree(t *testing.T) {
	src := newZipSource(t)
	m := newChainManager(t, src)

	noNative := makeTestZip(t, map[string]string{
		exeName:          "fake-exe",
		portableMarkName: "",
	})
	zipBytes, err := os.ReadFile(noNative)
	if err != nil {
		t.Fatal(err)
	}
	seedRemote(t, "4.5.0", testAssetName, int64(len(zipBytes)), shaHex(zipBytes))
	src.set(zipBytes, false)

	err = m.Download(newTxnID(), "4.5.0", nil)
	if err == nil || !strings.Contains(err.Error(), "原生组件") {
		t.Fatalf("缺原生 DLL 锚点应拒装, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(m.versionsDir, "quicklook_4.5.0")); !os.IsNotExist(statErr) {
		t.Error("锚点不符的包不得落位")
	}
	assertNoTransactionLeftovers(t, m.versionsDir)
}

// TestReinstallSamePackageIdempotent 同版本同包摘要重复落位 → 内核幂等复用不报错；
// 同版本异摘要 → 拒绝覆盖防漂移。
func TestReinstallSamePackageIdempotent(t *testing.T) {
	src := newZipSource(t)
	m := newChainManager(t, src)
	zipBytes := buildPortableZip(t, "same-exe")
	seedRemote(t, "4.5.0", testAssetName, int64(len(zipBytes)), shaHex(zipBytes))
	src.set(zipBytes, false)

	if err := m.Download(newTxnID(), "4.5.0", nil); err != nil {
		t.Fatalf("首装: %v", err)
	}
	if err := m.Download(newTxnID(), "4.5.0", nil); err != nil {
		t.Fatalf("同摘要重装应幂等: %v", err)
	}

	// 同版本号换内容（模拟上游同 tag 重传）：必须拒绝
	tampered := buildPortableZip(t, "same-exe-tampered")
	seedRemote(t, "4.5.0", testAssetName, int64(len(tampered)), shaHex(tampered))
	src.set(tampered, false)
	err := m.Download(newTxnID(), "4.5.0", nil)
	if err == nil || !strings.Contains(err.Error(), "漂移") {
		t.Fatalf("同版本异摘要应拒绝覆盖, got %v", err)
	}
}

// TestUpdateChainCoexist 更新成功链：4.4.0 → 4.5.0 双版本共存、列表最新在前、
// 逐版本 Resolve 稳定；卸载新版不动旧版目录。
func TestUpdateChainCoexist(t *testing.T) {
	src := newZipSource(t)
	m := newChainManager(t, src)
	installViaChain(t, m, src, "4.4.0", "exe-4.4.0")

	zipBytes := buildPortableZip(t, "exe-4.5.0")
	seedRemote(t, "4.5.0", testAssetName, int64(len(zipBytes)), shaHex(zipBytes))
	src.set(zipBytes, false)
	rec := &progressRecorder{}
	if err := m.Download(newTxnID(), "4.5.0", rec.cb()); err != nil {
		t.Fatalf("Download(4.5.0): %v", err)
	}

	installed, err := m.ListInstalled()
	if err != nil {
		t.Fatal(err)
	}
	if len(installed) != 2 {
		t.Fatalf("应双版本共存, got %+v", installed)
	}
	if installed[0].Version != "4.5.0" || installed[1].Version != "4.4.0" {
		t.Errorf("列表应最新在前, got %s, %s", installed[0].Version, installed[1].Version)
	}
	if !strings.HasPrefix(installed[0].InstalledAt, "20") || len(installed[0].InstalledAt) != len("2006-01-02 15:04:05") {
		t.Errorf("installedAt 应保持 yyyy-MM-dd HH:mm:ss 展示口径: %q", installed[0].InstalledAt)
	}

	exe440, err := m.ResolveExe("4.4.0")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Remove("4.5.0"); err != nil {
		t.Fatalf("Remove(4.5.0): %v", err)
	}
	if fi, serr := os.Stat(exe440); serr != nil || fi.Size() == 0 {
		t.Error("卸载新版不得动旧版目录")
	}
	if _, err := m.ResolveExe("4.5.0"); err == nil {
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
