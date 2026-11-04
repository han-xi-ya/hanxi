package version

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func shaHex(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// fakeReleasesJSON 构造与真实 GitHub API 同构的样例响应（结构体编码，免
// 手写 JSON 的引号漂移），覆盖阶段 0 实证形态：裸名资产 19 连版稳定（名字
// 不含版本段）、多版乱序（v1.9.0 vs v1.10.0 校验语义序非字典序）、托管下限
// 恰界（v1.1.0 入列、v1.0.9 出局）、.zip.sha256 sidecar（选中与展示都不得
// 沾染——唯一信任根恒为 digest）、mac/linux 变体、draft/预发布/非规范 tag/
// digest 缺失或非法/size<=0/无本平台资产各例。
func fakeReleasesJSON() []byte {
	h := func(ch byte) string { return "sha256:" + strings.Repeat(string(ch), 64) }
	rel := func(tag string, pre, draft bool, assets ...asset) release {
		return release{TagName: tag, PublishedAt: "2026-09-24T08:00:00Z", Prerelease: pre, Draft: draft, Assets: assets}
	}
	ast := func(name string, size int64, digest string) asset {
		return asset{Name: name, URL: "https://api.github.com/x/" + name, Size: size, Digest: digest}
	}
	releases := []release{
		rel("v1.2.0", false, false, ast(assetName, 48000000, h('a'))),
		rel("v1.10.0", false, false, ast(assetName, 48100000, h('b'))),
		rel("v1.9.0", false, false,
			ast(assetName, 48050000, h('c')),
			ast(assetName+".sha256", 80, h('9')), // 停发前的 sidecar：不选中、不展示
		),
		rel("v1.1.0", false, false, ast(assetName, 47000000, h('d'))),                      // 下限恰界
		rel("v1.0.9", false, false, ast(assetName, 46000000, h('e'))),                      // 低于托管下限（无机读模式）
		rel("v1.3.0", true, false, ast(assetName, 48000000, h('f'))),                       // 预发布
		rel("v1.4.0", false, true, ast(assetName, 48000000, h('1'))),                       // draft
		rel("release-2026", true, false, ast(assetName, 48000000, h('2'))),                 // 非规范 tag
		rel("v1.5.0", false, false, ast(assetName, 48000000, "")),                          // digest 缺失：宁拒不猜
		rel("v1.6.0", false, false, ast(assetName, 48000000, "sha256:short")),              // digest 非法
		rel("v1.7.0", false, false, ast(assetName, 0, h('3'))),                             // size<=0
		rel("v1.5.1", false, false, ast("piik-app-darwin-arm64.tar.gz", 40000000, h('4'))), // 无 Windows 裸名资产
	}
	body, err := json.Marshal(releases)
	if err != nil {
		panic(err)
	}
	return body
}

// TestParseReleasesBody 过滤与定序：入列恒 [v1.10.0, v1.9.0, v1.2.0, v1.1.0]
// （语义降序；1.10 > 1.9 钉死非字典序；v1.1.0 恰界入列）；v1.0.9（低于托管
// 下限）、v1.3.0（预发布）、v1.4.0（draft）、release-2026（非规范 tag）、
// v1.5.0（digest 缺失）、v1.6.0（digest 非法）、v1.7.0（size<=0）、
// v1.5.1（无 Windows 资产）一律出局。
func TestParseReleasesBody(t *testing.T) {
	list, err := parseReleasesBody(fakeReleasesJSON())
	if err != nil {
		t.Fatalf("parseReleasesBody: %v", err)
	}
	var got []string
	for _, r := range list {
		got = append(got, r.Version)
	}
	want := []string{"v1.10.0", "v1.9.0", "v1.2.0", "v1.1.0"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("列表漂移: %v", got)
	}
	for _, r := range list {
		if r.AssetName != assetName {
			t.Errorf("资产名应为裸名常量: %s", r.AssetName)
		}
		if len(r.SHA256) != 64 || strings.HasPrefix(r.SHA256, "sha256:") {
			t.Errorf("digest 剥前缀失败: %q", r.SHA256)
		}
		// sidecar 既不被选中也不进展示矩阵
		for _, n := range r.Assets {
			if strings.HasSuffix(strings.ToLower(n.Label), ".sha256") {
				t.Errorf("校验和 sidecar 混入展示矩阵: %+v", n)
			}
		}
	}
	top := list[0]
	if top.Size != 48100000 || top.SHA256 != strings.Repeat("b", 64) {
		t.Errorf("v1.10.0 解析错误: %+v", top)
	}
	// v1.9.0 的 mac/sidecar 伴生件展示纪律：chosen 标本托管，其他不标
	var sidecar, arm bool
	for _, n := range list[1].Assets {
		switch {
		case strings.HasSuffix(n.Label, ".sha256"):
			sidecar = true
		case n.Managed && n.Label != assetName:
			t.Errorf("非选中资产标了本托管: %+v", n)
		case !n.Managed && n.Label == assetName:
			t.Errorf("选中资产没标本托管: %+v", n)
		}
	}
	if sidecar || arm {
		t.Error("sidecar 应被滤除")
	}
}

func TestFindPortableAsset(t *testing.T) {
	assets := []asset{
		{Name: "piik-app-darwin-arm64.tar.gz", Size: 1},
		{Name: "piik-app-linux-amd64.tar.gz", Size: 1},
		{Name: "piik-app-windows-amd64.zip.sha256", Size: 80}, // sidecar 永不选中
		{Name: "Piik-App-Windows-AMD64.zip", Size: 1},         // 异形大小写宁拒不猜
		{Name: assetName, Size: 48000000, Digest: "sha256:" + strings.Repeat("a", 64)},
	}
	got, ok := findPortableAsset(assets)
	if !ok || got.Name != assetName || got.Size != 48000000 {
		t.Fatalf("应命中裸名资产: %+v", got)
	}
	if _, ok := findPortableAsset(assets[:4]); ok {
		t.Error("无裸名资产的 release 不应命中")
	}
}

func TestPlainSemverTagAndAssetNameGates(t *testing.T) {
	for _, tag := range []string{"v1.1.0", "v1.10.0", "v12.3.4"} {
		if !plainSemverTag.MatchString(tag) {
			t.Errorf("%s 应过 tag 闸", tag)
		}
	}
	for _, tag := range []string{"1.2.0", "v1.2", "v1.2.0-beta", "V1.2.0", "v1.2.0.1", "nightly"} {
		if plainSemverTag.MatchString(tag) {
			t.Errorf("%s 不应过 tag 闸", tag)
		}
	}
}

// TestAssetMirrors 镜像家族形制：GitHub 直链主址在首，家族镜像随后；
// Gitee 镜像备位仅注记未实测——候选列表里不得出现来路不明的域名。
func TestAssetMirrors(t *testing.T) {
	urls := assetMirrors("v1.2.0", assetName)
	if urls[0] != "https://github.com/TNTcraftHIM/Piik/releases/download/v1.2.0/"+assetName {
		t.Errorf("主址应为 GitHub 直链: %s", urls[0])
	}
	joined := strings.Join(urls, ",")
	if !strings.Contains(joined, "ghfast.top") || !strings.Contains(joined, "gh-proxy.com") || !strings.Contains(joined, "mirror.ghproxy.com") {
		t.Errorf("家族镜像缺失: %v", urls)
	}
	if strings.Contains(strings.ToLower(joined), "gitee") {
		t.Errorf("未实测 Gitee 备位不得进候选: %v", urls)
	}
}

func TestVersionFromToken(t *testing.T) {
	tests := []struct {
		token   string
		wantVer string
		wantOK  bool
	}{
		{"1.2.0", "v1.2.0", true},
		{"imported-20260926-150405", "vimported-20260926-150405", true},
		{"v1.2.0", "", false}, // 带 v 前缀目录名非本模块落位形状
		{"1.2", "", false},    // 必须纯 x.y.z
		{"..", "", false},
		{"", "", false},
	}
	for _, tt := range tests {
		ver, ok := versionFromToken(tt.token)
		if ok != tt.wantOK || ver != tt.wantVer {
			t.Errorf("versionFromToken(%q) = (%q,%v), want (%q,%v)", tt.token, ver, ok, tt.wantVer, tt.wantOK)
		}
	}
}

// ---------- 布局不变式 ----------

// makeTestZip 构造测试 zip（条目名用 "/" 分隔，runtime 子树与实证平铺一致）。
func makeTestZip(t *testing.T, entries map[string]string) string {
	t.Helper()
	f, err := os.CreateTemp("", "piiktest-*.zip")
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

// piikZipEntries 上游根平铺形状，条目名**逐字取自外部实测**而非按实现反推：
// 2026-11-04 用 HTTP Range 只拉 zip 中央目录，比过 v1.6.5 / v2.0.0 / v2.2.0 /
// v2.4.0 / v2.5.3 五版，十条目名完全一致（探测法与清单见
// docs/TROUBLESHOOTING.md #146）。
//
// 这条夹具曾经就是事故的共犯：实现把告知文件写成 NOTICES，夹具照抄 NOTICES，
// 于是"全绿"与"上游从来没有这个条目名"同时成立——断言抄实现等于没测。现在
// 夹具的来源是上游包本身，改名前必须先重新实测一次中央目录。
func piikZipEntries() map[string]string {
	return map[string]string{
		exeName:                             "fake-piik-app-bytes",
		licenseFileName:                     "MIT License TNTcraftHIM",
		revisionFileName:                    "cafe1234deadbeef",
		noticesFileName:                     "third-party notices",
		captureExeRel:                       "fake-capture-bytes",
		cloudflaredExeRel:                   "fake-cloudflared-bytes",
		"runtime/tunnel/" + noticesFileName: "tunnel notices",
		"runtime/native/extra":              "sibling asset",
		"junk.txt":                          "包内杂物不得影响判定",
	}
}

func materializeZip(t *testing.T, entries map[string]string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "staged")
	zp := makeTestZip(t, entries)
	r, err := zip.OpenReader(zp)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	for _, f := range r.File {
		target := filepath.Join(dir, filepath.FromSlash(f.Name))
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			t.Fatal(err)
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		out, err := os.Create(target)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := out.ReadFrom(rc); err != nil {
			t.Fatal(err)
		}
		out.Close()
		rc.Close()
	}
	return dir
}

func TestCheckPiikLayout(t *testing.T) {
	dir := materializeZip(t, piikZipEntries())
	if err := checkPiikLayout(dir); err != nil {
		t.Fatalf("上游实测形状应通过: %v", err)
	}

	// 硬不变式＝"这份安装能不能跑"：主 exe 与 runtime 双 exe 缺失/置空都必须拒
	// （fail-closed 面逐条钉死）。
	cases := []struct {
		name   string
		mutate func(map[string]string)
		want   string
	}{
		{"主 exe 缺失", func(m map[string]string) { delete(m, exeName) }, exeName},
		{"主 exe 空件", func(m map[string]string) { m[exeName] = "" }, exeName},
		{"capture 缺失", func(m map[string]string) { delete(m, captureExeRel) }, captureExeRel},
		{"capture 空件", func(m map[string]string) { m[captureExeRel] = "" }, captureExeRel},
		{"cloudflared 缺失", func(m map[string]string) { delete(m, cloudflaredExeRel) }, cloudflaredExeRel},
		{"cloudflared 空件", func(m map[string]string) { m[cloudflaredExeRel] = "" }, cloudflaredExeRel},
	}
	for _, c := range cases {
		entries := piikZipEntries()
		c.mutate(entries)
		bad := materializeZip(t, entries)
		err := checkPiikLayout(bad)
		if err == nil || !strings.Contains(err.Error(), filepath.Base(c.want)) {
			t.Errorf("%s: 应拒装且点名 %s，得 %v", c.name, c.want, err)
		}
	}

	// 软面＝说明文档族：缺失/空件/改名一律**不得**拒装。#146 的回归钉——
	// 本包曾把告知文件按 NOTICES 钉成拒装条件（上游从来没这个名），结果是
	// 远程下载链一个版本都没装成过；文档条目属上游惯例，改名不应砖化功能，
	// 完整性已由 GitHub 官方 digest 在解包前收口。
	softCases := []struct {
		name   string
		mutate func(map[string]string)
	}{
		{"LICENSE 缺失", func(m map[string]string) { delete(m, licenseFileName) }},
		{"REVISION 缺失", func(m map[string]string) { delete(m, revisionFileName) }},
		{"REVISION 空件", func(m map[string]string) { m[revisionFileName] = "" }},
		{"告知文件缺失", func(m map[string]string) { delete(m, noticesFileName) }},
		{"告知文件空件", func(m map[string]string) { m[noticesFileName] = "" }},
		{"告知文件改名（上游漂移样本）", func(m map[string]string) {
			m["THIRD-PARTY-NOTICES"] = m[noticesFileName]
			delete(m, noticesFileName)
		}},
		{"tunnel 内告知文件缺失", func(m map[string]string) { delete(m, "runtime/tunnel/"+noticesFileName) }},
	}
	for _, c := range softCases {
		entries := piikZipEntries()
		c.mutate(entries)
		soft := materializeZip(t, entries)
		if err := checkPiikLayout(soft); err != nil {
			t.Errorf("%s: 文档族缺位不得拒装，得 %v", c.name, err)
		}
	}

	// runtime 目录整体不在（兄弟路径解析地基坍塌）
	noRuntime := piikZipEntries()
	delete(noRuntime, captureExeRel)
	delete(noRuntime, cloudflaredExeRel)
	delete(noRuntime, "runtime/native/extra")
	dir2 := materializeZip(t, noRuntime)
	if err := checkPiikLayout(dir2); err == nil || !strings.Contains(err.Error(), "runtime") {
		t.Errorf("runtime 目录缺失应拒装并点名，得 %v", err)
	}
}

func TestReadRevision(t *testing.T) {
	dir := materializeZip(t, piikZipEntries())
	if got := readRevision(dir); got != "cafe1234deadbeef" {
		t.Errorf("REVISION 记账漂移: %q", got)
	}
	missing := t.TempDir()
	if got := readRevision(missing); got != "" {
		t.Errorf("REVISION 缺失应空串降级: %q", got)
	}
}
