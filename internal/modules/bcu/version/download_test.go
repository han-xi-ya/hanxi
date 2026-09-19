// 下载→校验→解包→落位全链的失败注入测试：以回环 httptest 服务器为"源"，
// 真实驱动内核 artifact.Fetch/UnpackZip/Tree 路径（真 tmp 目录，无网络依赖），
// 验证中断、坏摘要、半件残留、双版本共存、双层布局账本锚点（meta.Entry 指
// win-x64 真身）等收口行为（markeron/ccswitch 同构黄金样本，bcu 特有变体分叉）。
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
// BCU 的官方摘要直接携带在 BCURelease.SHA256 / FddSHA256（远程解析层已剥前缀）。
func seedRemote(t *testing.T, rel BCURelease) {
	t.Helper()
	remoteCache.mu.Lock()
	defer remoteCache.mu.Unlock()
	remoteCache.data = []BCURelease{rel}
	remoteCache.fetchedAt = time.Now() // 处于 TTL 内，Download 不会再走网络拉列表
}

func seedPortable(t *testing.T, version, tag, assetName string, size int64, sha string) {
	t.Helper()
	seedRemote(t, BCURelease{Version: version, Tag: tag, AssetName: assetName, Size: size, SHA256: sha})
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
		return []string{src.srv.URL + "/BCUninstaller_portable.zip"}
	}
	return m
}

// buildPortableZip 自包含便携 zip 形状：外层 bootstrapper + win-x64 真身
// （innerContent 为真身字节，账本锚点应指它）+ 附带文件。
func buildPortableZip(t *testing.T, innerContent string) []byte {
	t.Helper()
	path := makeTestZip(t, map[string]string{
		exeName:                "bootstrapper",
		"win-x64/" + exeName:   innerContent,
		"win-x64/bulkcrap.dll": "fake-dll",
		settingsName:           "first-run-absent-ok",
		"LICENSE":              "Apache-2.0",
	})
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// buildFddZip 框架依赖精简包形态（无 win-x64 子层：外层本体即真身）。
func buildFddZip(t *testing.T, exeContent string) []byte {
	t.Helper()
	path := makeTestZip(t, map[string]string{
		exeName:        exeContent,
		"bulkcrap.dll": "fake-dll",
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

// installViaChain 走真实下载链装一个便携版正例（夹具复用）。
func installViaChain(t *testing.T, m *Manager, src *zipSource, version, innerContent string) {
	t.Helper()
	zipBytes := buildPortableZip(t, innerContent)
	seedPortable(t, version, "v"+strings.Join(strings.Split(version, ".")[:2], "."),
		fmt.Sprintf("BCUninstaller_%s_portable.zip", version), int64(len(zipBytes)), shaHex(zipBytes))
	src.set(zipBytes, false)
	if err := m.Download(newTxnID(), version, VariantPortable, nil); err != nil {
		t.Fatalf("Download(%s): %v", version, err)
	}
}

// ---------- 失败注入用例 ----------

// TestDownloadInterruptKeepsOldVersion 下载中断（传输截断，全部候选源失败）→
// 报 error 进度、旧版本完好、版本树无新目录与半件残留。
func TestDownloadInterruptKeepsOldVersion(t *testing.T) {
	src := newZipSource(t)
	m := newChainManager(t, src)
	installViaChain(t, m, src, "6.1.0.1", "old-exe-bytes")

	newZip := buildPortableZip(t, "brand-new-exe")
	seedPortable(t, "6.2.0", "v6.2", "BCUninstaller_6.2.0_portable.zip", int64(len(newZip)), shaHex(newZip))
	src.set(newZip, true) // 半路掐线

	rec := &progressRecorder{}
	err := m.Download(newTxnID(), "6.2.0", VariantPortable, rec.cb())
	if err == nil {
		t.Fatal("下载中断应报错")
	}
	stages := rec.stages()
	if !strings.Contains(strings.Join(stages, ","), "error") || strings.Contains(strings.Join(stages, ","), "done") {
		t.Fatalf("进度事件应含 error 且不得含 done: %v", stages)
	}
	for _, e := range rec.events {
		if e.Variant != VariantPortable {
			t.Fatalf("进度载荷必须携带变体标识（前端按版本+变体索引下载卡片）: %+v", e)
		}
	}
	if _, statErr := os.Stat(filepath.Join(m.versionsDir, "bcu_6.2.0")); !os.IsNotExist(statErr) {
		t.Error("新半件目录不得出现")
	}
	assertNoTransactionLeftovers(t, m.versionsDir)
	installed, lerr := m.ListInstalled()
	if lerr != nil || len(installed) != 1 || installed[0].Version != "6.1.0.1" {
		t.Fatalf("旧版本应完好可用, got %+v err %v", installed, lerr)
	}
}

// TestDownloadBadDigestRejects 摘要错（源内容与官方摘要不符）→ 不落位且旧版完好。
func TestDownloadBadDigestRejects(t *testing.T) {
	src := newZipSource(t)
	m := newChainManager(t, src)
	installViaChain(t, m, src, "6.1.0.1", "old-exe-bytes")
	oldDir := filepath.Join(m.versionsDir, "bcu_6.1.0.1", "win-x64", exeName)
	oldExeHashBefore, _ := fileSHA256(oldDir)

	newZip := buildPortableZip(t, "tampered-payload")
	seedPortable(t, "6.2.0", "v6.2", "BCUninstaller_6.2.0_portable.zip", int64(len(newZip)),
		shaHex([]byte("NOT-THE-REAL-DIGEST"))) // 官方摘要对不上任何传输字节
	src.set(newZip, false)

	err := m.Download(newTxnID(), "6.2.0", VariantPortable, nil)
	if err == nil {
		t.Fatal("摘要不符应拒收")
	}
	if !strings.Contains(err.Error(), "SHA256") {
		t.Errorf("错误应指明摘要校验失败: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(m.versionsDir, "bcu_6.2.0")); !os.IsNotExist(statErr) {
		t.Error("坏包不得落位")
	}
	assertNoTransactionLeftovers(t, m.versionsDir)
	oldExeHashAfter, _ := fileSHA256(oldDir)
	if oldExeHashBefore != oldExeHashAfter || oldExeHashBefore == "" {
		t.Error("旧版本目录必须原样完好")
	}
}

// TestDownloadMissingDigestRefusesUnverified 上游无官方摘要 → 拒无校验安装，
// 且一个下载请求都不发出（先审配置，再碰网络）。
func TestDownloadMissingDigestRefusesUnverified(t *testing.T) {
	src := newZipSource(t)
	m := newChainManager(t, src)
	zipBytes := buildPortableZip(t, "x")
	seedPortable(t, "6.2.0", "v6.2", "BCUninstaller_6.2.0_portable.zip", int64(len(zipBytes)), "")
	src.set(zipBytes, false)

	err := m.Download(newTxnID(), "6.2.0", VariantPortable, nil)
	if err == nil || !strings.Contains(err.Error(), "拒绝无校验安装") {
		t.Fatalf("应拒绝无校验安装, got %v", err)
	}
	if hits := src.count(); hits != 0 {
		t.Errorf("摘要缺失时不得发起下载请求, hits = %d", hits)
	}
}

// TestDownloadVariantForks 变体分叉的既有错误口径：未知变体拒装；
// 无 fdd 变体的版本点 fdd 拒装（原实现文案逐字保持）。
func TestDownloadVariantForks(t *testing.T) {
	src := newZipSource(t)
	m := newChainManager(t, src)
	zipBytes := buildPortableZip(t, "x")
	seedPortable(t, "6.1.0.1", "v6.1", "BCUninstaller_6.1.0.1_portable.zip", int64(len(zipBytes)), shaHex(zipBytes))
	src.set(zipBytes, false)

	if err := m.Download(newTxnID(), "6.1.0.1", "setup", nil); err == nil ||
		!strings.Contains(err.Error(), "未知变体: setup") {
		t.Fatalf("未知变体应拒装, got %v", err)
	}
	if err := m.Download(newTxnID(), "6.1.0.1", VariantFdd, nil); err == nil ||
		!strings.Contains(err.Error(), "无框架依赖变体") {
		t.Fatalf("缺失 fdd 变体应拒装, got %v", err)
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

// TestUpdateChainCoexist 更新成功链：6.1.0.1 → 6.2.0 双版本共存、列表最新在前、
// 逐版本 Resolve 稳定；卸载新版不动旧版目录。账本锚点收口：新下载的
// meta.Entry 必须记录 win-x64 内层真身相对路径（外层 bootstrapper 秒退，
// 绝不容为托管/账本锚），AssetSHA256 即真身自哈希。
func TestUpdateChainCoexist(t *testing.T) {
	src := newZipSource(t)
	m := newChainManager(t, src)
	installViaChain(t, m, src, "6.1.0.1", "exe-6.1.0.1")

	rec := &progressRecorder{}
	zipBytes := buildPortableZip(t, "exe-6.2.0")
	seedPortable(t, "6.2.0", "v6.2", "BCUninstaller_6.2.0_portable.zip", int64(len(zipBytes)), shaHex(zipBytes))
	src.set(zipBytes, false)
	if err := m.Download(newTxnID(), "6.2.0", VariantPortable, rec.cb()); err != nil {
		t.Fatalf("Download(6.2.0): %v", err)
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
	if installed[0].Version != "6.2.0" || installed[1].Version != "6.1.0.1" {
		t.Errorf("列表应最新在前, got %s, %s", installed[0].Version, installed[1].Version)
	}
	if installed[0].InstalledAt == "" || installed[0].IsImport {
		t.Errorf("展示账目字段应齐备且非导入: %+v", installed[0])
	}
	if !strings.HasPrefix(installed[0].InstalledAt, "20") || len(installed[0].InstalledAt) != len("2006-01-02 15:04:05") {
		t.Errorf("installedAt 应保持 yyyy-MM-dd HH:mm:ss 展示口径: %q", installed[0].InstalledAt)
	}
	if installed[0].ExePath != filepath.Join(installed[0].Dir, "win-x64", exeName) {
		t.Errorf("展示 ExePath 应指内层真身: %+v", installed[0])
	}
	if installed[0].Source != "" {
		t.Errorf("新下载链不应有导入来源: %+v", installed[0])
	}

	// 落位账本由内核统一形状写入：Entry 记录内层相对路径（双层布局锚点契约）
	metaRaw, err := os.ReadFile(filepath.Join(installed[0].Dir, "meta.json"))
	if err != nil {
		t.Fatal(err)
	}
	var meta artifact.Meta
	if err := json.Unmarshal(metaRaw, &meta); err != nil {
		t.Fatal(err)
	}
	if want := filepath.ToSlash(innerExeRel); meta.Entry != want {
		t.Errorf("meta.Entry 应记录内层真身相对路径: got %q want %q", meta.Entry, want)
	}
	if meta.ZipSHA256 != shaHex(zipBytes) || meta.Source != artifact.SourceRemote {
		t.Errorf("账本内容异常: %+v", meta)
	}
	if want := shaHex([]byte("exe-6.2.0")); meta.AssetSHA256 != want {
		t.Errorf("AssetSHA256 应为内层真身自哈希（账本与启动同锚点）: got %q want %q", meta.AssetSHA256, want)
	}

	exe6101, err := m.ResolveExe("6.1.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Remove("6.2.0"); err != nil {
		t.Fatalf("Remove(6.2.0): %v", err)
	}
	if fi, serr := os.Stat(exe6101); serr != nil || fi.Size() == 0 {
		t.Error("卸载新版不得动旧版目录")
	}
	if _, err := m.ResolveExe("6.2.0"); err == nil {
		t.Error("已卸载版本应不可解析")
	}
}

// TestReinstallSamePackageIdempotent 同版本同包摘要重复落位 → 内核幂等复用不报错；
// 同版本异摘要 → 拒绝覆盖防漂移。（zip 条目顺序随 map 迭代变化，重装复用首次落位的
// 同一份字节，摘要才可比对。）
func TestReinstallSamePackageIdempotent(t *testing.T) {
	src := newZipSource(t)
	m := newChainManager(t, src)
	zipBytes := buildPortableZip(t, "same-exe")
	seedPortable(t, "6.1.0.1", "v6.1", "BCUninstaller_6.1.0.1_portable.zip", int64(len(zipBytes)), shaHex(zipBytes))
	src.set(zipBytes, false)
	if err := m.Download(newTxnID(), "6.1.0.1", VariantPortable, nil); err != nil {
		t.Fatalf("首装: %v", err)
	}
	if err := m.Download(newTxnID(), "6.1.0.1", VariantPortable, nil); err != nil {
		t.Fatalf("同摘要重装应幂等: %v", err)
	}

	// 同版本号换内容（异摘要）：模拟上游同 tag 重传，必须拒绝
	tampered := buildPortableZip(t, "same-exe-tampered")
	seedPortable(t, "6.1.0.1", "v6.1", "BCUninstaller_6.1.0.1_portable.zip", int64(len(tampered)), shaHex(tampered))
	src.set(tampered, false)
	err := m.Download(newTxnID(), "6.1.0.1", VariantPortable, nil)
	if err == nil || !strings.Contains(err.Error(), "漂移") {
		t.Fatalf("同版本异摘要应拒绝覆盖, got %v", err)
	}
}

// TestZipLayoutRejectedKeepsTree 布局自检不符的包（根目录缺外层
// BCUninstaller.exe——只有 win-x64 子层的异形包）：解包进 staging 后自检拒装，
// 最终目录不出现、半件不残留。
func TestZipLayoutRejectedKeepsTree(t *testing.T) {
	src := newZipSource(t)
	m := newChainManager(t, src)

	weird := makeTestZip(t, map[string]string{ // 缺根目录启动器
		"win-x64/" + exeName: "fake-exe",
	})
	zipBytes, err := os.ReadFile(weird)
	if err != nil {
		t.Fatal(err)
	}
	seedPortable(t, "6.2.0", "v6.2", "BCUninstaller_6.2.0_portable.zip", int64(len(zipBytes)), shaHex(zipBytes))
	src.set(zipBytes, false)

	err = m.Download(newTxnID(), "6.2.0", VariantPortable, nil)
	if err == nil || !strings.Contains(err.Error(), "zip 布局无效") {
		t.Fatalf("布局不符的包应拒装, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(m.versionsDir, "bcu_6.2.0")); !os.IsNotExist(statErr) {
		t.Error("布局不符的包不得落位")
	}
	assertNoTransactionLeftovers(t, m.versionsDir)
}

// TestDownloadFddVariantChain fdd 精简包成功链：无 win-x64 子层布局下
// 账本锚点回退外层本体（meta.Entry=BCUninstaller.exe，AssetSHA256=外层自哈希），
// ResolveExe/ListInstalled 同步回退——老布局回退兜底与双层锚点一体收口。
func TestDownloadFddVariantChain(t *testing.T) {
	src := newZipSource(t)
	m := newChainManager(t, src)
	zipBytes := buildFddZip(t, "fdd-exe")
	seedRemote(t, BCURelease{
		Version: "6.2.0", Tag: "v6.2",
		AssetName: "BCUninstaller_6.2.0_portable.zip", Size: 1, SHA256: shaHex([]byte("unused-portable")),
		FddName: "BCUninstaller_6.2.0_net8.0-windows10.0.18362.0.zip", FddSize: int64(len(zipBytes)), FddSHA256: shaHex(zipBytes),
	})
	src.set(zipBytes, false)

	rec := &progressRecorder{}
	if err := m.Download(newTxnID(), "6.2.0", VariantFdd, rec.cb()); err != nil {
		t.Fatalf("Download(fdd): %v", err)
	}
	for _, e := range rec.events {
		if e.Variant != VariantFdd {
			t.Fatalf("变体标识应贯穿全部阶段事件: %+v", e)
		}
	}

	installed, err := m.ListInstalled()
	if err != nil || len(installed) != 1 {
		t.Fatalf("fdd 装后应可见: %+v %v", installed, err)
	}
	wantExe := filepath.Join(installed[0].Dir, exeName)
	if installed[0].ExePath != wantExe {
		t.Errorf("无内层布局 ExePath 应回退外层: %q", installed[0].ExePath)
	}
	metaRaw, err := os.ReadFile(filepath.Join(installed[0].Dir, "meta.json"))
	if err != nil {
		t.Fatal(err)
	}
	var meta artifact.Meta
	if err := json.Unmarshal(metaRaw, &meta); err != nil {
		t.Fatal(err)
	}
	if meta.Entry != exeName || meta.AssetSHA256 != shaHex([]byte("fdd-exe")) {
		t.Errorf("fdd 账本锚点异常: %+v", meta)
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
