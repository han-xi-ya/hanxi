// 下载→校验→解包→布局吸收/锚点自检/PE 核对→落位全链的失败注入测试：以回环
// httptest 服务器为"源"，真实驱动内核 artifact.Fetch/UnpackZip/Tree 路径
// （真 tmp 目录，无网络依赖），验证中断、坏摘要、半件残留、双版本共存、
// PE 核账拒装与嵌套布局吸收等收口行为（ccswitch/markeron 同构黄金样本，
// zip 形状为 LiteMonitor 官方单层包装布局）。
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
// LiteMonitor 的官方摘要直接携带在 LMRelease.SHA256（远程解析层已剥前缀）。
func seedRemote(t *testing.T, version, assetName string, size int64, sha string) {
	t.Helper()
	remoteCache.mu.Lock()
	defer remoteCache.mu.Unlock()
	remoteCache.data = []LMRelease{{
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

// newChainManager 构造指向假源的 Manager：仅镜像 URL 接缝改指回环 http，
// fileVersion 接缝改为"exe 内容即裸版本号（X.Y.Z 三段+四段 .0 归一）"的假 PE
// 账目（真机 versioninfo 对假包必然失败），下载/校验/解包/落位全链仍走生产实现
// artifact.Fetch/UnpackZip/Tree。
func newChainManager(t *testing.T, src *zipSource) *Manager {
	t.Helper()
	m := NewManager(t.TempDir())
	m.mirrors = func(_, _ string) []string {
		return []string{src.srv.URL + "/LiteMonitor-win-x64.zip"}
	}
	m.fileVersion = func(path string) (string, error) {
		b, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(b)) + ".0", nil
	}
	return m
}

// buildLMZip 官方 zip 真实形状：单层包装目录 + exe（内容=裸版本号，供假 PE
// 核对）+ 语言包锚点 + 主题文件。
func buildLMZip(t *testing.T, token string) []byte {
	t.Helper()
	names, contents := lmZipEntries("LiteMonitor_v" + token + "-win-x64")
	contents[names[0]] = token
	path := makeTestZip(t, names, contents)
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
func installViaChain(t *testing.T, m *Manager, src *zipSource, version string) {
	t.Helper()
	token := strings.TrimPrefix(version, "v")
	zipBytes := buildLMZip(t, token)
	seedRemote(t, version, "LiteMonitor_v"+token+"-win-x64.zip", int64(len(zipBytes)), shaHex(zipBytes))
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
	installViaChain(t, m, src, "v1.3.5")

	newZip := buildLMZip(t, "1.3.6")
	seedRemote(t, "v1.3.6", "LiteMonitor_v1.3.6-win-x64.zip", int64(len(newZip)), shaHex(newZip))
	src.set(newZip, true) // 半路掐线

	rec := &progressRecorder{}
	err := m.Download(newTxnID(), "v1.3.6", rec.cb())
	if err == nil {
		t.Fatal("下载中断应报错")
	}
	stages := rec.stages()
	if !strings.Contains(strings.Join(stages, ","), "error") || strings.Contains(strings.Join(stages, ","), "done") {
		t.Fatalf("进度事件应含 error 且不得含 done: %v", stages)
	}
	if _, statErr := os.Stat(filepath.Join(m.versionsDir, "litemonitor_1.3.6")); !os.IsNotExist(statErr) {
		t.Error("新半件目录不得出现")
	}
	assertNoTransactionLeftovers(t, m.versionsDir)
	installed, lerr := m.ListInstalled()
	if lerr != nil || len(installed) != 1 || installed[0].Version != "v1.3.5" {
		t.Fatalf("旧版本应完好可用, got %+v err %v", installed, lerr)
	}
}

// TestDownloadBadDigestRejects 摘要错（源内容与官方摘要不符）→ 不落位且旧版完好。
func TestDownloadBadDigestRejects(t *testing.T) {
	src := newZipSource(t)
	m := newChainManager(t, src)
	installViaChain(t, m, src, "v1.3.5")
	oldExeHashBefore, _ := fileSHA256(filepath.Join(m.versionsDir, "litemonitor_1.3.5", exeName))

	newZip := buildLMZip(t, "1.3.6")
	seedRemote(t, "v1.3.6", "LiteMonitor_v1.3.6-win-x64.zip", int64(len(newZip)),
		shaHex([]byte("NOT-THE-REAL-DIGEST"))) // 官方摘要对不上任何传输字节
	src.set(newZip, false)

	err := m.Download(newTxnID(), "v1.3.6", nil)
	if err == nil {
		t.Fatal("摘要不符应拒收")
	}
	if !strings.Contains(err.Error(), "SHA256") {
		t.Errorf("错误应指明摘要校验失败: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(m.versionsDir, "litemonitor_1.3.6")); !os.IsNotExist(statErr) {
		t.Error("坏包不得落位")
	}
	assertNoTransactionLeftovers(t, m.versionsDir)
	oldExeHashAfter, _ := fileSHA256(filepath.Join(m.versionsDir, "litemonitor_1.3.5", exeName))
	if oldExeHashBefore != oldExeHashAfter || oldExeHashBefore == "" {
		t.Error("旧版本目录必须原样完好")
	}
}

// TestDownloadMissingDigestRefusesUnverified 上游无官方摘要 → 拒无校验安装，
// 且一个下载请求都不发出（先审配置，再碰网络）。
func TestDownloadMissingDigestRefusesUnverified(t *testing.T) {
	src := newZipSource(t)
	m := newChainManager(t, src)
	zipBytes := buildLMZip(t, "1.3.6")
	seedRemote(t, "v1.3.6", "LiteMonitor_v1.3.6-win-x64.zip", int64(len(zipBytes)), "")
	src.set(zipBytes, false)

	err := m.Download(newTxnID(), "v1.3.6", nil)
	if err == nil || !strings.Contains(err.Error(), "拒绝无校验安装") {
		t.Fatalf("应拒绝无校验安装, got %v", err)
	}
	if hits := src.count(); hits != 0 {
		t.Errorf("摘要缺失时不得发起下载请求, hits = %d", hits)
	}
}

// TestDownloadPEVersionMismatchRejects PE 核账（模块策略留本包）：包内 exe 的
// FileVersion 与目录名不一致 = 内容与名不符的伪安装 → 拒落位、半件不残留。
func TestDownloadPEVersionMismatchRejects(t *testing.T) {
	src := newZipSource(t)
	m := newChainManager(t, src)
	installViaChain(t, m, src, "v1.3.5")

	// 假 PE 账目按 exe 内容报版本：装 v1.3.6 但包内 exe 写着 9.9.9（同 tag 重传/篡改）
	names, contents := lmZipEntries("LiteMonitor_v1.3.6-win-x64")
	contents[names[0]] = "9.9.9"
	badZipPath := makeTestZip(t, names, contents)
	badZip, err := os.ReadFile(badZipPath)
	if err != nil {
		t.Fatal(err)
	}
	seedRemote(t, "v1.3.6", "LiteMonitor_v1.3.6-win-x64.zip", int64(len(badZip)), shaHex(badZip))
	src.set(badZip, false)

	err = m.Download(newTxnID(), "v1.3.6", nil)
	if err == nil || !strings.Contains(err.Error(), "文件版本不匹配") {
		t.Fatalf("PE 核对失败应拒装, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(m.versionsDir, "litemonitor_1.3.6")); !os.IsNotExist(statErr) {
		t.Error("名实不符的包不得落位")
	}
	assertNoTransactionLeftovers(t, m.versionsDir)
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

// TestUpdateChainCoexist 更新成功链：1.3.5 → 1.3.6 双版本共存、列表最新在前、
// 嵌套包装目录被吸收（落位根即套件内容）、逐版本 Resolve 稳定；卸载新版不动旧版目录。
func TestUpdateChainCoexist(t *testing.T) {
	src := newZipSource(t)
	m := newChainManager(t, src)
	installViaChain(t, m, src, "v1.3.5")

	rec := &progressRecorder{}
	zipBytes := buildLMZip(t, "1.3.6")
	seedRemote(t, "v1.3.6", "LiteMonitor_v1.3.6-win-x64.zip", int64(len(zipBytes)), shaHex(zipBytes))
	src.set(zipBytes, false)
	if err := m.Download(newTxnID(), "v1.3.6", rec.cb()); err != nil {
		t.Fatalf("Download(v1.3.6): %v", err)
	}

	// 进度词表零漂移：只允许既有词汇，且顺序为 downloading → extract → done
	// （verify 由内核折进 download、install 折进 done，不造幻影步骤）
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
	if installed[0].Version != "v1.3.6" || installed[1].Version != "v1.3.5" {
		t.Errorf("列表应最新在前, got %s, %s", installed[0].Version, installed[1].Version)
	}
	if installed[0].InstalledAt == "" || installed[0].IsImport {
		t.Errorf("展示账目字段应齐备且非导入: %+v", installed[0])
	}
	if !strings.HasPrefix(installed[0].InstalledAt, "20") || len(installed[0].InstalledAt) != len("2006-01-02 15:04:05") {
		t.Errorf("installedAt 应保持 yyyy-MM-dd HH:mm:ss 展示口径: %q", installed[0].InstalledAt)
	}
	// 嵌套吸收：落位目录顶层就是套件（无残留包装目录层）
	if _, err := os.Stat(filepath.Join(installed[0].Dir, exeName)); err != nil {
		t.Errorf("落位根应直接含 %s: %v", exeName, err)
	}
	if _, err := os.Stat(filepath.Join(installed[0].Dir, "LiteMonitor_v1.3.6-win-x64")); !os.IsNotExist(err) {
		t.Error("包装目录层不应带入最终目录")
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

	exe135, err := m.ResolveExe("v1.3.5")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Remove("v1.3.6"); err != nil {
		t.Fatalf("Remove(v1.3.6): %v", err)
	}
	if fi, serr := os.Stat(exe135); serr != nil || fi.Size() == 0 {
		t.Error("卸载新版不得动旧版目录")
	}
	if _, err := m.ResolveExe("v1.3.6"); err == nil {
		t.Error("已卸载版本应不可解析")
	}
}

// TestReinstallSamePackageIdempotent 同版本同包摘要重复落位 → 内核幂等复用不报错；
// 同版本异摘要 → 拒绝覆盖防漂移。
func TestReinstallSamePackageIdempotent(t *testing.T) {
	src := newZipSource(t)
	m := newChainManager(t, src)
	zipBytes := buildLMZip(t, "1.3.5")
	seedRemote(t, "v1.3.5", "LiteMonitor_v1.3.5-win-x64.zip", int64(len(zipBytes)), shaHex(zipBytes))
	src.set(zipBytes, false)
	if err := m.Download(newTxnID(), "v1.3.5", nil); err != nil {
		t.Fatalf("首装: %v", err)
	}
	if err := m.Download(newTxnID(), "v1.3.5", nil); err != nil {
		t.Fatalf("同摘要重装应幂等: %v", err)
	}

	// 同版本号换内容（异摘要，PE 版本不变、主题文件被换）：模拟上游同 tag 重传，必须拒绝
	names, contents := lmZipEntries("LiteMonitor_v1.3.5-win-x64")
	contents[names[0]] = "1.3.5"
	contents[names[2]] = `{"name":"Tampered_Theme"}`
	tamperedPath := makeTestZip(t, names, contents)
	tampered, err := os.ReadFile(tamperedPath)
	if err != nil {
		t.Fatal(err)
	}
	seedRemote(t, "v1.3.5", "LiteMonitor_v1.3.5-win-x64.zip", int64(len(tampered)), shaHex(tampered))
	src.set(tampered, false)
	err = m.Download(newTxnID(), "v1.3.5", nil)
	if err == nil || !strings.Contains(err.Error(), "漂移") {
		t.Fatalf("同版本异摘要应拒绝覆盖, got %v", err)
	}
}

// TestZipLayoutRejectedKeepsTree 双锚点自检不符的包（缺语言包 zh.json）：解包进
// staging 后自检拒装，最终目录不出现、半件不残留。
func TestZipLayoutRejectedKeepsTree(t *testing.T) {
	src := newZipSource(t)
	m := newChainManager(t, src)

	names := []string{"LiteMonitor_v1.3.6-win-x64/" + exeName} // 缺 resources/lang/zh.json
	contents := map[string]string{names[0]: "1.3.6"}
	noAnchor := makeTestZip(t, names, contents)
	zipBytes, err := os.ReadFile(noAnchor)
	if err != nil {
		t.Fatal(err)
	}
	seedRemote(t, "v1.3.6", "LiteMonitor_v1.3.6-win-x64.zip", int64(len(zipBytes)), shaHex(zipBytes))
	src.set(zipBytes, false)

	err = m.Download(newTxnID(), "v1.3.6", nil)
	if err == nil || !strings.Contains(err.Error(), "语言包") && !strings.Contains(err.Error(), "resources/lang/zh.json") {
		t.Fatalf("缺语言包锚点应拒装, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(m.versionsDir, "litemonitor_1.3.6")); !os.IsNotExist(statErr) {
		t.Error("锚点不符的包不得落位")
	}
	assertNoTransactionLeftovers(t, m.versionsDir)
}

// TestDownloadFlatLayoutAccepted 上游若改平铺发布（无包装目录），吸收策略同样落位。
func TestDownloadFlatLayoutAccepted(t *testing.T) {
	src := newZipSource(t)
	m := newChainManager(t, src)

	names, contents := lmZipEntries("")
	contents[names[0]] = "1.3.6"
	zipPath := makeTestZip(t, names, contents)
	zipBytes, err := os.ReadFile(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	seedRemote(t, "v1.3.6", "LiteMonitor_v1.3.6-win-x64.zip", int64(len(zipBytes)), shaHex(zipBytes))
	src.set(zipBytes, false)
	if err := m.Download(newTxnID(), "v1.3.6", nil); err != nil {
		t.Fatalf("平铺布局 Download: %v", err)
	}
	installed, err := m.ListInstalled()
	if err != nil || len(installed) != 1 || installed[0].Version != "v1.3.6" {
		t.Fatalf("平铺包应正常落位列出: %+v err %v", installed, err)
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
