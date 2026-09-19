// 下载→校验→解包→落位全链的失败注入测试：以回环 httptest 服务器为"源"，
// 真实驱动 bespoke 下载/MD5 校验段与内核 UnpackZip/Tree 落位链（真 tmp 目录，
// 无外网依赖），验证中断、坏摘要、无摘要拒装、ZipSlip、半件残留、双版本共存、
// 同版本幂等/防漂移等收口行为（ccswitch/markeron 同构黄金样本）。
package version

import (
	"archive/zip"
	"bytes"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"hanxi/packages/go/artifact"
)

// writeZipToBuffer 按名/内容表写 zip 字节（按名排序保证产出确定性；名以 "/"
// 结尾视为目录 entry）。
func writeZipToBuffer(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	names := make([]string, 0, len(entries))
	for n := range entries {
		names = append(names, n)
	}
	sort.Strings(names)
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, name := range names {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(entries[name]))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func md5Hex(b []byte) string {
	h := md5.Sum(b)
	return hex.EncodeToString(h[:])
}

func shaHex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// ---------- 测试夹具：远程列表注入与假资产源 ----------

const testAssetName = "GuoheView_v3.2.7.98-便携版.zip"

// seedRemote 注入伪远程列表。remoteCache 是包内全局缓存（生产语义即进程级
// 单例），本包测试串行执行（不使用 t.Parallel），互相隔离靠 setup 重灌。
// 果核看图的官方摘要只有 MD5（bespoke 校验链信任根），Size 供字节数核验。
func seedRemote(t *testing.T, url, version, assetName string, size int64, md5sum string) {
	t.Helper()
	remoteCache.mu.Lock()
	defer remoteCache.mu.Unlock()
	remoteCache.data = []ViewRelease{{
		Version:   version,
		Channel:   "stable",
		AssetName: assetName,
		AssetURL:  url,
		Size:      size,
		MD5:       md5sum,
	}}
	remoteCache.fetchedAt = time.Now() // 处于 TTL 内，Download 不会再走网络拉列表
}

// seedServe 把 body 装入假源并重灌远程列表（version 缺省 v3.2.7.98）。
func seedServe(t *testing.T, src *zipSource, version string, body []byte, md5sum string) {
	t.Helper()
	if version == "" {
		version = "v3.2.7.98"
	}
	seedRemote(t, src.srv.URL+"/"+testAssetName, version, testAssetName, int64(len(body)), md5sum)
	src.set(body, false)
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

// newChainManager 构造指向假源的 Manager：bespoke 下载链仍走生产 downloadTo，
// 仅把 HTTP 客户端换成"回环强制绕代理"注入（本机环境代理会劫持 127.0.0.1 请求）。
func newChainManager(t *testing.T, src *zipSource) *Manager {
	t.Helper()
	m := NewManager(t.TempDir())
	m.client = &http.Client{
		Timeout:   30 * time.Second,
		Transport: &http.Transport{Proxy: nil},
	}
	return m
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
func installViaChain(t *testing.T, m *Manager, src *zipSource, version string) {
	t.Helper()
	zipBytes := buildPortableZip(t)
	seedServe(t, src, version, zipBytes, md5Hex(zipBytes))
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

// TestDownloadChainSuccess 正例全链：下载→字节数→官方 MD5→UnpackZip→收割
// 包装目录→补写便携标记→Tree.Commit。断言布局、账本、进度词表零漂移。
func TestDownloadChainSuccess(t *testing.T) {
	src := newZipSource(t)
	m := newChainManager(t, src)
	zipBytes := buildPortableZip(t)
	seedServe(t, src, "v3.2.7.98", zipBytes, md5Hex(zipBytes))

	rec := &progressRecorder{}
	if err := m.Download(newTxnID(), "v3.2.7.98", rec.cb()); err != nil {
		t.Fatalf("Download: %v", err)
	}

	// 进度词表零漂移：既有词汇 downloading → verify → extract → done
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

	dir := filepath.Join(m.versionsDir, "guoheview_3.2.7.98")
	for _, rel := range []string{exeName, "ghde.dll", portableMarkName, filepath.Join("plugins", "decoder", "readme.txt")} {
		if _, err := os.Stat(filepath.Join(dir, rel)); err != nil {
			t.Errorf("缺少 %s: %v", rel, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "GuoheViewPortable")); !os.IsNotExist(err) {
		t.Error("包装目录不应落进版本目录")
	}
	if _, err := os.Stat(filepath.Join(dir, "README-outside.txt")); !os.IsNotExist(err) {
		t.Error("根外杂质不应落进版本目录")
	}

	// 落位账本由内核统一形状写入（官方 MD5 是校验信任根，本地 SHA-256 是防漂移账）
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

	list, lerr := m.ListInstalled()
	if lerr != nil || len(list) != 1 || list[0].Version != "v3.2.7.98" || list[0].IsImport || list[0].InstalledAt == "" {
		t.Fatalf("列表账目异常: %+v %v", list, lerr)
	}
	assertNoTransactionLeftovers(t, m.versionsDir)
}

// TestDownloadInterruptKeepsOldVersion 下载中断（传输截断，官方单域多轮重试
// 全败）→ 报 error 进度、旧版本完好、版本树无新目录与半件残留。
func TestDownloadInterruptKeepsOldVersion(t *testing.T) {
	src := newZipSource(t)
	m := newChainManager(t, src)
	installViaChain(t, m, src, "v3.2.7.97")

	// 中断注入：远程列表换成新版本的源，但每次传输半路掐线
	remoteCache.mu.Lock()
	rel := &remoteCache.data[0]
	rel.Version = "v3.2.7.98"
	remoteCache.mu.Unlock()
	newZip := buildPortableZip(t)
	src.set(newZip, true)

	rec := &progressRecorder{}
	err := m.Download(newTxnID(), "v3.2.7.98", rec.cb())
	if err == nil {
		t.Fatal("下载中断应报错")
	}
	stages := strings.Join(rec.stages(), ",")
	if !strings.Contains(stages, "error") || strings.Contains(stages, "done") {
		t.Fatalf("进度事件应含 error 且不得含 done: %v", stages)
	}
	if _, statErr := os.Stat(filepath.Join(m.versionsDir, "guoheview_3.2.7.98")); !os.IsNotExist(statErr) {
		t.Error("新半件目录不得出现")
	}
	assertNoTransactionLeftovers(t, m.versionsDir)
	installed, lerr := m.ListInstalled()
	if lerr != nil || len(installed) != 1 || installed[0].Version != "v3.2.7.97" {
		t.Fatalf("旧版本应完好可用, got %+v err %v", installed, lerr)
	}
}

// TestDownloadBadMD5Rejects 官方 MD5 不符（内容与摘要对不上）→ 拒落位且旧版完好。
// bespoke 段的安全底线：弱摘要也必检。
func TestDownloadBadMD5Rejects(t *testing.T) {
	src := newZipSource(t)
	m := newChainManager(t, src)
	installViaChain(t, m, src, "v3.2.7.97")

	newZip := buildPortableZip(t)
	seedRemote(t, src.srv.URL+"/"+testAssetName, "v3.2.7.98", testAssetName,
		int64(len(newZip)), md5Hex([]byte("NOT-THE-OFFICIAL-MD5")))
	src.set(newZip, false)

	rec := &progressRecorder{}
	err := m.Download(newTxnID(), "v3.2.7.98", rec.cb())
	if err == nil || !strings.Contains(err.Error(), "官方哈希校验失败") {
		t.Fatalf("MD5 不符应拒装, got %v", err)
	}
	stages := strings.Join(rec.stages(), ",")
	if !strings.Contains(stages, "verify") || !strings.Contains(stages, "error") {
		t.Errorf("失败应发生在 verify 阶段: %v", stages)
	}
	if _, statErr := os.Stat(filepath.Join(m.versionsDir, "guoheview_3.2.7.98")); !os.IsNotExist(statErr) {
		t.Error("坏包不得落位")
	}
	assertNoTransactionLeftovers(t, m.versionsDir)
}

// TestDownloadMissingMD5RefusesUnverified 上游无官方 MD5 → 拒无校验安装，
// 且一个下载请求都不发出（先审配置，再碰网络）。
func TestDownloadMissingMD5RefusesUnverified(t *testing.T) {
	src := newZipSource(t)
	m := newChainManager(t, src)
	zipBytes := buildPortableZip(t)
	seedRemote(t, src.srv.URL+"/"+testAssetName, "v3.2.7.98", testAssetName, int64(len(zipBytes)), "")
	src.set(zipBytes, false)

	err := m.Download(newTxnID(), "v3.2.7.98", nil)
	if err == nil || !strings.Contains(err.Error(), "拒绝无校验安装") {
		t.Fatalf("应拒绝无校验安装, got %v", err)
	}
	if hits := src.count(); hits != 0 {
		t.Errorf("摘要缺失时不得发起下载请求, hits = %d", hits)
	}
}

// TestDownloadZipSlipRejected 逃逸路径 entry：安全闸门收口内核 UnpackZip，
// 拒收报错、半件不落位、逃逸文件不落盘。
func TestDownloadZipSlipRejected(t *testing.T) {
	src := newZipSource(t)
	m := newChainManager(t, src)

	evil := writeZipToBuffer(t, map[string]string{
		exeName:          "fake-exe",
		portableMarkName: ";",
		"../../evil.dll": "x",
	})
	seedServe(t, src, "v3.2.7.98", evil, md5Hex(evil))

	err := m.Download(newTxnID(), "v3.2.7.98", nil)
	if err == nil {
		t.Fatal("ZipSlip entry 应报错")
	}
	if _, statErr := os.Stat(filepath.Join(m.versionsDir, "guoheview_3.2.7.98")); !os.IsNotExist(statErr) {
		t.Error("恶意包不得落位")
	}
	if _, statErr := os.Stat(filepath.Join(m.versionsDir, "..", "evil.dll")); statErr == nil {
		t.Error("逃逸文件不应落盘")
	}
	assertNoTransactionLeftovers(t, m.versionsDir)
}

// TestDownloadMissingPortableMarkWritesIt 缺便携标记的官方包：迁移后的口径是
// ensurePortableMark 补写（官方开关语义安全）而非拒装——保证托管实例配置
// 不外溢 %APPDATA%。
func TestDownloadMissingPortableMarkWritesIt(t *testing.T) {
	src := newZipSource(t)
	m := newChainManager(t, src)

	noMark := writeZipToBuffer(t, map[string]string{
		"GuoheViewPortable/":           "",
		"GuoheViewPortable/" + exeName: "fake-exe",
		"GuoheViewPortable/ghde.dll":   "dll",
	})
	seedServe(t, src, "v3.2.7.98", noMark, md5Hex(noMark))

	if err := m.Download(newTxnID(), "v3.2.7.98", nil); err != nil {
		t.Fatalf("缺便携标记应补写而非拒装: %v", err)
	}
	dir := filepath.Join(m.versionsDir, "guoheview_3.2.7.98")
	if _, err := os.Stat(filepath.Join(dir, portableMarkName)); err != nil {
		t.Errorf("便携标记应被补写: %v", err)
	}
	// 补写后 ListInstalled 锚点自检通过（配置外溢防护闭环）
	if list, err := m.ListInstalled(); err != nil || len(list) != 1 {
		t.Errorf("列表应可见该安装: %+v %v", list, err)
	}
}

// TestReinstallSamePackageIdempotent 同版本同包摘要重复落位 → 内核幂等复用不报错；
// 同版本异 MD5 内容（异本地 SHA-256）→ 拒绝覆盖防漂移。
func TestReinstallSamePackageIdempotent(t *testing.T) {
	src := newZipSource(t)
	m := newChainManager(t, src)
	zipBytes := buildPortableZip(t)
	seedServe(t, src, "v3.2.7.98", zipBytes, md5Hex(zipBytes))

	if err := m.Download(newTxnID(), "v3.2.7.98", nil); err != nil {
		t.Fatalf("首装: %v", err)
	}
	if err := m.Download(newTxnID(), "v3.2.7.98", nil); err != nil {
		t.Fatalf("同摘要重装应幂等: %v", err)
	}

	// 同版本号换内容（模拟上游同版本重传）：必须拒绝
	tampered := writeZipToBuffer(t, map[string]string{
		"GuoheViewPortable/":                    "",
		"GuoheViewPortable/" + exeName:          "tampered-exe",
		"GuoheViewPortable/" + portableMarkName: ";",
	})
	seedServe(t, src, "v3.2.7.98", tampered, md5Hex(tampered))
	err := m.Download(newTxnID(), "v3.2.7.98", nil)
	if err == nil || !strings.Contains(err.Error(), "漂移") {
		t.Fatalf("同版本异摘要应拒绝覆盖, got %v", err)
	}
}

// TestUpdateChainCoexist 更新成功链：3.2.7.97 → 3.2.7.98 双版本共存、列表
// 最新在前、逐版本 Resolve 稳定；卸载新版不动旧版目录。
func TestUpdateChainCoexist(t *testing.T) {
	src := newZipSource(t)
	m := newChainManager(t, src)
	installViaChain(t, m, src, "v3.2.7.97")

	newZip := writeZipToBuffer(t, map[string]string{
		"GuoheViewPortable/":                    "",
		"GuoheViewPortable/" + exeName:          "exe-3.2.7.98",
		"GuoheViewPortable/" + portableMarkName: "; portable",
	})
	seedServe(t, src, "v3.2.7.98", newZip, md5Hex(newZip))
	if err := m.Download(newTxnID(), "v3.2.7.98", nil); err != nil {
		t.Fatalf("Download(v3.2.7.98): %v", err)
	}

	installed, err := m.ListInstalled()
	if err != nil {
		t.Fatal(err)
	}
	if len(installed) != 2 {
		t.Fatalf("应双版本共存, got %+v", installed)
	}
	if installed[0].Version != "v3.2.7.98" || installed[1].Version != "v3.2.7.97" {
		t.Errorf("列表应最新在前, got %s, %s", installed[0].Version, installed[1].Version)
	}
	if !strings.HasPrefix(installed[0].InstalledAt, "20") || len(installed[0].InstalledAt) != len("2006-01-02 15:04:05") {
		t.Errorf("installedAt 应保持 yyyy-MM-dd HH:mm:ss 展示口径: %q", installed[0].InstalledAt)
	}

	exeOld, err := m.ResolveExe("v3.2.7.97")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Remove("v3.2.7.98"); err != nil {
		t.Fatalf("Remove(v3.2.7.98): %v", err)
	}
	if fi, serr := os.Stat(exeOld); serr != nil || fi.Size() == 0 {
		t.Error("卸载新版不得动旧版目录")
	}
	if _, err := m.ResolveExe("v3.2.7.98"); err == nil {
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
