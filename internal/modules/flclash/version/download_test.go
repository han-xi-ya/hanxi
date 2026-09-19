// 下载→校验→解包→落位全链的失败注入测试：以回环 httptest 服务器为"源"，
// 真实驱动内核 artifact.Fetch/UnpackZip/Tree 路径（真 tmp 目录，无网络依赖），
// 验证中断、坏摘要、半件残留、双版本共存等收口行为（markeron/ccswitch 同构黄金样本）。
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
// FlClash 的官方摘要直接携带在 FlClashRelease.SHA256（远程解析层已剥前缀）。
func seedRemote(t *testing.T, version, assetName string, size int64, sha string) {
	t.Helper()
	remoteCache.mu.Lock()
	defer remoteCache.mu.Unlock()
	remoteCache.data = []FlClashRelease{{
		Version:   version,
		Tag:       "v" + version,
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
// 下载/校验/解包/落位全链仍走生产实现 artifact.Fetch/UnpackZip/Tree）。
func newChainManager(t *testing.T, src *zipSource) *Manager {
	t.Helper()
	m := NewManager(t.TempDir())
	m.mirrors = func(_, _ string) []string {
		return []string{src.srv.URL + "/FlClash-Windows-Portable.zip"}
	}
	return m
}

// buildFlClashZip 官方便携 zip 形状：FlClash.exe + flutter_windows.dll + data/
// 全套（保布局平铺，锚点只认 exe）。
func buildFlClashZip(t *testing.T, exeContent string) []byte {
	t.Helper()
	path := makeTestZip(t, map[string]string{
		exeName:               exeContent,
		"flutter_windows.dll": "fake-dll",
		"data/flutter_assets": "assets",
		"README.md":           "FlClash",
	})
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func assetNameFor(version string) string {
	return "FlClash-" + version + "-windows-amd64.zip"
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
func installViaChain(t *testing.T, m *Manager, src *zipSource, version, exeContent string) {
	t.Helper()
	zipBytes := buildFlClashZip(t, exeContent)
	seedRemote(t, version, assetNameFor(version), int64(len(zipBytes)), shaHex(zipBytes))
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
	m := newChainManager(t, src)
	installViaChain(t, m, src, "0.8.95", "old-exe-bytes")

	newZip := buildFlClashZip(t, "brand-new-exe")
	seedRemote(t, "0.8.96", assetNameFor("0.8.96"), int64(len(newZip)), shaHex(newZip))
	src.set(newZip, true) // 半路掐线

	rec := &progressRecorder{}
	err := m.Download(newTxnID(), "0.8.96", rec.cb())
	if err == nil {
		t.Fatal("下载中断应报错")
	}
	stages := rec.stages()
	if !strings.Contains(strings.Join(stages, ","), "error") || strings.Contains(strings.Join(stages, ","), "done") {
		t.Fatalf("进度事件应含 error 且不得含 done: %v", stages)
	}
	if _, statErr := os.Stat(filepath.Join(m.versionsDir, "flclash_0.8.96")); !os.IsNotExist(statErr) {
		t.Error("新半件目录不得出现")
	}
	assertNoTransactionLeftovers(t, m.versionsDir)
	installed, lerr := m.ListInstalled()
	if lerr != nil || len(installed) != 1 || installed[0].Version != "0.8.95" {
		t.Fatalf("旧版本应完好可用, got %+v err %v", installed, lerr)
	}
}

// TestDownloadBadDigestRejects 摘要错（源内容与官方摘要不符）→ 不落位且旧版完好。
func TestDownloadBadDigestRejects(t *testing.T) {
	src := newZipSource(t)
	m := newChainManager(t, src)
	installViaChain(t, m, src, "0.8.95", "old-exe-bytes")
	oldExeHashBefore, _ := fileSHA256(filepath.Join(m.versionsDir, "flclash_0.8.95", exeName))

	newZip := buildFlClashZip(t, "tampered-payload")
	seedRemote(t, "0.8.96", assetNameFor("0.8.96"), int64(len(newZip)),
		shaHex([]byte("NOT-THE-REAL-DIGEST"))) // 官方摘要对不上任何传输字节
	src.set(newZip, false)

	err := m.Download(newTxnID(), "0.8.96", nil)
	if err == nil {
		t.Fatal("摘要不符应拒收")
	}
	if !strings.Contains(err.Error(), "SHA256") {
		t.Errorf("错误应指明摘要校验失败: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(m.versionsDir, "flclash_0.8.96")); !os.IsNotExist(statErr) {
		t.Error("坏包不得落位")
	}
	assertNoTransactionLeftovers(t, m.versionsDir)
	oldExeHashAfter, _ := fileSHA256(filepath.Join(m.versionsDir, "flclash_0.8.95", exeName))
	if oldExeHashBefore != oldExeHashAfter || oldExeHashBefore == "" {
		t.Error("旧版本目录必须原样完好")
	}
}

// TestDownloadMissingDigestRefusesUnverified 上游无官方摘要 → 拒无校验安装，
// 且一个下载请求都不发出（先审配置，再碰网络）。
func TestDownloadMissingDigestRefusesUnverified(t *testing.T) {
	src := newZipSource(t)
	m := newChainManager(t, src)
	zipBytes := buildFlClashZip(t, "x")
	seedRemote(t, "0.8.96", assetNameFor("0.8.96"), int64(len(zipBytes)), "")
	src.set(zipBytes, false)

	err := m.Download(newTxnID(), "0.8.96", nil)
	if err == nil || !strings.Contains(err.Error(), "拒绝无校验安装") {
		t.Fatalf("应拒绝无校验安装, got %v", err)
	}
	if hits := src.count(); hits != 0 {
		t.Errorf("摘要缺失时不得发起下载请求, hits = %d", hits)
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

// TestUpdateChainCoexist 更新成功链：0.8.95 → 0.8.96 双版本共存、列表最新在前、
// 逐版本 Resolve 稳定；卸载新版不动旧版目录。
func TestUpdateChainCoexist(t *testing.T) {
	src := newZipSource(t)
	m := newChainManager(t, src)
	installViaChain(t, m, src, "0.8.95", "exe-0.8.95")

	rec := &progressRecorder{}
	zipBytes := buildFlClashZip(t, "exe-0.8.96")
	seedRemote(t, "0.8.96", assetNameFor("0.8.96"), int64(len(zipBytes)), shaHex(zipBytes))
	src.set(zipBytes, false)
	if err := m.Download(newTxnID(), "0.8.96", rec.cb()); err != nil {
		t.Fatalf("Download(0.8.96): %v", err)
	}

	// 进度词表零漂移：只允许既有词汇，且顺序为 downloading → extract → done
	// （verify 由内核折进 download，不造幻影步骤）
	for _, e := range rec.events {
		switch e.Stage {
		case "downloading", "extract", "done":
		default:
			t.Fatalf("出现了词表之外的进度阶段 %q", e.Stage)
		}
	}
	if got := strings.Join(rec.stages(), ","); got != "downloading,extract,done" {
		t.Errorf("进度阶段序列 = %q", got)
	}

	installed, err := m.ListInstalled()
	if err != nil {
		t.Fatal(err)
	}
	if len(installed) != 2 {
		t.Fatalf("应双版本共存, got %+v", installed)
	}
	if installed[0].Version != "0.8.96" || installed[1].Version != "0.8.95" {
		t.Errorf("列表应最新在前, got %s, %s", installed[0].Version, installed[1].Version)
	}
	if installed[0].InstalledAt == "" || installed[0].IsImport {
		t.Errorf("展示账目字段应齐备且非导入: %+v", installed[0])
	}
	if !strings.HasPrefix(installed[0].InstalledAt, "20") || len(installed[0].InstalledAt) != len("2006-01-02 15:04:05") {
		t.Errorf("installedAt 应保持 yyyy-MM-dd HH:mm:ss 展示口径: %q", installed[0].InstalledAt)
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
	if meta.Entry != exeName || meta.ZipSHA256 != shaHex(zipBytes) || meta.Source != artifact.SourceRemote {
		t.Errorf("账本内容异常: %+v", meta)
	}

	exe3095, err := m.ResolveExe("0.8.95")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Remove("0.8.96"); err != nil {
		t.Fatalf("Remove(0.8.96): %v", err)
	}
	if fi, serr := os.Stat(exe3095); serr != nil || fi.Size() == 0 {
		t.Error("卸载新版不得动旧版目录")
	}
	if _, err := m.ResolveExe("0.8.96"); err == nil {
		t.Error("已卸载版本应不可解析")
	}
}

// TestReinstallSamePackageIdempotent 同版本同包摘要重复落位 → 内核幂等复用不报错；
// 同版本异摘要 → 拒绝覆盖防漂移。（zip 条目顺序随 map 迭代变化，重装复用首次落位的
// 同一份字节，摘要才可比对。）
func TestReinstallSamePackageIdempotent(t *testing.T) {
	src := newZipSource(t)
	m := newChainManager(t, src)
	zipBytes := buildFlClashZip(t, "same-exe")
	seedRemote(t, "0.8.96", assetNameFor("0.8.96"), int64(len(zipBytes)), shaHex(zipBytes))
	src.set(zipBytes, false)
	if err := m.Download(newTxnID(), "0.8.96", nil); err != nil {
		t.Fatalf("首装: %v", err)
	}
	if err := m.Download(newTxnID(), "0.8.96", nil); err != nil {
		t.Fatalf("同摘要重装应幂等: %v", err)
	}

	// 同版本号换内容（异摘要）：模拟上游同 tag 重传，必须拒绝
	tampered := buildFlClashZip(t, "same-exe-tampered")
	seedRemote(t, "0.8.96", assetNameFor("0.8.96"), int64(len(tampered)), shaHex(tampered))
	src.set(tampered, false)
	err := m.Download(newTxnID(), "0.8.96", nil)
	if err == nil || !strings.Contains(err.Error(), "漂移") {
		t.Fatalf("同版本异摘要应拒绝覆盖, got %v", err)
	}
}

// TestZipLayoutRejectedKeepsTree 锚点不符的包（缺 FlClash.exe）：解包进
// staging 后自检拒装，最终目录不出现、半件不残留。
func TestZipLayoutRejectedKeepsTree(t *testing.T) {
	src := newZipSource(t)
	m := newChainManager(t, src)

	noExe := makeTestZip(t, map[string]string{"flutter_windows.dll": "fake-dll"}) // 缺锚点
	zipBytes, err := os.ReadFile(noExe)
	if err != nil {
		t.Fatal(err)
	}
	seedRemote(t, "0.8.96", assetNameFor("0.8.96"), int64(len(zipBytes)), shaHex(zipBytes))
	src.set(zipBytes, false)

	err = m.Download(newTxnID(), "0.8.96", nil)
	if err == nil || !strings.Contains(err.Error(), "缺少可用的 FlClash.exe") {
		t.Fatalf("缺锚点应拒装, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(m.versionsDir, "flclash_0.8.96")); !os.IsNotExist(statErr) {
		t.Error("锚点不符的包不得落位")
	}
	assertNoTransactionLeftovers(t, m.versionsDir)
}

// TestDownloadUsesTagForMirrorPath 镜像拼接使用 release 自带 tag（v0.8.96 与
// 资产版本同形是上游契约；接缝参数收 tag 防回归为收 tag 传成 version 丢 v）。
func TestDownloadUsesTagForMirrorPath(t *testing.T) {
	src := newZipSource(t)
	m := newChainManager(t, src)
	var gotTag, gotAsset string
	m.mirrors = func(tag, assetName string) []string {
		gotTag, gotAsset = tag, assetName
		return []string{src.srv.URL + "/x.zip"}
	}
	zipBytes := buildFlClashZip(t, "exe")
	seedRemote(t, "0.8.96", assetNameFor("0.8.96"), int64(len(zipBytes)), shaHex(zipBytes))
	src.set(zipBytes, false)
	if err := m.Download(newTxnID(), "0.8.96", nil); err != nil {
		t.Fatalf("Download: %v", err)
	}
	if gotTag != "v0.8.96" || gotAsset != assetNameFor("0.8.96") {
		t.Errorf("镜像接缝应收到 tag=v0.8.96 asset=%s, got tag=%q asset=%q",
			assetNameFor("0.8.96"), gotTag, gotAsset)
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
