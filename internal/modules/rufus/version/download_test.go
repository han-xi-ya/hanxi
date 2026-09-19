// 下载→校验→落位全链的失败注入测试（markeron 金样本同构，资产形态换成
// 单文件 exe）：以回环 httptest 服务器为"源"，真实驱动内核
// artifact.Fetch/Tree 路径（真 tmp 目录，无网络依赖），验证中断、坏摘要、
// 半件残留、双版本共存、非 MZ 拒收等收口行为。
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

// seedRemote 注入伪远程列表（RufusRelease 自带官方摘要，无独立 digests 表）。
// remoteCache 是包内全局缓存（生产语义即进程级单例），本包测试串行执行
// （不使用 t.Parallel），互相隔离靠 setup 重灌。
func seedRemote(t *testing.T, version, assetName string, size int64, digest string) {
	t.Helper()
	remoteCache.mu.Lock()
	defer remoteCache.mu.Unlock()
	remoteCache.data = []RufusRelease{{
		Version:   version,
		AssetName: assetName,
		AssetURL:  "http://127.0.0.1:0/placeholder",
		Size:      size,
		SHA256:    digest,
	}}
	remoteCache.fetchedAt = time.Now() // 处于 TTL 内，Download 不会再走网络拉列表
}

// exeSource 可编程假资产源：按当前 body 提供下载，支持截断（模拟中断）与命中计数。
// 默认 body 带 MZ 头（领域断言要求）；bodyRaw 供构造"合法摘要 + 非法形态"样本。
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

// newChainManager 构造指向假源的 Manager（仅镜像 URL 接缝改指回环 http，
// 下载/校验/落位全链仍走生产实现 artifact.Fetch/Tree）。
func newChainManager(t *testing.T, src *exeSource) *Manager {
	t.Helper()
	m := NewManager(t.TempDir())
	m.mirrors = func(_, _ string) []string {
		return []string{src.srv.URL + "/rufus-4.15p.exe"}
	}
	return m
}

// fakeExe 构造带 MZ 头的伪 exe 字节。
func fakeExe(payload string) []byte { return []byte("MZ" + payload) }

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
func installViaChain(t *testing.T, m *Manager, src *exeSource, version, payload string) {
	t.Helper()
	exeBytes := fakeExe(payload)
	seedRemote(t, version, "rufus-"+strings.TrimPrefix(version, "v")+"p.exe",
		int64(len(exeBytes)), shaHex(exeBytes))
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
	installViaChain(t, m, src, "v4.9", "old-exe-bytes")

	newExe := fakeExe("brand-new-exe")
	seedRemote(t, "v4.15", "rufus-4.15p.exe", int64(len(newExe)), shaHex(newExe))
	src.set(newExe, true) // 半路掐线

	rec := &progressRecorder{}
	err := m.Download(newTxnID(), "v4.15", rec.cb())
	if err == nil {
		t.Fatal("下载中断应报错")
	}
	stages := rec.stages()
	joined := strings.Join(stages, ",")
	if !strings.Contains(joined, "error") || strings.Contains(joined, "done") {
		t.Fatalf("进度事件应含 error 且不得含 done: %v", stages)
	}
	if _, statErr := os.Stat(filepath.Join(m.versionsDir, "rufus_4.15")); !os.IsNotExist(statErr) {
		t.Error("新半件目录不得出现")
	}
	assertNoTransactionLeftovers(t, m.versionsDir)
	installed, lerr := m.ListInstalled()
	if lerr != nil || len(installed) != 1 || installed[0].Version != "v4.9" {
		t.Fatalf("旧版本应完好可用, got %+v err %v", installed, lerr)
	}
}

// TestDownloadBadDigestRejects 摘要错（源内容与官方摘要不符）→ 不落位且旧版完好。
func TestDownloadBadDigestRejects(t *testing.T) {
	src := newExeSource(t)
	m := newChainManager(t, src)
	installViaChain(t, m, src, "v4.9", "old-exe-bytes")
	oldExeHashBefore := fileSHA256(filepath.Join(m.versionsDir, "rufus_4.9", exeName))

	newExe := fakeExe("tampered-payload")
	seedRemote(t, "v4.15", "rufus-4.15p.exe", int64(len(newExe)), shaHex([]byte("NOT-THE-REAL-DIGEST")))
	src.set(newExe, false)

	err := m.Download(newTxnID(), "v4.15", nil)
	if err == nil {
		t.Fatal("摘要不符应拒收")
	}
	if !strings.Contains(err.Error(), "SHA256") {
		t.Errorf("错误应指明摘要校验失败: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(m.versionsDir, "rufus_4.15")); !os.IsNotExist(statErr) {
		t.Error("坏包不得落位")
	}
	assertNoTransactionLeftovers(t, m.versionsDir)
	oldExeHashAfter := fileSHA256(filepath.Join(m.versionsDir, "rufus_4.9", exeName))
	if oldExeHashBefore != oldExeHashAfter || oldExeHashBefore == "" {
		t.Error("旧版本目录必须原样完好")
	}
}

// TestDownloadMissingDigestRefusesUnverified 缓存里无官方摘要 → 拒无校验安装，
// 且一个下载请求都不发出（先审配置，再碰网络）。
func TestDownloadMissingDigestRefusesUnverified(t *testing.T) {
	src := newExeSource(t)
	m := newChainManager(t, src)
	seedRemote(t, "v4.15", "rufus-4.15p.exe", 10, "")
	src.set(fakeExe("x"), false)

	err := m.Download(newTxnID(), "v4.15", nil)
	if err == nil || !strings.Contains(err.Error(), "拒绝无校验安装") {
		t.Fatalf("应拒绝无校验安装, got %v", err)
	}
	if hits := src.count(); hits != 0 {
		t.Errorf("摘要缺失时不得发起下载请求, hits = %d", hits)
	}
}

// TestDownloadNonMagicRejected 摘要正确但字节以非 MZ 开头（镜像错误页伪装）→
// verify 阶段领域断言拒收，不落位。
func TestDownloadNonMagicRejected(t *testing.T) {
	src := newExeSource(t)
	m := newChainManager(t, src)
	body := []byte("<html>404 Not Found</html>")
	seedRemote(t, "v4.15", "rufus-4.15p.exe", int64(len(body)), shaHex(body))
	src.set(body, false)

	rec := &progressRecorder{}
	err := m.Download(newTxnID(), "v4.15", rec.cb())
	if err == nil || !strings.Contains(err.Error(), "Windows 可执行体") {
		t.Fatalf("非 MZ 字节应被领域断言拒收, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(m.versionsDir, "rufus_4.15")); !os.IsNotExist(statErr) {
		t.Error("非 PE 字节不得落位")
	}
	assertNoTransactionLeftovers(t, m.versionsDir)
}

// TestUpdateChainCoexist 更新成功链：4.9 → 4.15 双版本共存、列表最新在前、
// 逐版本 Resolve 稳定；卸载新版不动旧版目录；落位账本三件套齐备
// （内核 meta.json + 模块来源账 + 便携 rufus.ini）。
func TestUpdateChainCoexist(t *testing.T) {
	src := newExeSource(t)
	m := newChainManager(t, src)
	installViaChain(t, m, src, "v4.9", "exe-4.9")

	rec := &progressRecorder{}
	exeBytes := fakeExe("exe-4.15")
	seedRemote(t, "v4.15", "rufus-4.15p.exe", int64(len(exeBytes)), shaHex(exeBytes))
	src.set(exeBytes, false)
	if err := m.Download(newTxnID(), "v4.15", rec.cb()); err != nil {
		t.Fatalf("Download(v4.15): %v", err)
	}

	// 进度词表零漂移：只允许既有词汇，且顺序为 resolve → downloading → verify → install → done
	for _, e := range rec.events {
		switch e.Stage {
		case "resolve", "downloading", "verify", "install", "done":
		default:
			t.Fatalf("出现了词表之外的进度阶段 %q", e.Stage)
		}
	}
	if got := strings.Join(rec.stages(), ","); got != "resolve,downloading,verify,install,done" {
		t.Errorf("进度阶段序列 = %q", got)
	}

	installed, err := m.ListInstalled()
	if err != nil {
		t.Fatal(err)
	}
	if len(installed) != 2 {
		t.Fatalf("应双版本共存, got %+v", installed)
	}
	if installed[0].Version != "v4.15" || installed[1].Version != "v4.9" {
		t.Errorf("列表应最新在前, got %s, %s", installed[0].Version, installed[1].Version)
	}
	if installed[0].InstalledAt == "" {
		t.Errorf("展示账目字段应齐备: %+v", installed[0])
	}
	if installed[0].IsImport {
		t.Error("远程安装不应标导入")
	}
	if installed[0].Source != "rufus-4.15p.exe" {
		t.Errorf("远程安装的 Source 应记资产名（历史口径）: %+v", installed[0])
	}

	// 落位账本：内核统一形状 + 模块来源账 + 便携开关播种
	metaRaw, err := os.ReadFile(filepath.Join(installed[0].Dir, "meta.json"))
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
	if mm := readModuleMeta(installed[0].Dir); mm.Source != "rufus-4.15p.exe" || mm.IsImport {
		t.Errorf("模块来源账异常: %+v", mm)
	}
	if _, err := os.Stat(filepath.Join(installed[0].Dir, iniFileName)); err != nil {
		t.Errorf("安装应播种 %s 便携开关: %v", iniFileName, err)
	}

	exe09, err := m.ResolveExe("v4.9")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Remove("v4.15"); err != nil {
		t.Fatalf("Remove(v4.15): %v", err)
	}
	if fi, serr := os.Stat(exe09); serr != nil || fi.Size() == 0 {
		t.Error("卸载新版不得动旧版目录")
	}
	if _, err := m.ResolveExe("v4.15"); err == nil {
		t.Error("已卸载版本应不可解析")
	}
}

// TestReinstallSamePackageIdempotent 同版本同摘要重复落位 → 内核幂等复用不报错；
// 同版本异摘要 → 拒绝覆盖防漂移。
func TestReinstallSamePackageIdempotent(t *testing.T) {
	src := newExeSource(t)
	m := newChainManager(t, src)
	exeBytes := fakeExe("same-exe")
	seedRemote(t, "v4.9", "rufus-4.9p.exe", int64(len(exeBytes)), shaHex(exeBytes))
	src.set(exeBytes, false)
	if err := m.Download(newTxnID(), "v4.9", nil); err != nil {
		t.Fatalf("首装: %v", err)
	}
	if err := m.Download(newTxnID(), "v4.9", nil); err != nil {
		t.Fatalf("同摘要重装应幂等: %v", err)
	}

	// 同版本号换内容（异摘要）：模拟上游同 tag 重传，必须拒绝
	tampered := fakeExe("same-exe-tampered")
	seedRemote(t, "v4.9", "rufus-4.9p.exe", int64(len(tampered)), shaHex(tampered))
	src.set(tampered, false)
	err := m.Download(newTxnID(), "v4.9", nil)
	if err == nil || !strings.Contains(err.Error(), "漂移") {
		t.Fatalf("同版本异摘要应拒绝覆盖, got %v", err)
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
	if err := os.WriteFile(filepath.Join(staging, exeName), fakeExe("half"), 0644); err != nil {
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
