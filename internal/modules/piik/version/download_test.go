// 下载→校验→解包→落位全链的失败注入测试：以回环 httptest 服务器为"源"，
// 真实驱动内核 artifact.Fetch/UnpackZip/Tree 路径（真 tmp 目录，零真网），
// 验证中断、坏摘要、无摘要拒装、半件残留清理、布局不变式拒装（runtime 双
// exe 缺位）、落位摘要记账与 mtime 闸控漂移复算、同版本异摘要防漂移、
// 双版本共存等收口行为（DBX 黄金样本同构，Piik 领域化：裸名资产、平铺
// 十条目布局、REVISION 记账）。
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

// ---------- 测试夹具：假资产源 ----------

// zipSource 可编程假资产源：按请求路径分发便携 zip，支持截断（模拟中断）
// 与命中计数。Piik 无备用摘要清单链（sidecar 停发且不可信），不设清单分发。
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
	if strings.Contains(r.URL.Path, assetName) {
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
	m.mirrors = func(_, asset string) []string {
		return []string{src.srv.URL + "/releases/download/v1.2.0/" + asset}
	}
	return m
}

// buildPiikZip 实证根平铺便携 zip 字节（主 exe 内容由 exeContent 决定，
// 便于篡改复算路径）。
func buildPiikZip(t *testing.T, exeContent string) []byte {
	t.Helper()
	entries := piikZipEntries()
	entries[exeName] = exeContent
	path := makeTestZip(t, entries)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// seedReleaseFor 按 zip 字节注入远程条目（摘要=官方口径实测 zip sha）。
func seedReleaseFor(body []byte) PiikRelease {
	return PiikRelease{
		Version:   "v1.2.0",
		Published: "2026-09-24T08:00:00Z",
		AssetName: assetName,
		AssetURL:  "https://github.com/TNTcraftHIM/Piik/releases/download/v1.2.0/" + assetName,
		Size:      int64(len(body)),
		SHA256:    shaHex(body),
	}
}

// progressTracker 记录进度词表序列。
type progressTracker struct {
	mu     sync.Mutex
	stages []string
}

func (p *progressTracker) fn() func(DownloadProgress) {
	return func(d DownloadProgress) {
		p.mu.Lock()
		defer p.mu.Unlock()
		p.stages = append(p.stages, d.Stage)
	}
}

func (p *progressTracker) join() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return strings.Join(p.stages, ",")
}

// ---------- 正链 ----------

func TestDownloadHappyPath(t *testing.T) {
	exeContent := "fake-piik-app-v120"
	body := buildPiikZip(t, exeContent)
	src := newZipSource(t)
	src.setZip(body, false)
	m := newChainManager(t, src)
	seedRemote(t, []PiikRelease{seedReleaseFor(body)})

	tracker := &progressTracker{}
	if err := m.Download("txn-happy", "v1.2.0", tracker.fn()); err != nil {
		t.Fatalf("Download: %v", err)
	}
	// 词表序列：downloading → extract → done（verify 由内核折进 downloading，不造幻影）
	st := tracker.join()
	if !strings.Contains(st, "downloading") || !strings.Contains(st, "extract") || !strings.HasSuffix(st, "done") {
		t.Errorf("进度词表漂移: %s", st)
	}

	verDir := filepath.Join(m.versionsDir, "piik_1.2.0")
	// 平铺自管目录解包：条目直落版本根（无包裹层），布局不变式在位
	if err := checkPiikLayout(verDir); err != nil {
		t.Fatalf("落位布局不完整: %v", err)
	}
	// 内核账本 + 模块账本双账
	rawMeta, err := os.ReadFile(filepath.Join(verDir, "meta.json"))
	if err != nil {
		t.Fatal(err)
	}
	var meta artifact.Meta
	if err := json.Unmarshal(rawMeta, &meta); err != nil {
		t.Fatalf("内核账本形状异常: %v", err)
	}
	if meta.Entry != exeName || meta.Source != artifact.SourceRemote ||
		meta.ZipSHA256 != shaHex(body) || meta.AssetSHA256 != shaHex([]byte(exeContent)) ||
		meta.InstalledAt.IsZero() {
		t.Errorf("内核账本漂移: %+v", meta)
	}
	mm := readModuleMeta(verDir)
	if !mm.VerifiedHash || mm.IsImport || mm.Revision != "cafe1234deadbeef" {
		t.Errorf("模块账本漂移: %+v", mm)
	}

	// ListInstalled 投影
	list, err := m.ListInstalled()
	if err != nil || len(list) != 1 {
		t.Fatalf("ListInstalled: %v / %d 条", err, len(list))
	}
	info := list[0]
	if info.Version != "v1.2.0" || !info.VerifiedHash || info.IsImport ||
		info.SHA256 != shaHex([]byte(exeContent)) || info.Revision != "cafe1234deadbeef" ||
		info.InstalledAt == "" || info.Size <= 0 ||
		strings.Contains(info.DriftNote, "账外漂移") {
		t.Errorf("已装投影漂移: %+v", info)
	}
	// 刚落位：mtime 不晚于落位时刻 → 成本闸关闭，复算未触发
	if info.HashDrifted || !strings.Contains(info.DriftNote, "复算未触发") {
		t.Errorf("刚落位应走 mtime 成本闸不触发复算: %+v", info)
	}
}

// ---------- 失败注入 ----------

func TestDownloadTruncatedAndRetry(t *testing.T) {
	body := buildPiikZip(t, "fake-piik-app")
	src := newZipSource(t)
	src.setZip(body, true)
	m := newChainManager(t, src)
	seedRemote(t, []PiikRelease{seedReleaseFor(body)})

	tracker := &progressTracker{}
	if err := m.Download("txn-trunc", "v1.2.0", tracker.fn()); err == nil {
		t.Fatal("截断源必须失败")
	}
	if !strings.Contains(tracker.join(), "error") {
		t.Errorf("失败应上报 error 阶段: %s", tracker.join())
	}
	// 半件不留：版本目录与 staging 残骸都不得存在
	if _, err := os.Stat(filepath.Join(m.versionsDir, "piik_1.2.0")); err == nil {
		t.Error("失败下载不得落位版本目录")
	}
	assertNoStaging(t, m.versionsDir)

	// 修复源后重试成功（同事务 ID 复用亦不残留）
	src.setZip(body, false)
	if err := m.Download("txn-trunc", "v1.2.0", nil); err != nil {
		t.Fatalf("重试应成功: %v", err)
	}
}

func TestDownloadDigestMismatchRejects(t *testing.T) {
	body := buildPiikZip(t, "fake-piik-app")
	src := newZipSource(t)
	// 源给的是另一件包（同版本内容漂移的正牌攻击面）：digest 不匹配必拒
	src.setZip(buildPiikZip(t, "trojan-piik-app"), false)
	m := newChainManager(t, src)
	seedRemote(t, []PiikRelease{seedReleaseFor(body)})

	if err := m.Download("txn-mismatch", "v1.2.0", nil); err == nil {
		t.Fatal("官方摘要不匹配必须拒装")
	}
	assertNoStaging(t, m.versionsDir)
	if _, err := os.Stat(filepath.Join(m.versionsDir, "piik_1.2.0")); err == nil {
		t.Error("摘要不匹配的包不得落位")
	}
}

func TestDownloadNoSHARefused(t *testing.T) {
	// 列表层已挡无摘要 release；Download 侧的纵深防御直灌缓存验证
	body := buildPiikZip(t, "fake-piik-app")
	src := newZipSource(t)
	src.setZip(body, false)
	m := newChainManager(t, src)
	rel := seedReleaseFor(body)
	rel.SHA256 = ""
	seedRemote(t, []PiikRelease{rel})

	err := m.Download("txn-nosha", "v1.2.0", nil)
	if err == nil || !strings.Contains(err.Error(), "拒绝无校验安装") {
		t.Fatalf("无官方摘要必须拒装，得 %v", err)
	}
}

func TestDownloadBadLayoutRefused(t *testing.T) {
	// runtime 双 exe 缺位的"完整但残缺"包：摘要校验通过也必须被布局不变式拒下
	entries := piikZipEntries()
	delete(entries, cloudflaredExeRel)
	path := makeTestZip(t, entries)
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	src := newZipSource(t)
	src.setZip(body, false)
	m := newChainManager(t, src)
	seedRemote(t, []PiikRelease{seedReleaseFor(body)})

	err = m.Download("txn-badlayout", "v1.2.0", nil)
	if err == nil || !strings.Contains(err.Error(), "cloudflared") {
		t.Fatalf("布局不变式应拒装并点名缺位件，得 %v", err)
	}
	assertNoStaging(t, m.versionsDir)
}

func TestDownloadSameVersionDifferentDigestRefused(t *testing.T) {
	body := buildPiikZip(t, "fake-piik-app-v1")
	src := newZipSource(t)
	src.setZip(body, false)
	m := newChainManager(t, src)
	seedRemote(t, []PiikRelease{seedReleaseFor(body)})
	if err := m.Download("txn-1", "v1.2.0", nil); err != nil {
		t.Fatalf("首装: %v", err)
	}

	// 同版本异内容（上游重打 tag 的漂移场景）：Commit 摘要闸拒覆盖
	body2 := buildPiikZip(t, "fake-piik-app-v2")
	src.setZip(body2, false)
	seedRemote(t, []PiikRelease{seedReleaseFor(body2)})
	err := m.Download("txn-2", "v1.2.0", nil)
	if err == nil || !strings.Contains(err.Error(), "漂移") {
		t.Fatalf("同版本异摘要必须拒覆盖，得 %v", err)
	}
	// 已装内容仍是第一件
	info, _ := m.ListInstalled()
	if len(info) != 1 || info[0].SHA256 != shaHex([]byte("fake-piik-app-v1")) {
		t.Errorf("拒覆盖后已装账目应原样: %+v", info)
	}
}

func TestDownloadIdempotent(t *testing.T) {
	body := buildPiikZip(t, "fake-piik-app")
	src := newZipSource(t)
	src.setZip(body, false)
	m := newChainManager(t, src)
	seedRemote(t, []PiikRelease{seedReleaseFor(body)})

	if err := m.Download("txn-a", "v1.2.0", nil); err != nil {
		t.Fatalf("首装: %v", err)
	}
	// 同版本同摘要：幂等复用，不报错不落双目录
	if err := m.Download("txn-b", "v1.2.0", nil); err != nil {
		t.Fatalf("同摘要重装应幂等: %v", err)
	}
	entries, _ := os.ReadDir(m.versionsDir)
	count := 0
	for _, e := range entries {
		if e.IsDir() && e.Name() == "piik_1.2.0" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("应仅一个版本目录: %v", entries)
	}
}

func TestDownloadUnknownVersion(t *testing.T) {
	m := NewManager(t.TempDir())
	seedRemote(t, nil)
	err := m.Download("txn-x", "v9.9.9", nil)
	if err == nil || !strings.Contains(err.Error(), "不存在") {
		t.Fatalf("远程无此版本应报错，得 %v", err)
	}
}

// ---------- 漂移护栏（落位 sha256 记账 + mtime 闸控复算） ----------

func TestDriftGuardPaths(t *testing.T) {
	const exeContent = "original-exe-bytes"
	body := buildPiikZip(t, exeContent)
	src := newZipSource(t)
	src.setZip(body, false)
	m := newChainManager(t, src)
	seedRemote(t, []PiikRelease{seedReleaseFor(body)})
	if err := m.Download("txn-drift", "v1.2.0", nil); err != nil {
		t.Fatalf("首装: %v", err)
	}
	exe := filepath.Join(m.versionsDir, "piik_1.2.0", exeName)

	// 无改动迹象 → 复算未触发（成本闸）
	if drifted, note := m.VerifyLedger("v1.2.0"); drifted || !strings.Contains(note, "复算未触发") {
		t.Errorf("刚落位应不触发复算: %v / %s", drifted, note)
	}

	// 账外漂移：改内容 + mtime 推后（上游自更新/手工换件的正牌场景）→ 如实报漂
	writeExe(t, exe, "replaced-by-someone")
	if drifted, note := m.VerifyLedger("v1.2.0"); !drifted || !strings.Contains(note, "账外漂移") {
		t.Errorf("篡改应报账外漂移: %v / %s", drifted, note)
	}
	list, _ := m.ListInstalled()
	if len(list) != 1 || !list[0].HashDrifted || !strings.Contains(list[0].DriftNote, "账外漂移") {
		t.Errorf("ListInstalled 漂移投影缺失: %+v", list)
	}

	// 迹象仍在但内容复位：复算一致（改动迹象经全量哈希排除）
	writeExe(t, exe, exeContent)
	if drifted, note := m.VerifyLedger("v1.2.0"); drifted || !strings.Contains(note, "复算一致") {
		t.Errorf("复位后应报复算一致: %v / %s", drifted, note)
	}

	// 账本无摘要的异端安装：无法比对，不猜没猜
	orphan := filepath.Join(m.versionsDir, "piik_9.9.9")
	if err := os.MkdirAll(orphan, 0755); err != nil {
		t.Fatal(err)
	}
	writeExe(t, filepath.Join(orphan, exeName), "no-ledger")
	writeExe(t, filepath.Join(orphan, captureExeRel), "c")
	writeExe(t, filepath.Join(orphan, cloudflaredExeRel), "f")
	for _, n := range []string{"LICENSE", revisionFileName, "NOTICES"} {
		writeExe(t, filepath.Join(orphan, n), "x")
	}
	if drifted, note := m.VerifyLedger("v9.9.9"); drifted || !strings.Contains(note, "无法比对") {
		t.Errorf("无账摘要应报无法比对: %v / %s", drifted, note)
	}
}

// writeExe 写文件并把 mtime 推到未来（触发漂移复算的"改动迹象"闸）。
func writeExe(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0755); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(1 * time.Minute)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatal(err)
	}
}

// assertNoStaging 版本根不得残留 .tmp-* 中转目录（discard 收口纪律）。
func assertNoStaging(t *testing.T, root string) {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".tmp-") || strings.Contains(e.Name(), ".removing-") {
			t.Errorf("事务残骸未清理: %s", e.Name())
		}
	}
}

// ---------- 版本八件套其余面 ----------

func TestRemoveAndResolveExe(t *testing.T) {
	body := buildPiikZip(t, "fake-piik-app")
	src := newZipSource(t)
	src.setZip(body, false)
	m := newChainManager(t, src)
	seedRemote(t, []PiikRelease{seedReleaseFor(body)})
	if err := m.Download("txn-r", "v1.2.0", nil); err != nil {
		t.Fatal(err)
	}

	exe, err := m.ResolveExe("v1.2.0")
	if err != nil || exe != filepath.Join(m.versionsDir, "piik_1.2.0", exeName) {
		t.Fatalf("ResolveExe: %v %s", err, exe)
	}
	// 非法令牌（路径穿越）先于磁盘访问被拒
	if _, err := m.ResolveExe("../../etc"); err == nil {
		t.Error("路径穿越令牌必须拒")
	}
	if _, err := m.ResolveExe("v1.3.0"); err == nil {
		t.Error("未装版本必须拒")
	}
	if err := m.Remove("v1.2.0"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	list, _ := m.ListInstalled()
	if len(list) != 0 {
		t.Errorf("卸载后仍列出: %+v", list)
	}
}
