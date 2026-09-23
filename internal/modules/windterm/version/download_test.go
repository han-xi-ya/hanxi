// 下载→校验→解包→落位全链的失败注入测试：回环 httptest 假源真实驱动
// bespoke 降级链（无官方摘要——上游现状）与 artifact.UnpackZip/Tree 生产路径。
// 特别锁定降级链独有关卡：多镜像回退恢复、字节数断言、流式上限防放大、
// verifiedHash 如实入账、未来 digest 回填自动升格。
package version

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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

// ---------- 夹具 ----------

// seedRemote 注入伪远程列表与官方摘要映射（digest 传空 = 上游现状）。
// remoteCache 为包内全局（生产语义进程级单例），本包测试串行、逐例重灌。
func seedRemote(t *testing.T, version, assetName string, size int64, digest string) {
	t.Helper()
	remoteCache.mu.Lock()
	defer remoteCache.mu.Unlock()
	remoteCache.data = []WindTermRelease{{
		Version:   version,
		AssetName: assetName,
		AssetURL:  "http://127.0.0.1:0/placeholder",
		Size:      size,
	}}
	remoteCache.digests = map[string]string{}
	if digest != "" {
		remoteCache.digests[version] = digest
	}
	remoteCache.fetchedAt = time.Now()
}

// zipSource 可编程假资产源：支持首拍掐线（镜像/重试恢复验证）、恒定截断、
// 体积膨胀（超出账本声明）。
type zipSource struct {
	mu        sync.Mutex
	body      []byte
	truncate  bool
	failFirst bool
	hits      int
	srv       *httptest.Server
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
	if z.failFirst && z.hits > 1 {
		truncate = false // 首拍后恢复：验证换次/换源重试语义
	}
	z.mu.Unlock()

	w.Header().Set("Content-Length", fmt.Sprint(len(body)))
	if truncate {
		_, _ = w.Write(body[:len(body)/2])
		panic(http.ErrAbortHandler)
	}
	_, _ = w.Write(body)
}

func (z *zipSource) set(body []byte, truncate, failFirst bool) {
	z.mu.Lock()
	defer z.mu.Unlock()
	z.body, z.truncate, z.failFirst = body, truncate, failFirst
}

func (z *zipSource) count() int {
	z.mu.Lock()
	defer z.mu.Unlock()
	return z.hits
}

// newChainManager 指向假源的 Manager（镜像 URL 接缝改指回环 http，其余全走生产实现）。
func newChainManager(t *testing.T, src *zipSource) *Manager {
	t.Helper()
	m := NewManager(t.TempDir())
	m.mirrors = func(_, _ string) []string { return []string{src.srv.URL + "/pkg.zip"} }
	return m
}

// buildPortableZip 官方便携包形状：WindTerm_X.Y.Z/ 根目录包裹。
func buildPortableZip(t *testing.T, tag, exeContent string) []byte {
	t.Helper()
	return writeTestZip(t, map[string]string{
		fmt.Sprintf("WindTerm_%s/%s", tag, exeName):                    exeContent,
		fmt.Sprintf("WindTerm_%s/winpty-agent.exe", tag):               "agent",
		fmt.Sprintf("WindTerm_%s/translations/windterm_zh_CN.qm", tag): "qm",
	})
}

func shaHex(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

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

// assertNoLeftovers 版本树根不得留 .tmp-/ 隔离残留。
func assertNoLeftovers(t *testing.T, root string) {
	t.Helper()
	ents, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		t.Fatal(err)
	}
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), ".tmp-") || strings.HasPrefix(e.Name(), ".corrupt-") {
			t.Fatalf("残留半件: %s", e.Name())
		}
	}
}

// ---------- 降级链正例与如实账目 ----------

func TestDownloadUnverifiedChainSuccess(t *testing.T) {
	src := newZipSource(t)
	m := newChainManager(t, src)

	zipBytes := buildPortableZip(t, "2.7.0", "MZ-windterm")
	seedRemote(t, "v2.7.0", "WindTerm_2.7.0_Windows_Portable_x86_64.zip", int64(len(zipBytes)), "")
	src.set(zipBytes, false, false)

	rec := &progressRecorder{}
	txnID := newTxnID()
	if err := m.Download(txnID, "2.7.0", rec.cb()); err != nil {
		t.Fatalf("Download: %v", err)
	}
	wantStages := []string{"downloading", "verify", "extract", "done"}
	got := rec.stages()
	if len(got) != len(wantStages) || got[0] != "downloading" || got[len(got)-1] != "done" {
		t.Fatalf("阶段叙事失真: %v", got)
	}

	list, err := m.ListInstalled()
	if err != nil || len(list) != 1 {
		t.Fatalf("ListInstalled: %+v err=%v", list, err)
	}
	if list[0].VerifiedHash {
		t.Fatal("无官方摘要安装必须记 verifiedHash=false")
	}
	if !strings.HasSuffix(list[0].ExePath, filepath.Join("WindTerm_2.7.0", exeName)) {
		t.Fatalf("嵌套 payload 布局应保持: %s", list[0].ExePath)
	}
	mm := readModuleMeta(list[0].Dir)
	if mm.ComputedSHA != shaHex(zipBytes) || mm.ZipSize != int64(len(zipBytes)) {
		t.Fatalf("自算基线摘要必须入账（重装比对信任根）: %+v", mm)
	}
	if src.count() != 1 {
		t.Fatalf("健康路径不应触发重试: hits=%d", src.count())
	}
	assertNoLeftovers(t, m.versionsDir)
}

// ---------- 传输层收口 ----------

func TestDownloadRecoversViaRetryAfterAbort(t *testing.T) {
	src := newZipSource(t)
	m := newChainManager(t, src)
	zipBytes := buildPortableZip(t, "2.6.1", "MZ-261")
	seedRemote(t, "v2.6.1", "WindTerm_2.6.1_Windows_Portable_x86_64.zip", int64(len(zipBytes)), "")
	src.set(zipBytes, true, true) // 首拍掐线、其后恢复

	if err := m.Download(newTxnID(), "v2.6.1", nil); err != nil {
		t.Fatalf("重试应恢复: %v", err)
	}
	if src.count() < 2 {
		t.Fatalf("必须观察到重试拍: hits=%d", src.count())
	}
	list, _ := m.ListInstalled()
	if len(list) != 1 {
		t.Fatalf("恢复后应成功落位: %+v", list)
	}
}

func TestDownloadInterruptAllAttemptsFail(t *testing.T) {
	src := newZipSource(t)
	m := newChainManager(t, src)
	zipBytes := buildPortableZip(t, "2.7.0", "MZ-x")
	seedRemote(t, "v2.7.0", "WindTerm_2.7.0_Windows_Portable_x86_64.zip", int64(len(zipBytes)), "")
	src.set(zipBytes, true, false) // 恒截断

	rec := &progressRecorder{}
	err := m.Download(newTxnID(), "v2.7.0", rec.cb())
	if err == nil {
		t.Fatal("恒截断必须报错")
	}
	stages := rec.stages()
	if stages[len(stages)-1] != "error" {
		t.Fatalf("失败必须以 error 收口: %v", stages)
	}
	if list, lerr := m.ListInstalled(); lerr != nil || len(list) != 0 {
		t.Fatalf("失败后不得有半件落位: %+v", list)
	}
	assertNoLeftovers(t, m.versionsDir)
}

func TestDownloadSizeMismatchRejected(t *testing.T) {
	src := newZipSource(t)
	m := newChainManager(t, src)
	zipBytes := buildPortableZip(t, "2.7.0", "MZ-x")
	// 账本声明大于实发字节（截断到 3/4 且不报错、Content-Length 撒谎）
	srcBody := zipBytes[:len(zipBytes)*3/4]
	seedRemote(t, "v2.7.0", "WindTerm_2.7.0_Windows_Portable_x86_64.zip", int64(len(zipBytes)), "")
	src.mu.Lock()
	src.body = srcBody // 不设 truncate：EOF 正常但字节数不足
	src.mu.Unlock()

	err := m.Download(newTxnID(), "v2.7.0", nil)
	if err == nil || !strings.Contains(err.Error(), "下载不完整") {
		t.Fatalf("短收必须被字节数关卡拒下: %v", err)
	}
	assertNoLeftovers(t, m.versionsDir)
}

func TestDownloadOverBudgetStreamAborted(t *testing.T) {
	src := newZipSource(t)
	m := newChainManager(t, src)
	zipBytes := buildPortableZip(t, "2.7.0", "MZ-x")
	pad := make([]byte, len(zipBytes)+4096)
	copy(pad, zipBytes)
	for i := len(zipBytes); i < len(pad); i++ {
		pad[i] = 'p' // 异常放大（伪造尾部）
	}
	seedRemote(t, "v2.7.0", "WindTerm_2.7.0_Windows_Portable_x86_64.zip", int64(len(zipBytes)), "") // 声明小、实发大
	src.set(pad, false, false)

	err := m.Download(newTxnID(), "v2.7.0", nil)
	if err == nil || !strings.Contains(err.Error(), "超出期望体积") {
		t.Fatalf("流式上限必须断异常放大: %v", err)
	}
	assertNoLeftovers(t, m.versionsDir)
}

func TestDownloadCtxCancelPrecheck(t *testing.T) {
	src := newZipSource(t)
	m := newChainManager(t, src)
	zipBytes := buildPortableZip(t, "2.7.0", "MZ-x")
	seedRemote(t, "v2.7.0", "WindTerm_2.7.0_Windows_Portable_x86_64.zip", int64(len(zipBytes)), "")
	src.set(zipBytes, false, false)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := m.DownloadContext(ctx, newTxnID(), "v2.7.0", nil); err == nil {
		t.Fatal("已取消 ctx 必须即刻拒")
	}
	if src.count() != 0 {
		t.Fatalf("预检失败不得触网: hits=%d", src.count())
	}
}

// ---------- 布局与账本关卡 ----------

func TestDownloadInvalidLayoutDiscardsStaging(t *testing.T) {
	src := newZipSource(t)
	m := newChainManager(t, src)
	zipBytes := writeTestZip(t, map[string]string{"readme.txt": "没有主程序的包"})
	seedRemote(t, "v0.0.1", "WindTerm_0.0.1_Windows_Portable_x86_64.zip", int64(len(zipBytes)), "")
	src.set(zipBytes, false, false)

	err := m.Download(newTxnID(), "v0.0.1", nil)
	if err == nil || !strings.Contains(err.Error(), "zip 布局无效") {
		t.Fatalf("布局自检必须拒收: %v", err)
	}
	assertNoLeftovers(t, m.versionsDir)
}

func TestReinstallSameVersionRefusedWithoutOfficialDigest(t *testing.T) {
	src := newZipSource(t)
	m := newChainManager(t, src)
	zipBytes := buildPortableZip(t, "2.7.0", "MZ-x")
	seedRemote(t, "v2.7.0", "WindTerm_2.7.0_Windows_Portable_x86_64.zip", int64(len(zipBytes)), "")
	src.set(zipBytes, false, false)

	if err := m.Download(newTxnID(), "v2.7.0", nil); err != nil {
		t.Fatal(err)
	}
	// 降级链账本 ZipSHA256 留空（诚实呈现无官方信任根）：重复下载由
	// Tree.Commit 一律拒绝覆盖（服务层 already-installed 门在前，此处锁死
	// 底层语义：绝不静默覆盖已装内容）
	err := m.Download(newTxnID(), "v2.7.0", nil)
	if err == nil || !strings.Contains(err.Error(), "拒绝覆盖") {
		t.Fatalf("同版本重装必须拒: %v", err)
	}
}

func TestFutureDigestUpgradesToKernelFetch(t *testing.T) {
	zipBytes := buildPortableZip(t, "2.8.0", "MZ-280")
	digest := shaHex(zipBytes)
	seedRemote(t, "v2.8.0", "WindTerm_2.8.0_Windows_Portable_x86_64.zip", int64(len(zipBytes)), digest)

	// 生产镜像 seam（不覆盖）+ 抓 Fetch 接缝：验证"信任根在场即升格内核主流程"
	// 的分支选择、官方摘要透传与 GitHub 直链/镜像模板；内核自身的摘要双核由
	// artifact 包测试矩阵担保。
	m := NewManager(t.TempDir())
	var captured artifact.Source
	m.digestFetcher = func(_ context.Context, s artifact.Source, dest string, _ func(artifact.Progress), _ time.Duration) error {
		captured = s
		return os.WriteFile(dest, zipBytes, 0644)
	}
	if err := m.Download(newTxnID(), "v2.8.0", nil); err != nil {
		t.Fatal(err)
	}
	wantURL := "https://github.com/kingToolbox/WindTerm/releases/download/2.8.0/WindTerm_2.8.0_Windows_Portable_x86_64.zip"
	if captured.URL != wantURL || captured.SHA256 != digest || len(captured.Mirrors) != 3 {
		t.Fatalf("应走内核 Fetch 且带镜像回退: %+v", captured)
	}
	list, _ := m.ListInstalled()
	if len(list) != 1 || !list[0].VerifiedHash {
		t.Fatalf("官方摘要在场必须记 verifiedHash=true: %+v", list)
	}
}
