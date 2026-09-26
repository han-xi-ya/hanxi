package version

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hanxi/packages/go/artifact"
)

func shaHex(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// fakeReleasesJSON 构造与真实 GitHub API 同构的样例响应，覆盖上游实证形态：
// 稳定版（digest 全量）、dev-latest 滚动预发布污染例（资产与稳定版同名同形）、
// v0.8.9 前无 Windows 资产的旧版本例、digest 缺失但有 SHA256SUMS 附件的
// 备用摘要例、双源俱缺例、非规范 tag 例、版本错配挂名资产例。
func fakeReleasesJSON(t *testing.T) []byte {
	t.Helper()
	h := func(ch byte) string { return "sha256:" + strings.Repeat(string(ch), 64) }
	body := `[
  {
    "tag_name": "v0.9.0",
    "published_at": "2026-09-10T08:00:00Z",
    "prerelease": false,
    "draft": false,
    "assets": [
      {"name": "GoNavi-0.9.0-Windows-Amd64-Portable.zip", "url": "https://api.github.com/x/1", "size": 31457280, "digest": "` + h('a') + `"},
      {"name": "GoNavi-0.9.0-Windows-Amd64-Installer.msi", "url": "https://api.github.com/x/2", "size": 33000000, "digest": "` + h('b') + `"},
      {"name": "GoNavi-0.9.0-Windows-Amd64-Portable.exe", "url": "https://api.github.com/x/3", "size": 33000000, "digest": "` + h('c') + `"},
      {"name": "GoNavi-0.9.0-Linux-X86_64.AppImage", "url": "https://api.github.com/x/4", "size": 40000000, "digest": "` + h('d') + `"},
      {"name": "SHA256SUMS", "url": "https://api.github.com/x/5", "size": 200}
    ]
  },
  {
    "tag_name": "dev-latest",
    "published_at": "2026-09-25T08:00:00Z",
    "prerelease": true,
    "draft": false,
    "assets": [
      {"name": "GoNavi-0.9.0-Windows-Amd64-Portable.zip", "url": "https://api.github.com/x/6", "size": 31457280, "digest": "` + h('e') + `"}
    ]
  },
  {
    "tag_name": "v0.8.9",
    "published_at": "2026-05-01T08:00:00Z",
    "prerelease": false,
    "draft": false,
    "assets": [
      {"name": "GoNavi-0.8.9-Windows-Amd64-Portable.zip", "url": "https://api.github.com/x/7", "size": 30000000},
      {"name": "SHA256SUMS", "url": "https://api.github.com/x/12", "size": 200}
    ]
  },
  {
    "tag_name": "v0.8.8",
    "published_at": "2026-04-01T08:00:00Z",
    "prerelease": false,
    "draft": false,
    "assets": [
      {"name": "GoNavi-0.8.8-Windows-Amd64-Portable.zip", "url": "https://api.github.com/x/8", "size": 29000000}
    ]
  },
  {
    "tag_name": "v0.7.5",
    "published_at": "2026-01-01T08:00:00Z",
    "prerelease": false,
    "draft": false,
    "assets": [
      {"name": "GoNavi-0.7.5-Linux-X86_64.tar.gz", "url": "https://api.github.com/x/9", "size": 40000000, "digest": "` + h('f') + `"},
      {"name": "gonavi-cli-0.7.5-windows-x64.zip", "url": "https://api.github.com/x/10", "size": 8000000, "digest": "` + h('0') + `"}
    ]
  },
  {
    "tag_name": "v0.9.1-beta.1",
    "published_at": "2026-09-20T08:00:00Z",
    "prerelease": true,
    "draft": false,
    "assets": [
      {"name": "GoNavi-0.9.1-Windows-Amd64-Portable.zip", "url": "https://api.github.com/x/11", "size": 31000000, "digest": "` + h('1') + `"}
    ]
  }
]`
	return []byte(body)
}

// TestParseReleasesBody 解析过滤：v0.9.0 正常入列表（digest 剥前缀）；
// v0.8.9 digest 缺失但有 SHA256SUMS 附件 → 降级备用摘要源保留（SHA256 空、
// SumsAsset 携带附件名）；v0.8.8 双源俱缺丢弃；dev-latest 滚动预发布丢弃；
// v0.7.5 无 Windows 便携资产（旧版本形态）丢弃；beta tag 丢弃。
// 列表按版本降序定序（最新在前）。
func TestParseReleasesBody(t *testing.T) {
	list, err := parseReleasesBody(fakeReleasesJSON(t))
	if err != nil {
		t.Fatalf("parseReleasesBody: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("期望 2 个版本，实际 %d: %+v", len(list), list)
	}
	top := list[0]
	if top.Version != "v0.9.0" ||
		top.SHA256 != strings.Repeat("a", 64) ||
		top.Size != 31457280 ||
		top.AssetName != "GoNavi-0.9.0-Windows-Amd64-Portable.zip" ||
		top.SumsAsset != "" {
		t.Errorf("v0.9.0 解析错误: %+v", top)
	}
	backup := list[1]
	if backup.Version != "v0.8.9" || backup.SHA256 != "" || backup.SumsAsset != "SHA256SUMS" {
		t.Errorf("digest 缺失应降级备用摘要源: %+v", backup)
	}
	for _, gone := range []string{"dev-latest", "v0.8.8", "v0.7.5", "v0.9.1-beta.1"} {
		for _, r := range list {
			if r.Version == gone {
				t.Errorf("%s 不应入列表", gone)
			}
		}
	}
	for _, r := range list {
		if strings.Contains(strings.ToLower(r.AssetName), "msi") ||
			strings.HasSuffix(strings.ToLower(r.AssetName), ".exe") {
			t.Errorf("混入非便携 zip 资产: %s", r.AssetName)
		}
		if r.SHA256 != "" && len(r.SHA256) != 64 {
			t.Errorf("sha256 格式异常: %q", r.SHA256)
		}
	}
}

// TestFindPortableAsset 资产筛选：精确命中形如
// GoNavi-0.9.0-Windows-Amd64-Portable.zip；msi/SFX exe/Linux/cli/版本错配
// 资产绝不混入。
func TestFindPortableAsset(t *testing.T) {
	assets := []asset{
		{Name: "GoNavi-0.9.0-Windows-Amd64-Installer.msi", Size: 1},
		{Name: "GoNavi-0.9.0-Windows-Amd64-Portable.exe", Size: 1},
		{Name: "GoNavi-0.9.1-Windows-Amd64-Portable.zip", Size: 1}, // 版本错配挂名
		{Name: "GoNavi-0.9.0-Linux-Amd64-Portable.zip", Size: 1},
		{Name: "GoNavi-0.9.0-Windows-Amd64-Portable.zip", Size: 31457280},
	}
	got, ok := findPortableAsset(assets, "v0.9.0")
	if !ok {
		t.Fatal("应命中 Windows x64 便携资产")
	}
	if got.Name != "GoNavi-0.9.0-Windows-Amd64-Portable.zip" || got.Size != 31457280 {
		t.Errorf("命中错误资产: %+v", got)
	}
	if _, ok := findPortableAsset(assets[:4], "v0.9.0"); ok {
		t.Error("无便携 zip 资产的 release 不应命中")
	}
}

// TestSumsDigest 备用摘要清单解析：`<hex> <name>` 与 `*<name>` 二进制前缀
// 方言兼容；找不到条目报错（宁拒不猜）。
func TestSumsDigest(t *testing.T) {
	want := strings.Repeat("c", 64)
	body := []byte("# comment line\n" + want + "  GoNavi-0.8.9-Windows-Amd64-Portable.zip\n" +
		strings.Repeat("d", 64) + " *GoNavi-0.8.9-Windows-Amd64-Installer.msi\nbadline\n")
	got, err := sumsDigest(body, "GoNavi-0.8.9-Windows-Amd64-Portable.zip")
	if err != nil || got != want {
		t.Fatalf("sumsDigest = (%q,%v), want %q", got, err, want)
	}
	if _, err := sumsDigest(body, "missing.zip"); err == nil {
		t.Error("清单缺条目应报错")
	}
}

// makeTestZip 构造测试用 zip
func makeTestZip(t *testing.T, entries map[string]string) string {
	t.Helper()
	f, err := os.CreateTemp("", "gonavitest-*.zip")
	if err != nil {
		t.Fatal(err)
	}
	path := f.Name()
	t.Cleanup(func() { os.Remove(path) })
	zw := zip.NewWriter(f)
	for name, content := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()
	return path
}

// TestVersionFromToken 版本令牌形状：纯 x.y.z 与 imported-时间戳收纳，
// v 前缀/两段/中文目录名拒绝。
func TestVersionFromToken(t *testing.T) {
	tests := []struct {
		token   string
		wantVer string
		wantOK  bool
	}{
		{"0.8.9", "v0.8.9", true},
		{"imported-20260926-150405", "vimported-20260926-150405", true},
		{"v0.8.9", "", false}, // 带 v 前缀的目录名非本模块落位格式
		{"0.8", "", false},    // 必须纯 x.y.z
		{"", "", false},
	}
	for _, tt := range tests {
		ver, ok := versionFromToken(tt.token)
		if ok != tt.wantOK || ver != tt.wantVer {
			t.Errorf("versionFromToken(%q) = (%q,%v), want (%q,%v)", tt.token, ver, ok, tt.wantVer, tt.wantOK)
		}
	}
}

// ---------- 内核解包 + 模块布局自检 ----------

func TestUnpackWithGoNaviLayout(t *testing.T) {
	zipPath := makeTestZip(t, map[string]string{
		exeName:   "fake-exe",
		"LICENSE": "Apache License 2.0",
		"NOTICE":  "GoNavi product notice",
	})
	staging := filepath.Join(t.TempDir(), "staging")
	if err := artifact.UnpackZip(zipPath, staging, artifact.DefaultLimits, nil); err != nil {
		t.Fatalf("UnpackZip: %v", err)
	}
	if err := checkPortableLayout(staging); err != nil {
		t.Fatalf("checkPortableLayout: %v", err)
	}
	// Apache-2.0 合规：解包保布局，许可文本天然随版本目录保留
	for _, name := range []string{exeName, "LICENSE", "NOTICE"} {
		if _, err := os.Stat(filepath.Join(staging, name)); err != nil {
			t.Errorf("布局缺失 %s: %v", name, err)
		}
	}
}

func TestUnpackRejectsPathTraversal(t *testing.T) {
	zipPath := makeTestZip(t, map[string]string{"../evil.txt": "escape"})
	staging := filepath.Join(t.TempDir(), "staging")
	if err := artifact.UnpackZip(zipPath, staging, artifact.DefaultLimits, nil); err == nil {
		t.Fatal("UnpackZip 应拒绝路径逃逸条目")
	}
}

func TestLayoutMissingExe(t *testing.T) {
	staging := t.TempDir()
	if err := os.WriteFile(filepath.Join(staging, "LICENSE"), []byte("Apache"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := checkPortableLayout(staging); err == nil {
		t.Fatal("缺 GoNavi.exe 应自检失败")
	}
}

func TestLayoutEmptyExe(t *testing.T) {
	staging := t.TempDir()
	if err := os.WriteFile(filepath.Join(staging, exeName), nil, 0644); err != nil {
		t.Fatal(err)
	}
	if err := checkPortableLayout(staging); err == nil {
		t.Fatal("空 exe 应判定为损坏安装")
	}
}

// ---------- 本地版本账与账外漂移护栏 ----------

// writeLedgerVersion 手工造一个带内核账本的已装版本目录（漂移用例的锚）。
func writeLedgerVersion(t *testing.T, versionsDir, token, exeContent, assetSHA string, landedAt string) string {
	t.Helper()
	dir := filepath.Join(versionsDir, dirPrefix+token)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, exeName), []byte(exeContent), 0644); err != nil {
		t.Fatal(err)
	}
	if assetSHA != "" || landedAt != "" {
		meta := fmt.Sprintf(`{"schema":1,"tool":"gonavi","version":%q,"entry":"GoNavi.exe","assetSHA256":%q,"installedAt":%q,"source":"remote"}`,
			token, assetSHA, landedAt)
		if err := os.WriteFile(filepath.Join(dir, "meta.json"), []byte(meta), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// TestListInstalledAndDriftProjection 列表扫描 + 漂移投影四态：
// 账实相符（mtime 迹象触发复算、一致）、账外漂移（不符、如实标记）、
// 无改动迹象（mtime 早于落位记录、复算未触发=成本控制主路径）、
// 账本无摘要（无法比对、不猜）。异模块目录与缺 exe 损坏目录跳过。
func TestListInstalledAndDriftProjection(t *testing.T) {
	versionsDir := t.TempDir()
	content := []byte("fake-exe")
	matchSHA := shaHex(content)

	writeLedgerVersion(t, versionsDir, "0.8.9", string(content), matchSHA, "2020-01-01T00:00:00Z")                // 复算一致
	writeLedgerVersion(t, versionsDir, "0.8.8", string(content), strings.Repeat("a", 64), "2020-01-01T00:00:00Z") // 账外漂移
	writeLedgerVersion(t, versionsDir, "0.8.7", string(content), matchSHA, "2099-01-01T00:00:00Z")                // 未触发复算
	writeLedgerVersion(t, versionsDir, "imported-20260101-120000", string(content), "", "")                       // 无法比对
	os.MkdirAll(filepath.Join(versionsDir, "markeron_v2.9.4"), 0755)                                              // 异模块必须跳过
	os.MkdirAll(filepath.Join(versionsDir, dirPrefix+"0.8.6"), 0755)                                              // 缺 exe 损坏必须跳过

	m := NewManager(versionsDir)
	list, err := m.ListInstalled()
	if err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}
	if len(list) != 4 {
		t.Fatalf("期望 4 个版本，实际 %d: %+v", len(list), list)
	}
	byVer := map[string]GoNaviVersionInfo{}
	for _, v := range list {
		byVer[v.Version] = v
	}
	if v := byVer["v0.8.9"]; v.HashDrifted || !strings.Contains(v.DriftNote, "复算一致") {
		t.Errorf("0.8.9 应报复算一致: %+v", v)
	}
	if v := byVer["v0.8.8"]; !v.HashDrifted || !strings.Contains(v.DriftNote, "账外漂移") {
		t.Errorf("0.8.8 应如实报账外漂移: %+v", v)
	}
	if v := byVer["v0.8.7"]; v.HashDrifted || !strings.Contains(v.DriftNote, "复算未触发") {
		t.Errorf("0.8.7 应走 mtime 成本闸不触发复算: %+v", v)
	}
	if v := byVer["vimported-20260101-120000"]; !strings.Contains(v.DriftNote, "无法比对") {
		t.Errorf("无账本摘要应报无法比对: %+v", v)
	}

	// ResolveExe / 非法版本 / 未安装
	if exe, err := m.ResolveExe("v0.8.9"); err != nil || filepath.Base(exe) != exeName {
		t.Errorf("ResolveExe(v0.8.9): %v %v", exe, err)
	}
	if _, err := m.ResolveExe("0.8.9"); err != nil {
		t.Errorf("无 v 前缀应可解析: %v", err)
	}
	if _, err := m.ResolveExe("v9.9.9"); err == nil {
		t.Error("未安装版本应报错")
	}
	if _, err := m.ResolveExe("../../windows"); err == nil {
		t.Error("路径穿越式版本号必须报错")
	}

	if err := m.Remove("v0.8.9"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	list, _ = m.ListInstalled()
	if len(list) != 3 {
		t.Errorf("卸载后应剩 3 个版本，实际 %d", len(list))
	}
}

// TestVerifyLedger 只读漂移复查定向入口（service 层对 active 版本用）：
// 漂移/一致/未触发/未安装四态与 ListInstalled 投影同口径。
func TestVerifyLedger(t *testing.T) {
	versionsDir := t.TempDir()
	content := []byte("fake-exe")
	writeLedgerVersion(t, versionsDir, "0.8.8", string(content), strings.Repeat("b", 64), "2020-01-01T00:00:00Z")
	writeLedgerVersion(t, versionsDir, "0.8.7", string(content), shaHex(content), "2099-01-01T00:00:00Z")
	m := NewManager(versionsDir)

	if drifted, note := m.VerifyLedger("v0.8.8"); !drifted || !strings.Contains(note, "账外漂移") {
		t.Errorf("0.8.8 应报漂移: (%v,%q)", drifted, note)
	}
	if drifted, note := m.VerifyLedger("v0.8.7"); drifted || !strings.Contains(note, "复算未触发") {
		t.Errorf("0.8.7 应走成本闸: (%v,%q)", drifted, note)
	}
	if drifted, note := m.VerifyLedger("v9.9.9"); drifted || !strings.Contains(note, "无法比对") {
		t.Errorf("未安装应报无法比对: (%v,%q)", drifted, note)
	}
}

// TestImportLocal 导入链：假 exe 无 PE 版本资源 → imported-时间戳兜底；
// exe+LICENSE+NOTICE 白名单搬运（副本携带许可文本），噪声文件不搬；
// 落位账本记实测摘要（漂移锚点）、verifiedHash 如实 false；
// 可解析、可卸载、重复导入与无 exe 目录报错。
func TestImportLocal(t *testing.T) {
	m := NewManager(t.TempDir())

	src := t.TempDir()
	content := []byte("fake-exe-bytes")
	os.WriteFile(filepath.Join(src, exeName), content, 0644)
	os.WriteFile(filepath.Join(src, "LICENSE"), []byte("Apache License 2.0"), 0644)
	os.WriteFile(filepath.Join(src, "NOTICE"), []byte("GoNavi notice"), 0644)
	os.WriteFile(filepath.Join(src, "config.json"), []byte("noise"), 0644)

	info, err := m.ImportLocal(src)
	if err != nil {
		t.Fatalf("ImportLocal: %v", err)
	}
	if !strings.HasPrefix(info.Version, "vimported-") {
		t.Errorf("版本号格式异常: %q", info.Version)
	}
	if !info.IsImport || info.Source != src || info.VerifiedHash {
		t.Errorf("导入标记错误: %+v", info)
	}
	if info.SHA256 != shaHex(content) {
		t.Errorf("导入摘要应记 exe 实测值: %q", info.SHA256)
	}
	for _, name := range []string{exeName, "LICENSE", "NOTICE"} {
		if _, err := os.Stat(filepath.Join(info.Dir, name)); err != nil {
			t.Errorf("导入后缺少 %s: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(info.Dir, "config.json")); !os.IsNotExist(err) {
		t.Error("非白名单文件不应被搬运")
	}
	// 内核统一账本落位（漂移护栏依赖 assetSHA256）
	raw, err := os.ReadFile(filepath.Join(info.Dir, "meta.json"))
	if err != nil {
		t.Fatal(err)
	}
	var meta artifact.Meta
	if err := json.Unmarshal(raw, &meta); err != nil {
		t.Fatal(err)
	}
	if meta.AssetSHA256 != shaHex(content) || meta.Source != artifact.SourceImported {
		t.Errorf("落位账本异常: %+v", meta)
	}
	// 刚导入的 exe mtime 早于落位时刻 → 成本闸关闭，复算未触发
	if drifted, note := m.VerifyLedger(info.Version); drifted || !strings.Contains(note, "复算未触发") {
		t.Errorf("导入后应无漂移迹象: (%v,%q)", drifted, note)
	}

	if _, err := m.ResolveExe(info.Version); err != nil {
		t.Errorf("导入版本应可解析: %v", err)
	}
	if _, err := m.ImportLocal(src); err == nil {
		t.Error("重复导入应报错")
	}
	if _, err := m.ImportLocal(t.TempDir()); err == nil {
		t.Error("无 exe 的目录应报错")
	}
	if err := m.Remove(info.Version); err != nil {
		t.Errorf("导入版本应可卸载: %v", err)
	}
}

// TestListRemoteFormBackfillNoCachePollution N13 形态标注纪律：ListRemote
// 回填 Form 走切片拷贝，TTL 命中的缓存源不得被污染（并发读互不踩踏）。
func TestListRemoteFormBackfillNoCachePollution(t *testing.T) {
	m := NewManager(t.TempDir())
	seedRemote(t, GoNaviRelease{Version: "v0.9.0", AssetName: "GoNavi-0.9.0-Windows-Amd64-Portable.zip", Size: 10})

	list, err := m.ListRemote()
	if err != nil || len(list) != 1 || list[0].Form != hostedForm {
		t.Fatalf("ListRemote 应回填 Form: %+v err %v", list, err)
	}
	remoteCache.mu.Lock()
	polluted := remoteCache.data[0].Form
	remoteCache.mu.Unlock()
	if polluted != "" {
		t.Errorf("回填不得污染缓存源: %q", polluted)
	}
}
