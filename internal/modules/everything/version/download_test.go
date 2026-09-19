// 下载→校验→解包→落位全链的失败注入测试：以回环 httptest 服务器为"源"，
// 真实驱动内核 artifact.Fetch/UnpackZip/Tree 路径（真 tmp 目录，无网络依赖），
// 验证中断、坏摘要、缺摘要拒装、半件残留、大小写锚点、双版本共存等收口行为
// （markeron/ccswitch 同构黄金样本；远程槽位解析层经 enrich 接缝恒等注入隔离）。
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

// ---------- 测试夹具：远程槽位注入与假资产源 ----------

// seedRemote 注入伪远程列表。remoteCache 是包内全局缓存（生产语义即进程级单例），
// 本包测试串行执行（不使用 t.Parallel），互相隔离靠 setup 重灌。
// Everything 的官方摘要由 remote 解析层直接携带在 EverythingRelease.SHA256，
// AssetURL 指回环假源（Fetch 纪律允许 http 仅限回环）。
func seedRemote(t *testing.T, version string, size int64, sha string, url string) {
	t.Helper()
	remoteCache.mu.Lock()
	defer remoteCache.mu.Unlock()
	remoteCache.data = []EverythingRelease{{
		Version:  version,
		Channel:  "stable",
		AssetURL: url,
		Size:     size,
		SHA256:   sha,
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

// url 返回当前假源的资产下载地址（回环 http，Fetch 白名单内）。
func (z *zipSource) url(name string) string {
	return z.srv.URL + "/" + name
}

// newChainManager 构造指向假源的 Manager：远程解析层不触网（enrich 注入恒等，
// 槽位记录由 seedRemote 预灌），下载/校验/解包/落位全链仍走生产实现
// artifact.Fetch/UnpackZip/Tree。
func newChainManager(t *testing.T) *Manager {
	t.Helper()
	m := NewManager(t.TempDir())
	m.enrich = func(rel EverythingRelease) EverythingRelease { return rel }
	return m
}

// buildPortableZip 官方 x64 便携 zip 形状（平铺布局）：exe + ini + 语言包。
// exeName 决定条目大小写形态（1.4 小写 / 1.5 大写）。
func buildPortableZip(t *testing.T, exeName, exeContent string) []byte {
	t.Helper()
	path := makeTestZip(t, map[string]string{
		exeName:          exeContent,
		"Everything.ini": "[Everything]\napp_data=0\n",
		"Language.dll":   "lng",
	})
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
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

// newTxnID 生成唯一且过 ValidateVersionToken 白名单的事务 ID：Download 的
// staging 目录名 .tmp-<txnID> 由它派生（journal 背书恢复定位现场的交接面）。
func newTxnID() string {
	return fmt.Sprintf("txn-%d", time.Now().UnixNano())
}

// installViaChain 走真实下载链装一个版本（正例夹具复用）。
func installViaChain(t *testing.T, m *Manager, src *zipSource, version, exeName, exeContent string) {
	t.Helper()
	zipBytes := buildPortableZip(t, exeName, exeContent)
	seedRemote(t, version, int64(len(zipBytes)), shaHex(zipBytes), src.url(assetName(version)))
	src.set(zipBytes, false)
	if err := m.Download(newTxnID(), version, nil); err != nil {
		t.Fatalf("Download(%s): %v", version, err)
	}
}

// ---------- 失败注入用例 ----------

// TestDownloadInterruptKeepsOldVersion 下载中断（传输截断，全部候选源失败）→
// 报 error 进度、旧版本完好、版本树无新目录与半件残留。
func TestDownloadInterruptKeepsOldVersion(t *testing.T) {
	src := newZipSource(t)
	m := newChainManager(t)
	installViaChain(t, m, src, "1.4.1.1032", "everything.exe", "old-exe-bytes")

	newZip := buildPortableZip(t, "Everything.exe", "brand-new-exe")
	seedRemote(t, "1.5.0.1422b", int64(len(newZip)), shaHex(newZip), src.url(assetName("1.5.0.1422b")))
	src.set(newZip, true) // 半路掐线

	rec := &progressRecorder{}
	err := m.Download(newTxnID(), "1.5.0.1422b", rec.cb())
	if err == nil {
		t.Fatal("下载中断应报错")
	}
	stages := rec.stages()
	if !strings.Contains(strings.Join(stages, ","), "error") || strings.Contains(strings.Join(stages, ","), "done") {
		t.Fatalf("进度事件应含 error 且不得含 done: %v", stages)
	}
	if _, statErr := os.Stat(filepath.Join(m.versionsDir, "everything_v1.5.0.1422b")); !os.IsNotExist(statErr) {
		t.Error("新半件目录不得出现")
	}
	assertNoTransactionLeftovers(t, m.versionsDir)
	installed, lerr := m.ListInstalled()
	if lerr != nil || len(installed) != 1 || installed[0].Version != "1.4.1.1032" {
		t.Fatalf("旧版本应完好可用, got %+v err %v", installed, lerr)
	}
}

// TestDownloadBadDigestRejects 摘要错（源内容与官方清单哈希不符）→ 不落位且旧版完好。
func TestDownloadBadDigestRejects(t *testing.T) {
	src := newZipSource(t)
	m := newChainManager(t)
	installViaChain(t, m, src, "1.4.1.1032", "everything.exe", "old-exe-bytes")
	oldExeHashBefore, _ := fileSHA256(filepath.Join(m.versionsDir, "everything_v1.4.1.1032", "everything.exe"))

	newZip := buildPortableZip(t, "Everything.exe", "tampered-payload")
	seedRemote(t, "1.5.0.1422b", int64(len(newZip)),
		shaHex([]byte("NOT-THE-REAL-DIGEST")), src.url(assetName("1.5.0.1422b")))
	src.set(newZip, false)

	err := m.Download(newTxnID(), "1.5.0.1422b", nil)
	if err == nil {
		t.Fatal("摘要不符应拒收")
	}
	if !strings.Contains(err.Error(), "SHA256") {
		t.Errorf("错误应指明摘要校验失败: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(m.versionsDir, "everything_v1.5.0.1422b")); !os.IsNotExist(statErr) {
		t.Error("坏包不得落位")
	}
	assertNoTransactionLeftovers(t, m.versionsDir)
	oldExeHashAfter, _ := fileSHA256(filepath.Join(m.versionsDir, "everything_v1.4.1.1032", "everything.exe"))
	if oldExeHashBefore != oldExeHashAfter || oldExeHashBefore == "" {
		t.Error("旧版本目录必须原样完好")
	}
}

// TestDownloadMissingDigestRefusesUnverified 官方 sha256 清单不可得（槽位摘要为空
// 且活体补齐无果）→ 拒无校验安装（原"四级兜底"的降级直装形态按内核纪律废止），
// 且一个下载请求都不发出（先审配置，再碰网络）。
func TestDownloadMissingDigestRefusesUnverified(t *testing.T) {
	src := newZipSource(t)
	m := newChainManager(t)
	zipBytes := buildPortableZip(t, "Everything.exe", "x")
	seedRemote(t, "1.5.0.1422b", int64(len(zipBytes)), "", src.url(assetName("1.5.0.1422b")))
	src.set(zipBytes, false)

	err := m.Download(newTxnID(), "1.5.0.1422b", nil)
	if err == nil || !strings.Contains(err.Error(), "拒绝无校验安装") {
		t.Fatalf("应拒绝无校验安装, got %v", err)
	}
	if hits := src.count(); hits != 0 {
		t.Errorf("摘要缺失时不得发起下载请求, hits = %d", hits)
	}
}

// TestDownloadMissingSlot 远程列表不存在的版本 → 既有文案报错。
func TestDownloadMissingSlot(t *testing.T) {
	m := newChainManager(t)
	seedRemote(t, "1.4.1.1032", 100, strings.Repeat("a", 64), "http://127.0.0.1:0/nope.zip")
	err := m.Download(newTxnID(), "9.9.9", nil)
	if err == nil || !strings.Contains(err.Error(), "远程列表不存在版本 9.9.9") {
		t.Fatalf("未知槽位应报既有文案, got %v", err)
	}
}

// TestZipLayoutRejectedKeepsTree 锚点自检不符的包（zip 无 Everything.exe）：解包进
// staging 后 Commit 前自检拒装，最终目录不出现、半件不残留。
func TestZipLayoutRejectedKeepsTree(t *testing.T) {
	src := newZipSource(t)
	m := newChainManager(t)

	noExe := makeTestZip(t, map[string]string{"README.txt": "hello"})
	zipBytes, err := os.ReadFile(noExe)
	if err != nil {
		t.Fatal(err)
	}
	seedRemote(t, "1.5.0.1422b", int64(len(zipBytes)), shaHex(zipBytes), src.url(assetName("1.5.0.1422b")))
	src.set(zipBytes, false)

	err = m.Download(newTxnID(), "1.5.0.1422b", nil)
	if err == nil || !strings.Contains(err.Error(), "zip 布局无效") {
		t.Fatalf("缺锚点应拒装, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(m.versionsDir, "everything_v1.5.0.1422b")); !os.IsNotExist(statErr) {
		t.Error("锚点不符的包不得落位")
	}
	assertNoTransactionLeftovers(t, m.versionsDir)
}

// TestDownloadZipSlipRejected 恶意条目（../逃逸）由内核 UnpackZip 闸门拒收，
// 半件不残留（原 extractAll 自研 ZipSlip 防护的收口对照，行为口径不回退）。
func TestDownloadZipSlipRejected(t *testing.T) {
	src := newZipSource(t)
	m := newChainManager(t)

	ev := makeTestZip(t, map[string]string{
		"Everything.exe": "fake",
		"../evil.txt":    "evil",
	})
	zipBytes, err := os.ReadFile(ev)
	if err != nil {
		t.Fatal(err)
	}
	seedRemote(t, "1.5.0.1422b", int64(len(zipBytes)), shaHex(zipBytes), src.url(assetName("1.5.0.1422b")))
	src.set(zipBytes, false)

	if err := m.Download(newTxnID(), "1.5.0.1422b", nil); err == nil {
		t.Fatal("ZipSlip 条目应被拒绝")
	}
	if _, statErr := os.Stat(filepath.Join(m.versionsDir, "evil.txt")); !os.IsNotExist(statErr) {
		t.Fatal("恶意条目逃逸到了版本树之外")
	}
	assertNoTransactionLeftovers(t, m.versionsDir)
}

// TestDownloadLowercaseExeAnchor 大小写容错锚点链：1.4 通道小写 everything.exe 的包
// 正常落位，账本 Entry 记录实际文件名，ResolveExe 可定位。
func TestDownloadLowercaseExeAnchor(t *testing.T) {
	src := newZipSource(t)
	m := newChainManager(t)
	installViaChain(t, m, src, "1.4.1.1032", "everything.exe", "lower-exe")

	metaRaw, err := os.ReadFile(filepath.Join(m.versionsDir, "everything_v1.4.1.1032", "meta.json"))
	if err != nil {
		t.Fatal(err)
	}
	var meta artifact.Meta
	if err := json.Unmarshal(metaRaw, &meta); err != nil {
		t.Fatal(err)
	}
	if meta.Entry != "everything.exe" || meta.Source != artifact.SourceRemote {
		t.Errorf("账本应记录实际锚点名: %+v", meta)
	}
	exe, err := m.ResolveExe("1.4.1.1032")
	if err != nil || !strings.EqualFold(filepath.Base(exe), "everything.exe") {
		t.Errorf("ResolveExe 大小写容错失败: %v %v", exe, err)
	}
}

// TestUpdateChainCoexist 更新成功链：1.4.1.1032 → 1.5.0.1422b 双版本共存、
// 列表最新在前（尾字母修正版参与排序）；进度词表零漂移（含如实映射的 verify）；
// 新链账本为内核统一形状且来源重建为官方资产名；卸载新版不动旧版目录。
func TestUpdateChainCoexist(t *testing.T) {
	src := newZipSource(t)
	m := newChainManager(t)
	installViaChain(t, m, src, "1.4.1.1032", "everything.exe", "exe-1.4.1.1032")

	rec := &progressRecorder{}
	zipBytes := buildPortableZip(t, "Everything.exe", "exe-1.5.0.1422b")
	seedRemote(t, "1.5.0.1422b", int64(len(zipBytes)), shaHex(zipBytes), src.url(assetName("1.5.0.1422b")))
	src.set(zipBytes, false)
	if err := m.Download(newTxnID(), "1.5.0.1422b", rec.cb()); err != nil {
		t.Fatalf("Download(1.5.0.1422b): %v", err)
	}

	// 进度词表零漂移：只允许既有词汇，且顺序为 downloading → verify → extract → done
	// （verify 是内核流式+落盘双摘要核验的如实映射，不造幻影、也不丢既有词）
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

	installed, err := m.ListInstalled()
	if err != nil {
		t.Fatal(err)
	}
	if len(installed) != 2 {
		t.Fatalf("应双版本共存, got %+v", installed)
	}
	if installed[0].Version != "1.5.0.1422b" || installed[1].Version != "1.4.1.1032" {
		t.Errorf("列表应最新在前, got %s, %s", installed[0].Version, installed[1].Version)
	}
	if installed[0].InstalledAt == "" || installed[0].IsImport {
		t.Errorf("展示账目字段应齐备且非导入: %+v", installed[0])
	}
	if !strings.HasPrefix(installed[0].InstalledAt, "20") || len(installed[0].InstalledAt) != len("2006-01-02 15:04:05") {
		t.Errorf("installedAt 应保持 yyyy-MM-dd HH:mm:ss 展示口径: %q", installed[0].InstalledAt)
	}
	if installed[0].Source != assetName("1.5.0.1422b") {
		t.Errorf("新链来源展示应重建为官方资产名, got %q", installed[0].Source)
	}

	// 落位账本由内核统一形状写入
	metaRaw, err := os.ReadFile(filepath.Join(installed[0].Dir, "meta.json"))
	if err != nil {
		t.Fatal(err)
	}
	var meta artifact.Meta
	if err := json.Unmarshal(metaRaw, &meta); err != nil {
		t.Fatal(err)
	}
	if meta.Entry != "Everything.exe" || meta.ZipSHA256 != shaHex(zipBytes) || meta.Source != artifact.SourceRemote {
		t.Errorf("账本内容异常: %+v", meta)
	}

	exeOld, err := m.ResolveExe("1.4.1.1032")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Remove("1.5.0.1422b"); err != nil {
		t.Fatalf("Remove(1.5.0.1422b): %v", err)
	}
	if fi, serr := os.Stat(exeOld); serr != nil || fi.Size() == 0 {
		t.Error("卸载新版不得动旧版目录")
	}
	if _, err := m.ResolveExe("1.5.0.1422b"); err == nil {
		t.Error("已卸载版本应不可解析")
	}
}

// TestReinstallSamePackageIdempotent 同版本同包摘要重复落位 → 内核幂等复用不报错；
// 同版本异摘要 → 拒绝覆盖防漂移。（重装复用首次落位的同一份字节，摘要才可比对。）
func TestReinstallSamePackageIdempotent(t *testing.T) {
	src := newZipSource(t)
	m := newChainManager(t)
	zipBytes := buildPortableZip(t, "Everything.exe", "same-exe")
	seedRemote(t, "1.4.1.1032", int64(len(zipBytes)), shaHex(zipBytes), src.url(assetName("1.4.1.1032")))
	src.set(zipBytes, false)
	if err := m.Download(newTxnID(), "1.4.1.1032", nil); err != nil {
		t.Fatalf("首装: %v", err)
	}
	if err := m.Download(newTxnID(), "1.4.1.1032", nil); err != nil {
		t.Fatalf("同摘要重装应幂等: %v", err)
	}

	// 同版本号换内容（异摘要）：模拟上游同名资产重传，必须拒绝
	tampered := buildPortableZip(t, "Everything.exe", "same-exe-tampered")
	seedRemote(t, "1.4.1.1032", int64(len(tampered)), shaHex(tampered), src.url(assetName("1.4.1.1032")))
	src.set(tampered, false)
	err := m.Download(newTxnID(), "1.4.1.1032", nil)
	if err == nil || !strings.Contains(err.Error(), "漂移") {
		t.Fatalf("同版本异摘要应拒绝覆盖, got %v", err)
	}
}

// TestStagedResidueAbandoned 落位中途强杀等价模拟：staging 半件残留 →
// Versions 不显示半件；CleanupAbandoned 收尸后目录干净。
func TestStagedResidueAbandoned(t *testing.T) {
	m := newChainManager(t)
	staging, discard, err := m.tree.StageDir("inst-kill-sim")
	if err != nil {
		t.Fatal(err)
	}
	_ = discard // 模拟强杀：不收口、不丢弃
	if err := os.WriteFile(filepath.Join(staging, "Everything.exe"), []byte("half"), 0644); err != nil {
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
