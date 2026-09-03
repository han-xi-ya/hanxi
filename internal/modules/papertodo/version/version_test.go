package version

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeReleasesJSON 构造与真实 GitHub API 同构的样例响应（字段形态取自
// api.github.com/repos/snownico0722/PaperTodo/releases 实测输出）：
// 覆盖双变体 + win7 变体、无 digest（现状）、未来带 digest、预发布 rc、
// 非规范 tag、变体不齐、draft。
func fakeReleasesJSON(t *testing.T) []byte {
	t.Helper()
	body := `[
  {
    "tag_name": "v3.31",
    "published_at": "2026-08-30T17:28:21Z",
    "prerelease": false,
    "draft": false,
    "assets": [
      {"name": "PaperTodo-v3.31-win-x64-no-runtime.exe", "url": "https://api.github.com/x/1", "size": 2391163},
      {"name": "PaperTodo-v3.31-win-x64-self-contained.exe", "url": "https://api.github.com/x/2", "size": 71303625},
      {"name": "PaperTodo-v3.31-win7BestEffort-win-x64-self-contained.exe", "url": "https://api.github.com/x/3", "size": 71291990},
      {"name": "Source code (zip)", "url": "https://api.github.com/x/4", "size": 5000000}
    ]
  },
  {
    "tag_name": "v3.3",
    "published_at": "2026-08-25T10:00:00Z",
    "prerelease": false,
    "draft": false,
    "assets": [
      {"name": "PaperTodo-v3.3-win-x64-self-contained.exe", "url": "https://api.github.com/x/5", "size": 71000000},
      {"name": "PaperTodo-v3.3-win-x64-no-runtime.exe", "url": "https://api.github.com/x/6", "size": 2300000}
    ]
  },
  {
    "tag_name": "v2.1rc1",
    "published_at": "2026-05-01T10:00:00Z",
    "prerelease": true,
    "draft": false,
    "assets": [
      {"name": "PaperTodo-v2.1rc1-win-x64-self-contained.exe", "url": "https://api.github.com/x/7", "size": 60000000},
      {"name": "PaperTodo-v2.1rc1-win-x64-no-runtime.exe", "url": "https://api.github.com/x/8", "size": 2000000}
    ]
  },
  {
    "tag_name": "nightly",
    "published_at": "2026-06-01T10:00:00Z",
    "prerelease": false,
    "draft": false,
    "assets": [
      {"name": "PaperTodo-nightly-win-x64-self-contained.exe", "url": "https://api.github.com/x/9", "size": 60000000},
      {"name": "PaperTodo-nightly-win-x64-no-runtime.exe", "url": "https://api.github.com/x/10", "size": 2000000}
    ]
  },
  {
    "tag_name": "v3.0",
    "published_at": "2026-04-01T10:00:00Z",
    "prerelease": false,
    "draft": false,
    "assets": [
      {"name": "PaperTodo-v3.0-win-x64-self-contained.exe", "url": "https://api.github.com/x/11", "size": 60000000}
    ]
  },
  {
    "tag_name": "v9.9.9",
    "published_at": "2026-09-01T10:00:00Z",
    "prerelease": false,
    "draft": false,
    "assets": [
      {"name": "PaperTodo-v9.9.9-win-x64-self-contained.exe", "url": "https://api.github.com/x/12", "size": 71000000, "digest": "sha256:AA37942F63A40C7BA57749D413D0DA4C6347DB2A29205F6D6E02EC86617D208A"},
      {"name": "PaperTodo-v9.9.9-win-x64-no-runtime.exe", "url": "https://api.github.com/x/13", "size": 2300000, "digest": "sha256:bb37942f63a40c7ba57749d413d0da4c6347db2a29205f6d6e02ec86617d208b"}
    ]
  },
  {
    "tag_name": "v8.8.8",
    "published_at": "2026-09-01T10:00:00Z",
    "prerelease": false,
    "draft": true,
    "assets": [
      {"name": "PaperTodo-v8.8.8-win-x64-self-contained.exe", "url": "https://api.github.com/x/14", "size": 71000000},
      {"name": "PaperTodo-v8.8.8-win-x64-no-runtime.exe", "url": "https://api.github.com/x/15", "size": 2300000}
    ]
  }
]`
	return []byte(body)
}

// TestParseReleasesBody 过滤口径：GA 且双变体齐备才入列表；
// win7/源码包资产不参与；rc/nightly/变体不齐/draft 全部剔除；
// 无 digest 不剔除（与 ccswitch 的关键差异），有 digest 则归一收录。
func TestParseReleasesBody(t *testing.T) {
	list, err := parseReleasesBody(fakeReleasesJSON(t))
	if err != nil {
		t.Fatalf("parseReleasesBody: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("期望 3 个版本，实际 %d: %+v", len(list), list)
	}
	byVer := map[string]PaperRelease{}
	for _, r := range list {
		byVer[r.Version] = r
	}
	for _, want := range []string{"v3.31", "v3.3", "v9.9.9"} {
		if _, ok := byVer[want]; !ok {
			t.Errorf("%s 应入列表", want)
		}
	}
	for _, gone := range []string{"v2.1rc1", "nightly", "v3.0", "v8.8.8"} {
		if _, ok := byVer[gone]; ok {
			t.Errorf("%s 不应入列表", gone)
		}
	}

	r331 := byVer["v3.31"]
	if r331.SelfContained.Name != "PaperTodo-v3.31-win-x64-self-contained.exe" || r331.SelfContained.Size != 71303625 {
		t.Errorf("v3.31 self-contained 解析错误: %+v", r331.SelfContained)
	}
	if r331.NoRuntime.Name != "PaperTodo-v3.31-win-x64-no-runtime.exe" || r331.NoRuntime.Size != 2391163 {
		t.Errorf("v3.31 no-runtime 解析错误: %+v", r331.NoRuntime)
	}
	if r331.SelfContained.SHA256 != "" || r331.NoRuntime.SHA256 != "" {
		t.Errorf("上游无 digest 时应为空: %+v", r331)
	}
	// 未来上游资产补上 digest：大小写归一 + 前缀剥离
	if got := byVer["v9.9.9"].SelfContained.SHA256; got != "aa37942f63a40c7ba57749d413d0da4c6347db2a29205f6d6e02ec86617d208a" {
		t.Errorf("digest 归一错误: %q", got)
	}
}

func TestAssetFor(t *testing.T) {
	rel := PaperRelease{
		Version:       "v3.31",
		SelfContained: PaperAsset{Name: "sc.exe", Size: 7},
		NoRuntime:     PaperAsset{Name: "fdd.exe", Size: 2},
	}
	if a, err := rel.assetFor(VariantSelfContained); err != nil || a.Name != "sc.exe" {
		t.Errorf("self-contained 选择错误: %+v %v", a, err)
	}
	if a, err := rel.assetFor(VariantNoRuntime); err != nil || a.Name != "fdd.exe" {
		t.Errorf("no-runtime 选择错误: %+v %v", a, err)
	}
	if _, err := rel.assetFor("win7"); err == nil {
		t.Error("未知变体应报错")
	}
}

func TestNormalizeDigest(t *testing.T) {
	long := strings.Repeat("a", 64)
	if got := normalizeDigest("sha256:" + strings.ToUpper(long)); got != long {
		t.Errorf("应剥离前缀并转小写: %q", got)
	}
	if got := normalizeDigest(long); got != long {
		t.Errorf("无前缀也应接受: %q", got)
	}
	for _, bad := range []string{"", "sha256:", "sha256:abc", "null"} {
		if got := normalizeDigest(bad); got != "" {
			t.Errorf("非法 digest 应为空串: %q -> %q", bad, got)
		}
	}
}

func TestPEVersionMatches(t *testing.T) {
	cases := []struct {
		pe, tag string
		want    bool
	}{
		{"3.31.0.0", "v3.31", true},
		{"3.31", "v3.31", true},
		{"3.31.0", "v3.31", true},
		{"3.3.0.0", "v3.31", false},  // 字典序陷阱的数值反例
		{"3.31.1.0", "v3.31", false}, // 补丁段非零不匹配
		{"3.31.0.0-beta", "v3.31", false},
		{"v3.31.0.0", "3.31", true},
	}
	for _, c := range cases {
		if got := peVersionMatches(c.pe, c.tag); got != c.want {
			t.Errorf("peVersionMatches(%q, %q) = %v, 期望 %v", c.pe, c.tag, got, c.want)
		}
	}
}

// writeFakeExe 在 dir 放一个"像样"的假 exe（内容任意非空字节）。
func writeFakeExe(t *testing.T, dir, name string, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0755); err != nil {
		t.Fatal(err)
	}
}

func TestSwapInExe(t *testing.T) {
	dir := t.TempDir()

	// 首次安装：无旧 exe，直接换入
	tmp := filepath.Join(dir, exeName+tmpSuffix)
	if err := os.WriteFile(tmp, []byte("NEW"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := swapInExe(dir, tmp); err != nil {
		t.Fatalf("首次换入失败: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, exeName))
	if string(data) != "NEW" {
		t.Errorf("换入内容错误: %q", data)
	}

	// 覆盖升级：旧 exe 被替换且不留 .bak
	if err := os.WriteFile(tmp, []byte("NEW2"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := swapInExe(dir, tmp); err != nil {
		t.Fatalf("覆盖换入失败: %v", err)
	}
	data, _ = os.ReadFile(filepath.Join(dir, exeName))
	if string(data) != "NEW2" {
		t.Errorf("覆盖内容错误: %q", data)
	}
	if _, err := os.Stat(filepath.Join(dir, exeName+bakSuffix)); !os.IsNotExist(err) {
		t.Error("成功安装后不应残留 .bak")
	}

	// tmp 缺失 → 报错且不得抹掉现有 exe
	if err := swapInExe(dir, filepath.Join(dir, "missing"+tmpSuffix)); err == nil {
		t.Error("tmp 缺失应报错")
	}
	if data, _ := os.ReadFile(filepath.Join(dir, exeName)); string(data) != "NEW2" {
		t.Error("失败路径不得破坏现有 exe")
	}
}

func TestListInstalledAndRemove(t *testing.T) {
	versionsDir := t.TempDir()
	m := NewManager(versionsDir)

	// 空目录：未安装
	list, err := m.ListInstalled()
	if err != nil || len(list) != 0 {
		t.Fatalf("空目录应返回未安装: %v %+v", err, list)
	}

	// 0 字节 exe 视为损坏隐身
	dir := m.InstallDir()
	writeFakeExe(t, dir, exeName, "")
	if err := os.Truncate(filepath.Join(dir, exeName), 0); err != nil {
		t.Fatal(err)
	}
	if list, _ := m.ListInstalled(); len(list) != 0 {
		t.Error("0 字节 exe 应视为未安装")
	}

	// 正常安装 + meta + 便签数据
	writeFakeExe(t, dir, exeName, "MZfake-exe-bytes")
	writeFakeExe(t, dir, dataFileName, `{"papers":[]}`)
	meta := map[string]any{"installedAt": "2026-09-01 10:00:00", "tag": "v3.31", "variant": "self-contained", "assetSHA256": "ab12"}
	b, _ := json.Marshal(meta)
	if err := os.WriteFile(filepath.Join(dir, metaFileName), b, 0644); err != nil {
		t.Fatal(err)
	}
	list, err = m.ListInstalled()
	if err != nil || len(list) != 1 {
		t.Fatalf("应返回一条安装: %v %+v", err, list)
	}
	got := list[0]
	if got.Version != "v3.31" || got.Variant != "self-contained" || !got.HasData || got.Size == 0 {
		t.Errorf("安装信息错误: %+v", got)
	}
	if exe, err := m.ResolveExe(); err != nil || exe != got.ExePath {
		t.Errorf("ResolveExe 错误: %v %s", err, exe)
	}

	// Remove：删 exe/meta，保留便签数据（卸载不丢创作内容是红线）
	if err := os.WriteFile(filepath.Join(dir, exeName+bakSuffix), []byte("stale"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := m.Remove(); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	for _, gone := range []string{exeName, exeName + bakSuffix, metaFileName} {
		if _, err := os.Stat(filepath.Join(dir, gone)); !os.IsNotExist(err) {
			t.Errorf("%s 应已删除", gone)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, dataFileName)); err != nil {
		t.Errorf("便签数据必须保留: %v", err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("数据仍在时托管目录不应被移除: %v", err)
	}

	// 数据也不存在时目录随手清空
	if err := os.Remove(filepath.Join(dir, dataFileName)); err != nil {
		t.Fatal(err)
	}
	writeFakeExe(t, dir, exeName, "again")
	if err := m.Remove(); err != nil {
		t.Fatalf("Remove 二轮: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Error("空目录应被移除")
	}
	// 再删：报"无托管安装"
	if err := m.Remove(); err == nil {
		t.Error("无安装时 Remove 应报错")
	}
}

func TestImportLocal(t *testing.T) {
	versionsDir := t.TempDir()
	m := NewManager(versionsDir)

	src := filepath.Join(t.TempDir(), "Apps 模拟")
	writeFakeExe(t, src, exeName, "MZuser-copy")
	writeFakeExe(t, src, dataFileName, `{"papers":["x"]}`)
	writeFakeExe(t, filepath.Join(src, "plugins"), "README.txt", "hi") // 子目录随行
	// 残留垃圾不得带入
	writeFakeExe(t, src, exeName+bakSuffix, "junk")

	info, err := m.ImportLocal(src)
	if err != nil {
		t.Fatalf("ImportLocal: %v", err)
	}
	if !info.IsImport || !info.HasData || info.Source != src {
		t.Errorf("导入信息错误: %+v", info)
	}
	if _, err := os.Stat(filepath.Join(info.Dir, dataFileName)); err != nil {
		t.Errorf("便签数据未随行: %v", err)
	}
	if _, err := os.Stat(filepath.Join(info.Dir, "plugins", "README.txt")); err != nil {
		t.Errorf("plugins 子目录未随行: %v", err)
	}
	if _, err := os.Stat(filepath.Join(info.Dir, exeName+bakSuffix)); !os.IsNotExist(err) {
		t.Error("备份残留不应导入")
	}

	// 已有托管安装 → 拒绝二次导入
	if _, err := m.ImportLocal(src); err == nil {
		t.Error("已有托管安装时应拒绝导入")
	}

	// 源目录缺 exe → 拒绝
	if _, err := m.ImportLocal(filepath.Join(t.TempDir(), "empty")); err == nil {
		t.Error("缺 exe 的源目录应拒绝")
	}
}

func TestImportLocalRejectsSelfNesting(t *testing.T) {
	versionsDir := t.TempDir()
	m := NewManager(versionsDir)

	// 伪造"源目录包含托管目录"的套娃场：src 是 versionsDir 的父目录
	src := filepath.Dir(versionsDir)
	if src == versionsDir || src == "." || src == string(filepath.Separator) {
		t.Skip("TempDir 布局无法构造父目录，跳过")
	}
	writeFakeExe(t, src, exeName, "MZ")
	if _, err := m.ImportLocal(src); err == nil || !strings.Contains(err.Error(), "自嵌套") {
		t.Errorf("应拒绝包含 Hanxi 数据目录的源: %v", err)
	}
}

func TestVersionHelpers(t *testing.T) {
	if !ValidVariant(VariantSelfContained) || !ValidVariant(VariantNoRuntime) || ValidVariant("win7") {
		t.Error("ValidVariant 判定错误")
	}
	if ReleasesPageURL() != "https://github.com/snownico0722/PaperTodo/releases/latest" {
		t.Errorf("ReleasesPageURL 错误: %s", ReleasesPageURL())
	}
}
