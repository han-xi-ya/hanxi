// 下载→官方摘要双核→清单交叉比对→NSIS 静默安装全链的失败注入测试：以回环
// httptest 服务器为"源"，真实驱动内核 artifact.Fetch 路径 + nsisInstall/
// foreignInstallCheck/purgeShortcuts 三接缝假实现（真 tmp 目录、真安装器缓存，
// 无网络依赖、不真跑安装器/不碰注册表/不删真实快捷方式），验证中断、坏摘要、
// 清单矛盾、外部安装卫兵、布局自检拒装等收口行为
// （markeron/ccswitch/translucenttb 同构黄金样本，NSIS 安装器特例差异在安装段；
// keyviz MSI 接缝先例）。
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

// TestMain 全局中和 NSIS 链的宿主副作用：假安装器默认拒绝（成功用例各自
// 注入落盘假实现）、外部安装卫兵恒"无外部安装"、快捷方式清理 no-op——
// 单测绝不在真机注册表/桌面/开始菜单上动盘（宿主有真实 Recordly 时旧实现
// 的 Remove 链会误删用户快捷方式，本接缝纪律一并堵住该隐患）。
func TestMain(m *testing.M) {
	oldInstall, oldForeign, oldPurge := nsisInstall, foreignInstallCheck, purgeShortcuts
	nsisInstall = func(string, string) error { return fmt.Errorf("test default: 未注入假安装器") }
	foreignInstallCheck = func(string) (string, bool) { return "", false }
	purgeShortcuts = func(string, string) {}
	code := m.Run()
	nsisInstall, foreignInstallCheck, purgeShortcuts = oldInstall, oldForeign, oldPurge
	os.Exit(code)
}

// ---------- 测试夹具：远程列表注入与假资产源 ----------

// seedRemote 注入伪远程列表。remoteCache 是包内全局缓存（生产语义即进程级
// 单例），本包测试串行执行（不使用 t.Parallel），互相隔离靠 setup 重灌。
// Recordly 的官方摘要直接携带在 RecordlyRelease.SHA256（远程解析层已剥前缀）。
func seedRemote(t *testing.T, version, assetName string, size int64, sha string) {
	t.Helper()
	remoteCache.mu.Lock()
	defer remoteCache.mu.Unlock()
	remoteCache.data = []RecordlyRelease{{
		Version:   version,
		AssetName: assetName,
		AssetURL:  "http://127.0.0.1:0/placeholder",
		Size:      size,
		SHA256:    sha,
	}}
	remoteCache.fetchedAt = time.Now() // 处于 TTL 内，Download 不会再走网络拉列表
}

// installerSource 可编程假资产源：按请求路径分派安装器字节与 SHA256SUMS.txt
// 清单（同一 mirrors 接缝供两路），支持截断（模拟中断）、清单缺席（404）与命中计数。
type installerSource struct {
	mu           sync.Mutex
	body         []byte
	sums         []byte // nil = 清单缺席（404）
	sumsHits     bool
	truncate     bool
	hits         int
	installerURL string
	sumsURL      string
	srv          *httptest.Server
}

func newInstallerSource(t *testing.T) *installerSource {
	t.Helper()
	s := &installerSource{}
	s.srv = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.srv.Close)
	s.installerURL = s.srv.URL + "/" + installerAssetName
	s.sumsURL = s.srv.URL + "/" + sumsAssetName
	return s
}

func (s *installerSource) serve(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	body, sums, truncate := s.body, s.sums, s.truncate
	s.hits++
	isSums := strings.HasSuffix(r.URL.Path, "/"+sumsAssetName)
	if isSums {
		s.sumsHits = true
	}
	s.mu.Unlock()

	if isSums {
		if sums == nil {
			http.NotFound(w, r) // 清单缺失：交叉比对段降级放行
			return
		}
		_, _ = w.Write(sums)
		return
	}

	w.Header().Set("Content-Length", fmt.Sprint(len(body)))
	if truncate {
		_, _ = w.Write(body[:len(body)/2])
		panic(http.ErrAbortHandler) // 掐断连接：客户端收到短读（传输中断）
	}
	_, _ = w.Write(body)
}

func (s *installerSource) set(body []byte, truncate bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.body = body
	s.truncate = truncate
}

func (s *installerSource) setSums(sums []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sums = sums
}

func (s *installerSource) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hits
}

func (s *installerSource) sumsRequested() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sumsHits
}

// newChainManager 构造指向假源的 Manager：镜像 URL 接缝改指回环 http（真实
// artifact.Fetch 下载/校验链不变，安装器与清单两路同被引导），nsisInstall 接缝
// 注入"按当前 exe 内容落 Electron 布局"的假实现（真实 NSIS 静默链已真机验证，
// 失败用例各自覆写本接缝）。
func newChainManager(t *testing.T, src *installerSource) *Manager {
	t.Helper()
	m := NewManager(t.TempDir())
	m.mirrors = func(_, assetName string) []string {
		if strings.EqualFold(assetName, sumsAssetName) {
			return []string{src.sumsURL}
		}
		return []string{src.installerURL}
	}
	withNSISInstall(t, func(_, targetDir string) error {
		return writeFakeInstall(t, targetDir)
	})
	return m
}

// withNSISInstall/withForeignGuard/withPurge 临时替换 NSIS 链接缝，t.Cleanup 复位。
func withNSISInstall(t *testing.T, fn func(installer, targetDir string) error) {
	t.Helper()
	old := nsisInstall
	nsisInstall = fn
	t.Cleanup(func() { nsisInstall = old })
}

func withForeignGuard(t *testing.T, fn func(versionsDir string) (string, bool)) {
	t.Helper()
	old := foreignInstallCheck
	foreignInstallCheck = fn
	t.Cleanup(func() { foreignInstallCheck = old })
}

func withPurge(t *testing.T, fn func(versionsDir, desktopDir string)) {
	t.Helper()
	old := purgeShortcuts
	purgeShortcuts = fn
	t.Cleanup(func() { purgeShortcuts = old })
}

// fakeExeContent 当前假安装体的 exe 内容（区分新旧安装的观测量）。
var fakeExeContent = "fake-exe"

// writeFakeInstall 假 NSIS 安装器：在目标目录落 Electron 布局（exe 内容取
// fakeExeContent，供"旧版本完好"断言区分代际）。
func writeFakeInstall(t *testing.T, targetDir string) error {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(targetDir, "resources"), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(targetDir, exeName), []byte(fakeExeContent), 0644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(targetDir, asarRelPath), []byte("fake-asar"), 0644)
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

// newTxnID 生成唯一且过 ValidateVersionToken 白名单的事务 ID（Download 的
// 安装器缓存目录名前缀由它派生，崩溃现场可按事务定位）。
func newTxnID() string {
	return fmt.Sprintf("txn-%d", time.Now().UnixNano())
}

// installViaChain 走真实下载链 + 假 NSIS 装一个版本（正例夹具复用）。
// exe 内容按版本代际打标，供"旧安装完好"类断言区分代际。
func installViaChain(t *testing.T, m *Manager, src *installerSource, version string) {
	t.Helper()
	fakeExeContent = "fake-exe:" + version
	instBytes := []byte("nsis-installer:" + version)
	seedRemote(t, version, installerAssetName, int64(len(instBytes)), shaHex(instBytes))
	src.set(instBytes, false)
	if err := m.Download(newTxnID(), version, nil); err != nil {
		t.Fatalf("Download(%s): %v", version, err)
	}
}

// ---------- 失败注入用例 ----------

// TestUpdateChainStagesAndLedger 正例全链：进度词表零漂移（downloading → verify
// → install → done，内核 Fetch 的摘要双核映射既有 verify 词汇，不造幻影步骤）；
// 覆盖式单目录代际更新（旧安装被假 NSIS 替换）；hanxi-meta.json 账本入账
// tag/资产名/安装体摘要（beta 后缀等 PE 承载不了的信息唯一来源）。
func TestUpdateChainStagesAndLedger(t *testing.T) {
	src := newInstallerSource(t)
	m := newChainManager(t, src)
	installViaChain(t, m, src, "v1.3.3")

	rec := &progressRecorder{}
	newBytes := []byte("nsis-installer:v1.3.5")
	seedRemote(t, "v1.3.5", installerAssetName, int64(len(newBytes)), shaHex(newBytes))
	src.set(newBytes, false)
	if err := m.Download(newTxnID(), "v1.3.5", rec.cb()); err != nil {
		t.Fatalf("Download(v1.3.5): %v", err)
	}

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

	// 单目录覆盖语义：列表恒至多一条，版本走 meta tag
	installed, err := m.ListInstalled()
	if err != nil || len(installed) != 1 || installed[0].Version != "v1.3.5" {
		t.Fatalf("覆盖升级后应只剩最新代际: %+v err %v", installed, err)
	}
	if installed[0].InstalledAt == "" || installed[0].IsImport {
		t.Errorf("展示账目字段异常: %+v", installed[0])
	}
	if installed[0].Source != installerAssetName {
		t.Errorf("远程安装的来源账保持资产名口径（迁移前后一致）: %+v", installed[0])
	}
	if !strings.HasPrefix(installed[0].InstalledAt, "20") || len(installed[0].InstalledAt) != len("2006-01-02 15:04:05") {
		t.Errorf("installedAt 应保持 yyyy-MM-dd HH:mm:ss 展示口径: %q", installed[0].InstalledAt)
	}

	// 模块自持账本（hanxi-meta.json 非内核 Meta——ADR-0002 §5 "共享包不抽模块业务"）
	metaRaw, err := os.ReadFile(filepath.Join(m.InstallDir(), "hanxi-meta.json"))
	if err != nil {
		t.Fatal(err)
	}
	var mm map[string]any
	if err := json.Unmarshal(metaRaw, &mm); err != nil {
		t.Fatal(err)
	}
	if mm["tag"] != "v1.3.5" || mm["installerSHA256"] != shaHex(newBytes) || mm["verifiedHash"] != true {
		t.Errorf("安装账本内容异常: %v", mm)
	}
}

// TestDownloadInterruptKeepsOldInstall 下载中断（传输截断，全部候选源失败）→
// 报 error 进度、旧安装完好、托管根无新半件。
func TestDownloadInterruptKeepsOldInstall(t *testing.T) {
	src := newInstallerSource(t)
	m := newChainManager(t, src)
	installViaChain(t, m, src, "v1.3.3")
	exePath := filepath.Join(m.InstallDir(), exeName)

	newBytes := []byte("nsis-installer:v1.3.5-interrupted")
	seedRemote(t, "v1.3.5", installerAssetName, int64(len(newBytes)), shaHex(newBytes))
	src.set(newBytes, true) // 半路掐线

	rec := &progressRecorder{}
	err := m.Download(newTxnID(), "v1.3.5", rec.cb())
	if err == nil {
		t.Fatal("下载中断应报错")
	}
	stages := rec.stages()
	if !strings.Contains(strings.Join(stages, ","), "error") || strings.Contains(strings.Join(stages, ","), "done") {
		t.Fatalf("进度事件应含 error 且不得含 done: %v", stages)
	}
	got, rerr := os.ReadFile(exePath)
	if rerr != nil || string(got) != "fake-exe:v1.3.3" {
		t.Errorf("旧安装必须原样完好: %q err %v", got, rerr)
	}
	assertNoTransactionLeftovers(t, m.versionsDir)
}

// TestDownloadBadDigestRejects 摘要错（源内容与官方摘要不符）→ 不落安装段、旧版完好。
func TestDownloadBadDigestRejects(t *testing.T) {
	src := newInstallerSource(t)
	m := newChainManager(t, src)
	installViaChain(t, m, src, "v1.3.3")
	exePath := filepath.Join(m.InstallDir(), exeName)

	newBytes := []byte("tampered-payload")
	seedRemote(t, "v1.3.5", installerAssetName, int64(len(newBytes)),
		shaHex([]byte("NOT-THE-REAL-DIGEST"))) // 官方摘要对不上任何传输字节
	src.set(newBytes, false)

	err := m.Download(newTxnID(), "v1.3.5", nil)
	if err == nil {
		t.Fatal("摘要不符应拒收")
	}
	if !strings.Contains(err.Error(), "SHA256") {
		t.Errorf("错误应指明摘要校验失败: %v", err)
	}
	if got, _ := os.ReadFile(exePath); string(got) != "fake-exe:v1.3.3" {
		t.Error("坏包不得进入安装段")
	}
	assertNoTransactionLeftovers(t, m.versionsDir)
}

// TestDownloadMissingDigestRefusesUnverified 上游无官方摘要 → 拒无校验安装，
// 且一个下载请求都不发出（先审配置，再碰网络）。
func TestDownloadMissingDigestRefusesUnverified(t *testing.T) {
	src := newInstallerSource(t)
	m := newChainManager(t, src)
	zipBytes := []byte("installer-bytes")
	seedRemote(t, "v1.3.5", installerAssetName, int64(len(zipBytes)), "")
	src.set(zipBytes, false)

	err := m.Download(newTxnID(), "v1.3.5", nil)
	if err == nil || !strings.Contains(err.Error(), "拒绝无校验安装") {
		t.Fatalf("应拒绝无校验安装, got %v", err)
	}
	if hits := src.count(); hits != 0 {
		t.Errorf("摘要缺失时不得发起下载请求, hits = %d", hits)
	}
}

// TestDownloadBlockedByForeignInstall 外部安装卫兵红线：HKCU 卸载注册表指向
// 托管目录之外的 Recordly（用户自装副本）时，oneClick 会连带静默卸载它——
// 必须在发起任何网络请求前拒绝，并给出"先卸载/先收编"指引。
func TestDownloadBlockedByForeignInstall(t *testing.T) {
	src := newInstallerSource(t)
	m := newChainManager(t, src)
	withForeignGuard(t, func(string) (string, bool) {
		return `C:\Users\someone\AppData\Local\Programs\Recordly`, true
	})
	instBytes := []byte("installer-bytes")
	seedRemote(t, "v1.3.5", installerAssetName, int64(len(instBytes)), shaHex(instBytes))
	src.set(instBytes, false)

	err := m.Download(newTxnID(), "v1.3.5", nil)
	if err == nil || !strings.Contains(err.Error(), "检测到独立安装的 Recordly") {
		t.Fatalf("外部安装应被卫兵拦截, got %v", err)
	}
	if hits := src.count(); hits != 0 {
		t.Errorf("卫兵拒绝时不得发起下载请求, hits = %d", hits)
	}
}

// TestNSISInstallFailureReportsErrorStage 安装段失败（接缝返回 NSIS 语义错误）：
// 错误经既有 error 进度词表送达，done 不出现。
func TestNSISInstallFailureReportsErrorStage(t *testing.T) {
	src := newInstallerSource(t)
	m := newChainManager(t, src)
	withNSISInstall(t, func(string, string) error {
		return fmt.Errorf("NSIS 安装器退出码 -1602：%s", decodeInstallerExit(1223))
	})
	instBytes := []byte("nsis-installer:v1.3.5")
	seedRemote(t, "v1.3.5", installerAssetName, int64(len(instBytes)), shaHex(instBytes))
	src.set(instBytes, false)

	rec := &progressRecorder{}
	err := m.Download(newTxnID(), "v1.3.5", rec.cb())
	if err == nil {
		t.Fatal("安装失败应报错")
	}
	// 既有契约：返回值携带安装器语义原文（service 层再包"安装失败"通知），
	// error 进度事件携"静默安装失败"前缀口径
	if !strings.Contains(err.Error(), "NSIS 安装器退出码") {
		t.Errorf("返回错误应保留安装器语义原文: %v", err)
	}
	found := false
	for _, e := range rec.events {
		if e.Stage == "error" && strings.Contains(e.Message, "静默安装失败") {
			found = true
		}
	}
	if !found {
		t.Errorf("error 事件应保留静默安装失败口径: %+v", rec.events)
	}
	stages := strings.Join(rec.stages(), ",")
	if !strings.Contains(stages, "install") || !strings.Contains(stages, "error") || strings.Contains(stages, "done") {
		t.Errorf("进度序列异常: %q", stages)
	}
}

// TestLayoutCheckFailureKeepsErrorStage 假安装器"成功返回"但未落合法布局：
// 安装后布局自检（exe 非空 + app.asar）拒收，error 段说明布局无效。
func TestLayoutCheckFailureKeepsErrorStage(t *testing.T) {
	src := newInstallerSource(t)
	m := newChainManager(t, src)
	withNSISInstall(t, func(_, targetDir string) error {
		return os.MkdirAll(targetDir, 0755) // 只建目录不落文件：半件安装
	})
	instBytes := []byte("nsis-installer:v1.3.5")
	seedRemote(t, "v1.3.5", installerAssetName, int64(len(instBytes)), shaHex(instBytes))
	src.set(instBytes, false)

	rec := &progressRecorder{}
	err := m.Download(newTxnID(), "v1.3.5", rec.cb())
	if err == nil || !strings.Contains(err.Error(), "安装布局无效") {
		t.Fatalf("布局自检应拒装, got %v", err)
	}
	if st := rec.stages(); !strings.Contains(strings.Join(st, ","), "error") {
		t.Errorf("布局失败应经 error 词表送达: %v", st)
	}
}

// TestCrossCheckManifestConflict SHA256SUMS.txt 与 GitHub digest 两个官方来源
// 互相矛盾 → 硬失败（下载链路疑似被篡改），不进入安装段。
func TestCrossCheckManifestConflict(t *testing.T) {
	src := newInstallerSource(t)
	m := newChainManager(t, src)
	instBytes := []byte("nsis-installer:v1.3.5")
	seedRemote(t, "v1.3.5", installerAssetName, int64(len(instBytes)), shaHex(instBytes))
	src.set(instBytes, false)
	src.setSums([]byte(shaHex([]byte("DIFFERENT-OFFICIAL-SOURCE")) + "  " + installerAssetName + "\n"))

	err := m.Download(newTxnID(), "v1.3.5", nil)
	if err == nil || !strings.Contains(err.Error(), "交叉比对失败") {
		t.Fatalf("两官方源矛盾应硬失败, got %v", err)
	}
	if !src.sumsRequested() {
		t.Error("清单请求应发出（比对确实执行）")
	}
}

// TestCrossCheckManifestConsistentOrAbsent 清单一致放行、清单缺席降级为单一
// 官方源（digest 已双核）——两种形态都必须装成功。
func TestCrossCheckManifestConsistentOrAbsent(t *testing.T) {
	src := newInstallerSource(t)
	m := newChainManager(t, src)
	instBytes := []byte("nsis-installer:v1.3.5")
	seedRemote(t, "v1.3.5", installerAssetName, int64(len(instBytes)), shaHex(instBytes))
	src.set(instBytes, false)
	src.setSums([]byte(shaHex(instBytes) + " *" + installerAssetName + "\n"))
	if err := m.Download(newTxnID(), "v1.3.5", nil); err != nil {
		t.Fatalf("清单一致应放行: %v", err)
	}

	if err := m.Download(newTxnID(), "v1.3.5", nil); err != nil {
		// 第二笔：源仍带清单，走同一放行路径（幂等覆盖安装）
		t.Fatalf("重复覆盖安装应成功: %v", err)
	}
	src.setSums(nil) // 清单 404：降级单一官方源，不阻断
	if err := m.Download(newTxnID(), "v1.3.5", nil); err != nil {
		t.Fatalf("清单缺席应降级放行: %v", err)
	}
}

// TestShortcutsPurgedAfterInstallAndRemove 快捷方式清理接线：静默安装成功后
// （安装器顺手建的桌面/开始菜单快捷方式，防绕过托管）与卸载后各清理一次，
// versionsDir 如实传参。
func TestShortcutsPurgedAfterInstallAndRemove(t *testing.T) {
	src := newInstallerSource(t)
	m := newChainManager(t, src)
	var purged []string
	withPurge(t, func(versionsDir, _ string) {
		purged = append(purged, versionsDir)
	})
	installViaChain(t, m, src, "v1.3.5")
	if err := m.Remove("v1.3.5"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if len(purged) != 2 || purged[0] != m.versionsDir || purged[1] != m.versionsDir {
		t.Fatalf("安装/卸载后应各清理一次快捷方式: %v", purged)
	}
}

// TestDownloadRejectsUnknownVersion 远程列表没有的目标版本直接拒绝，不发请求。
func TestDownloadRejectsUnknownVersion(t *testing.T) {
	src := newInstallerSource(t)
	m := newChainManager(t, src)
	seedRemote(t, "v1.3.3", installerAssetName, 10, shaHex([]byte("x")))
	src.set([]byte("whatever"), false)

	err := m.Download(newTxnID(), "v9.9.9", nil)
	if err == nil || !strings.Contains(err.Error(), "远程列表不存在版本") {
		t.Fatalf("未知版本应拒绝, got %v", err)
	}
	if hits := src.count(); hits != 0 {
		t.Errorf("解析失败时不得发起下载, hits = %d", hits)
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
			t.Errorf("托管根残留事务/临时件: %s", name)
		}
	}
}
