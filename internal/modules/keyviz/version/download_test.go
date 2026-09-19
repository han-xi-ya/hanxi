// 下载→官方摘要双核→管理提取→落位全链的失败注入测试：以回环 httptest 服务器
// 为"源"，真实驱动内核 artifact.Fetch/Tree 路径 + msiExtract 接缝假实现
// （真 tmp 目录，无网络依赖、不依赖本机 Installer 服务），验证中断、坏摘要、
// 提取失败半件残留、双版本共存等收口行为（ccswitch/markeron 同构黄金样本，
// MSI 特例差异在提取段）。
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

// seedRemote 注入伪远程列表。remoteCache 是包内全局缓存（生产语义即进程级
// 单例），本包测试串行执行（不使用 t.Parallel），互相隔离靠 setup 重灌。
// Keyviz 的官方摘要直接携带在 KeyvizRelease.SHA256（远程解析层已剥前缀）。
func seedRemote(t *testing.T, version, assetName string, size int64, sha string) {
	t.Helper()
	remoteCache.mu.Lock()
	defer remoteCache.mu.Unlock()
	remoteCache.data = []KeyvizRelease{{
		Version:   version,
		AssetName: assetName,
		AssetURL:  "http://127.0.0.1:0/placeholder",
		Size:      size,
		SHA256:    sha,
	}}
	remoteCache.fetchedAt = time.Now() // 处于 TTL 内，Download 不会再走网络拉列表
}

// msiSource 可编程假 MSI 资产源：按当前 body 提供下载，支持截断（模拟中断）与命中计数。
type msiSource struct {
	mu       sync.Mutex
	body     []byte
	truncate bool
	hits     int
	srv      *httptest.Server
}

func newMSISource(t *testing.T) *msiSource {
	t.Helper()
	z := &msiSource{}
	z.srv = httptest.NewServer(http.HandlerFunc(z.serve))
	t.Cleanup(z.srv.Close)
	return z
}

func (z *msiSource) serve(w http.ResponseWriter, r *http.Request) {
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

func (z *msiSource) set(body []byte, truncate bool) {
	z.mu.Lock()
	defer z.mu.Unlock()
	z.body = body
	z.truncate = truncate
}

func (z *msiSource) count() int {
	z.mu.Lock()
	defer z.mu.Unlock()
	return z.hits
}

// newChainManager 构造指向假源的 Manager：镜像 URL 接缝改指回环 http（真实
// artifact.Fetch 下载/校验链不变），msiExtract 接缝注入"落盘假管理映像"的
// 成功实现（真实提取已侦查阶段真机验证；失败用例各自覆写本接缝）。
func newChainManager(t *testing.T, src *msiSource) *Manager {
	t.Helper()
	m := NewManager(t.TempDir())
	m.mirrors = func(_, _ string) []string {
		return []string{src.srv.URL + "/keyviz_windows.msi"}
	}
	withFakeMSIExtract(t, func(_, stage string) error {
		writeFakeAdminImage(t, stage)
		return nil
	})
	return m
}

// buildFakeMSI 假 MSI 字节：提取段由假接缝接管，内核只按官方摘要校验传输完整性，
// 内容形状对下载链无意义。
func buildFakeMSI(t *testing.T, tag string) []byte {
	t.Helper()
	return []byte("fake-msi-payload:" + tag)
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

// installViaChain 走真实下载链 + 假提取装一个版本（正例夹具复用）。
func installViaChain(t *testing.T, m *Manager, src *msiSource, version, tag string) {
	t.Helper()
	msiBytes := buildFakeMSI(t, tag)
	seedRemote(t, version, "keyviz_"+strings.TrimPrefix(version, "v")+"_windows.msi",
		int64(len(msiBytes)), shaHex(msiBytes))
	src.set(msiBytes, false)
	if err := m.Download(newTxnID(), version, nil); err != nil {
		t.Fatalf("Download(%s): %v", version, err)
	}
}

// ---------- 全链用例 ----------

// TestDownloadChainSuccess 正例全链：进度词表零漂移（downloading→verify→extract→done，
// verify 由内核 Fetch 摘要双核如实映射既有词汇）；落位目录含提取收割的 payload
// 平铺布局与双账本（内核 meta.json + 模块侧 msi-extract.json），映像根部源 msi
// 副本不入安装目录，版本树无事务残留。
func TestDownloadChainSuccess(t *testing.T) {
	src := newMSISource(t)
	m := newChainManager(t, src)
	rec := &progressRecorder{}

	msiBytes := buildFakeMSI(t, "2.1.1")
	seedRemote(t, "v2.1.1", "keyviz_2.1.1_windows.msi", int64(len(msiBytes)), shaHex(msiBytes))
	src.set(msiBytes, false)

	if err := m.Download(newTxnID(), "v2.1.1", rec.cb()); err != nil {
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

	dir := filepath.Join(m.versionsDir, "keyviz_2.1.1")
	for _, rel := range []string{exeName, filepath.Join("extra", "icon.png")} {
		if _, err := os.Stat(filepath.Join(dir, rel)); err != nil {
			t.Errorf("落位缺 %s: %v", rel, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "keyviz.msi")); !os.IsNotExist(err) {
		t.Error("管理映像根部源 msi 副本不得落位")
	}

	metaRaw, err := os.ReadFile(filepath.Join(dir, "meta.json"))
	if err != nil {
		t.Fatal(err)
	}
	var meta artifact.Meta
	if err := json.Unmarshal(metaRaw, &meta); err != nil {
		t.Fatal(err)
	}
	if meta.Schema != artifact.DefaultSchema || meta.Entry != exeName ||
		meta.ZipSHA256 != shaHex(msiBytes) || meta.Source != artifact.SourceRemote ||
		meta.AssetSHA256 == "" || meta.InstalledAt.IsZero() {
		t.Errorf("内核账本异常: %+v", meta)
	}

	infoRaw, err := os.ReadFile(filepath.Join(dir, msiExtractInfoName))
	if err != nil {
		t.Fatalf("模块侧提取账本缺失: %v", err)
	}
	var info map[string]any
	if err := json.Unmarshal(infoRaw, &info); err != nil {
		t.Fatal(err)
	}
	if info["method"] != "msiexec /a administrative install" ||
		info["asset"] != "keyviz_2.1.1_windows.msi" || info["msiSHA256"] != shaHex(msiBytes) ||
		info["extractedEntry"] != exeName {
		t.Errorf("提取账本异常: %v", info)
	}

	list, err := m.ListInstalled()
	if err != nil || len(list) != 1 || list[0].Version != "v2.1.1" || list[0].IsImport {
		t.Fatalf("ListInstalled 异常: %+v %v", list, err)
	}
	assertNoTransactionLeftovers(t, m.versionsDir)
}

// TestDownloadInterruptKeepsOldVersion 下载中断（传输截断，全部候选源失败）→
// 报 error 进度、旧版本完好、版本树无新目录与半件残留。
func TestDownloadInterruptKeepsOldVersion(t *testing.T) {
	src := newMSISource(t)
	m := newChainManager(t, src)
	installViaChain(t, m, src, "v2.1.0", "old")

	newMSI := buildFakeMSI(t, "interrupted")
	seedRemote(t, "v2.1.1", "keyviz_2.1.1_windows.msi", int64(len(newMSI)), shaHex(newMSI))
	src.set(newMSI, true) // 半路掐线

	rec := &progressRecorder{}
	err := m.Download(newTxnID(), "v2.1.1", rec.cb())
	if err == nil {
		t.Fatal("下载中断应报错")
	}
	stages := strings.Join(rec.stages(), ",")
	if !strings.Contains(stages, "error") || strings.Contains(stages, "done") {
		t.Fatalf("进度事件应含 error 且不得含 done: %v", rec.stages())
	}
	if _, statErr := os.Stat(filepath.Join(m.versionsDir, "keyviz_2.1.1")); !os.IsNotExist(statErr) {
		t.Error("新半件目录不得出现")
	}
	assertNoTransactionLeftovers(t, m.versionsDir)
	installed, lerr := m.ListInstalled()
	if lerr != nil || len(installed) != 1 || installed[0].Version != "v2.1.0" {
		t.Fatalf("旧版本应完好可用, got %+v err %v", installed, lerr)
	}
}

// TestDownloadBadDigestRejects 摘要错（源内容与官方摘要不符）→ 不落位且旧版完好。
func TestDownloadBadDigestRejects(t *testing.T) {
	src := newMSISource(t)
	m := newChainManager(t, src)
	installViaChain(t, m, src, "v2.1.0", "old")

	newMSI := buildFakeMSI(t, "tampered")
	seedRemote(t, "v2.1.1", "keyviz_2.1.1_windows.msi", int64(len(newMSI)),
		shaHex([]byte("NOT-THE-REAL-DIGEST"))) // 官方摘要对不上任何传输字节
	src.set(newMSI, false)

	err := m.Download(newTxnID(), "v2.1.1", nil)
	if err == nil {
		t.Fatal("摘要不符应拒收")
	}
	if !strings.Contains(err.Error(), "SHA256") {
		t.Errorf("错误应指明摘要校验失败: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(m.versionsDir, "keyviz_2.1.1")); !os.IsNotExist(statErr) {
		t.Error("坏包不得落位")
	}
	assertNoTransactionLeftovers(t, m.versionsDir)
}

// TestDownloadMissingDigestRefusesUnverified 上游无官方摘要 → 拒无校验安装，
// 且一个下载请求都不发出（先审配置，再碰网络）。
func TestDownloadMissingDigestRefusesUnverified(t *testing.T) {
	src := newMSISource(t)
	m := newChainManager(t, src)
	msiBytes := buildFakeMSI(t, "x")
	seedRemote(t, "v2.1.1", "keyviz_2.1.1_windows.msi", int64(len(msiBytes)), "")
	src.set(msiBytes, false)

	err := m.Download(newTxnID(), "v2.1.1", nil)
	if err == nil || !strings.Contains(err.Error(), "拒绝无校验安装") {
		t.Fatalf("应拒绝无校验安装, got %v", err)
	}
	if hits := src.count(); hits != 0 {
		t.Errorf("摘要缺失时不得发起下载请求, hits = %d", hits)
	}
}

// TestDownloadExtractFailureDiscardsStaging 提取段失败（假 msiExtract 报错）→
// error 事件、staging 丢弃、版本树无半件、旧版完好。
func TestDownloadExtractFailureDiscardsStaging(t *testing.T) {
	src := newMSISource(t)
	m := newChainManager(t, src)
	installViaChain(t, m, src, "v2.1.0", "old")

	withFakeMSIExtract(t, func(string, string) error {
		return fmt.Errorf("msiexec 管理提取失败: fake")
	})
	newMSI := buildFakeMSI(t, "extract-fail")
	seedRemote(t, "v2.1.1", "keyviz_2.1.1_windows.msi", int64(len(newMSI)), shaHex(newMSI))
	src.set(newMSI, false)

	rec := &progressRecorder{}
	err := m.Download(newTxnID(), "v2.1.1", rec.cb())
	if err == nil {
		t.Fatal("提取失败应报错")
	}
	if got := strings.Join(rec.stages(), ","); !strings.Contains(got, "extract,error") || strings.Contains(got, "done") {
		t.Fatalf("应在 extract 后收口 error: %s", got)
	}
	assertNoTransactionLeftovers(t, m.versionsDir)
}

// TestDownloadEmptyImageRejected 提取"成功"但映像无 payload → 布局自检拒装
// （轮询窗口压缩免等 10s）。
func TestDownloadEmptyImageRejected(t *testing.T) {
	src := newMSISource(t)
	m := newChainManager(t, src)
	withFakeMSIExtract(t, func(_, _ string) error { return nil }) // 空映像
	compressMSIPollWindow(t)

	msiBytes := buildFakeMSI(t, "empty-image")
	seedRemote(t, "v2.1.1", "keyviz_2.1.1_windows.msi", int64(len(msiBytes)), shaHex(msiBytes))
	src.set(msiBytes, false)

	err := m.Download(newTxnID(), "v2.1.1", nil)
	if err == nil || !strings.Contains(err.Error(), "管理提取无效") {
		t.Fatalf("空映像应拒装, got %v", err)
	}
	assertNoTransactionLeftovers(t, m.versionsDir)
}

// TestReinstallSamePackageIdempotent 同版本同包摘要重复落位 → 内核幂等复用不报错；
// 同版本异摘要 → 拒绝覆盖防漂移。
func TestReinstallSamePackageIdempotent(t *testing.T) {
	src := newMSISource(t)
	m := newChainManager(t, src)
	msiBytes := buildFakeMSI(t, "same")
	seedRemote(t, "v2.1.1", "keyviz_2.1.1_windows.msi", int64(len(msiBytes)), shaHex(msiBytes))
	src.set(msiBytes, false)
	if err := m.Download(newTxnID(), "v2.1.1", nil); err != nil {
		t.Fatalf("首装: %v", err)
	}
	if err := m.Download(newTxnID(), "v2.1.1", nil); err != nil {
		t.Fatalf("同摘要重装应幂等: %v", err)
	}

	// 同版本号换内容（异摘要）：模拟上游同 tag 重传，必须拒绝
	tampered := buildFakeMSI(t, "tampered")
	seedRemote(t, "v2.1.1", "keyviz_2.1.1_windows.msi", int64(len(tampered)), shaHex(tampered))
	src.set(tampered, false)
	err := m.Download(newTxnID(), "v2.1.1", nil)
	if err == nil || !strings.Contains(err.Error(), "漂移") {
		t.Fatalf("同版本异摘要应拒绝覆盖, got %v", err)
	}
}

// TestUpdateChainCoexist 更新成功链：2.1.0 → 2.1.1 双版本共存、列表最新在前、
// 卸载新版不动旧版目录。
func TestUpdateChainCoexist(t *testing.T) {
	src := newMSISource(t)
	m := newChainManager(t, src)
	installViaChain(t, m, src, "v2.1.0", "exe-2.1.0")
	installViaChain(t, m, src, "v2.1.1", "exe-2.1.1")

	installed, err := m.ListInstalled()
	if err != nil {
		t.Fatal(err)
	}
	if len(installed) != 2 || installed[0].Version != "v2.1.1" || installed[1].Version != "v2.1.0" {
		t.Fatalf("应双版本共存且最新在前, got %+v", installed)
	}
	if installed[0].InstalledAt == "" || installed[0].IsImport {
		t.Errorf("展示账目字段应齐备且非导入: %+v", installed[0])
	}
	exe210, err := m.ResolveExe("v2.1.0")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Remove("v2.1.1"); err != nil {
		t.Fatalf("Remove(v2.1.1): %v", err)
	}
	if fi, serr := os.Stat(exe210); serr != nil || fi.Size() == 0 {
		t.Error("卸载新版不得动旧版目录")
	}
}

// TestStagedResidueAbandoned 落位中途强杀等价模拟：staging 半件残留 →
// Versions 不显示半件；CleanupAbandoned 收尸后目录干净。
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
