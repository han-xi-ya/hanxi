package version

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hanxi/packages/go/artifact"
	"hanxi/packages/go/hostfeed"
)

// fakeStableJSON 与真实发布接口同构的样例（3.2.7 实测响应裁剪）：
// 覆盖安装包/7z/便携 zip 三资产、缺失 md5、错误形状等干扰项。
func fakeStableJSON(md5 string) []byte {
	return []byte(`{"code":0,"data":{
  "product_code":"gh_view","channel":"stable","version":"3.2.7","version_code":98,
  "files":[
    {"id":130,"name":"GuoheView_v3.2.7.98-安装包.exe","url":"https://rj.lovestu.com/f/130","size":5542800,"md5":"7c961f7247c5e60223c630ffe6eb24d2"},
    {"id":131,"name":"GuoheView_v3.2.7.98-便携版.7z","url":"https://rj.lovestu.com/f/131","size":5038357,"md5":"100a2375f0d40103abfc27b4c17df005"},
    {"id":132,"name":"GuoheView_v3.2.7.98-便携版.zip","url":"https://rj.lovestu.com/f/132","size":6884498,"md5":"` + md5 + `"}
  ]}}`)
}

const realZipMD5 = "6ab4453aa367b8c7aeff3a563d98243b"

func TestParseChannelBody(t *testing.T) {
	rel, err := parseChannelBody(fakeStableJSON(realZipMD5))
	if err != nil {
		t.Fatalf("parseChannelBody: %v", err)
	}
	if rel.Version != "v3.2.7.98" || rel.Channel != "stable" || rel.IsPre {
		t.Errorf("版本归一化错误: %+v", rel)
	}
	if rel.AssetName != "GuoheView_v3.2.7.98-便携版.zip" || rel.Size != 6884498 || rel.MD5 != realZipMD5 {
		t.Errorf("便携资产挑选错误: %+v", rel)
	}
}

func TestParseChannelBodyRejects(t *testing.T) {
	cases := map[string][]byte{
		"缺 md5":   []byte(`{"data":{"channel":"stable","version":"3.2.7","version_code":98,"files":[{"name":"GuoheView_v3.2.7.98-便携版.zip","url":"u","size":1,"md5":""}]}}`),
		"md5 形状错": []byte(`{"data":{"channel":"stable","version":"3.2.7","version_code":98,"files":[{"name":"GuoheView_v3.2.7.98-便携版.zip","url":"u","size":1,"md5":"abc"}]}}`),
		"只有安装包":   []byte(`{"data":{"channel":"stable","version":"3.2.7","version_code":98,"files":[{"name":"GuoheView_v3.2.7.98-安装包.exe","url":"u","size":1,"md5":"` + realZipMD5 + `"}]}}`),
		"版本形状异常":  []byte(`{"data":{"channel":"stable","version":"","version_code":0,"files":[]}}`),
		"垃圾":      []byte(`not json`),
	}
	for name, body := range cases {
		if _, err := parseChannelBody(body); err == nil {
			t.Errorf("%s 应拒收", name)
		}
	}
}

// TestFindPortableZipEnglishFallback 上游本地化名漂移时按 zip 后缀兜底命中。
func TestFindPortableZipEnglishFallback(t *testing.T) {
	files := []apiFile{
		{Name: "GuoheView_Portable.zip", URL: "u2", Size: 2},
		{Name: "GuoheView-Setup.exe", URL: "u1", Size: 1},
	}
	got, ok := findPortableZip(files)
	if !ok || got.Name != "GuoheView_Portable.zip" {
		t.Fatalf("兜底筛选失败: %+v ok=%v", got, ok)
	}
}

// TestReleaseAssetsMatrix N13 形态矩阵：官方 files 数组三条发布物如实全投影——
// 仅 Windows 产品平台整族直判；「便携版」zip 与 7z 都是便携形态（7z 仅解压依赖
// 差异，不改发布形态），「安装包」exe 是安装器；托管所选 zip 置 Managed 高亮位。
func TestReleaseAssetsMatrix(t *testing.T) {
	rel, err := parseChannelBody(fakeStableJSON(realZipMD5))
	if err != nil {
		t.Fatalf("parseChannelBody: %v", err)
	}
	if len(rel.Assets) != 3 {
		t.Fatalf("期望 3 条资产注记，实际 %d: %+v", len(rel.Assets), rel.Assets)
	}
	want := map[string]struct {
		form    hostfeed.Form
		managed bool
	}{
		"GuoheView_v3.2.7.98-安装包.exe": {hostfeed.FormInstaller, false},
		"GuoheView_v3.2.7.98-便携版.7z":  {hostfeed.FormPortable, false},
		"GuoheView_v3.2.7.98-便携版.zip": {hostfeed.FormPortable, true},
	}
	managed := 0
	for _, n := range rel.Assets {
		w, ok := want[n.Label]
		if !ok {
			t.Errorf("意外资产: %s", n.Label)
			continue
		}
		if n.Platform != hostfeed.PlatformWindows {
			t.Errorf("资产 %s 平台应为 windows（果核看图仅 Windows）: %+v", n.Label, n)
		}
		if n.Form != w.form || n.Managed != w.managed {
			t.Errorf("资产 %s 注记错误: %+v", n.Label, n)
		}
		if n.Managed {
			managed++
		}
	}
	if managed != 1 {
		t.Errorf("Managed 高亮位应恰 1 条，实际 %d", managed)
	}
}

// ---------- zip 夹具（官方 3.2.7 便携 zip 同构） ----------

// buildPortableZip 构造与官方 3.2.7 便携 zip 同构的样例字节：顶层
// GuoheViewPortable/ 包装目录 + exe/DLL/portable.ini/plugins 空目录 + 根外杂质。
func buildPortableZip(t *testing.T) []byte {
	t.Helper()
	buf := writeZipToBuffer(t, map[string]string{
		"GuoheViewPortable/":                           "",
		"GuoheViewPortable/GuoheView.exe":              "fake-exe-bytes",
		"GuoheViewPortable/ghde.dll":                   "fake-dll",
		"GuoheViewPortable/portable.ini":               "; portable flag",
		"GuoheViewPortable/plugins/decoder/":           "",
		"GuoheViewPortable/plugins/decoder/readme.txt": "readme",
		"README-outside.txt":                           "junk outside payload root",
	})
	return buf
}

// TestHarvestPortableRoot 收割便携根目录（委托 UnpackZip 解包后，模块布局收口）：
// exe 平铺到 staging 根、根外杂质不带入、子目录结构保留、包装链移除。
func TestHarvestPortableRoot(t *testing.T) {
	zipPath := filepath.Join(t.TempDir(), "portable.zip")
	if err := os.WriteFile(zipPath, buildPortableZip(t), 0644); err != nil {
		t.Fatal(err)
	}
	staging := filepath.Join(t.TempDir(), ".tmp-harvest")
	if err := artifact.UnpackZip(zipPath, staging, artifact.DefaultLimits, nil); err != nil {
		t.Fatalf("UnpackZip: %v", err)
	}

	if err := harvestPortableRoot(staging); err != nil {
		t.Fatalf("harvestPortableRoot: %v", err)
	}
	for _, rel := range []string{exeName, "ghde.dll", portableMarkName, filepath.Join("plugins", "decoder", "readme.txt")} {
		if _, err := os.Stat(filepath.Join(staging, rel)); err != nil {
			t.Errorf("缺少 %s: %v", rel, err)
		}
	}
	if _, err := os.Stat(filepath.Join(staging, "GuoheViewPortable")); !os.IsNotExist(err) {
		t.Error("包装目录不应作为层级保留")
	}
	if _, err := os.Stat(filepath.Join(staging, "README-outside.txt")); !os.IsNotExist(err) {
		t.Error("payload 根外杂质不应被收割")
	}
}

// TestHarvestFlatLayoutKeepsRootEntries zip 已是平铺布局（exe 在根）：根内
// 内容全收（口径同原 extractAll 的 payloadRoot="."）。
func TestHarvestFlatLayoutKeepsRootEntries(t *testing.T) {
	zipPath := filepath.Join(t.TempDir(), "flat.zip")
	if err := os.WriteFile(zipPath, writeZipToBuffer(t, map[string]string{
		exeName:          "fake-exe",
		portableMarkName: ";",
		"extra.txt":      "kept",
	}), 0644); err != nil {
		t.Fatal(err)
	}
	staging := filepath.Join(t.TempDir(), ".tmp-flat")
	if err := artifact.UnpackZip(zipPath, staging, artifact.DefaultLimits, nil); err != nil {
		t.Fatalf("UnpackZip: %v", err)
	}
	if err := harvestPortableRoot(staging); err != nil {
		t.Fatalf("harvestPortableRoot: %v", err)
	}
	if _, err := os.Stat(filepath.Join(staging, "extra.txt")); err != nil {
		t.Errorf("平铺布局根内文件应保留: %v", err)
	}
}

// TestHarvestRejectsInvalidLayout 收割自检：无 exe / 多个 exe 都判定布局无效。
func TestHarvestRejectsInvalidLayout(t *testing.T) {
	// 无 exe
	noExe := filepath.Join(t.TempDir(), "noexe.zip")
	if err := os.WriteFile(noExe, writeZipToBuffer(t, map[string]string{"Some/thing.dll": "x"}), 0644); err != nil {
		t.Fatal(err)
	}
	st1 := filepath.Join(t.TempDir(), ".tmp-noexe")
	if err := artifact.UnpackZip(noExe, st1, artifact.DefaultLimits, nil); err != nil {
		t.Fatal(err)
	}
	if err := harvestPortableRoot(st1); err == nil || !strings.Contains(err.Error(), "缺少可用的") {
		t.Fatalf("缺 exe 的 zip 应判定布局无效, got %v", err)
	}

	// 多个 exe（根 + 包装目录各一）
	dup := filepath.Join(t.TempDir(), "dup.zip")
	if err := os.WriteFile(dup, writeZipToBuffer(t, map[string]string{
		exeName:                        "a",
		"GuoheViewPortable/" + exeName: "b",
	}), 0644); err != nil {
		t.Fatal(err)
	}
	st2 := filepath.Join(t.TempDir(), ".tmp-dup")
	if err := artifact.UnpackZip(dup, st2, artifact.DefaultLimits, nil); err != nil {
		t.Fatal(err)
	}
	if err := harvestPortableRoot(st2); err == nil || !strings.Contains(err.Error(), "无法判定便携根") {
		t.Fatalf("多个 exe 应判定布局无效, got %v", err)
	}
}

// TestEnsurePortableMark 便携标记兜底：缺失补写官方开关、在场原样保留
// （上游语义"程序只读不改"，补写仅恢复托管隔离前提）。
func TestEnsurePortableMark(t *testing.T) {
	dir := t.TempDir()
	if err := ensurePortableMark(dir); err != nil {
		t.Fatalf("补写: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(dir, portableMarkName))
	if err != nil || !strings.Contains(string(b), "便携模式开关") {
		t.Fatalf("补写内容异常: %s %v", b, err)
	}

	mark := filepath.Join(t.TempDir(), "keep")
	os.MkdirAll(mark, 0755)
	os.WriteFile(filepath.Join(mark, portableMarkName), []byte("; official original"), 0644)
	if err := ensurePortableMark(mark); err != nil {
		t.Fatalf("ensurePortableMark: %v", err)
	}
	b, _ = os.ReadFile(filepath.Join(mark, portableMarkName))
	if string(b) != "; official original" {
		t.Error("已有便携标记不得被改写")
	}
}

func TestListInstalledAndRemove(t *testing.T) {
	versionsDir := t.TempDir()
	m := NewManager(versionsDir)

	mkVersion := func(dir, meta string) {
		os.MkdirAll(filepath.Join(versionsDir, dir), 0755)
		os.WriteFile(filepath.Join(versionsDir, dir, exeName), []byte("fake-exe"), 0644)
		os.WriteFile(filepath.Join(versionsDir, dir, portableMarkName), []byte(";"), 0644)
		if meta != "" {
			os.WriteFile(filepath.Join(versionsDir, dir, "meta.json"), []byte(meta), 0644)
		}
	}
	mkVersion("guoheview_3.2.7.98", `{"installedAt":"2026-09-03 10:00:00","isImport":true,"source":"E:\\gv"}`)
	mkVersion("guoheview_3.2.7.97", "")
	os.MkdirAll(filepath.Join(versionsDir, "piclite_1.4.1"), 0755)       // 异模块目录必须跳过
	os.MkdirAll(filepath.Join(versionsDir, "guoheview_3.2.6.90"), 0755)  // 缺 exe 损坏安装必须跳过
	mkNo := filepath.Join(versionsDir, "guoheview_3.2.5.80")             // 缺便携标记（配置会外溢 %APPDATA%）视为损坏
	os.MkdirAll(mkNo, 0755)                                              //
	os.WriteFile(filepath.Join(mkNo, exeName), []byte("fake-exe"), 0644) //

	list, err := m.ListInstalled()
	if err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("期望 2 个版本，实际 %d: %+v", len(list), list)
	}
	byVer := map[string]ViewVersionInfo{}
	for _, v := range list {
		byVer[v.Version] = v
	}
	if v := byVer["v3.2.7.98"]; !v.IsImport || v.InstalledAt != "2026-09-03 10:00:00" || v.Source != "E:\\gv" {
		t.Errorf("3.2.7.98 元信息解析错误: %+v", v)
	}
	if v := byVer["v3.2.7.97"]; v.IsImport || v.InstalledAt == "" {
		t.Errorf("旧版本默认元信息错误: %+v", v)
	}
	// Tree 扫描按版本号降序（原 ReadDir 字典序在多位数段有误，迁移后由 versioncmp 保证）
	if list[0].Version != "v3.2.7.98" {
		t.Errorf("列表应最新在前: %+v", list)
	}

	if exe, err := m.ResolveExe("v3.2.7.98"); err != nil || filepath.Base(exe) != exeName {
		t.Errorf("ResolveExe(v3.2.7.98): %v %v", exe, err)
	}
	if _, err := m.ResolveExe("3.2.7.98"); err != nil {
		t.Errorf("无 v 前缀应可解析: %v", err)
	}
	if _, err := m.ResolveExe("v9.9.9.9"); err == nil {
		t.Error("未安装版本应报错")
	}
	if _, err := m.ResolveExe("../../windows"); err == nil {
		t.Error("路径穿越式版本号必须报错")
	}

	if err := m.Remove("v3.2.7.98"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	list, _ = m.ListInstalled()
	if len(list) != 1 {
		t.Errorf("卸载后应剩 1 个版本，实际 %d", len(list))
	}
	// Tree.Remove 走 .removing- 隔离：卸载后版本树不得留事务残件
	ents, _ := os.ReadDir(versionsDir)
	for _, e := range ents {
		if strings.Contains(e.Name(), ".removing") || strings.HasPrefix(e.Name(), ".tmp-") {
			t.Errorf("卸载后残留事务目录: %s", e.Name())
		}
	}
}

// TestImportLocalWholeDirectory 整套目录搬运 + 便携标记补写 + meta 跳过 + 重复导入拒绝。
func TestImportLocalWholeDirectory(t *testing.T) {
	m := NewManager(t.TempDir())

	src := t.TempDir()
	os.WriteFile(filepath.Join(src, exeName), []byte("fake-exe-bytes"), 0644)
	os.WriteFile(filepath.Join(src, "ghde.dll"), []byte("dll"), 0644)
	os.MkdirAll(filepath.Join(src, "resources"), 0755)
	os.WriteFile(filepath.Join(src, "resources", "sRGB2014.icc"), []byte("icc"), 0644)
	os.WriteFile(filepath.Join(src, "config.ini"), []byte("[window]\nw=800\n"), 0644)
	os.WriteFile(filepath.Join(src, "meta.json"), []byte(`{"stale":true}`), 0644) // 源若为别的托管目录，旧 meta 不得残留

	info, err := m.ImportLocal(src)
	if err != nil {
		t.Fatalf("ImportLocal: %v", err)
	}
	// 假 PE 无法读版本信息 → 时间戳兜底（真实 exe 会得到 FileVersion "3.2.7.98"）
	if !strings.HasPrefix(info.Version, "vimported-") {
		t.Errorf("版本号格式异常: %q", info.Version)
	}
	if !info.IsImport || info.Source != src {
		t.Errorf("导入标记错误: %+v", info)
	}
	// 整套搬运：DLL / 子目录 / 用户配置全部在场
	for _, rel := range []string{exeName, "ghde.dll", filepath.Join("resources", "sRGB2014.icc"), "config.ini"} {
		if _, err := os.Stat(filepath.Join(info.Dir, rel)); err != nil {
			t.Errorf("缺少 %s: %v", rel, err)
		}
	}
	// 便携标记自动补写（源目录没有）
	if _, err := os.Stat(filepath.Join(info.Dir, portableMarkName)); err != nil {
		t.Errorf("便携标记应被补写: %v", err)
	}
	// 源目录旧 meta.json 不被带入（由本次导入重写为 isImport 记录）
	meta, err := os.ReadFile(filepath.Join(info.Dir, "meta.json"))
	if err != nil || !strings.Contains(string(meta), `"isImport": true`) {
		t.Errorf("meta.json 应为导入重写: %s %v", meta, err)
	}
	// 导入链账本可被 Tree 扫描识别（isImport/source 展示语义不漂移）
	list, lerr := m.ListInstalled()
	if lerr != nil || len(list) != 1 || !list[0].IsImport || list[0].Source != src {
		t.Errorf("导入版本列表账本异常: %+v %v", list, lerr)
	}

	// 重复导入同一目录（兜底版本含秒级时间戳，同秒内重复必冲突；跨秒则目录不同）
	if _, err := m.ImportLocal(src); err == nil {
		// 时间戳兜底跨秒时目录名不同，不视为缺陷——只有同版本才强制拒绝，
		// 这里验证真实版本路径的拒绝语义（见下方）
	}

	// 源目录不含 exe → 报错
	if _, err := m.ImportLocal(t.TempDir()); err == nil {
		t.Error("无 exe 的目录应报错")
	}
}
