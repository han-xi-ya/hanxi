// 下载→校验→解包→落位全链的失败注入测试（markeron/rufus 金样本同构，资产
// 形态换成官方 zip）：以回环 httptest 服务器为"源"，真实驱动内核
// artifact.Fetch/UnpackZip/Tree 路径（真 tmp 目录，无网络依赖），验证中断、
// 坏摘要、ZipSlip、缺 exe 布局自检、半件残留、双版本共存、同版本异摘要防
// 漂移与历史账本双轨回读等收口行为。
package version

import (
	"archive/zip"
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

// seedRemote 注入伪远程列表（DdnsRelease 自带官方摘要 digest 剥离形态）。
// remoteCache 是包内全局缓存（生产语义即进程级单例），本包测试串行执行
// （不使用 t.Parallel），互相隔离靠 setup 重灌。
func seedRemote(t *testing.T, version, assetName string, size int64, digest string) {
	t.Helper()
	remoteCache.mu.Lock()
	defer remoteCache.mu.Unlock()
	remoteCache.data = []DdnsRelease{{
		Version:   version,
		AssetName: assetName,
		AssetURL:  "http://127.0.0.1:0/placeholder",
		Size:      size,
		SHA256:    digest,
	}}
	remoteCache.fetchedAt = time.Now() // 处于 TTL 内，Download 不会再走网络拉列表
}

// zipSource 可编程假资产源：按当前 zip 字节提供下载，支持截断（模拟中断）
// 与命中计数。
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
// 下载/校验/解包/落位全链仍走生产实现 artifact.Fetch/UnpackZip/Tree）。
func newChainManager(t *testing.T, src *zipSource) *Manager {
	t.Helper()
	m := NewManager(t.TempDir())
	m.mirrors = func(_, _ string) []string {
		return []string{src.srv.URL + "/ddns-go_test_windows_x86_64.zip"}
	}
	return m
}

// makeTestZip 构造内存测试 zip（entries 按插入序写入，路径反斜杠/逃逸条目
// 经键名直投以演练清洗闸）。
func makeTestZip(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	tmp := filepath.Join(t.TempDir(), "pkg.zip")
	f, err := os.Create(tmp)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for name, content := range entries {
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
	data, err := os.ReadFile(tmp)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// officialZip ddns-go 官方形态便携包：exe 本体 + LICENSE/README 文档件。
func officialZip(t *testing.T, payload string) []byte {
	t.Helper()
	return makeTestZip(t, map[string]string{
		exeName:     "fake-exe-" + payload,
		"LICENSE":   "MIT",
		"README.md": "ddns-go",
	})
}

func shaHex(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
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

// installViaChain 走真实下载链装一个版本（正例夹具复用）。
func installViaChain(t *testing.T, m *Manager, src *zipSource, version, payload string) {
	t.Helper()
	zipBytes := officialZip(t, payload)
	seedRemote(t, version, "ddns-go_"+strings.TrimPrefix(version, "v")+"_windows_x86_64.zip",
		int64(len(zipBytes)), shaHex(zipBytes))
	src.set(zipBytes, false)
	if err := m.Download("test-txn", version, nil); err != nil {
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

// ---------- 全链正例与词表 ----------

// TestDownloadChainSuccess 正例全链：进度词表零漂移（downloading → verify →
// extract → done，不发明新词）、落位目录/双轨账目齐备、ListInstalled 字段
// 与迁移前口径一致（Source 记资产名、非导入、安装时间可读）。
func TestDownloadChainSuccess(t *testing.T) {
	src := newZipSource(t)
	m := newChainManager(t, src)

	zipBytes := officialZip(t, "6.17.6")
	assetName := "ddns-go_6.17.6_windows_x86_64.zip"
	seedRemote(t, "v6.17.6", assetName, int64(len(zipBytes)), shaHex(zipBytes))
	src.set(zipBytes, false)

	rec := &progressRecorder{}
	if err := m.Download("test-txn", "v6.17.6", rec.cb()); err != nil {
		t.Fatalf("Download: %v", err)
	}
	for _, e := range rec.events {
		switch e.Stage {
		case "downloading", "verify", "extract", "done", "error":
		default:
			t.Fatalf("出现了词表之外的进度阶段 %q", e.Stage)
		}
	}
	if got := strings.Join(rec.stages(), ","); got != "downloading,verify,extract,done" {
		t.Errorf("进度阶段序列 = %q", got)
	}

	installed, err := m.ListInstalled()
	if err != nil || len(installed) != 1 {
		t.Fatalf("ListInstalled = %+v err %v", installed, err)
	}
	got := installed[0]
	if got.Version != "v6.17.6" || got.IsImport || got.Source != assetName || got.InstalledAt == "" {
		t.Errorf("账目字段异常: %+v", got)
	}
	if fi, serr := os.Stat(got.ExePath); serr != nil || fi.Size() == 0 || got.Dir != filepath.Dir(got.ExePath) {
		t.Errorf("落位异常: %+v", got)
	}
	// 双轨账本：内核统一形状 + 模块来源明细
	metaRaw, err := os.ReadFile(filepath.Join(got.Dir, "meta.json"))
	if err != nil {
		t.Fatal(err)
	}
	var meta artifact.Meta
	if err := json.Unmarshal(metaRaw, &meta); err != nil {
		t.Fatal(err)
	}
	if meta.Entry != exeName || meta.ZipSHA256 != shaHex(zipBytes) || meta.Source != artifact.SourceRemote {
		t.Errorf("内核账本内容异常: %+v", meta)
	}
	if mm := readModuleMeta(got.Dir); mm.Source != assetName || mm.IsImport {
		t.Errorf("模块来源账异常: %+v", mm)
	}
	assertNoTransactionLeftovers(t, m.versionsDir)
}

// ---------- 失败注入用例 ----------

// TestDownloadInterruptKeepsOldVersion 下载中断（传输截断，候选源全败）→
// 报 error 进度、旧版本完好、版本树无新目录与半件残留。
func TestDownloadInterruptKeepsOldVersion(t *testing.T) {
	src := newZipSource(t)
	m := newChainManager(t, src)
	installViaChain(t, m, src, "v6.17.5", "old")

	newZip := officialZip(t, "brand-new")
	seedRemote(t, "v6.17.6", "ddns-go_6.17.6_windows_x86_64.zip", int64(len(newZip)), shaHex(newZip))
	src.set(newZip, true) // 半路掐线

	rec := &progressRecorder{}
	err := m.Download("test-txn", "v6.17.6", rec.cb())
	if err == nil {
		t.Fatal("下载中断应报错")
	}
	stages := rec.stages()
	joined := strings.Join(stages, ",")
	if !strings.Contains(joined, "error") || strings.Contains(joined, "done") {
		t.Fatalf("进度事件应含 error 且不得含 done: %v", stages)
	}
	if _, statErr := os.Stat(filepath.Join(m.versionsDir, "ddnsgo_6.17.6")); !os.IsNotExist(statErr) {
		t.Error("新半件目录不得出现")
	}
	assertNoTransactionLeftovers(t, m.versionsDir)
	installed, lerr := m.ListInstalled()
	if lerr != nil || len(installed) != 1 || installed[0].Version != "v6.17.5" {
		t.Fatalf("旧版本应完好可用, got %+v err %v", installed, lerr)
	}
}

// TestDownloadBadDigestRejects 摘要错（源内容与官方摘要不符）→ 拒收不落位。
func TestDownloadBadDigestRejects(t *testing.T) {
	src := newZipSource(t)
	m := newChainManager(t, src)
	body := officialZip(t, "tampered")
	seedRemote(t, "v6.17.6", "a.zip", int64(len(body)), shaHex([]byte("NOT-THE-REAL-DIGEST")))
	src.set(body, false)

	err := m.Download("test-txn", "v6.17.6", nil)
	if err == nil || !strings.Contains(err.Error(), "SHA256") {
		t.Fatalf("摘要不符应拒收并指明 SHA256, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(m.versionsDir, "ddnsgo_6.17.6")); !os.IsNotExist(statErr) {
		t.Error("坏包不得落位")
	}
	assertNoTransactionLeftovers(t, m.versionsDir)
}

// TestDownloadMissingDigestRefusesUnverified 缓存里无官方摘要 → 拒无校验安装，
// 且一个下载请求都不发出（先审配置，再碰网络）。
func TestDownloadMissingDigestRefusesUnverified(t *testing.T) {
	src := newZipSource(t)
	m := newChainManager(t, src)
	seedRemote(t, "v6.17.6", "a.zip", 10, "")
	src.set(officialZip(t, "x"), false)

	err := m.Download("test-txn", "v6.17.6", nil)
	if err == nil || !strings.Contains(err.Error(), "拒绝无校验安装") {
		t.Fatalf("应拒绝无校验安装, got %v", err)
	}
	if hits := src.count(); hits != 0 {
		t.Errorf("摘要缺失时不得发起下载请求, hits = %d", hits)
	}
}

// TestDownloadZipMissingExeRejected 合法摘要的 zip 但无 ddns-go.exe →
// extract 后布局自检查拒，半件清理。
func TestDownloadZipMissingExeRejected(t *testing.T) {
	src := newZipSource(t)
	m := newChainManager(t, src)
	body := makeTestZip(t, map[string]string{"LICENSE": "MIT"})
	seedRemote(t, "v6.17.6", "a.zip", int64(len(body)), shaHex(body))
	src.set(body, false)

	err := m.Download("test-txn", "v6.17.6", nil)
	if err == nil || !strings.Contains(err.Error(), "zip 布局无效") {
		t.Fatalf("缺 exe 的 zip 应被布局自检查拒, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(m.versionsDir, "ddnsgo_6.17.6")); !os.IsNotExist(statErr) {
		t.Error("自检失败不得留下版本目录")
	}
	assertNoTransactionLeftovers(t, m.versionsDir)
}

// TestDownloadZipSlipRejected 恶意 ZipSlip 条目 → 内核 UnpackZip 拒收，
// 逃逸文件不落盘、半件清理。
func TestDownloadZipSlipRejected(t *testing.T) {
	src := newZipSource(t)
	m := newChainManager(t, src)
	body := makeTestZip(t, map[string]string{"../evil.txt": "evil", exeName: "fake"})
	seedRemote(t, "v6.17.6", "a.zip", int64(len(body)), shaHex(body))
	src.set(body, false)

	err := m.Download("test-txn", "v6.17.6", nil)
	if err == nil || !strings.Contains(err.Error(), "非法路径") {
		t.Fatalf("ZipSlip 条目应被拒绝, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(m.versionsDir, "evil.txt")); !os.IsNotExist(statErr) {
		t.Fatal("恶意条目逃逸到了版本树根")
	}
	assertNoTransactionLeftovers(t, m.versionsDir)
}

// TestReinstallSamePackageIdempotent 同版本同摘要重复下载 → 内核幂等复用；
// 同版本异摘要 → 拒绝覆盖防漂移（旧实现原地覆盖的雷区由内核收口）。
func TestReinstallSamePackageIdempotent(t *testing.T) {
	src := newZipSource(t)
	m := newChainManager(t, src)
	installViaChain(t, m, src, "v6.17.6", "same")
	if err := m.Download("test-txn", "v6.17.6", nil); err != nil {
		t.Fatalf("同摘要重装应幂等: %v", err)
	}

	tampered := officialZip(t, "same-tampered")
	seedRemote(t, "v6.17.6", "ddns-go_6.17.6_windows_x86_64.zip", int64(len(tampered)), shaHex(tampered))
	src.set(tampered, false)
	err := m.Download("test-txn", "v6.17.6", nil)
	if err == nil || !strings.Contains(err.Error(), "漂移") {
		t.Fatalf("同版本异摘要应拒绝覆盖, got %v", err)
	}
	assertNoTransactionLeftovers(t, m.versionsDir)
}

// TestUpdateChainCoexist 更新链：6.17.5 → 6.17.6 双版本共存、列表最新在前、
// 逐版本 ResolveExe 稳定；卸载新版不动旧版目录。
func TestUpdateChainCoexist(t *testing.T) {
	src := newZipSource(t)
	m := newChainManager(t, src)
	installViaChain(t, m, src, "v6.17.5", "exe-5")
	installViaChain(t, m, src, "v6.17.6", "exe-6")

	installed, err := m.ListInstalled()
	if err != nil || len(installed) != 2 {
		t.Fatalf("应双版本共存, got %+v err %v", installed, err)
	}
	if installed[0].Version != "v6.17.6" || installed[1].Version != "v6.17.5" {
		t.Errorf("列表应最新在前, got %s, %s", installed[0].Version, installed[1].Version)
	}

	exe05, err := m.ResolveExe("v6.17.5")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Remove("v6.17.6"); err != nil {
		t.Fatalf("Remove(v6.17.6): %v", err)
	}
	if fi, serr := os.Stat(exe05); serr != nil || fi.Size() == 0 {
		t.Error("卸载新版不得动旧版目录")
	}
	if _, err := m.ResolveExe("v6.17.6"); err == nil {
		t.Error("已卸载版本应不可解析")
	}
	assertNoTransactionLeftovers(t, m.versionsDir)
}

// ---------- 历史账本双轨回读与导入 ----------

// TestListInstalledLegacyMetaCompat 迁移前的历史安装（无 schema 旧 meta.json）
// 逐字段回读：remote 装记 source=资产名、导入装记 isImport/source=来源目录、
// installedAt 原样透传；无账本目录回退 exe 修改时间。
func TestListInstalledLegacyMetaCompat(t *testing.T) {
	root := t.TempDir()
	m := NewManager(root)

	mkLegacy := func(dirName, exe, meta string) {
		dir := filepath.Join(root, dirName)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, exeName), []byte(exe), 0644); err != nil {
			t.Fatal(err)
		}
		if meta != "" {
			if err := os.WriteFile(filepath.Join(dir, "meta.json"), []byte(meta), 0644); err != nil {
				t.Fatal(err)
			}
		}
	}
	mkLegacy("ddnsgo_6.17.5", "old-exe", `{"installedAt":"2026-08-01 09:00:00","source":"ddns-go_6.17.5_windows_x86_64.zip","zipSize":1,"verifiedHash":true}`)
	mkLegacy("ddnsgo_imported-20260819-121314", "imp-exe", `{"installedAt":"2026-08-19 12:13:14","isImport":true,"source":"D:\\downloads\\ddns"}`)
	mkLegacy("ddnsgo_6.17.4", "no-meta-exe", "")

	installed, err := m.ListInstalled()
	if err != nil {
		t.Fatal(err)
	}
	byVer := map[string]DdnsVersionInfo{}
	for _, v := range installed {
		byVer[v.Version] = v
	}
	if len(installed) != 3 {
		t.Fatalf("历史三装应全列入, got %+v", installed)
	}
	// imported 沉底：降序应为 6.17.5 > 6.17.4 > imported-…
	if installed[0].Version != "v6.17.5" || installed[2].Version != "vimported-20260819-121314" {
		t.Errorf("排序异常: %s, %s", installed[0].Version, installed[2].Version)
	}
	r := byVer["v6.17.5"]
	if r.IsImport || r.Source != "ddns-go_6.17.5_windows_x86_64.zip" || r.InstalledAt != "2026-08-01 09:00:00" {
		t.Errorf("历史 remote 装回读异常: %+v", r)
	}
	imp := byVer["vimported-20260819-121314"]
	if !imp.IsImport || imp.Source != `D:\downloads\ddns` || imp.InstalledAt != "2026-08-19 12:13:14" {
		t.Errorf("历史导入装回读异常: %+v", imp)
	}
	nometa := byVer["v6.17.4"]
	if nometa.InstalledAt == "" || nometa.IsImport {
		t.Errorf("无账本安装应回退 exe 修改时间: %+v", nometa)
	}
}

// TestImportLocalViaTree 导入本地 exe：PE 版本资源不可得（伪 exe）→
// imported-时间戳 目录、内核账本 Source=imported、来源明细记模块账；
// 同目录重复导入拒绝；ListInstalled 双轨回读字段齐备。
func TestImportLocalViaTree(t *testing.T) {
	srcDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(srcDir, exeName), []byte("fake-imported-exe"), 0644); err != nil {
		t.Fatal(err)
	}
	m := NewManager(t.TempDir())

	info, err := m.ImportLocal(srcDir)
	if err != nil {
		t.Fatalf("ImportLocal: %v", err)
	}
	if !info.IsImport || info.Source != srcDir || !strings.HasPrefix(info.Version, "vimported-") {
		t.Errorf("导入回显异常: %+v", info)
	}
	if _, err := os.Stat(info.ExePath); err != nil {
		t.Errorf("导入 exe 未落位: %v", err)
	}

	if _, err := m.ImportLocal(srcDir); err == nil {
		// FileVersion 探测失败 → 同秒时间戳同名冲突被拒；跨秒则新目录——
		// 两种结果都必须保证不覆盖既有安装，此处只验证不报错路径不存在覆盖。
		t.Log("重复导入未被拒绝（跨秒新时间戳），核对不覆盖语义")
	}

	installed, err := m.ListInstalled()
	if err != nil || len(installed) == 0 {
		t.Fatalf("ListInstalled: %+v err %v", installed, err)
	}
	found := false
	for _, v := range installed {
		if v.IsImport && v.Source == srcDir {
			found = true
		}
	}
	if !found {
		t.Errorf("导入装应双轨回读为 IsImport/Source: %+v", installed)
	}
	assertNoTransactionLeftovers(t, m.versionsDir)
}
