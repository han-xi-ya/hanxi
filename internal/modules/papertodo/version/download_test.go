// 下载→校验→原子换入全链的失败注入测试（markeron/rufus 金样本同构，资产形态
// 换成"双变体择一的单 exe"）：以回环 httptest 服务器为"源"，真实驱动内核
// artifact.Fetch（官方 digest 在场路径）与本包降级传输链（digest 缺席路径），
// 真 tmp 目录、无网络依赖。验证中断、坏摘要、半件残留、数据原地保留、
// 非 MZ 拒收、PE 核对不符拒装、覆盖重装语义等收口行为。
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
)

// ---------- 测试夹具：远程列表注入与假资产源 ----------

// seedRemote 注入伪远程列表（双变体齐备；资产 SHA256 传空串构造降级链场）。
// remoteCache 是包内全局缓存（生产语义即进程级单例），本包测试串行执行
// （不使用 t.Parallel），互相隔离靠 setup 重灌。
func seedRemote(t *testing.T, version string, sc, nr PaperAsset) {
	t.Helper()
	remoteCache.mu.Lock()
	defer remoteCache.mu.Unlock()
	remoteCache.data = []PaperRelease{{
		Version:       version,
		Published:     "2026-08-30T17:28:21Z",
		SelfContained: sc,
		NoRuntime:     nr,
	}}
	remoteCache.fetchedAt = time.Now() // 处于 TTL 内，Download 不会再走网络拉列表
}

// exeSource 可编程假资产源：按当前 body 提供下载，支持截断（模拟中断）与命中计数。
// 默认 body 带 MZ 头（领域断言要求）。
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
// 下载/校验/换入全链仍走生产实现）。fileVersion 默认返回错误（假 exe 无 PE
// 资源），与真机上"资源缺失降级放行"同口径；PE 核对场按需覆盖注入。
func newChainManager(t *testing.T, src *exeSource) *Manager {
	t.Helper()
	m := NewManager(t.TempDir())
	m.mirrors = func(_, _ string) []string {
		return []string{src.srv.URL + "/PaperTodo-v3.31-win-x64-self-contained.exe"}
	}
	m.fileVersion = func(string) (string, error) { return "", fmt.Errorf("no PE resource") }
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

// paperAssetOf 构造双变体列表条目：目标变体给真包，另一变体给不可用占位
// （size/digest 互异，误选变体必被摘要/字节数闸拒收）。
func paperAssetOf(name string, body []byte, digest bool) PaperAsset {
	a := PaperAsset{Name: name, Size: int64(len(body))}
	if digest {
		a.SHA256 = shaHex(body)
	}
	return a
}

// installViaChain 走真实下载链装一个变体（正例夹具复用）。
func installViaChain(t *testing.T, m *Manager, src *exeSource, version, variant, payload string) {
	t.Helper()
	exeBytes := fakeExe(payload)
	name := fmt.Sprintf("PaperTodo-%s-win-x64-%s.exe", version, variant)
	asset := paperAssetOf(name, exeBytes, true)
	other := paperAssetOf("other.exe", fakeExe("other-"+payload), true)
	sc, nr := other, asset
	if variant == VariantSelfContained {
		sc, nr = asset, other
	}
	seedRemote(t, version, sc, nr)
	src.set(exeBytes, false)
	if err := m.Download(newTxnID(), version, variant, nil); err != nil {
		t.Fatalf("Download(%s,%s): %v", version, variant, err)
	}
}

func readHanxiMeta(t *testing.T, dir string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, metaFileName))
	if err != nil {
		t.Fatal(err)
	}
	var mm map[string]any
	if err := json.Unmarshal(raw, &mm); err != nil {
		t.Fatal(err)
	}
	return mm
}

// ---------- 失败注入与领域链用例 ----------

// TestDownloadChainWritesFingerprintAndKeepsData 正例（digest 在场）：换入后
// exe 字节与指纹账目正确，便签数据原地保留，事件词表零漂移
// （downloading → verify → done），staging 无残留。
func TestDownloadChainWritesFingerprintAndKeepsData(t *testing.T) {
	src := newExeSource(t)
	m := newChainManager(t, src)

	// 预放"历史用户数据"：升级链对 data.json 必须零触碰
	if err := os.MkdirAll(m.InstallDir(), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(m.InstallDir(), dataFileName), []byte(`{"papers":["keep"]}`), 0644); err != nil {
		t.Fatal(err)
	}

	rec := &progressRecorder{}
	exeBytes := fakeExe("kernel-verified-exe")
	asset := paperAssetOf("PaperTodo-v3.31-win-x64-self-contained.exe", exeBytes, true)
	other := paperAssetOf("PaperTodo-v3.31-win-x64-no-runtime.exe", fakeExe("nr"), true)
	seedRemote(t, "v3.31", asset, other)
	src.set(exeBytes, false)
	if err := m.Download(newTxnID(), "v3.31", VariantSelfContained, rec.cb()); err != nil {
		t.Fatalf("Download: %v", err)
	}
	for _, e := range rec.events {
		switch e.Stage {
		case "downloading", "verify", "done", "error":
		default:
			t.Fatalf("出现了词表之外的进度阶段 %q", e.Stage)
		}
	}
	if got := strings.Join(rec.stages(), ","); got != "downloading,verify,done" {
		t.Errorf("进度阶段序列 = %q", got)
	}

	got, err := os.ReadFile(filepath.Join(m.InstallDir(), exeName))
	if err != nil || string(got) != string(exeBytes) {
		t.Fatalf("换入内容错误: %v %q", err, got)
	}
	meta := readHanxiMeta(t, m.InstallDir())
	if meta["tag"] != "v3.31" || meta["variant"] != VariantSelfContained || meta["isImport"] == true {
		t.Errorf("账目字段错误: %+v", meta)
	}
	if meta["verifiedHash"] != true || meta["officialSHA256"] != shaHex(exeBytes) ||
		meta["assetSHA256"] != shaHex(exeBytes) {
		t.Errorf("官方摘要在场应硬校验并双摘要入账: %+v", meta)
	}
	if meta["peChecked"] != false || meta["peVersion"] != "" {
		t.Errorf("PE 资源缺失应如实记降级: %+v", meta)
	}
	data, derr := os.ReadFile(filepath.Join(m.InstallDir(), dataFileName))
	if derr != nil || string(data) != `{"papers":["keep"]}` {
		t.Errorf("便签数据必须原地保留: %v %q", derr, data)
	}
	assertNoTransactionLeftovers(t, m.versionsDir)
}

// TestDownloadInterruptKeepsOldExe 下载中断（digest 在场，传输截断，全部候选源
// 失败）→ error 进度、旧 exe 完好、无新半件残留。
func TestDownloadInterruptKeepsOldExe(t *testing.T) {
	src := newExeSource(t)
	m := newChainManager(t, src)
	installViaChain(t, m, src, "v3.3", "self-contained", "old-exe-bytes")
	oldPath := filepath.Join(m.InstallDir(), exeName)
	oldBefore, _ := os.ReadFile(oldPath)

	newExe := fakeExe("brand-new-exe")
	seedRemote(t, "v3.31",
		paperAssetOf("PaperTodo-v3.31-win-x64-self-contained.exe", newExe, true),
		paperAssetOf("PaperTodo-v3.31-win-x64-no-runtime.exe", fakeExe("nr"), true))
	src.set(newExe, true) // 半路掐线

	rec := &progressRecorder{}
	err := m.Download(newTxnID(), "v3.31", VariantSelfContained, rec.cb())
	if err == nil {
		t.Fatal("下载中断应报错")
	}
	stages := rec.stages()
	joined := strings.Join(stages, ",")
	if !strings.Contains(joined, "error") || strings.Contains(joined, "done") {
		t.Fatalf("进度事件应含 error 且不得含 done: %v", stages)
	}
	oldAfter, _ := os.ReadFile(oldPath)
	if string(oldAfter) != string(oldBefore) || len(oldBefore) == 0 {
		t.Error("旧版本 exe 必须原样完好")
	}
	assertNoTransactionLeftovers(t, m.versionsDir)
}

// TestDownloadBadDigestRejects 摘要错（源内容与官方摘要不符）→ 拒换入且旧版完好。
func TestDownloadBadDigestRejects(t *testing.T) {
	src := newExeSource(t)
	m := newChainManager(t, src)
	installViaChain(t, m, src, "v3.3", "self-contained", "old-exe-bytes")
	oldPath := filepath.Join(m.InstallDir(), exeName)
	oldBefore, _ := os.ReadFile(oldPath)

	tampered := fakeExe("tampered-payload")
	seedRemote(t, "v3.31",
		PaperAsset{Name: "PaperTodo-v3.31-win-x64-self-contained.exe", Size: int64(len(tampered)), SHA256: shaHex([]byte("NOT-THE-REAL-DIGEST"))},
		paperAssetOf("PaperTodo-v3.31-win-x64-no-runtime.exe", fakeExe("nr"), true))
	src.set(tampered, false)

	err := m.Download(newTxnID(), "v3.31", VariantSelfContained, nil)
	if err == nil || !strings.Contains(err.Error(), "SHA256") {
		t.Fatalf("摘要不符应拒收并指明校验: %v", err)
	}
	oldAfter, _ := os.ReadFile(oldPath)
	if string(oldAfter) != string(oldBefore) || len(oldBefore) == 0 {
		t.Error("坏包不得换入，旧版本必须原样完好")
	}
	assertNoTransactionLeftovers(t, m.versionsDir)
}

// TestDownloadNonMagicRejected 摘要正确但字节以非 MZ 开头（镜像错误页伪装）→
// verify 阶段领域断言拒收，不落位。
func TestDownloadNonMagicRejected(t *testing.T) {
	src := newExeSource(t)
	m := newChainManager(t, src)
	body := []byte("<html>404 Not Found</html>")
	seedRemote(t, "v3.31",
		paperAssetOf("PaperTodo-v3.31-win-x64-self-contained.exe", body, true),
		paperAssetOf("PaperTodo-v3.31-win-x64-no-runtime.exe", fakeExe("nr"), true))
	src.set(body, false)

	err := m.Download(newTxnID(), "v3.31", VariantSelfContained, nil)
	if err == nil || !strings.Contains(err.Error(), "不是 Windows 可执行文件") {
		t.Fatalf("非 MZ 字节应被领域断言拒收, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(m.InstallDir(), exeName)); !os.IsNotExist(statErr) {
		t.Error("非 PE 字节不得落位")
	}
	assertNoTransactionLeftovers(t, m.versionsDir)
}

// TestFallbackChainNoDigest 降级链正例（上游未回刷 digest 的资产）：传输成功 +
// 字节数/MZ 闸过 + PE 资源缺失降级放行 → 安装成功，账目如实记录
// verifiedHash=false 且无 officialSHA256。
func TestFallbackChainNoDigest(t *testing.T) {
	src := newExeSource(t)
	m := newChainManager(t, src)
	body := fakeExe("no-digest-exe")
	seedRemote(t, "v3.31",
		PaperAsset{Name: "PaperTodo-v3.31-win-x64-self-contained.exe", Size: int64(len(body))}, // SHA256 空 = 降级链
		PaperAsset{Name: "PaperTodo-v3.31-win-x64-no-runtime.exe", Size: 2})
	src.set(body, false)

	if err := m.Download(newTxnID(), "v3.31", VariantSelfContained, nil); err != nil {
		t.Fatalf("降级链安装失败: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(m.InstallDir(), exeName))
	if err != nil || string(got) != string(body) {
		t.Fatalf("换入内容错误: %v %q", err, got)
	}
	meta := readHanxiMeta(t, m.InstallDir())
	if meta["verifiedHash"] != false {
		t.Errorf("无官方摘要时 verifiedHash 应如实记 false: %+v", meta)
	}
	if _, ok := meta["officialSHA256"]; ok {
		t.Errorf("无官方摘要不得造 officialSHA256 字段: %+v", meta)
	}
	if meta["assetSHA256"] != shaHex(body) {
		t.Errorf("落盘指纹必须入账: %+v", meta)
	}
	list, lerr := m.ListInstalled()
	if lerr != nil || len(list) != 1 || list[0].Version != "v3.31" || list[0].Variant != VariantSelfContained {
		t.Fatalf("ListInstalled 应见降级安装: %+v %v", list, lerr)
	}
}

// TestFallbackChainSizeMismatch 降级链字节数闸：落盘字节数与 API 声明 size
// 不符（截断代理投毒换包的最廉价反例）→ 拒换入并保留旧版。
func TestFallbackChainSizeMismatch(t *testing.T) {
	src := newExeSource(t)
	m := newChainManager(t, src)
	installViaChain(t, m, src, "v3.3", "self-contained", "old-exe-bytes")
	oldPath := filepath.Join(m.InstallDir(), exeName)
	oldBefore, _ := os.ReadFile(oldPath)

	body := fakeExe("short-body")
	seedRemote(t, "v3.31",
		PaperAsset{Name: "PaperTodo-v3.31-win-x64-self-contained.exe", Size: int64(len(body)) + 10}, // 声明与实收不符
		PaperAsset{Name: "PaperTodo-v3.31-win-x64-no-runtime.exe", Size: 2})
	src.set(body, false)

	err := m.Download(newTxnID(), "v3.31", VariantSelfContained, nil)
	if err == nil || !strings.Contains(err.Error(), "下载不完整") {
		t.Fatalf("字节数不符应拒收, got %v", err)
	}
	oldAfter, _ := os.ReadFile(oldPath)
	if string(oldAfter) != string(oldBefore) || len(oldBefore) == 0 {
		t.Error("半件不得换入，旧版本必须原样完好")
	}
	assertNoTransactionLeftovers(t, m.versionsDir)
}

// TestPeVersionMismatchRejected PE 资源可读取且与目标版本不符（拿错包）→ 拒装。
// fileVersion 接缝注入"能读出 9.9.9.9"的假 PE，验证核对链不被简化。
func TestPeVersionMismatchRejected(t *testing.T) {
	src := newExeSource(t)
	m := newChainManager(t, src)
	m.fileVersion = func(string) (string, error) { return "9.9.9.9", nil }

	body := fakeExe("mismatched-exe")
	seedRemote(t, "v3.31",
		paperAssetOf("PaperTodo-v3.31-win-x64-self-contained.exe", body, true),
		paperAssetOf("PaperTodo-v3.31-win-x64-no-runtime.exe", fakeExe("nr"), true))
	src.set(body, false)

	err := m.Download(newTxnID(), "v3.31", VariantSelfContained, nil)
	if err == nil || !strings.Contains(err.Error(), "疑似拿错包") {
		t.Fatalf("PE 版本不符应拒装, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(m.InstallDir(), exeName)); !os.IsNotExist(statErr) {
		t.Error("PE 核对不过不得落位")
	}
	assertNoTransactionLeftovers(t, m.versionsDir)
}

// TestPeVersionMatchAccepts 数值核心等值核对（"3.31.0.0" ≡ v3.31）：不误伤。
func TestPeVersionMatchAccepts(t *testing.T) {
	src := newExeSource(t)
	m := newChainManager(t, src)
	m.fileVersion = func(string) (string, error) { return "3.31.0.0", nil }

	body := fakeExe("pe-checked-exe")
	seedRemote(t, "v3.31",
		paperAssetOf("PaperTodo-v3.31-win-x64-self-contained.exe", body, true),
		paperAssetOf("PaperTodo-v3.31-win-x64-no-runtime.exe", fakeExe("nr"), true))
	src.set(body, false)

	if err := m.Download(newTxnID(), "v3.31", VariantSelfContained, nil); err != nil {
		t.Fatalf("PE 一致不应拒装: %v", err)
	}
	meta := readHanxiMeta(t, m.InstallDir())
	if meta["peChecked"] != true || meta["peVersion"] != "3.31.0.0" {
		t.Errorf("PE 核对结果应如实入账: %+v", meta)
	}
}

// TestReinstallOverwriteAllowed 覆盖安装语义（刻意区别于内核 Tree 的漂移拒覆盖）：
// 同版本异摘要（上游同 tag 重传/换镜像包）与换变体重装都必须放行——
// 单目录覆盖是本模块用户拍板的核心契约（"装此版/切换变体"）。
func TestReinstallOverwriteAllowed(t *testing.T) {
	src := newExeSource(t)
	m := newChainManager(t, src)

	scBytes := fakeExe("self-contained-v331")
	nrBytes := fakeExe("no-runtime-v331")
	sc := paperAssetOf("PaperTodo-v3.31-win-x64-self-contained.exe", scBytes, true)
	nr := paperAssetOf("PaperTodo-v3.31-win-x64-no-runtime.exe", nrBytes, true)

	seedRemote(t, "v3.31", sc, nr)
	src.set(scBytes, false)
	if err := m.Download(newTxnID(), "v3.31", VariantSelfContained, nil); err != nil {
		t.Fatalf("首装: %v", err)
	}

	// 同版本异摘要（上游同 tag 重传新包）：覆盖放行，落位为新内容
	repack := fakeExe("self-contained-v331-repacked")
	scNew := paperAssetOf("PaperTodo-v3.31-win-x64-self-contained.exe", repack, true)
	seedRemote(t, "v3.31", scNew, nr)
	src.set(repack, false)
	if err := m.Download(newTxnID(), "v3.31", VariantSelfContained, nil); err != nil {
		t.Fatalf("同版本异摘要重装应放行: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(m.InstallDir(), exeName)); string(got) != string(repack) {
		t.Errorf("重装后 exe 内容应为新包, got %q", got)
	}

	// 换变体重装：同版本不同资产（摘要天然互异），放行且变体改账
	src.set(nrBytes, false)
	if err := m.Download(newTxnID(), "v3.31", VariantNoRuntime, nil); err != nil {
		t.Fatalf("换变体重装应放行: %v", err)
	}
	meta := readHanxiMeta(t, m.InstallDir())
	if meta["variant"] != VariantNoRuntime || meta["source"] != nr.Name {
		t.Errorf("换变体后账目应更新: %+v", meta)
	}
	if got, _ := os.ReadFile(filepath.Join(m.InstallDir(), exeName)); string(got) != string(nrBytes) {
		t.Error("换变体后 exe 应为精简版包体")
	}
	assertNoTransactionLeftovers(t, m.versionsDir)
}

// TestDownloadVariantAndListGuards 入口防御：未知变体与"列表不存在"必须早退，
// 一个下载请求都不发出（先审配置，再碰网络）。
func TestDownloadVariantAndListGuards(t *testing.T) {
	src := newExeSource(t)
	m := newChainManager(t, src)
	src.set(fakeExe("x"), false)

	seedRemote(t, "v3.31",
		paperAssetOf("PaperTodo-v3.31-win-x64-self-contained.exe", fakeExe("sc"), true),
		paperAssetOf("PaperTodo-v3.31-win-x64-no-runtime.exe", fakeExe("nr"), true))
	if err := m.Download(newTxnID(), "v3.31", "win7", nil); err == nil ||
		!strings.Contains(err.Error(), "未知运行库变体") || src.count() != 0 {
		t.Fatalf("未知变体应拒绝且不发起下载: %v hits=%d", err, src.count())
	}
	if err := m.Download(newTxnID(), "v9.9.9", VariantSelfContained, nil); err == nil ||
		!strings.Contains(err.Error(), "远程列表不存在版本") || src.count() != 0 {
		t.Fatalf("列表外版本应拒绝且不发起下载: %v hits=%d", err, src.count())
	}
}

// TestDownloadEmptyTxnIDRejected 空事务 ID → Tree.StageDir 令牌白名单拒收，
// error 事件如实送达，不碰网络。
func TestDownloadEmptyTxnIDRejected(t *testing.T) {
	src := newExeSource(t)
	m := newChainManager(t, src)
	body := fakeExe("x")
	seedRemote(t, "v3.31",
		paperAssetOf("PaperTodo-v3.31-win-x64-self-contained.exe", body, true),
		paperAssetOf("PaperTodo-v3.31-win-x64-no-runtime.exe", fakeExe("nr"), true))
	src.set(body, false)

	rec := &progressRecorder{}
	if err := m.Download("", "v3.31", VariantSelfContained, rec.cb()); err == nil {
		t.Fatal("空 txnID 应拒绝")
	}
	if got := strings.Join(rec.stages(), ","); !strings.Contains(got, "error") {
		t.Errorf("stages = %q, want 含 error", got)
	}
	if hits := src.count(); hits != 0 {
		t.Errorf("staging 创建失败时不得发起下载请求, hits = %d", hits)
	}
	assertNoTransactionLeftovers(t, m.versionsDir)
}

// TestStagedResidueAbandoned 换入前强杀等价模拟：staging 半件残留 →
// ListInstalled 不受影响（读固定托管目录）；CleanupAbandoned 收尸后根目录干净。
func TestStagedResidueAbandoned(t *testing.T) {
	m := NewManager(t.TempDir())
	staging, discard, err := m.tree.StageDir("kill-sim")
	if err != nil {
		t.Fatal(err)
	}
	_ = discard // 故意不调用 discard：模拟进程被强杀、中转目录留在盘上
	if err := os.WriteFile(filepath.Join(staging, exeName), fakeExe("half"), 0644); err != nil {
		t.Fatal(err)
	}

	if list, err := m.ListInstalled(); err != nil || len(list) != 0 {
		t.Fatalf("半件 staging 不得出现在安装列表: %+v err %v", list, err)
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
		if os.IsNotExist(err) {
			return
		}
		t.Fatal(err)
	}
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") || strings.Contains(name, ".part") || strings.Contains(name, ".removing") {
			t.Errorf("版本树根残留事务/临时件: %s", name)
		}
	}
}
