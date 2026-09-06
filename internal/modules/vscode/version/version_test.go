package version

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	fakeCommit1 = "a44adf7f53e00964ab890f9f8758a334f1fc15bc"
	fakeCommit2 = "520fb30b2d3d324b4cb2342f6e88e2cd93751de1"
	fakeSHA     = "deca16ea5c4d71ece23e50af57632bba3abd0e3577c3c8bfbcdd90b03f11e402"
)

// newFakeUpstream 模拟官方更新网关三端点：版本列表 / 版本化 302 重定向 / 最新清单 feed。
func newFakeUpstream(t *testing.T, versions []string, latestSHA string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/releases/stable", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(versions)
	})
	mux.HandleFunc("/api/update/", func(w http.ResponseWriter, r *http.Request) {
		// 路径 /api/update/{platform}/stable/{commit}：commit 段被忽略，恒返最新清单
		if latestSHA == "" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"url":            "https://cdn.example/" + fakeCommit1 + "/VSCode-win32-x64-1.136.1.zip",
			"version":        fakeCommit1,
			"productVersion": versions[0],
			"sha256hash":     latestSHA,
		})
	})
	// 版本化下载端点：/{ver}/{platform}/stable → 302 → CDN 直链
	for i, v := range versions {
		commit := fmt.Sprintf("c%040d", i)
		asset := fmt.Sprintf("VSCode-win32-x64-%s.zip", v)
		if v == versions[0] {
			commit = fakeCommit1
		}
		if v == versions[len(versions)-1] {
			commit = fakeCommit2
		}
		mux.HandleFunc("/"+v+"/", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/cdn/"+commit+"/"+asset, http.StatusFound)
		})
	}
	mux.HandleFunc("/cdn/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "348166501")
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	// 全局端点常量替换为测试服务器（仅本测试内生效）
	restore := setAPIRootForTest(srv.URL)
	t.Cleanup(restore)
	return srv
}

// setAPIRootForTest 临时改写 apiRoot（const → 变量化代价高，测试直接改包级
// 测试钩子；见 remote.go testOverrideRoot）。
func setAPIRootForTest(url string) func() {
	old := testAPIRoot
	testAPIRoot = url
	return func() { testAPIRoot = old }
}

func TestFetchRemoteList(t *testing.T) {
	newFakeUpstream(t, []string{"1.136.1", "1.136.0", "bad-tag", "1.135.0"}, fakeSHA)
	resetRemoteCaches()

	list, err := ListRemote(FormPortable)
	if err != nil {
		t.Fatalf("ListRemote: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("期望 3 个有效版本（bad-tag 丢弃），实际 %d: %+v", len(list), list)
	}
	if list[0].Version != "1.136.1" || list[0].Commit != fakeCommit1 {
		t.Errorf("首项解析错误: %+v", list[0])
	}
	if list[0].SHA256 != fakeSHA {
		t.Errorf("最新版应带官方 sha256: %+v", list[0])
	}
	if list[1].SHA256 != "" {
		t.Errorf("历史版本不应有官方哈希: %+v", list[1])
	}
	if list[0].Size != 348166501 {
		t.Errorf("大小解析错误: %d", list[0].Size)
	}
	if !strings.HasSuffix(list[0].DownloadURL, "VSCode-win32-x64-1.136.1.zip") {
		t.Errorf("直链解析错误: %s", list[0].DownloadURL)
	}
}

func TestFetchRemoteListNoOfficialHash(t *testing.T) {
	newFakeUpstream(t, []string{"1.136.1", "1.136.0"}, "")
	resetRemoteCaches()
	list, err := ListRemote(FormPortable)
	if err != nil {
		t.Fatalf("ListRemote: %v", err)
	}
	if list[0].SHA256 != "" {
		t.Errorf("feed 无哈希时列表应诚实留空: %q", list[0].SHA256)
	}
}

func TestFilterSemver(t *testing.T) {
	got := filterSemver([]string{"1.2.3", "v1.0.0", "insiders", "10.2.30", "1.2"}, 3)
	if len(got) != 2 || got[0] != "1.2.3" || got[1] != "10.2.30" {
		t.Errorf("过滤错误: %+v", got)
	}
}

func TestIsHex64(t *testing.T) {
	if !isHex64(fakeSHA) || isHex64("zz"+fakeSHA[:60]) || isHex64(fakeSHA[:63]) {
		t.Error("isHex64 判定错误")
	}
}

func TestLookupRemote(t *testing.T) {
	list := []Release{{Version: "1.1.1"}, {Version: "2.2.2"}}
	if _, ok := lookupRemote(list, "2.2.2"); !ok {
		t.Error("应命中")
	}
	if _, ok := lookupRemote(list, "3.3.3"); ok {
		t.Error("不应命中")
	}
}

// makeTestZip 构造测试 zip
func makeTestZip(t *testing.T, entries map[string]string) string {
	t.Helper()
	f, err := os.CreateTemp("", "vsctest-*.zip")
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

// goodPortableZip 官方归档布局：根级 Code.exe + bin/code.cmd + commit10/resources/app/product.json
func goodPortableZip(t *testing.T) string {
	t.Helper()
	return makeTestZip(t, map[string]string{
		"Code.exe":                              "fake-exe",
		"bin/code.cmd":                          "@echo off",
		"bin/code":                              "#!/bin/sh",
		"a44adf7f53/resources/app/product.json": `{"nameShort":"Code"}`,
	})
}

func TestExtractAll(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "vscode_1.136.1")
	if err := extractAll(goodPortableZip(t), dst); err != nil {
		t.Fatalf("extractAll: %v", err)
	}
	for _, rel := range []string{"Code.exe", filepath.Join("bin", "code.cmd")} {
		if _, err := os.Stat(filepath.Join(dst, rel)); err != nil {
			t.Errorf("%s 未解压: %v", rel, err)
		}
	}
	// 便携模式激活器必须自动补齐
	if fi, err := os.Stat(filepath.Join(dst, dataDirName)); err != nil || !fi.IsDir() {
		t.Errorf("extractAll 应补建 data\\ 目录: %v", err)
	}
}

func TestExtractAllZipSlip(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "dst")
	bad := makeTestZip(t, map[string]string{
		"../evil.txt":  "evil",
		"Code.exe":     "fake",
		"bin/code.cmd": "@",
	})
	if err := extractAll(bad, dst); err == nil {
		t.Fatal("ZipSlip 条目应被拒绝")
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Error("失败后目标目录应被清理")
	}
}

func TestExtractAllMissingBits(t *testing.T) {
	cases := map[string]map[string]string{
		"缺 exe":          {"bin/code.cmd": "@", "x/resources/app/product.json": "{}"},
		"缺 bin 脚本":       {"Code.exe": "fake", "x/resources/app/product.json": "{}"},
		"缺 product.json": {"Code.exe": "fake", "bin/code.cmd": "@"},
		"exe 为空":         {"Code.exe": "", "bin/code.cmd": "@", "x/resources/app/product.json": "{}"},
	}
	for name, entries := range cases {
		zipPath := makeTestZip(t, entries)
		dst := filepath.Join(t.TempDir(), "dst")
		if err := extractAll(zipPath, dst); err == nil {
			t.Errorf("%s: 应被布局自检拒绝", name)
		}
		if _, err := os.Stat(dst); !os.IsNotExist(err) {
			t.Errorf("%s: 失败后目标目录应被清理", name)
		}
	}
}

// mkPortableDir 造一个通过布局自检的便携版本目录
func mkPortableDir(t *testing.T, versionsDir, dirName, meta string) string {
	t.Helper()
	dir := filepath.Join(versionsDir, dirName)
	os.MkdirAll(filepath.Join(dir, "bin"), 0755)
	os.MkdirAll(filepath.Join(dir, dataDirName), 0755)
	os.WriteFile(filepath.Join(dir, "Code.exe"), []byte("fake-exe"), 0644)
	os.WriteFile(filepath.Join(dir, "bin", "code.cmd"), []byte("@"), 0644)
	if meta != "" {
		os.WriteFile(filepath.Join(dir, "meta.json"), []byte(meta), 0644)
	}
	return dir
}

func TestListInstalledAndRemove(t *testing.T) {
	versionsDir := t.TempDir()
	m := NewManager(versionsDir)

	mkPortableDir(t, versionsDir, "vscode_1.136.1",
		`{"installedAt":"2026-09-01 10:00:00","isImport":true,"source":"E:\\vs","verifiedHash":true}`)
	mkPortableDir(t, versionsDir, "vscode_1.135.0", "")
	os.MkdirAll(filepath.Join(versionsDir, "ccswitch_3.20.0"), 0755) // 异模块目录必须跳过
	os.MkdirAll(filepath.Join(versionsDir, "vscode_1.134.0"), 0755)  // 缺 exe 的损坏安装必须跳过

	list, err := m.ListInstalled()
	if err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("期望 2 个版本，实际 %d: %+v", len(list), list)
	}
	byVer := map[string]VersionInfo{}
	for _, v := range list {
		byVer[v.Version] = v
	}
	if v := byVer["1.136.1"]; !v.IsImport || v.InstalledAt != "2026-09-01 10:00:00" || !v.Verified {
		t.Errorf("1.136.1 元信息解析错误: %+v", v)
	}
	if v := byVer["1.135.0"]; v.IsImport || v.InstalledAt == "" {
		t.Errorf("1.135.0 默认元信息错误: %+v", v)
	}

	if exe, err := m.ResolveExe("1.136.1"); err != nil || filepath.Base(exe) != exeName {
		t.Errorf("ResolveExe: %v %v", exe, err)
	}
	if _, err := m.ResolveExe("v1.136.1"); err != nil {
		t.Errorf("v 前缀应兼容: %v", err)
	}
	if _, err := m.ResolveExe("9.9.9"); err == nil {
		t.Error("未安装版本应报错")
	}
	if _, err := m.ResolveExe("../../windows"); err == nil {
		t.Error("路径穿越式版本号必须报错")
	}

	if err := m.Remove("1.136.1"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	list, _ = m.ListInstalled()
	if len(list) != 1 {
		t.Errorf("卸载后应剩 1 个，实际 %d", len(list))
	}
}

func TestEnsureDataDir(t *testing.T) {
	versionsDir := t.TempDir()
	m := NewManager(versionsDir)
	dir := mkPortableDir(t, versionsDir, "vscode_1.136.1", "")
	// 模拟 data\ 被用户误删：启动前 EnsureDataDir 必须幂等补回
	os.RemoveAll(filepath.Join(dir, dataDirName))
	if err := m.EnsureDataDir("1.136.1"); err != nil {
		t.Fatalf("EnsureDataDir: %v", err)
	}
	if fi, err := os.Stat(filepath.Join(dir, dataDirName)); err != nil || !fi.IsDir() {
		t.Errorf("data\\ 未补回: %v", err)
	}
}

func TestImportLocalPortable(t *testing.T) {
	m := NewManager(t.TempDir())

	src := t.TempDir()
	os.MkdirAll(filepath.Join(src, "bin"), 0755)
	os.MkdirAll(filepath.Join(src, "a44adf7f53", "resources", "app"), 0755)
	os.MkdirAll(filepath.Join(src, dataDirName, "user-data"), 0755)
	os.WriteFile(filepath.Join(src, "Code.exe"), []byte("fake-exe-bytes"), 0644)
	os.WriteFile(filepath.Join(src, "bin", "code.cmd"), []byte("@"), 0644)
	os.WriteFile(filepath.Join(src, "a44adf7f53", "resources", "app", "product.json"), []byte("{}"), 0644)
	os.WriteFile(filepath.Join(src, dataDirName, "user-data", "settings.json"), []byte("{}"), 0644)

	info, err := m.ImportLocal(src)
	if err != nil {
		t.Fatalf("ImportLocal: %v", err)
	}
	// 假 PE 读不出版本 → imported- 时间戳兜底（真实 Code.exe 会得 FileVersion）
	if !strings.HasPrefix(info.Version, "imported-") {
		t.Errorf("版本兜底异常: %q", info.Version)
	}
	if !info.IsImport || info.Source != src {
		t.Errorf("导入标记错误: %+v", info)
	}
	if !validPortableLayout(info.Dir) {
		t.Error("导入后布局自检未通过")
	}
	// data\ 绝不随导入搬运（导入即全新自包含环境），但目录本身必须存在
	if _, err := os.Stat(filepath.Join(info.Dir, dataDirName, "user-data")); !os.IsNotExist(err) {
		t.Error("源 data\\ 内容不应被搬运")
	}
	if _, err := os.Stat(filepath.Join(info.Dir, dataDirName)); err != nil {
		t.Errorf("导入后 data\\ 必须存在: %v", err)
	}
	if _, err := m.ResolveExe(info.Version); err != nil {
		t.Errorf("兜底版本应可解析: %v", err)
	}
}

func TestImportLocalRejects(t *testing.T) {
	m := NewManager(t.TempDir())

	// 空目录拒绝
	if _, err := m.ImportLocal(t.TempDir()); err == nil {
		t.Error("空目录应报错")
	}
	// 安装版目录（unins000.exe）拒绝
	src := t.TempDir()
	os.MkdirAll(filepath.Join(src, "bin"), 0755)
	os.WriteFile(filepath.Join(src, "Code.exe"), []byte("fake"), 0644)
	os.WriteFile(filepath.Join(src, "bin", "code.cmd"), []byte("@"), 0644)
	os.WriteFile(filepath.Join(src, "unins000.exe"), []byte("fake"), 0644)
	if _, err := m.ImportLocal(src); err == nil {
		t.Error("安装版目录应被拒绝导入")
	}
}
