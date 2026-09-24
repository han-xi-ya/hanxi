// 便携 zip 下载→校验→解包→落位全链的失败注入测试：以回环 httptest 服务器为
// "源"，真实驱动内核 artifact.Fetch（最新版官方摘要路径）与模块降级链
// （历史版无摘要：downloadTo 重试 + 字节数核对）+ UnpackZip/Tree 全链
// （真 tmp 目录，无外网依赖），验证中断、坏摘要、锚点拒装、半件残留、双版本
// 共存与账本双轨（ccswitch 同构黄金样本）。
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

// seedPortableRemote 注入伪远程列表（便携形态缓存）。两形态缓存是包内全局
// （生产语义即进程级单例），本包测试串行执行（不使用 t.Parallel），互相隔离
// 靠 setup 重灌。fetchedAt 置于 TTL 内，Download 不再走网络拉列表。
func seedPortableRemote(t *testing.T, rel Release) {
	t.Helper()
	portableCache.mu.Lock()
	defer portableCache.mu.Unlock()
	portableCache.data = []Release{rel}
	portableCache.fetchedAt = time.Now()
}

// zipSource 可编程假资产源：按当前 body 提供下载，支持截断（模拟中断）与命中计数。
type zipSource struct {
	mu       sync.Mutex
	body     []byte
	truncate bool
	hits     int
	srv      *httptest.Server
}

// newTestManager N25 G5 收编：降级链 client 已切 netx 代理链
// （env→系统代理→直连），原来的 t.Setenv("NO_PROXY") 口径失效——
// WinINET 系统代理兜底不认 NO_PROXY，且 http.ProxyFromEnvironment 按进程
// 缓存（踩坑 #35），进程内改 env 验证不了什么。照 guoheview 先例改为
// Transport 注入式回环强制直连，超时语义仍随 NewManager 生产预算。
func newTestManager(t *testing.T) *Manager {
	t.Helper()
	m := NewManager(t.TempDir())
	m.client.Transport = &http.Transport{Proxy: nil}
	return m
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

// buildVSZip 官方便携归档形状：根级 Code.exe + bin/code.cmd +
// <commit10>/resources/app/product.json 锚点。
func buildVSZip(t *testing.T, exeContent string) []byte {
	t.Helper()
	path := makeTestZip(t, map[string]string{
		exeName:        exeContent,
		"bin/code.cmd": "@echo off",
		fakeCommit1[:10] + "/resources/app/product.json": `{"nameShort":"Code"}`,
	})
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// seedAndStage 灌一条指向假源的便携版远程记录（sha 传空串 = 模拟历史版本无官方摘要）。
func seedAndStage(t *testing.T, m *Manager, src *zipSource, version string, zipBytes []byte, sha string) {
	t.Helper()
	seedPortableRemote(t, Release{
		Version:     version,
		Commit:      fakeCommit1,
		DownloadURL: src.srv.URL + "/VSCode-win32-x64-" + version + ".zip",
		Size:        int64(len(zipBytes)),
		AssetName:   "VSCode-win32-x64-" + version + ".zip",
		SHA256:      sha,
	})
	src.set(zipBytes, false)
}

// installViaChain 走真实下载链装一个最新版（带官方摘要，正例夹具复用）。
func installViaChain(t *testing.T, m *Manager, src *zipSource, version, exeContent string) {
	t.Helper()
	zipBytes := buildVSZip(t, exeContent)
	seedAndStage(t, m, src, version, zipBytes, shaHex(zipBytes))
	if err := m.Download(newTxnID(), version, FormPortable, nil); err != nil {
		t.Fatalf("Download(%s): %v", version, err)
	}
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

// ---------- 下载链用例 ----------

// TestDownloadLatestStageAndLedger 最新版正例全链：进度词表逐字
// （downloading→verify→extract→done，不发明新词）、落位目录形状 vscode_<ver>、
// data\ 激活器补齐、内核 meta.json + 模块 meta.module.json 双轨账本。
func TestDownloadLatestStageAndLedger(t *testing.T) {
	src := newZipSource(t)
	m := newTestManager(t)
	zipBytes := buildVSZip(t, "latest-exe-bytes")
	seedAndStage(t, m, src, "1.136.1", zipBytes, shaHex(zipBytes))

	rec := &progressRecorder{}
	if err := m.Download(newTxnID(), "1.136.1", FormPortable, rec.cb()); err != nil {
		t.Fatalf("Download: %v", err)
	}
	if got := strings.Join(rec.stages(), ","); got != "downloading,verify,extract,done" {
		t.Errorf("进度阶段序列 = %q，应为 downloading,verify,extract,done", got)
	}

	dir := filepath.Join(m.versionsDir, "vscode_1.136.1")
	if _, err := os.Stat(filepath.Join(dir, "Code.exe")); err != nil {
		t.Fatalf("落位异常: %v", err)
	}
	if fi, err := os.Stat(filepath.Join(dir, dataDirName)); err != nil || !fi.IsDir() {
		t.Errorf("data\\ 便携激活器必须补齐: %v", err)
	}
	metaRaw, err := os.ReadFile(filepath.Join(dir, "meta.json"))
	if err != nil {
		t.Fatal(err)
	}
	var meta artifact.Meta
	if err := json.Unmarshal(metaRaw, &meta); err != nil {
		t.Fatal(err)
	}
	if meta.Entry != exeName || meta.ZipSHA256 != shaHex(zipBytes) || meta.Source != artifact.SourceRemote {
		t.Errorf("内核账本异常: %+v", meta)
	}
	mm := readModuleMeta(dir)
	if !mm.VerifiedHash || mm.Source != "VSCode-win32-x64-1.136.1.zip" || mm.Commit != fakeCommit1 ||
		mm.ZipSize != int64(len(zipBytes)) || mm.ComputedSHA != shaHex(zipBytes) {
		t.Errorf("模块侧账本异常: %+v", mm)
	}

	installed, err := m.ListInstalled()
	if err != nil || len(installed) != 1 {
		t.Fatalf("ListInstalled: %+v %v", installed, err)
	}
	v := installed[0]
	if v.Version != "1.136.1" || !v.Verified || v.IsImport || v.Source != "VSCode-win32-x64-1.136.1.zip" {
		t.Errorf("前端契约字段异常: %+v", v)
	}
	if !strings.HasPrefix(v.InstalledAt, "20") || len(v.InstalledAt) != len("2006-01-02 15:04:05") {
		t.Errorf("installedAt 应保持 yyyy-MM-dd HH:mm:ss 展示口径: %q", v.InstalledAt)
	}
	assertNoTransactionLeftovers(t, m.versionsDir)
}

// TestDownloadNoDigestLegacyChain 历史版本无官方摘要（上游接口形态如此）：走模块
// 降级链（downloadTo + 字节数核对），如实装成 verified=false（不冒领官方校验）。
func TestDownloadNoDigestLegacyChain(t *testing.T) {
	src := newZipSource(t)
	m := newTestManager(t)
	zipBytes := buildVSZip(t, "legacy-exe-bytes")
	seedAndStage(t, m, src, "1.135.0", zipBytes, "") // 无摘要

	rec := &progressRecorder{}
	if err := m.Download(newTxnID(), "1.135.0", FormPortable, rec.cb()); err != nil {
		t.Fatalf("降级链 Download: %v", err)
	}
	if hits := src.count(); hits != 1 {
		t.Errorf("降级链应恰发一次下载请求, hits = %d", hits)
	}
	installed, err := m.ListInstalled()
	if err != nil || len(installed) != 1 {
		t.Fatalf("ListInstalled: %+v %v", installed, err)
	}
	if installed[0].Verified {
		t.Errorf("无官方摘要不得冒领校验: %+v", installed[0])
	}
	if mm := readModuleMeta(filepath.Join(m.versionsDir, "vscode_1.135.0")); mm.VerifiedHash || mm.ComputedSHA != shaHex(zipBytes) {
		t.Errorf("降级链账本异常: %+v", mm)
	}
}

// TestDownloadInterruptKeepsOldVersion 下载中断（传输截断，候选源穷尽）→
// 报 error 进度、旧版本完好、版本树无新目录与半件残留。
func TestDownloadInterruptKeepsOldVersion(t *testing.T) {
	src := newZipSource(t)
	m := newTestManager(t)
	installViaChain(t, m, src, "1.135.0", "old-exe-bytes")

	newZip := buildVSZip(t, "brand-new-exe")
	seedPortableRemote(t, Release{
		Version:     "1.136.1",
		DownloadURL: src.srv.URL + "/VSCode-win32-x64-1.136.1.zip",
		Size:        int64(len(newZip)),
		AssetName:   "VSCode-win32-x64-1.136.1.zip",
		SHA256:      shaHex(newZip),
	})
	src.set(newZip, true) // 半路掐线

	rec := &progressRecorder{}
	err := m.Download(newTxnID(), "1.136.1", FormPortable, rec.cb())
	if err == nil {
		t.Fatal("下载中断应报错")
	}
	stages := rec.stages()
	joined := strings.Join(stages, ",")
	if !strings.Contains(joined, "error") || strings.Contains(joined, "done") {
		t.Fatalf("进度事件应含 error 且不得含 done: %v", stages)
	}
	if _, statErr := os.Stat(filepath.Join(m.versionsDir, "vscode_1.136.1")); !os.IsNotExist(statErr) {
		t.Error("新半件目录不得出现")
	}
	assertNoTransactionLeftovers(t, m.versionsDir)
	installed, lerr := m.ListInstalled()
	if lerr != nil || len(installed) != 1 || installed[0].Version != "1.135.0" {
		t.Fatalf("旧版本应完好可用, got %+v err %v", installed, lerr)
	}
}

// TestDownloadBadDigestRejects 摘要错（源内容与官方摘要不符）→ 不落位且旧版完好。
func TestDownloadBadDigestRejects(t *testing.T) {
	src := newZipSource(t)
	m := newTestManager(t)
	installViaChain(t, m, src, "1.135.0", "old-exe-bytes")
	oldExeHashBefore := fileSHA256(filepath.Join(m.versionsDir, "vscode_1.135.0", exeName))

	newZip := buildVSZip(t, "tampered-payload")
	seedAndStage(t, m, src, "1.136.1", newZip, shaHex([]byte("NOT-THE-REAL-DIGEST")))

	err := m.Download(newTxnID(), "1.136.1", FormPortable, nil)
	if err == nil {
		t.Fatal("摘要不符应拒收")
	}
	if !strings.Contains(err.Error(), "SHA256") {
		t.Errorf("错误应指明摘要校验失败: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(m.versionsDir, "vscode_1.136.1")); !os.IsNotExist(statErr) {
		t.Error("坏包不得落位")
	}
	assertNoTransactionLeftovers(t, m.versionsDir)
	oldExeHashAfter := fileSHA256(filepath.Join(m.versionsDir, "vscode_1.135.0", exeName))
	if oldExeHashBefore != oldExeHashAfter || oldExeHashBefore == "" {
		t.Error("旧版本目录必须原样完好")
	}
}

// TestZipLayoutRejectedKeepsTree 便携锚点不符的包：解包进 staging 后自检拒装，
// 最终目录不出现、半件不残留（缺 bin/code.cmd 与缺 product.json 两个锚点场）。
func TestZipLayoutRejectedKeepsTree(t *testing.T) {
	cases := map[string]map[string]string{
		"缺 bin/code.cmd": {exeName: "fake-exe", "x/resources/app/product.json": "{}"},
		"缺 product.json": {exeName: "fake-exe", "bin/code.cmd": "@"},
	}
	for name, entries := range cases {
		zipPath := makeTestZip(t, entries)
		zipBytes, err := os.ReadFile(zipPath)
		if err != nil {
			t.Fatal(err)
		}
		src := newZipSource(t)
		m := newTestManager(t)
		seedAndStage(t, m, src, "1.136.1", zipBytes, shaHex(zipBytes))

		err = m.Download(newTxnID(), "1.136.1", FormPortable, nil)
		if err == nil || !strings.Contains(err.Error(), "zip 布局无效") {
			t.Fatalf("%s: 锚点不符应拒装, got %v", name, err)
		}
		if _, statErr := os.Stat(filepath.Join(m.versionsDir, "vscode_1.136.1")); !os.IsNotExist(statErr) {
			t.Errorf("%s: 锚点不符的包不得落位", name)
		}
		assertNoTransactionLeftovers(t, m.versionsDir)
	}
}

// TestReinstallSamePackageIdempotent 同版本同包摘要重复落位 → 内核幂等复用不报错；
// 同版本异摘要 → 拒绝覆盖防漂移。（zip 条目顺序随 map 迭代变化，重装复用首次
// 落位的同一份字节，摘要才可比对。）
func TestReinstallSamePackageIdempotent(t *testing.T) {
	src := newZipSource(t)
	m := newTestManager(t)
	zipBytes := buildVSZip(t, "same-exe")
	seedAndStage(t, m, src, "1.136.1", zipBytes, shaHex(zipBytes))
	if err := m.Download(newTxnID(), "1.136.1", FormPortable, nil); err != nil {
		t.Fatalf("首装: %v", err)
	}
	if err := m.Download(newTxnID(), "1.136.1", FormPortable, nil); err != nil {
		t.Fatalf("同摘要重装应幂等: %v", err)
	}

	// 同版本号换内容（异摘要）：模拟上游同 tag 重传，必须拒绝
	tampered := buildVSZip(t, "same-exe-tampered")
	seedAndStage(t, m, src, "1.136.1", tampered, shaHex(tampered))
	err := m.Download(newTxnID(), "1.136.1", FormPortable, nil)
	if err == nil || !strings.Contains(err.Error(), "漂移") {
		t.Fatalf("同版本异摘要应拒绝覆盖, got %v", err)
	}
}

// TestUpdateChainCoexist 更新成功链：1.135.0 → 1.136.1 双版本共存（便携多版本
// 并存是 VS Code 托管核心卖点）、列表最新在前、卸载新版不动旧版。
func TestUpdateChainCoexist(t *testing.T) {
	src := newZipSource(t)
	m := newTestManager(t)
	installViaChain(t, m, src, "1.135.0", "exe-1.135.0")
	installViaChain(t, m, src, "1.136.1", "exe-1.136.1")

	installed, err := m.ListInstalled()
	if err != nil {
		t.Fatal(err)
	}
	if len(installed) != 2 || installed[0].Version != "1.136.1" || installed[1].Version != "1.135.0" {
		t.Fatalf("应双版本共存且最新在前, got %+v", installed)
	}

	exeOld, err := m.ResolveExe("1.135.0")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Remove("1.136.1"); err != nil {
		t.Fatalf("Remove(1.136.1): %v", err)
	}
	if fi, serr := os.Stat(exeOld); serr != nil || fi.Size() == 0 {
		t.Error("卸载新版不得动旧版目录")
	}
	if _, err := m.ResolveExe("1.136.1"); err == nil {
		t.Error("已卸载版本应不可解析")
	}
}

// TestStagedResidueAbandoned 落位中途强杀等价模拟：staging 半件残留 →
// Versions 不显示半件；CleanupAbandoned 收尸后目录干净。
func TestStagedResidueAbandoned(t *testing.T) {
	m := newTestManager(t)
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
