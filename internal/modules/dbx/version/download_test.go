// 下载→校验→解包→落位全链的失败注入测试：以回环 httptest 服务器为"源"，
// 真实驱动内核 artifact.Fetch/UnpackZip/Tree 路径（真 tmp 目录，无网络依赖），
// 验证中断、坏摘要、无摘要拒装、半件残留、布局自检（缺 portable.dbx 拒落位）、
// 旁证矛盾拒装、双版本共存等收口行为（gonavi 同构黄金样本，DBX 领域化：
// 布局锚点 exe+portable.dbx，官方摘要唯一来源 GitHub digest，无备用清单链）。
package version

import (
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
func seedRemote(t *testing.T, rel DBXRelease) {
	t.Helper()
	remoteCache.mu.Lock()
	defer remoteCache.mu.Unlock()
	remoteCache.data = []DBXRelease{rel}
	remoteCache.fetchedAt = time.Now() // 处于 TTL 内，Download 不会再走网络拉列表
}

// zipSource 可编程假资产源：按请求路径分发便携 zip，支持截断（模拟中断）
// 与命中计数。DBX 无备用摘要清单链，不设清单分发。
type zipSource struct {
	mu       sync.Mutex
	zipBody  []byte
	truncate bool
	zipHits  int
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
	defer z.mu.Unlock()
	if strings.Contains(r.URL.Path, "x64-portable.zip") {
		z.zipHits++
		body, truncate := z.zipBody, z.truncate
		if truncate {
			_, _ = w.Write(body[:len(body)/2])
			panic(http.ErrAbortHandler) // 掐断连接：客户端收到短读（传输中断）
		}
		w.Header().Set("Content-Length", fmt.Sprint(len(body)))
		_, _ = w.Write(body)
		return
	}
	w.WriteHeader(http.StatusNotFound)
}

func (z *zipSource) setZip(body []byte, truncate bool) {
	z.mu.Lock()
	defer z.mu.Unlock()
	z.zipBody = body
	z.truncate = truncate
}

func (z *zipSource) count() int {
	z.mu.Lock()
	defer z.mu.Unlock()
	return z.zipHits
}

// newChainManager 构造指向假源的 Manager（仅镜像 URL 接缝改指回环 http，
// 下载/校验/解包/落位全链仍走生产实现 artifact.Fetch/UnpackZip/Tree）。
func newChainManager(t *testing.T, src *zipSource) *Manager {
	t.Helper()
	m := NewManager(t.TempDir())
	m.mirrors = func(_, assetName string) []string {
		return []string{src.srv.URL + "/" + assetName}
	}
	return m
}

// buildPortableZip 官方便携 zip 形状（实证根平铺五条目）：DBX.exe +
// LICENSE + README.md + portable.dbx（空标记）+ portable-update.json
// （executable_sha256 与 exe 内容自洽，旁证复核走正路）。
func buildPortableZip(t *testing.T, exeContent string) []byte {
	t.Helper()
	path := makeTestZip(t, map[string]string{
		exeName:                exeContent,
		"LICENSE":              "Apache License 2.0",
		"README.md":            "DBX readme",
		portableMarkName:       "",
		portableUpdateFileName: fmt.Sprintf(`{"platforms":{"windows-x86_64":{"executable_sha256":"%s"}}}`, shaHex([]byte(exeContent))),
	})
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// portableRelease 常规便携条目夹具（digest 直给）。
func portableRelease(version string, zipBytes []byte) DBXRelease {
	return DBXRelease{
		Version:   version,
		AssetName: "DBX_" + strings.TrimPrefix(version, "v") + "_x64-portable.zip",
		Size:      int64(len(zipBytes)),
		SHA256:    shaHex(zipBytes),
	}
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

// newTxnID 生成唯一且过 ValidateVersionToken 白名单的事务 ID。
func newTxnID() string {
	return fmt.Sprintf("txn-%d", time.Now().UnixNano())
}

// installViaChain 走真实下载链装一个版本（正例夹具复用）。
func installViaChain(t *testing.T, m *Manager, src *zipSource, version, exeContent string) {
	t.Helper()
	zipBytes := buildPortableZip(t, exeContent)
	seedRemote(t, portableRelease(version, zipBytes))
	src.setZip(zipBytes, false)
	if err := m.Download(newTxnID(), version, nil); err != nil {
		t.Fatalf("Download(%s): %v", version, err)
	}
}

// ---------- 失败注入与降级用例 ----------

// TestDownloadInterruptKeepsOldVersion 下载中断（传输截断，全部候选源失败）→
// 报 error 进度、旧版本完好、版本树无新目录与半件残留。
func TestDownloadInterruptKeepsOldVersion(t *testing.T) {
	src := newZipSource(t)
	m := newChainManager(t, src)
	installViaChain(t, m, src, "v1.5.2", "old-exe-bytes")

	newZip := buildPortableZip(t, "brand-new-exe")
	seedRemote(t, portableRelease("v1.5.3", newZip))
	src.setZip(newZip, true) // 半路掐线

	rec := &progressRecorder{}
	err := m.Download(newTxnID(), "v1.5.3", rec.cb())
	if err == nil {
		t.Fatal("下载中断应报错")
	}
	stages := strings.Join(rec.stages(), ",")
	if !strings.Contains(stages, "error") || strings.Contains(stages, "done") {
		t.Fatalf("进度事件应含 error 且不得含 done: %v", stages)
	}
	if _, statErr := os.Stat(filepath.Join(m.versionsDir, "dbx_1.5.3")); !os.IsNotExist(statErr) {
		t.Error("新半件目录不得出现")
	}
	assertNoTransactionLeftovers(t, m.versionsDir)
	installed, lerr := m.ListInstalled()
	if lerr != nil || len(installed) != 1 || installed[0].Version != "v1.5.2" {
		t.Fatalf("旧版本应完好可用, got %+v err %v", installed, lerr)
	}
}

// TestDownloadBadDigestRejects 摘要错（源内容与官方摘要不符）→ 不落位且旧版完好。
func TestDownloadBadDigestRejects(t *testing.T) {
	src := newZipSource(t)
	m := newChainManager(t, src)
	installViaChain(t, m, src, "v1.5.2", "old-exe-bytes")
	oldExeHashBefore, _ := fileSHA256(filepath.Join(m.versionsDir, "dbx_1.5.2", exeName))

	newZip := buildPortableZip(t, "tampered-payload")
	rel := portableRelease("v1.5.3", newZip)
	rel.SHA256 = shaHex([]byte("NOT-THE-REAL-DIGEST")) // 官方摘要对不上任何传输字节
	seedRemote(t, rel)
	src.setZip(newZip, false)

	err := m.Download(newTxnID(), "v1.5.3", nil)
	if err == nil {
		t.Fatal("摘要不符应拒收")
	}
	if !strings.Contains(err.Error(), "SHA256") && !strings.Contains(err.Error(), "sha256") {
		t.Errorf("错误应指明摘要校验失败: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(m.versionsDir, "dbx_1.5.3")); !os.IsNotExist(statErr) {
		t.Error("坏包不得落位")
	}
	assertNoTransactionLeftovers(t, m.versionsDir)
	oldExeHashAfter, _ := fileSHA256(filepath.Join(m.versionsDir, "dbx_1.5.2", exeName))
	if oldExeHashBefore != oldExeHashAfter || oldExeHashBefore == "" {
		t.Error("旧版本目录必须原样完好")
	}
}

// TestDownloadMissingDigestRefusesUnverified 官方摘要缺失（GitHub digest
// 唯一信任根缺位；上游 .sig 属 minisign 通道不消费，没有降级去向）→
// 拒无校验安装，且一个 zip 下载请求都不发出（先审配置，再碰网络）。
func TestDownloadMissingDigestRefusesUnverified(t *testing.T) {
	src := newZipSource(t)
	m := newChainManager(t, src)
	zipBytes := buildPortableZip(t, "x")
	seedRemote(t, DBXRelease{
		Version:   "v1.5.1",
		AssetName: "DBX_1.5.1_x64-portable.zip",
		Size:      int64(len(zipBytes)),
	})
	src.setZip(zipBytes, false)

	err := m.Download(newTxnID(), "v1.5.1", nil)
	if err == nil || !strings.Contains(err.Error(), "拒绝无校验安装") {
		t.Fatalf("应拒绝无校验安装, got %v", err)
	}
	if hits := src.count(); hits != 0 {
		t.Errorf("摘要缺失时不得发起下载请求, hits = %d", hits)
	}
}

// TestZipLayoutRejectedKeepsTree 布局自检不符的包拒落位、半件不残留：
// ①根内无 DBX.exe；②exe 在而 portable.dbx 缺（核心不变式=两者同在，
// 缺便携标记的副本会以装机模式启动，托管语义失真，同样拒）。
func TestZipLayoutRejectedKeepsTree(t *testing.T) {
	cases := []struct {
		name    string
		entries map[string]string
	}{
		{"缺 exe", map[string]string{"LICENSE": "Apache", "README.md": "DBX", portableMarkName: ""}},
		{"缺 portable.dbx", map[string]string{exeName: "fake-exe", "LICENSE": "Apache", "README.md": "DBX"}},
	}
	for _, tc := range cases {
		src := newZipSource(t)
		m := newChainManager(t, src)
		path := makeTestZip(t, tc.entries)
		zipBytes, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		seedRemote(t, portableRelease("v1.5.3", zipBytes))
		src.setZip(zipBytes, false)

		err = m.Download(newTxnID(), "v1.5.3", nil)
		if err == nil || !strings.Contains(err.Error(), "布局无效") {
			t.Fatalf("%s: 应拒装, got %v", tc.name, err)
		}
		if _, statErr := os.Stat(filepath.Join(m.versionsDir, "dbx_1.5.3")); !os.IsNotExist(statErr) {
			t.Errorf("%s: 布局不符的包不得落位", tc.name)
		}
		assertNoTransactionLeftovers(t, m.versionsDir)
	}
}

// TestZipCorroborationMismatchRejects portable-update.json 声明的
// executable_sha256 与包内 exe 实测矛盾（上游包内自不一致异常态）→
// zip 摘要合法仍过第一层，第四层旁证拒装不落位。
func TestZipCorroborationMismatchRejects(t *testing.T) {
	src := newZipSource(t)
	m := newChainManager(t, src)
	path := makeTestZip(t, map[string]string{
		exeName:                "fake-exe",
		"LICENSE":              "Apache License 2.0",
		"README.md":            "DBX readme",
		portableMarkName:       "",
		portableUpdateFileName: fmt.Sprintf(`{"executable_sha256":"%s"}`, strings.Repeat("a", 64)),
	})
	zipBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	seedRemote(t, portableRelease("v1.5.3", zipBytes))
	src.setZip(zipBytes, false)

	err = m.Download(newTxnID(), "v1.5.3", nil)
	if err == nil || !strings.Contains(err.Error(), "包内自不一致") {
		t.Fatalf("旁证矛盾应拒装, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(m.versionsDir, "dbx_1.5.3")); !os.IsNotExist(statErr) {
		t.Error("旁证矛盾的包不得落位")
	}
	assertNoTransactionLeftovers(t, m.versionsDir)
}

// TestUpdateChainCoexist 更新成功链：1.5.2 → 1.5.3 双版本共存、列表最新在前、
// 落位账本齐备（exe 实测摘要入账=漂移护栏锚点、verifiedHash 徽章）；
// 卸载新版不动旧版目录。
func TestUpdateChainCoexist(t *testing.T) {
	src := newZipSource(t)
	m := newChainManager(t, src)
	installViaChain(t, m, src, "v1.5.2", "exe-1.5.2")

	rec := &progressRecorder{}
	zipBytes := buildPortableZip(t, "exe-1.5.3")
	seedRemote(t, portableRelease("v1.5.3", zipBytes))
	src.setZip(zipBytes, false)
	if err := m.Download(newTxnID(), "v1.5.3", rec.cb()); err != nil {
		t.Fatalf("Download(v1.5.3): %v", err)
	}

	// 进度词表零漂移：只允许既有词汇，且顺序为 downloading → extract → done
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
	if installed[0].Version != "v1.5.3" || installed[1].Version != "v1.5.2" {
		t.Errorf("列表应最新在前, got %s, %s", installed[0].Version, installed[1].Version)
	}
	top := installed[0]
	if !top.VerifiedHash || top.HashDrifted || top.IsImport || top.SHA256 != shaHex([]byte("exe-1.5.3")) {
		t.Errorf("落位账目异常: %+v", top)
	}
	if top.InstalledAt == "" {
		t.Errorf("installedAt 应齐备: %+v", top)
	}
	metaRaw, err := os.ReadFile(filepath.Join(top.Dir, "meta.json"))
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
	// 便携标记与许可文本随保布局解压天然落位
	for _, name := range []string{"LICENSE", "README.md", portableMarkName, portableUpdateFileName} {
		if _, err := os.Stat(filepath.Join(top.Dir, name)); err != nil {
			t.Errorf("落位后缺少 %s: %v", name, err)
		}
	}

	exe152, err := m.ResolveExe("v1.5.2")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Remove("v1.5.3"); err != nil {
		t.Fatalf("Remove(v1.5.3): %v", err)
	}
	if fi, serr := os.Stat(exe152); serr != nil || fi.Size() == 0 {
		t.Error("卸载新版不得动旧版目录")
	}
	if _, err := m.ResolveExe("v1.5.3"); err == nil {
		t.Error("已卸载版本应不可解析")
	}
}

// TestReinstallSamePackageIdempotent 同版本同包摘要重复落位 → 内核幂等复用
// 不报错；同版本异摘要 → 拒绝覆盖防漂移。
func TestReinstallSamePackageIdempotent(t *testing.T) {
	src := newZipSource(t)
	m := newChainManager(t, src)
	zipBytes := buildPortableZip(t, "same-exe")
	seedRemote(t, portableRelease("v1.5.3", zipBytes))
	src.setZip(zipBytes, false)
	if err := m.Download(newTxnID(), "v1.5.3", nil); err != nil {
		t.Fatalf("首装: %v", err)
	}
	if err := m.Download(newTxnID(), "v1.5.3", nil); err != nil {
		t.Fatalf("同摘要重装应幂等: %v", err)
	}

	tampered := buildPortableZip(t, "same-exe-tampered")
	seedRemote(t, portableRelease("v1.5.3", tampered))
	src.setZip(tampered, false)
	err := m.Download(newTxnID(), "v1.5.3", nil)
	if err == nil || !strings.Contains(err.Error(), "漂移") {
		t.Fatalf("同版本异摘要应拒绝覆盖, got %v", err)
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
