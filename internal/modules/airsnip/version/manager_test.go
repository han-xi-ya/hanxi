// 下载→校验→解包→布局自检→落位全链与本地导入的失败注入测试：以回环 httptest
// 服务器为"源"，真实驱动内核 artifact.Fetch/UnpackZipContext/Tree 路径
// （真 tmp 目录，无外网依赖）。zip 形状取**上游真实形状**：无根目录、exe 躺在包根
// （package.ps1 用 Compress-Archive -Path dist\<name>\* 打包，实测条目无包装层），
// 另备单层包装变体验证布局吸收。
//
// 本模块与 litemonitor/ccswitch 的关键差异在此有专门用例：**没有 PE FileVersion
// 核对层**（上游 release 未嵌 VERSIONINFO 资源），任何"读 PE 版本"的假象都不允许
// 出现在链路上，落位可信度由官方摘要 + 布局自检承担。
package version

import (
	"archive/zip"
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

	"context"
)

// ---------- 测试夹具：远程列表注入与假资产源 ----------

// seedRemote 注入伪远程列表。remoteCache 是包内全局缓存（生产语义即进程级单例），
// 本包测试串行执行（不使用 t.Parallel），互相隔离靠 setup 重灌。
func seedRemote(t *testing.T, version, assetName string, body []byte) {
	t.Helper()
	sum := sha256.Sum256(body)
	remoteCache.mu.Lock()
	defer remoteCache.mu.Unlock()
	remoteCache.data = []AirsnipRelease{{
		Version:   version,
		AssetName: assetName,
		AssetURL:  "http://127.0.0.1:0/placeholder",
		Size:      int64(len(body)),
		SHA256:    hex.EncodeToString(sum[:]),
	}}
	remoteCache.fetchedAt = time.Now() // 处于 TTL 内，Download 不再走网络拉列表
}

// zipSource 可编程假资产源：按当前 body 提供下载，支持掐断（传输中断）与命中计数。
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

func (z *zipSource) serve(w http.ResponseWriter, _ *http.Request) {
	z.mu.Lock()
	body, truncate := z.body, z.truncate
	z.hits++
	z.mu.Unlock()

	w.Header().Set("Content-Length", fmt.Sprint(len(body)))
	if truncate {
		_, _ = w.Write(body[:len(body)/2])
		panic(http.ErrAbortHandler) // 掐断连接：客户端收到短读
	}
	_, _ = w.Write(body)
}

func (z *zipSource) set(body []byte, truncate bool) {
	z.mu.Lock()
	defer z.mu.Unlock()
	z.body, z.truncate = body, truncate
}

// newChainManager 构造指向假源的 Manager：只把镜像 URL 接缝改指回环 http，
// 下载/校验/解包/落位全链仍走生产实现（本模块没有 PE 接缝可注，也不该有）。
func newChainManager(t *testing.T, src *zipSource) *Manager {
	t.Helper()
	m := NewManager(t.TempDir())
	m.mirrors = func(_, _ string) []string { return []string{src.srv.URL + "/airsnip.zip"} }
	return m
}

// ---------- zip 构造器（上游两种真实形状） ----------

// zipEntry 包内条目。
type zipEntry struct {
	name string
	body string
}

// buildZip 按给定条目造 zip（条目名即包内路径，含 "/" 时造出目录层级）。
func buildZip(t *testing.T, entries []zipEntry) []byte {
	t.Helper()
	tmp := filepath.Join(t.TempDir(), "built.zip")
	f, err := os.Create(tmp)
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)
	for _, e := range entries {
		hw, err := w.Create(e.name)
		if err != nil {
			t.Fatalf("写入条目 %s 失败: %v", e.name, err)
		}
		if _, err := hw.Write([]byte(e.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()
	data, err := os.ReadFile(tmp)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// flatZip 现行上游形状：exe + 三份文档全在包根。
func flatZip(t *testing.T) []byte {
	t.Helper()
	return buildZip(t, []zipEntry{
		{"airsnip.exe", "MZ-fake-exe-0.7.0"},
		{"LICENSE", "GPL-3.0"},
		{"README.md", "# AirSnip"},
		{"THIRD-PARTY.md", "third party"},
	})
}

// nestedZip 包装一层目录的变体（改名前历史包与用户自行再打包常见）。
func nestedZip(t *testing.T) []byte {
	t.Helper()
	return buildZip(t, []zipEntry{
		{"airsnip-v0.7.0-win-x64/airsnip.exe", "MZ-fake-exe-0.7.0"},
		{"airsnip-v0.7.0-win-x64/LICENSE", "GPL-3.0"},
	})
}

// ---------- 下载全链 ----------

// TestDownloadFlatLayout 无根目录的真形状装进 airsnip_0.7.0/，exe 与文档同层，
// 账本记下 zip 与 exe 双摘要。
func TestDownloadFlatLayout(t *testing.T) {
	body := flatZip(t)
	src := newZipSource(t)
	src.set(body, false)
	seedRemote(t, "v0.7.0", "airsnip-v0.7.0-win-x64.zip", body)
	m := newChainManager(t, src)

	var stages []string
	if err := m.Download("txn-flat", "v0.7.0", func(p DownloadProgress) { stages = append(stages, p.Stage) }); err != nil {
		t.Fatalf("下载链失败: %v", err)
	}
	exe := filepath.Join(m.versionsDir, "airsnip_0.7.0", "airsnip.exe")
	if _, err := os.Stat(exe); err != nil {
		t.Fatalf("落位后 exe 不在场: %v", err)
	}
	// 文档随包同层落位（GPL 许可文本属用户应得的合规材料）
	if _, err := os.Stat(filepath.Join(m.versionsDir, "airsnip_0.7.0", "LICENSE")); err != nil {
		t.Errorf("LICENSE 未随包落位: %v", err)
	}
	joined := strings.Join(stages, ",")
	for _, want := range []string{"downloading", "extract", "done"} {
		if !strings.Contains(joined, want) {
			t.Errorf("进度词表缺少 %s（实际 %q）", want, joined)
		}
	}
	installed, err := m.ListInstalled()
	if err != nil || len(installed) != 1 {
		t.Fatalf("ListInstalled 应见 1 版，got %+v err=%v", installed, err)
	}
	if installed[0].Version != "v0.7.0" {
		t.Errorf("版本令牌不符: %+v", installed[0])
	}
}

// TestDownloadNestedLayout 单层包装目录必须被吸收（不能把 airsnip.exe 留在
// 子目录里让 ResolveExe 找不到）。
func TestDownloadNestedLayout(t *testing.T) {
	body := nestedZip(t)
	src := newZipSource(t)
	src.set(body, false)
	seedRemote(t, "v0.7.0", "airsnip-v0.7.0-win-x64.zip", body)
	m := newChainManager(t, src)

	if err := m.Download("txn-nested", "v0.7.0", nil); err != nil {
		t.Fatalf("嵌套布局未被吸收: %v", err)
	}
	exe, err := m.ResolveExe("v0.7.0")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(filepath.Dir(exe)) != "airsnip_0.7.0" {
		t.Errorf("exe 应直接位于版本隔离目录内，实际 %s", exe)
	}
}

// TestDownloadRejectsBadDigest 摘要不符必须拒装且不留目录（官方摘要是信任根）。
func TestDownloadRejectsBadDigest(t *testing.T) {
	body := flatZip(t)
	src := newZipSource(t)
	src.set(body, false)
	seedRemote(t, "v0.7.0", "airsnip-v0.7.0-win-x64.zip", body)
	m := newChainManager(t, src)
	// 篡改源字节（远程列表里的摘要仍是原字节的），等价于镜像投毒/传输损坏
	src.set(append([]byte{}, append(body[:len(body)-1], 'X')...), false)

	if err := m.Download("txn-bad", "v0.7.0", nil); err == nil {
		t.Fatal("摘要不符却报成功")
	}
	if _, err := os.Stat(filepath.Join(m.versionsDir, "airsnip_0.7.0")); !os.IsNotExist(err) {
		t.Errorf("失败安装不得留下版本目录（err=%v）", err)
	}
	left, _ := m.ListInstalled()
	if len(left) != 0 {
		t.Errorf("失败安装不应可列: %+v", left)
	}
}

// TestDownloadInterrupted 传输中断：短读收口为失败，半件不残留。
func TestDownloadInterrupted(t *testing.T) {
	body := flatZip(t)
	src := newZipSource(t)
	src.set(body, true)
	seedRemote(t, "v0.7.0", "airsnip-v0.7.0-win-x64.zip", body)
	m := newChainManager(t, src)

	if err := m.Download("txn-cut", "v0.7.0", nil); err == nil {
		t.Fatal("掐断连接应当失败")
	}
	vers, _ := m.ListInstalled()
	if len(vers) != 0 {
		t.Errorf("中断不应留下可列版本: %+v", vers)
	}
}

// TestDownloadRejectsMissingExe 包内没有 exe：布局自检拒收（zip 是别的东西）。
func TestDownloadRejectsMissingExe(t *testing.T) {
	body := buildZip(t, []zipEntry{{"README.md", "只有文档"}, {"LICENSE", "GPL-3.0"}})
	src := newZipSource(t)
	src.set(body, false)
	seedRemote(t, "v0.7.0", "airsnip-v0.7.0-win-x64.zip", body)
	m := newChainManager(t, src)

	err := m.Download("txn-noexe", "v0.7.0", nil)
	if err == nil || !strings.Contains(err.Error(), "airsnip.exe") {
		t.Fatalf("缺 exe 应报布局错误并点名 airsnip.exe，got %v", err)
	}
}

// TestDownloadRejectsAmbiguousLayout 根与子目录各有一份 exe：布局二义，拒收不猜。
func TestDownloadRejectsAmbiguousLayout(t *testing.T) {
	body := buildZip(t, []zipEntry{
		{"airsnip.exe", "MZ-root"},
		{"sub/airsnip.exe", "MZ-sub"},
	})
	src := newZipSource(t)
	src.set(body, false)
	seedRemote(t, "v0.7.0", "airsnip-v0.7.0-win-x64.zip", body)
	m := newChainManager(t, src)

	if err := m.Download("txn-amb", "v0.7.0", nil); err == nil {
		t.Fatal("二义布局应当拒收")
	}
}

// TestDownloadUnknownVersion 远程列表里没有的版本：立即失败，不发请求。
func TestDownloadUnknownVersion(t *testing.T) {
	body := flatZip(t)
	src := newZipSource(t)
	src.set(body, false)
	seedRemote(t, "v0.7.0", "airsnip-v0.7.0-win-x64.zip", body)
	m := newChainManager(t, src)

	if err := m.Download("txn-x", "v9.9.9", nil); err == nil {
		t.Fatal("未知版本应当失败")
	}
	if src.hits != 0 {
		t.Errorf("目标版本不在列表时不应发起资产请求，hits=%d", src.hits)
	}
}

// TestDownloadCancelable 事务取消：ctx 已取消时不得继续下载（P0 批 2b 生命周期）。
func TestDownloadCancelable(t *testing.T) {
	body := flatZip(t)
	src := newZipSource(t)
	src.set(body, false)
	seedRemote(t, "v0.7.0", "airsnip-v0.7.0-win-x64.zip", body)
	m := newChainManager(t, src)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := m.DownloadContext(ctx, "txn-cancel", "v0.7.0", nil); err == nil {
		t.Fatal("已取消事务应当直接失败")
	}
	if src.hits != 0 {
		t.Errorf("取消后不得发起下载，hits=%d", src.hits)
	}
}

// ---------- 版本树读扫 ----------

// TestListInstalledSkipsCorrupt 空 exe / 缺 exe / 形状外目录一律跳过，
// imported- 兜底目录沉底（versioncmp 对非数字段退化字典序，不闸则反被当最新）。
func TestListInstalledSkipsCorrupt(t *testing.T) {
	root := t.TempDir()
	mk := func(dir, exeName, content string) {
		d := filepath.Join(root, dir)
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatal(err)
		}
		if exeName != "" {
			if err := os.WriteFile(filepath.Join(d, exeName), []byte(content), 0644); err != nil {
				t.Fatal(err)
			}
		}
	}
	mk("airsnip_0.7.0", "airsnip.exe", "MZ")
	mk("airsnip_0.10.0", "airsnip.exe", "MZ")
	mk("airsnip_0.9.0", "airsnip.exe", "")    // 空文件 = 损坏安装
	mk("airsnip_1.0.0", "", "")               // 缺 exe = 损坏安装
	mk("airsnip_v0.8.0", "airsnip.exe", "MZ") // 带 v 前缀的非法令牌 = 不列入
	mk("airsnip_imported-abcdef123456", "airsnip.exe", "MZ")

	m := NewManager(root)
	list, err := m.ListInstalled()
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, v := range list {
		got = append(got, v.Version)
	}
	want := []string{"v0.10.0", "v0.7.0", "vimported-abcdef123456"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("已装列表形状/排序不符\n got=%v\nwant=%v", got, want)
	}
}

// TestResolveVersionDirRejectsTraversal 路径穿越与形状外令牌在任何磁盘访问前被拒。
func TestResolveVersionDirRejectsTraversal(t *testing.T) {
	m := NewManager(t.TempDir())
	for _, bad := range []string{"../../etc", "..", "0.7", "v1.2.3.4", "imported-XYZ", ""} {
		if _, err := m.ResolveExe(bad); err == nil {
			t.Errorf("非法令牌 %q 竟被放行", bad)
		}
	}
}

// TestRemoveUninstalledVersion 未安装版本给出可操作的中文指引。
func TestRemoveUninstalledVersion(t *testing.T) {
	m := NewManager(t.TempDir())
	err := m.Remove("v0.7.0")
	if err == nil || !strings.Contains(err.Error(), "未安装") {
		t.Fatalf("应报\"未安装\"指引，got %v", err)
	}
}

// ---------- 本地导入 ----------

// TestImportLocalFromName 官方包名/解压目录名自带版本号时按名收录（**不读 PE**：
// 上游 exe 无 VERSIONINFO，读 PE 这条路在本模块不存在）。
func TestImportLocalFromName(t *testing.T) {
	src := filepath.Join(t.TempDir(), "airsnip-v0.7.0-win-x64")
	if err := os.MkdirAll(src, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "airsnip.exe"), []byte("MZ-fake"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "LICENSE"), []byte("GPL-3.0"), 0644); err != nil {
		t.Fatal(err)
	}

	m := NewManager(t.TempDir())
	info, err := m.ImportLocal(src)
	if err != nil {
		t.Fatal(err)
	}
	if info.Version != "v0.7.0" || !info.IsImport {
		t.Errorf("导入版本/标记不符: %+v", info)
	}
	if _, err := os.Stat(info.ExePath); err != nil {
		t.Errorf("导入后 exe 未落位: %v", err)
	}
	if _, err := os.Stat(filepath.Join(info.Dir, "LICENSE")); err != nil {
		t.Errorf("许可证文档应随导入落位: %v", err)
	}
	// 导入账本 installedAt/isImport/source 三件套
	installed, err := m.ListInstalled()
	if err != nil || len(installed) != 1 || installed[0].InstalledAt == "" {
		t.Errorf("导入账本不可用: %+v err=%v", installed, err)
	}
}

// TestImportLocalSingleWrapper 源目录多一层包装目录（用户解压后的常见形态）也要命中。
func TestImportLocalSingleWrapper(t *testing.T) {
	outer := t.TempDir()
	inner := filepath.Join(outer, "jietu-v0.5.0-win-x64")
	if err := os.MkdirAll(inner, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(inner, "airsnip.exe"), []byte("MZ"), 0644); err != nil {
		t.Fatal(err)
	}
	m := NewManager(t.TempDir())
	info, err := m.ImportLocal(outer)
	if err != nil {
		t.Fatal(err)
	}
	if info.Version != "v0.5.0" {
		t.Errorf("应从包装目录名嗅到 0.5.0，got %+v", info)
	}
}

// TestImportLocalHashFallback 名字里嗅不到版本（用户改名存放）→ 内容哈希兜底，
// 同一份文件重复导入必须被"已安装"挡住（哈希与时钟无关）。
func TestImportLocalHashFallback(t *testing.T) {
	src := t.TempDir()
	exe := filepath.Join(src, "airsnip.exe")
	if err := os.WriteFile(exe, []byte("MZ-unknown-build"), 0644); err != nil {
		t.Fatal(err)
	}
	m := NewManager(t.TempDir())
	info, err := m.ImportLocal(src)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(info.Version, "vimported-") {
		t.Errorf("版本探测失败应落 imported- 哈希兜底，got %q", info.Version)
	}
	if _, err := m.ImportLocal(src); err == nil || !strings.Contains(err.Error(), "已安装") {
		t.Errorf("同内容重复导入应被拒绝，got %v", err)
	}
	// 兜底目录在列表里沉底，不被当成"最新已装"
	list, _ := m.ListInstalled()
	if len(list) != 1 || list[0].Version != info.Version {
		t.Fatalf("兜底目录应可列且唯一: %+v", list)
	}
}

// TestImportLocalRejectsNoExe 源目录没有 exe：明确失败，不造空版本目录。
func TestImportLocalRejectsNoExe(t *testing.T) {
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "readme.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	m := NewManager(t.TempDir())
	if _, err := m.ImportLocal(src); err == nil {
		t.Fatal("缺 exe 的源目录应当失败")
	}
	entries, _ := os.ReadDir(m.versionsDir)
	if len(entries) != 0 {
		t.Errorf("导入失败不应创建版本目录，实际 %d 项", len(entries))
	}
}

// TestVersionFromImportName 名字嗅探的多形态（含改名前 jietu 前缀与裸下划线形态）。
func TestVersionFromImportName(t *testing.T) {
	cases := map[string]string{
		"airsnip-v0.7.0-win-x64": "0.7.0",
		"airsnip_0.6.0":          "0.6.0",
		"jietu-v0.5.0-win-x64":   "0.5.0",
		"下载":                     "",
		"airsnip":                "",
	}
	for name, want := range cases {
		root := filepath.Join(t.TempDir(), name)
		if got := versionFromImportName(root, root); got != want {
			t.Errorf("versionFromImportName(%q) = %q, want %q", name, got, want)
		}
	}
}

// ---------- 落位防漂移与账本 ----------

// TestDownloadSameVersionDifferentDigest 同版本号内容漂移必须被 Tree 拒绝
// （上游用同一 tag 重发不同字节，是本闸存在的唯一理由）。
func TestDownloadSameVersionDifferentDigest(t *testing.T) {
	first := flatZip(t)
	src := newZipSource(t)
	src.set(first, false)
	seedRemote(t, "v0.7.0", "airsnip-v0.7.0-win-x64.zip", first)
	m := newChainManager(t, src)
	if err := m.Download("txn-a", "v0.7.0", nil); err != nil {
		t.Fatal(err)
	}

	second := buildZip(t, []zipEntry{{"airsnip.exe", "MZ-repacked-different"}})
	seedRemote(t, "v0.7.0", "airsnip-v0.7.0-win-x64.zip", second)
	src.set(second, false)
	if err := m.Download("txn-b", "v0.7.0", nil); err == nil {
		t.Fatal("同版本异摘要应当拒装")
	}
}

// TestOpenTreeEntryPrefix 版本树前缀是跨包契约（装配根按它做 journal 背书清理），
// 改名必须让这里变红而不是静默换目录。
func TestOpenTreeEntryPrefix(t *testing.T) {
	tr := OpenTree(t.TempDir())
	if tr.EntryName != "airsnip" {
		t.Errorf("Tree 前缀 = %q, want airsnip", tr.EntryName)
	}
}

// TestDownloadNoPhantomStages 进度词表锁死为 downloading/extract/done：
// 内核把 verify 折进 download、place 折进 done，模块侧不得自造幻影步骤
// （前端共享面板按词表渲染进度条）。
func TestDownloadNoPhantomStages(t *testing.T) {
	body := flatZip(t)
	src := newZipSource(t)
	src.set(body, false)
	seedRemote(t, "v0.7.0", "airsnip-v0.7.0-win-x64.zip", body)
	m := newChainManager(t, src)

	var stages []string
	if err := m.Download("txn-stages", "v0.7.0", func(p DownloadProgress) { stages = append(stages, p.Stage) }); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, s := range stages {
		seen[s] = true
	}
	for s := range seen {
		switch s {
		case "downloading", "extract", "done", "error":
		default:
			t.Errorf("进度词表出现未约定阶段 %q（全部 %v）", s, stages)
		}
	}
	if !seen["downloading"] || !seen["extract"] || !seen["done"] {
		t.Errorf("词表缺阶段: %v", stages)
	}
}

// ---------- CheckUpdate（更新感知 shim） ----------

// TestCheckUpdate 未安装不谎报、装了落后于远程才报更新、imported 兜底不当"最新"。
func TestCheckUpdate(t *testing.T) {
	body := flatZip(t)
	src := newZipSource(t)
	src.set(body, false)
	seedRemote(t, "v0.7.0", "airsnip-v0.7.0-win-x64.zip", body)
	m := newChainManager(t, src)

	// 一台空机器：有远程、无本地 → hasUpdate=false（入口是"下载"不是"更新"）
	local, remote, has, err := m.CheckUpdate(context.Background())
	if err != nil || has || local != "" || remote != "v0.7.0" {
		t.Errorf("空机器判定不符: local=%q remote=%q has=%v err=%v", local, remote, has, err)
	}

	if err := m.Download("txn-up", "v0.7.0", nil); err != nil {
		t.Fatal(err)
	}
	local, remote, has, err = m.CheckUpdate(context.Background())
	if err != nil || has || local != "v0.7.0" || remote != "v0.7.0" {
		t.Errorf("已装最新版不应报更新: local=%q remote=%q has=%v err=%v", local, remote, has, err)
	}
}

// TestCheckUpdateOlderInstalled 已装 0.5.0、远程 0.7.0 → 报更新。
func TestCheckUpdateOlderInstalled(t *testing.T) {
	older := buildZip(t, []zipEntry{{"airsnip.exe", "MZ-0.5.0"}})
	src := newZipSource(t)
	src.set(older, false)
	// 远程列表两条：0.7.0 在前（CheckUpdate 取首个非 pre）
	seedRemote(t, "v0.7.0", "airsnip-v0.7.0-win-x64.zip", flatZip(t))
	remoteCache.mu.Lock()
	remoteCache.data = append(remoteCache.data, AirsnipRelease{Version: "v0.5.0", AssetName: "jietu-v0.5.0-win-x64.zip", Size: int64(len(older)), SHA256: shaHex(older)})
	remoteCache.mu.Unlock()

	m := newChainManager(t, src)
	if err := m.Download("txn-old", "v0.5.0", nil); err != nil {
		t.Fatal(err)
	}
	local, remote, has, err := m.CheckUpdate(context.Background())
	if err != nil || !has || local != "v0.5.0" || remote != "v0.7.0" {
		t.Errorf("落后版本应报更新: local=%q remote=%q has=%v err=%v", local, remote, has, err)
	}
}

func shaHex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
