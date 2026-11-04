// 远程列表解析与资产筛选的判据测试。
//
// 断言口径刻意对着"外部事实"写：上游真实 Release 的资产形状（同一 Release 并挂
// zip/裸 exe/latest.json 三份、v0.5.0 及更早前缀是 jietu-）由侦查阶段抓取的
// GitHub API 快照固化在样例里，而不是把实现的字符串形状抄一遍——实现改了判据
// （比如误收裸 exe、或忘了改名漂移）必须在这里变红。
package version

import (
	"encoding/json"
	"strings"
	"testing"
)

// realV070 上游 v0.7.0 的真实资产三元组（api.github.com releases 快照，2026-11-02 抓）。
const realV070 = `{
	"tag_name": "v0.7.0",
	"published_at": "2026-11-02T07:27:50Z",
	"prerelease": false,
	"draft": false,
	"assets": [
		{"name":"airsnip-v0.7.0-win-x64.exe","url":"https://api.github.com/repos/bd-dxg/airsnip/releases/assets/700900107","size":1264640,"digest":"sha256:058f9c3148a6e325ca3e11dceb65efd5915cbfffcf8c37087a1b7eea485316cc"},
		{"name":"airsnip-v0.7.0-win-x64.zip","url":"https://api.github.com/repos/bd-dxg/airsnip/releases/assets/700900106","size":707594,"digest":"sha256:e3bb4f412d9a3f393c7fbf790437d658d514a17996951acbc639d546743379b4"},
		{"name":"latest.json","url":"https://api.github.com/repos/bd-dxg/airsnip/releases/assets/700900855","size":528,"digest":"sha256:6a51c541977befa6126e629be5d49147f7df5706532b448f9e7a4959a03ae2ea"}
	]
}`

// realV050 改名前（jietu-）的真实资产形状：只有一份 zip，无任何 zip 之外的变体。
const realV050 = `{
	"tag_name": "v0.5.0",
	"published_at": "2026-10-28T13:24:13Z",
	"prerelease": false,
	"draft": false,
	"assets": [
		{"name":"jietu-v0.5.0-win-x64.zip","url":"https://api.github.com/repos/bd-dxg/airsnip/releases/assets/685077103","size":645162,"digest":"sha256:c7d53924fa1c6cc5cc55e241df67a1420c1afb14d488de1f2090e36225883c4e"}
	]
}`

// compact 校验样例 JSON 可解析并回填紧凑文本（便于做字符串级替换构造变体）。
func compact(t *testing.T, raw string) string {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("样例 JSON 不可解析: %v", err)
	}
	out, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("样例 JSON 再编码失败: %v", err)
	}
	return string(out)
}

// oneRelease 把单个 release 对象包成 API 响应数组。
func oneRelease(t *testing.T, obj string) []byte {
	t.Helper()
	return []byte("[" + obj + "]")
}

// assetsOf 取真实样例的资产切片（供 findPortableAsset 直接调用）。
func assetsOf(t *testing.T, raw string) []asset {
	t.Helper()
	var r release
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		t.Fatalf("样例 JSON 不可解析: %v", err)
	}
	return r.Assets
}

// TestFindPortableAssetPicksZipNotBareExe 命中的必须是 zip：上游 v0.7.0 起并挂
// 裸 exe（自动更新件），选错就把"包"当"已解压产物"落位，整条链静默错位。
func TestFindPortableAssetPicksZipNotBareExe(t *testing.T) {
	assets := assetsOf(t, realV070)
	a, ok := findPortableAsset(assets, "v0.7.0")
	if !ok {
		t.Fatal("真实 v0.7.0 资产应当命中便携 zip")
	}
	if a.Name != "airsnip-v0.7.0-win-x64.zip" {
		t.Errorf("应选 zip，实际选中 %s", a.Name)
	}
}

// TestFindPortableAssetSurvivesRename 改名漂移：v0.5.0 及更早前缀 jietu- 必须仍能
// 被后缀兜底召回，否则历史版本整列消失，用户无从回退。
func TestFindPortableAssetSurvivesRename(t *testing.T) {
	a, ok := findPortableAsset(assetsOf(t, realV050), "v0.5.0")
	if !ok || a.Name != "jietu-v0.5.0-win-x64.zip" {
		t.Fatalf("改名前的 jietu- 前缀应经后缀兜底命中，got ok=%v name=%q", ok, a.Name)
	}
}

// TestFindPortableAssetRejectsOtherShapes 他平台/他形态/元数据资产一律不选；
// 混排时唯一命中真 zip。
func TestFindPortableAssetRejectsOtherShapes(t *testing.T) {
	distractors := []string{
		"airsnip-v0.8.0-win-arm64.zip",
		"airsnip-v0.8.0-win-x64.msi",
		"airsnip-v0.8.0-win-x64.zip.sig",
		"latest.json",
	}
	for _, name := range distractors {
		assets := []asset{{Name: name, Digest: "sha256:" + strings.Repeat("a", 64)}}
		if got, ok := findPortableAsset(assets, "v0.8.0"); ok {
			t.Errorf("资产 %s 不该被当成便携 zip 选中（got %s）", name, got.Name)
		}
	}
	assets := []asset{{Name: distractors[0], Digest: "sha256:" + strings.Repeat("a", 64)}}
	assets = append(assets, asset{Name: "airsnip-v0.8.0-win-x64.zip", Digest: "sha256:" + strings.Repeat("b", 64)})
	if got, ok := findPortableAsset(assets, "v0.8.0"); !ok || got.Name != "airsnip-v0.8.0-win-x64.zip" {
		t.Errorf("混排时应唯一命中 win-x64.zip，got ok=%v name=%q", ok, got.Name)
	}
}

// TestParseReleasesBodyRealSample 真实 v0.7.0 快照入列后的每个字段都要对上外部事实。
func TestParseReleasesBodyRealSample(t *testing.T) {
	list, err := parseReleasesBody(oneRelease(t, compact(t, realV070)))
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("应恰好入列 1 条，实际 %d", len(list))
	}
	got := list[0]
	if got.Version != "v0.7.0" || got.AssetName != "airsnip-v0.7.0-win-x64.zip" {
		t.Errorf("版本/资产名不符: %+v", got)
	}
	if got.SHA256 != "e3bb4f412d9a3f393c7fbf790437d658d514a17996951acbc639d546743379b4" {
		t.Errorf("digest 前缀未正确剥离: %q", got.SHA256)
	}
	if got.Size != 707594 {
		t.Errorf("字节数应取 zip 的 707594（不是裸 exe 的 1264640），实际 %d", got.Size)
	}
	// 展示矩阵走共享分类器 hostfeed.Notes：签名/更新清单类（latest.json）属
	// 附属元数据，被 IsMetadata 闸滤出不进列表；剩下的 zip + 裸 exe 两份发布物
	// 必须都在，且只有 zip 标 Managed。
	if len(got.Assets) != 2 {
		t.Errorf("展示矩阵应收录 2 份发布物（latest.json 属元数据被滤），实际 %d: %+v", len(got.Assets), got.Assets)
	}
	for _, n := range got.Assets {
		if strings.EqualFold(n.Label, "latest.json") {
			t.Errorf("更新清单不应进展示矩阵: %+v", n)
		}
		if n.Managed != (n.Label == "airsnip-v0.7.0-win-x64.zip") {
			t.Errorf("Managed 标记只应落在被托管的 zip 上: %+v", n)
		}
	}
	if got.Published != "2026-11-02T07:27:50Z" {
		t.Errorf("发布时间透传错误: %q", got.Published)
	}
}

// TestParseReleasesBodyGates 入列闸门：缺 digest / draft / 非纯语义 tag / 只有裸 exe
// 四种情形一律不入列（完整性第一层不能缺位）。
func TestParseReleasesBodyGates(t *testing.T) {
	base := compact(t, realV070)
	cases := map[string]string{
		"缺官方 digest": strings.Replace(base, `"sha256:e3bb4f412d9a3f393c7fbf790437d658d514a17996951acbc639d546743379b4"`, `""`, 1),
		"draft":      strings.Replace(base, `"draft":false`, `"draft":true`, 1),
		"非纯语义 tag":   strings.Replace(base, `"tag_name":"v0.7.0"`, `"tag_name":"v0.7.0-beta.1"`, 1),
		"只有裸 exe": `{
			"tag_name":"v0.9.0","published_at":"2026-12-01T00:00:00Z","prerelease":false,"draft":false,
			"assets":[{"name":"airsnip-v0.9.0-win-x64.exe","url":"u","size":1,"digest":"sha256:` + strings.Repeat("f", 64) + `"}]
		}`,
	}
	for name, obj := range cases {
		list, err := parseReleasesBody(oneRelease(t, compact(t, obj)))
		if err != nil {
			t.Fatalf("%s: 解析失败 %v", name, err)
		}
		if len(list) != 0 {
			t.Errorf("%s: 该 release 不应入列，实际 %d 条", name, len(list))
		}
	}
}

// TestParseReleasesBodyMixed 混合批次：真实 v0.7.0 + 改名前 v0.5.0 + 只有裸 exe 的
// 未来形态，只有前两条入列且保持 API 返回序（新在上）。
func TestParseReleasesBodyMixed(t *testing.T) {
	body := []byte("[" + compact(t, realV070) + "," + compact(t, realV050) +
		`,{"tag_name":"v0.9.0","published_at":"2026-12-01T00:00:00Z","prerelease":false,"draft":false,"assets":[{"name":"airsnip-v0.9.0-win-x64.exe","url":"u","size":1,"digest":"sha256:` +
		strings.Repeat("f", 64) + `"}]}]`)
	list, err := parseReleasesBody(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("应入列 2 条，实际 %d", len(list))
	}
	if list[0].Version != "v0.7.0" || list[1].Version != "v0.5.0" {
		t.Errorf("版本序或资产选择错误: %+v", list)
	}
}

// TestRepoURL 上游地址是页面「打开仓库」与文档对账的唯一事实源。
func TestRepoURL(t *testing.T) {
	if got := RepoURL(); got != "https://github.com/bd-dxg/airsnip" {
		t.Errorf("RepoURL = %q", got)
	}
}
