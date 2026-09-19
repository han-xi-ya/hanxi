// 下载→校验→落位全链的失败注入测试（markeron/rufus 金样本同构，资产形态为
// 单便携 exe）：以回环 httptest 服务器为"源"，真实驱动内核 artifact.Fetch/Tree
// 路径（真 tmp 目录，无网络依赖），验证中断、坏摘要、缺摘要、PE 身份拒收、
// 双版本共存与落位账本。PE 校验接缝以 fakeVerifyExe 替换（真链无 MangoDisk
// 真 PE 可注入），编排/摘要/落位语义全部走生产实现。
package version

import (
	"bytes"
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

// ---------- 测试夹具：PE 桩、远程列表注入与假资产源 ----------

// fakeVerifyExe validateExecutable 的单测替身：假体 exe 布局为 "MZ<版本串>"，
// 版本从体内读出。MZ 头与 FileVersion 对版本的拒收路径保持生产语义。
func fakeVerifyExe(path, wantVersion string) (string, string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", "", err
	}
	if !bytes.HasPrefix(b, []byte("MZ")) {
		return "", "", fmt.Errorf("不是有效的 Windows PE 文件: 假体缺 MZ 头")
	}
	ver := strings.TrimPrefix(string(b), "MZ")
	if wantVersion != "" && ver != wantVersion {
		return ver, "MangoDisk", fmt.Errorf("FileVersion 不匹配：期望 %s，实际 %s", wantVersion, ver)
	}
	return ver, "MangoDisk", nil
}

// exeBody 构造 "MZ<版本串>" 形态的假体 exe 字节。
func exeBody(version string) []byte { return []byte("MZ" + version) }

// newTestManager 构造带 PE 桩的 Manager（纯目录场景无需改源接缝）。
func newTestManager(t *testing.T) *Manager {
	t.Helper()
	m := NewManager(t.TempDir())
	m.verifyExe = fakeVerifyExe
	return m
}

// seedRemote 注入伪远程列表（MangoDiskRelease 自带官方摘要，parseReleasesBody
// 已在生产链把无/坏 digest 挡在列表外）。remoteCache 是包内全局缓存（生产语义
// 即进程级单例），本包测试串行执行（不使用 t.Parallel），互相隔离靠重灌。
func seedRemote(t *testing.T, version, assetName string, size int64, digest string) {
	t.Helper()
	remoteCache.mu.Lock()
	defer remoteCache.mu.Unlock()
	remoteCache.data = []MangoDiskRelease{{
		Version:   version,
		AssetName: assetName,
		AssetURL:  "http://127.0.0.1:0/placeholder",
		Size:      size,
		SHA256:    digest,
	}}
	remoteCache.fetchedAt = time.Now() // 处于 TTL 内，Download 不会再走网络拉列表
}

// exeSource 可编程假资产源：按当前 body 提供下载，支持截断（模拟中断）与命中计数。
type exeSource struct {
	mu       sync.Mutex
	body     []byte
	truncate bool
	hits     int
	srv      *httptest.Server
}

func newExeSource(t *testing.T) *exeSource {
	t.Helper()
	z := &exeSource{}
	z.srv = httptest.NewServer(http.HandlerFunc(z.serve))
	t.Cleanup(z.srv.Close)
	return z
}

func (z *exeSource) serve(w http.ResponseWriter, r *http.Request) {
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

func (z *exeSource) set(body []byte, truncate bool) {
	z.mu.Lock()
	defer z.mu.Unlock()
	z.body = body
	z.truncate = truncate
}

func (z *exeSource) count() int {
	z.mu.Lock()
	defer z.mu.Unlock()
	return z.hits
}

// newChainManager 构造指向假源的 Manager（镜像 URL 接缝改指回环 http、PE 桩
// 注入；下载/校验/落位全链仍走生产实现 artifact.Fetch/Tree）。
func newChainManager(t *testing.T, src *exeSource) *Manager {
	t.Helper()
	m := NewManager(t.TempDir())
	m.verifyExe = fakeVerifyExe
	m.mirrors = func(_, _ string) []string {
		return []string{src.srv.URL + "/MangoDisk-1.0.7-windows-portable.exe"}
	}
	return m
}

func shaHex(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// newTxnID 生成唯一且过 ValidateVersionToken 白名单的事务 ID：Download 的
// staging 目录名 .tmp-<txnID> 由它派生（journal 背书恢复定位现场的交接面）。
func newTxnID() string {
	return fmt.Sprintf("txn-%d", time.Now().UnixNano())
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
func installViaChain(t *testing.T, m *Manager, src *exeSource, version string) {
	t.Helper()
	exeBytes := exeBody(strings.TrimPrefix(version, "v"))
	seedRemote(t, version, expectedAssetName(version), int64(len(exeBytes)), shaHex(exeBytes))
	src.set(exeBytes, false)
	if err := m.Download(newTxnID(), version, nil); err != nil {
		t.Fatalf("Download(%s): %v", version, err)
	}
}

// ---------- 失败注入用例 ----------

// TestDownloadInterruptKeepsOldVersion 下载中断（传输截断，全部候选源失败）→
// 报 error 进度、旧版本完好、版本树无新目录与半件残留。
func TestDownloadInterruptKeepsOldVersion(t *testing.T) {
	src := newExeSource(t)
	m := newChainManager(t, src)
	installViaChain(t, m, src, "v1.0.6")

	newExe := exeBody("1.0.7")
	seedRemote(t, "v1.0.7", expectedAssetName("v1.0.7"), int64(len(newExe)), shaHex(newExe))
	src.set(newExe, true) // 半路掐线

	rec := &progressRecorder{}
	err := m.Download(newTxnID(), "v1.0.7", rec.cb())
	if err == nil {
		t.Fatal("下载中断应报错")
	}
	stages := rec.stages()
	joined := strings.Join(stages, ",")
	if !strings.Contains(joined, "error") || strings.Contains(joined, "done") {
		t.Fatalf("进度事件应含 error 且不得含 done: %v", stages)
	}
	if _, statErr := os.Stat(filepath.Join(m.versionsDir, "mangodisk_1.0.7")); !os.IsNotExist(statErr) {
		t.Error("新半件目录不得出现")
	}
	assertNoTransactionLeftovers(t, m.versionsDir)
	installed, lerr := m.ListInstalled()
	if lerr != nil || len(installed) != 1 || installed[0].Version != "v1.0.6" {
		t.Fatalf("旧版本应完好可用, got %+v err %v", installed, lerr)
	}
}

// TestDownloadBadDigestRejects 摘要错（源内容与官方摘要不符）→ 不落位且旧版完好。
func TestDownloadBadDigestRejects(t *testing.T) {
	src := newExeSource(t)
	m := newChainManager(t, src)
	installViaChain(t, m, src, "v1.0.6")
	oldExeHashBefore := fileSHA256(filepath.Join(m.versionsDir, "mangodisk_1.0.6", exeName))

	newExe := exeBody("1.0.7")
	seedRemote(t, "v1.0.7", expectedAssetName("v1.0.7"), int64(len(newExe)), shaHex([]byte("NOT-THE-REAL-CONTENT")))
	src.set(newExe, false)

	err := m.Download(newTxnID(), "v1.0.7", nil)
	if err == nil {
		t.Fatal("摘要不符应拒收")
	}
	if !strings.Contains(err.Error(), "SHA256") {
		t.Errorf("错误应指明摘要校验失败: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(m.versionsDir, "mangodisk_1.0.7")); !os.IsNotExist(statErr) {
		t.Error("坏包不得落位")
	}
	assertNoTransactionLeftovers(t, m.versionsDir)
	oldExeHashAfter := fileSHA256(filepath.Join(m.versionsDir, "mangodisk_1.0.6", exeName))
	if oldExeHashBefore != oldExeHashAfter || oldExeHashBefore == "" {
		t.Error("旧版本目录必须原样完好")
	}
}

// TestDownloadMissingDigestRefusesUnverified 缓存里无官方摘要 → 拒无校验安装，
// 且一个下载请求都不发出（先审配置，再碰网络）。
func TestDownloadMissingDigestRefusesUnverified(t *testing.T) {
	src := newExeSource(t)
	m := newChainManager(t, src)
	seedRemote(t, "v1.0.7", expectedAssetName("v1.0.7"), int64(len(exeBody("1.0.7"))), "")
	src.set(exeBody("1.0.7"), false)

	err := m.Download(newTxnID(), "v1.0.7", nil)
	if err == nil || !strings.Contains(err.Error(), "拒绝无校验安装") {
		t.Fatalf("应拒绝无校验安装, got %v", err)
	}
	if hits := src.count(); hits != 0 {
		t.Errorf("摘要缺失时不得发起下载请求, hits = %d", hits)
	}
}

// TestDownloadFileVersionMismatchRejected 摘要正确但 PE FileVersion 与目标版本
// 不符（镜像错位资产伪装）→ verify 阶段领域断言拒收，不落位。
func TestDownloadFileVersionMismatchRejected(t *testing.T) {
	src := newExeSource(t)
	m := newChainManager(t, src)
	body := exeBody("9.9.9") // 摘要真实、版本错位
	seedRemote(t, "v1.0.7", expectedAssetName("v1.0.7"), int64(len(body)), shaHex(body))
	src.set(body, false)

	err := m.Download(newTxnID(), "v1.0.7", nil)
	if err == nil || !strings.Contains(err.Error(), "FileVersion 不匹配") {
		t.Fatalf("版本错位资产应被领域断言拒收, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(m.versionsDir, "mangodisk_1.0.7")); !os.IsNotExist(statErr) {
		t.Error("版本错位资产不得落位")
	}
	assertNoTransactionLeftovers(t, m.versionsDir)
}

// TestDownloadInstalledRejected 已装版本重复下载 → 既有"已安装"口径拒绝，
// 不发起任何网络请求。
func TestDownloadInstalledRejected(t *testing.T) {
	src := newExeSource(t)
	m := newChainManager(t, src)
	installViaChain(t, m, src, "v1.0.7")
	hitsAfterInstall := src.count()

	if err := m.Download(newTxnID(), "v1.0.7", nil); err == nil ||
		!strings.Contains(err.Error(), "已安装") {
		t.Fatalf("已装版本重复下载应拒绝, got %v", err)
	}
	if src.count() != hitsAfterInstall {
		t.Error("已装拦截后不得发起下载请求")
	}
}

// TestUpdateChainCoexist 更新成功链：1.0.6 → 1.0.7 双版本共存、列表最新在前、
// 逐版本 Resolve 稳定；卸载新版不动旧版目录；落位账本齐备（内核 meta.json +
// 模块 installMeta 基线账本 + 定名 exe）。
func TestUpdateChainCoexist(t *testing.T) {
	src := newExeSource(t)
	m := newChainManager(t, src)
	installViaChain(t, m, src, "v1.0.6")

	rec := &progressRecorder{}
	exeBytes := exeBody("1.0.7")
	seedRemote(t, "v1.0.7", expectedAssetName("v1.0.7"), int64(len(exeBytes)), shaHex(exeBytes))
	src.set(exeBytes, false)
	if err := m.Download(newTxnID(), "v1.0.7", rec.cb()); err != nil {
		t.Fatalf("Download(v1.0.7): %v", err)
	}

	// 进度词表零漂移：只允许既有词汇，且顺序为 downloading → verify → install → done
	for _, e := range rec.events {
		switch e.Stage {
		case "downloading", "verify", "install", "done":
		default:
			t.Fatalf("出现了词表之外的进度阶段 %q", e.Stage)
		}
	}
	if got := strings.Join(rec.stages(), ","); got != "downloading,verify,install,done" {
		t.Errorf("进度阶段序列 = %q", got)
	}

	installed, err := m.ListInstalled()
	if err != nil {
		t.Fatal(err)
	}
	if len(installed) != 2 {
		t.Fatalf("应双版本共存, got %+v", installed)
	}
	if installed[0].Version != "v1.0.7" || installed[1].Version != "v1.0.6" {
		t.Errorf("列表应最新在前, got %s, %s", installed[0].Version, installed[1].Version)
	}
	head := installed[0]
	if head.Integrity != IntegrityVerified {
		t.Errorf("刚落位的官方安装应为 verified, got %+v", head)
	}
	if head.ExePath != filepath.Join(head.Dir, exeName) {
		t.Errorf("落盘应定名 %s: %+v", exeName, head)
	}
	if head.IsImport || head.Source != expectedAssetName("v1.0.7") || head.CurrentSHA256 != shaHex(exeBytes) {
		t.Errorf("来源/哈希账目异常: %+v", head)
	}
	if head.InstalledAt == "" {
		t.Errorf("展示账目字段应齐备: %+v", head)
	}

	// 落位账本：内核统一形状 + 模块基线账本
	metaRaw, err := os.ReadFile(filepath.Join(head.Dir, "meta.json"))
	if err != nil {
		t.Fatal(err)
	}
	var meta artifact.Meta
	if err := json.Unmarshal(metaRaw, &meta); err != nil {
		t.Fatal(err)
	}
	if meta.Entry != exeName || meta.ZipSHA256 != shaHex(exeBytes) || meta.Source != artifact.SourceRemote {
		t.Errorf("内核账本内容异常: %+v", meta)
	}
	baseline, err := readMeta(filepath.Join(head.Dir, moduleMetaFileName))
	if err != nil {
		t.Fatal(err)
	}
	if baseline.Version != "v1.0.7" || !baseline.VerifiedOfficial ||
		baseline.ExpectedSHA256 != shaHex(exeBytes) || baseline.InstalledSHA256 != shaHex(exeBytes) ||
		baseline.FileVersion != "1.0.7" || baseline.ProductName != "MangoDisk" {
		t.Errorf("模块基线账异常: %+v", baseline)
	}

	// 启动前完整性闸门放行 verified
	if _, err := m.VerifyBeforeLaunch("v1.0.7"); err != nil {
		t.Errorf("verified 版本应放行启动: %v", err)
	}

	exe06, err := m.ResolveExe("v1.0.6")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Remove("v1.0.7"); err != nil {
		t.Fatalf("Remove(v1.0.7): %v", err)
	}
	if fi, serr := os.Stat(exe06); serr != nil || fi.Size() == 0 {
		t.Error("卸载新版不得动旧版目录")
	}
	if _, err := m.ResolveExe("v1.0.7"); err == nil {
		t.Error("已卸载版本应不可解析")
	}
	assertNoTransactionLeftovers(t, m.versionsDir)
}

// TestStagedResidueAbandoned 落位中途强杀等价模拟：staging 半件残留 →
// ListInstalled 不显示半件；CleanupAbandoned 收尸后目录干净。
func TestStagedResidueAbandoned(t *testing.T) {
	m := newTestManager(t)
	staging, discard, err := m.tree.StageDir("inst-kill-sim")
	if err != nil {
		t.Fatal(err)
	}
	_ = discard // 故意不调用 discard：模拟进程被强杀、中转目录留在盘上
	if err := os.WriteFile(filepath.Join(staging, exeName), exeBody("1.0.7"), 0644); err != nil {
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
