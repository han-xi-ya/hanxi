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
// 日更多版（v1.5.2/v1.5.3/v1.5.10，数组序故意打乱验证版本降序定序，
// 1.5.10 vs 1.5.3 同时校验语义序而非字典序）、arm64 便携变体例、minisign
// .sig 附属例（自带 digest 也不得被选中，且不进展示矩阵）、setup/msi 例、
// 无 x64 便携资产的 arm64-only 例、digest 缺失例、预发布例、非规范 tag 例、
// 版本错配挂名资产例、size<=0 例。
func fakeReleasesJSON(t *testing.T) []byte {
	t.Helper()
	h := func(ch byte) string { return "sha256:" + strings.Repeat(string(ch), 64) }
	body := `[
  {
    "tag_name": "v1.5.2",
    "published_at": "2026-09-23T08:00:00Z",
    "prerelease": false,
    "draft": false,
    "assets": [
      {"name": "DBX_1.5.2_x64-portable.zip", "url": "https://api.github.com/x/1", "size": 26214400, "digest": "` + h('a') + `"}
    ]
  },
  {
    "tag_name": "v1.5.10",
    "published_at": "2026-09-25T08:00:00Z",
    "prerelease": false,
    "draft": false,
    "assets": [
      {"name": "DBX_1.5.10_x64-portable.zip", "url": "https://api.github.com/x/2", "size": 26314400, "digest": "` + h('b') + `"},
      {"name": "DBX_1.5.10_x64-portable.zip.sig", "url": "https://api.github.com/x/3", "size": 400, "digest": "` + h('9') + `"},
      {"name": "DBX_1.5.10_x64-setup.exe", "url": "https://api.github.com/x/4", "size": 30000000, "digest": "` + h('c') + `"},
      {"name": "latest.json", "url": "https://api.github.com/x/5", "size": 900}
    ]
  },
  {
    "tag_name": "v1.5.3",
    "published_at": "2026-09-24T08:00:00Z",
    "prerelease": false,
    "draft": false,
    "assets": [
      {"name": "DBX_1.5.3_x64-portable.zip", "url": "https://api.github.com/x/6", "size": 26264400, "digest": "` + h('d') + `"},
      {"name": "DBX_1.5.3_arm64-portable.zip", "url": "https://api.github.com/x/7", "size": 25264400, "digest": "` + h('e') + `"},
      {"name": "DBX.1.5.3.x64.msi", "url": "https://api.github.com/x/8", "size": 31000000, "digest": "` + h('f') + `"}
    ]
  },
  {
    "tag_name": "v1.5.9",
    "published_at": "2026-09-22T08:00:00Z",
    "prerelease": false,
    "draft": false,
    "assets": [
      {"name": "DBX_1.5.9_arm64-portable.zip", "url": "https://api.github.com/x/9", "size": 25000000, "digest": "` + h('1') + `"},
      {"name": "DBX_1.5.9_aarch64-setup.exe", "url": "https://api.github.com/x/10", "size": 29000000, "digest": "` + h('2') + `"}
    ]
  },
  {
    "tag_name": "v1.5.8",
    "published_at": "2026-09-21T08:00:00Z",
    "prerelease": false,
    "draft": false,
    "assets": [
      {"name": "DBX_1.5.8_x64-portable.zip", "url": "https://api.github.com/x/11", "size": 26000000}
    ]
  },
  {
    "tag_name": "v1.5.7",
    "published_at": "2026-09-20T08:00:00Z",
    "prerelease": true,
    "draft": false,
    "assets": [
      {"name": "DBX_1.5.7_x64-portable.zip", "url": "https://api.github.com/x/12", "size": 26000000, "digest": "` + h('3') + `"}
    ]
  },
  {
    "tag_name": "nightly",
    "published_at": "2026-09-26T02:00:00Z",
    "prerelease": true,
    "draft": false,
    "assets": [
      {"name": "DBX_1.5.11_x64-portable.zip", "url": "https://api.github.com/x/13", "size": 26000000, "digest": "` + h('4') + `"}
    ]
  },
  {
    "tag_name": "v1.5.6",
    "published_at": "2026-09-19T08:00:00Z",
    "prerelease": false,
    "draft": false,
    "assets": [
      {"name": "DBX_1.5.0_x64-portable.zip", "url": "https://api.github.com/x/14", "size": 26000000, "digest": "` + h('5') + `"}
    ]
  },
  {
    "tag_name": "v1.5.5",
    "published_at": "2026-09-18T08:00:00Z",
    "prerelease": false,
    "draft": false,
    "assets": [
      {"name": "DBX_1.5.5_x64-portable.zip", "url": "https://api.github.com/x/15", "size": 0, "digest": "` + h('6') + `"}
    ]
  }
]`
	return []byte(body)
}

// TestParseReleasesBody 解析过滤：日更多版全部入列且按版本降序定序
// （v1.5.10 在 v1.5.3 之前——语义序而非字典序，数组输入序被打乱）；
// digest 剥前缀；arm64 变体不被选中；.sig（自带 digest）绝不入选择、
// 不进展示矩阵；setup/msi/latest.json 依纪律处理；v1.5.9（无 x64 便携）、
// v1.5.8（digest 缺失宁拒不猜）、v1.5.7（预发布）、nightly（非规范 tag）、
// v1.5.6（版本错配挂名）、v1.5.5（size<=0）一律丢弃。
func TestParseReleasesBody(t *testing.T) {
	list, err := parseReleasesBody(fakeReleasesJSON(t))
	if err != nil {
		t.Fatalf("parseReleasesBody: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("期望 3 个版本，实际 %d: %+v", len(list), list)
	}
	if list[0].Version != "v1.5.10" || list[1].Version != "v1.5.3" || list[2].Version != "v1.5.2" {
		t.Errorf("列表应语义版本降序（日更多版排序列）: %s, %s, %s", list[0].Version, list[1].Version, list[2].Version)
	}
	top := list[0]
	if top.SHA256 != strings.Repeat("b", 64) ||
		top.Size != 26314400 ||
		top.AssetName != "DBX_1.5.10_x64-portable.zip" {
		t.Errorf("v1.5.10 解析错误: %+v", top)
	}
	for _, gone := range []string{"v1.5.9", "v1.5.8", "v1.5.7", "nightly", "v1.5.6", "v1.5.5"} {
		for _, r := range list {
			if r.Version == gone {
				t.Errorf("%s 不应入列表", gone)
			}
		}
	}
	for _, r := range list {
		lower := strings.ToLower(r.AssetName)
		if strings.Contains(lower, "arm64") || strings.Contains(lower, "msi") ||
			strings.HasSuffix(lower, ".exe") || strings.HasSuffix(lower, ".sig") {
			t.Errorf("混入非 x64 便携 zip 资产: %s", r.AssetName)
		}
		if len(r.SHA256) != 64 {
			t.Errorf("sha256 格式异常: %q", r.SHA256)
		}
	}
	// 展示矩阵纪律：.sig 与 latest.json 元数据被分类器滤除；.sig 绝不标"本托管"
	for _, n := range top.Assets {
		if strings.HasSuffix(strings.ToLower(n.Label), ".sig") || n.Label == "latest.json" {
			t.Errorf("签名/清单元数据不应进展示矩阵: %+v", n)
		}
		if strings.Contains(strings.ToLower(n.Label), "arm64") && n.Managed {
			t.Errorf("arm64 资产不应标本托管: %+v", n)
		}
	}
	armSeen := false
	for _, n := range list[1].Assets {
		if n.Label == "DBX_1.5.3_arm64-portable.zip" {
			armSeen = true
			if n.Managed {
				t.Errorf("arm64 变体不应标本托管: %+v", n)
			}
		}
	}
	if !armSeen {
		t.Errorf("v1.5.3 arm64 变体应作为展示行存在: %+v", list[1].Assets)
	}
}

// TestFindPortableAsset 资产筛选：精确命中形如 DBX_1.5.3_x64-portable.zip；
// arm64/setup/msi/offline/win7 变体、同名 .sig、版本错配挂名资产绝不混入。
func TestFindPortableAsset(t *testing.T) {
	assets := []asset{
		{Name: "DBX_1.5.3_x64-setup.exe", Size: 1},
		{Name: "DBX.1.5.3.x64.msi", Size: 1},
		{Name: "DBX_1.5.3_arm64-portable.zip", Size: 1},
		{Name: "DBX_1.5.3_x64-portable.zip.sig", Size: 1}, // minisign 附属：不识别不消费
		{Name: "DBX_1.5.0_x64-portable.zip", Size: 1},     // 版本错配挂名
		{Name: "latest.json", Size: 1},
		{Name: "DBX_1.5.3_x64-portable.zip", Size: 26264400},
	}
	got, ok := findPortableAsset(assets, "v1.5.3")
	if !ok {
		t.Fatal("应命中 Windows x64 便携资产")
	}
	if got.Name != "DBX_1.5.3_x64-portable.zip" || got.Size != 26264400 {
		t.Errorf("命中错误资产: %+v", got)
	}
	if _, ok := findPortableAsset(assets[:6], "v1.5.3"); ok {
		t.Error("无 x64 便携 zip 资产的 release 不应命中")
	}
}

// TestPortableAssetRe 资产名正则闸：setup/msi/offline/win7/arm64/.sig 变体
// 与大小写异形的名字全部拒绝（sig 不识别的名单层兜底断言）。
func TestPortableAssetRe(t *testing.T) {
	accept := []string{"DBX_1.5.3_x64-portable.zip"}
	reject := []string{
		"DBX_1.5.3_x64-portable.zip.sig",
		"DBX_1.5.3_arm64-portable.zip",
		"DBX_1.5.3_x64-setup.exe",
		"DBX.1.5.3.x64.msi",
		"DBX_1.5.3_x64-offline-installer.exe",
		"DBX_1.5.3_x64-portable-win7.zip",
		"dbx_1.5.3_x64-portable.zip",
		"DBX_1.5_x64-portable.zip",
	}
	for _, n := range accept {
		if !portableAssetRe.MatchString(n) {
			t.Errorf("%s 应过资产正则", n)
		}
	}
	for _, n := range reject {
		if portableAssetRe.MatchString(n) {
			t.Errorf("%s 不应过资产正则", n)
		}
	}
}

// TestAssetMirrors 镜像回退家族形制：GitHub 直链主址在首位，家族镜像随后，
// 上游官网 dl.dbxio.com 备位排尾（未实测源，digest 同闸兜底）。
func TestAssetMirrors(t *testing.T) {
	urls := assetMirrors("v1.5.3", "DBX_1.5.3_x64-portable.zip")
	if len(urls) < 2 {
		t.Fatalf("镜像候选异常: %v", urls)
	}
	if urls[0] != "https://github.com/t8y2/dbx/releases/download/v1.5.3/DBX_1.5.3_x64-portable.zip" {
		t.Errorf("主址应为 GitHub 直链: %s", urls[0])
	}
	last := urls[len(urls)-1]
	if last != "https://dl.dbxio.com/releases/latest/DBX_1.5.3_x64-portable.zip" {
		t.Errorf("未实测官网备位应排末位: %s", last)
	}
	if !strings.Contains(strings.Join(urls, ","), "ghfast.top") {
		t.Errorf("家族镜像缺失: %v", urls)
	}
}

// makeTestZip 构造测试用 zip
func makeTestZip(t *testing.T, entries map[string]string) string {
	t.Helper()
	f, err := os.CreateTemp("", "dbxtest-*.zip")
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
		{"1.5.3", "v1.5.3", true},
		{"imported-20260926-150405", "vimported-20260926-150405", true},
		{"v1.5.3", "", false}, // 带 v 前缀的目录名非本模块落位格式
		{"1.5", "", false},    // 必须纯 x.y.z
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

func TestUnpackWithDBXLayout(t *testing.T) {
	zipPath := makeTestZip(t, map[string]string{
		exeName:                "fake-exe",
		"LICENSE":              "Apache License 2.0",
		"README.md":            "DBX readme",
		portableMarkName:       "", // 实证为空标记文件
		portableUpdateFileName: `{"platforms":{"windows-x86_64":{"executable_sha256":"` + shaHex([]byte("fake-exe")) + `"}}}`,
	})
	staging := filepath.Join(t.TempDir(), "staging")
	if err := artifact.UnpackZip(zipPath, staging, artifact.DefaultLimits, nil); err != nil {
		t.Fatalf("UnpackZip: %v", err)
	}
	if err := checkPortableLayout(staging); err != nil {
		t.Fatalf("checkPortableLayout: %v", err)
	}
	// Apache-2.0 合规 + 便携语义：解包保布局，五条目天然随版本目录保留
	for _, name := range []string{exeName, "LICENSE", "README.md", portableMarkName, portableUpdateFileName} {
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
	if err := os.WriteFile(filepath.Join(staging, portableMarkName), nil, 0644); err != nil {
		t.Fatal(err)
	}
	if err := checkPortableLayout(staging); err == nil {
		t.Fatal("缺 DBX.exe 应自检失败")
	}
}

func TestLayoutEmptyExe(t *testing.T) {
	staging := t.TempDir()
	if err := os.WriteFile(filepath.Join(staging, exeName), nil, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staging, portableMarkName), nil, 0644); err != nil {
		t.Fatal(err)
	}
	if err := checkPortableLayout(staging); err == nil {
		t.Fatal("空 exe 应判定为损坏安装")
	}
}

// TestLayoutMissingPortableMark 核心不变式：DBX.exe 与 portable.dbx 同在。
// 缺便携标记的包即使 exe 完好也拒落位——没有它应用以装机模式启动，托管语义失真。
func TestLayoutMissingPortableMark(t *testing.T) {
	staging := t.TempDir()
	if err := os.WriteFile(filepath.Join(staging, exeName), []byte("fake-exe"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staging, "LICENSE"), []byte("Apache"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := checkPortableLayout(staging); err == nil || !strings.Contains(err.Error(), portableMarkName) {
		t.Fatalf("缺 portable.dbx 应自检失败并点名, got %v", err)
	}
}

// TestCheckExeCorroboration portable-update.json exe 级旁证复核的宽容策略：
// 只在"声明值合法且与实测不符"时拒，缺失/坏 JSON/无可比对值一律放行。
func TestCheckExeCorroboration(t *testing.T) {
	content := []byte("fake-exe")
	actual := shaHex(content)

	staging := t.TempDir()
	if err := os.WriteFile(filepath.Join(staging, exeName), content, 0644); err != nil {
		t.Fatal(err)
	}
	// 无清单文件 → 静默通过
	if err := checkExeCorroboration(staging, actual); err != nil {
		t.Fatalf("缺旁证文件不应拒: %v", err)
	}
	write := func(body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(staging, portableUpdateFileName), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write(fmt.Sprintf(`{"version":"1.5.3","platforms":{"windows-x86_64":{"executable_sha256":"%s"}}}`, actual))
	if err := checkExeCorroboration(staging, actual); err != nil {
		t.Fatalf("旁证一致应通过: %v", err)
	}
	write(fmt.Sprintf(`{"platforms":{"windows-x86_64":{"executable_sha256":"%s"}}}`, strings.Repeat("a", 64)))
	if err := checkExeCorroboration(staging, actual); err == nil || !strings.Contains(err.Error(), "包内自不一致") {
		t.Fatalf("旁证矛盾应拒装, got %v", err)
	}
	write(`{not-json`)
	if err := checkExeCorroboration(staging, actual); err != nil {
		t.Fatalf("旁证不可解析应静默通过: %v", err)
	}
	write(`{"platforms":{"windows-x86_64":{"executable_sha256":"not-hex"}}}`)
	if err := checkExeCorroboration(staging, actual); err != nil {
		t.Fatalf("旁证格式非法应静默通过: %v", err)
	}
}

// ---------- 本地版本账与账外漂移护栏 ----------

// writeLedgerVersion 手工造一个带内核账本的已装版本目录（漂移用例的锚）。
// exe 与 portable.dbx 标记同在（ListInstalled 的布局不变式）。
func writeLedgerVersion(t *testing.T, versionsDir, token, exeContent, assetSHA string, landedAt string) string {
	t.Helper()
	dir := filepath.Join(versionsDir, dirPrefix+token)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, exeName), []byte(exeContent), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, portableMarkName), nil, 0644); err != nil {
		t.Fatal(err)
	}
	if assetSHA != "" || landedAt != "" {
		meta := fmt.Sprintf(`{"schema":1,"tool":"dbx","version":%q,"entry":"DBX.exe","assetSHA256":%q,"installedAt":%q,"source":"remote"}`,
			token, assetSHA, landedAt)
		if err := os.WriteFile(filepath.Join(dir, "meta.json"), []byte(meta), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// TestListInstalledAndDriftProjection 列表扫描 + 漂移投影四态：
// 账实相符（mtime 迹象触发复算、一致）、账外漂移（不符、如实标记——上游
// 自更新器原地换 exe 的正牌场景）、无改动迹象（mtime 早于落位记录、复算
// 未触发=成本控制主路径）、账本无摘要（无法比对、不猜）。异模块目录、
// 缺 exe 与缺便携标记的损坏目录跳过。
func TestListInstalledAndDriftProjection(t *testing.T) {
	versionsDir := t.TempDir()
	content := []byte("fake-exe")
	matchSHA := shaHex(content)

	writeLedgerVersion(t, versionsDir, "1.5.3", string(content), matchSHA, "2020-01-01T00:00:00Z")                // 复算一致
	writeLedgerVersion(t, versionsDir, "1.5.2", string(content), strings.Repeat("a", 64), "2020-01-01T00:00:00Z") // 账外漂移
	writeLedgerVersion(t, versionsDir, "1.5.1", string(content), matchSHA, "2099-01-01T00:00:00Z")                // 未触发复算
	writeLedgerVersion(t, versionsDir, "imported-20260101-120000", string(content), "", "")                       // 无法比对
	os.MkdirAll(filepath.Join(versionsDir, "gonavi_v0.8.9"), 0755)                                                // 异模块必须跳过
	os.MkdirAll(filepath.Join(versionsDir, dirPrefix+"1.5.0"), 0755)                                              // 缺 exe 损坏必须跳过
	broken := writeLedgerVersion(t, versionsDir, "1.4.9", string(content), matchSHA, "2020-01-01T00:00:00Z")      // 缺便携标记损坏必须跳过
	if err := os.Remove(filepath.Join(broken, portableMarkName)); err != nil {
		t.Fatal(err)
	}

	m := NewManager(versionsDir)
	list, err := m.ListInstalled()
	if err != nil {
		t.Fatalf("ListInstalled: %v", err)
	}
	if len(list) != 4 {
		t.Fatalf("期望 4 个版本，实际 %d: %+v", len(list), list)
	}
	byVer := map[string]DBXVersionInfo{}
	for _, v := range list {
		byVer[v.Version] = v
	}
	if v := byVer["v1.5.3"]; v.HashDrifted || !strings.Contains(v.DriftNote, "复算一致") {
		t.Errorf("1.5.3 应报复算一致: %+v", v)
	}
	if v := byVer["v1.5.2"]; !v.HashDrifted || !strings.Contains(v.DriftNote, "账外漂移") {
		t.Errorf("1.5.2 应如实报账外漂移: %+v", v)
	}
	if v := byVer["v1.5.1"]; v.HashDrifted || !strings.Contains(v.DriftNote, "复算未触发") {
		t.Errorf("1.5.1 应走 mtime 成本闸不触发复算: %+v", v)
	}
	if v := byVer["vimported-20260101-120000"]; !strings.Contains(v.DriftNote, "无法比对") {
		t.Errorf("无账本摘要应报无法比对: %+v", v)
	}

	// ResolveExe / 非法版本 / 未安装
	if exe, err := m.ResolveExe("v1.5.3"); err != nil || filepath.Base(exe) != exeName {
		t.Errorf("ResolveExe(v1.5.3): %v %v", exe, err)
	}
	if _, err := m.ResolveExe("1.5.3"); err != nil {
		t.Errorf("无 v 前缀应可解析: %v", err)
	}
	if _, err := m.ResolveExe("v9.9.9"); err == nil {
		t.Error("未安装版本应报错")
	}
	if _, err := m.ResolveExe("../../windows"); err == nil {
		t.Error("路径穿越式版本号必须报错")
	}

	if err := m.Remove("v1.5.3"); err != nil {
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
	writeLedgerVersion(t, versionsDir, "1.5.2", string(content), strings.Repeat("b", 64), "2020-01-01T00:00:00Z")
	writeLedgerVersion(t, versionsDir, "1.5.1", string(content), shaHex(content), "2099-01-01T00:00:00Z")
	m := NewManager(versionsDir)

	if drifted, note := m.VerifyLedger("v1.5.2"); !drifted || !strings.Contains(note, "账外漂移") {
		t.Errorf("1.5.2 应报漂移: (%v,%q)", drifted, note)
	}
	if drifted, note := m.VerifyLedger("v1.5.1"); drifted || !strings.Contains(note, "复算未触发") {
		t.Errorf("1.5.1 应走成本闸: (%v,%q)", drifted, note)
	}
	if drifted, note := m.VerifyLedger("v9.9.9"); drifted || !strings.Contains(note, "无法比对") {
		t.Errorf("未安装应报无法比对: (%v,%q)", drifted, note)
	}
}

// TestImportLocal 导入链：假 exe 无 PE 版本资源 → imported-时间戳兜底；
// 白名单五条目全搬（portable.dbx 副本必须在场，否则落位即损坏），噪声文件
// （数据文件）不搬；落位账本记实测摘要（漂移锚点）、verifiedHash 如实 false；
// 可解析、可卸载、重复导入、无 exe 目录与非便携形态（缺 portable.dbx）报错。
func TestImportLocal(t *testing.T) {
	m := NewManager(t.TempDir())

	src := t.TempDir()
	content := []byte("fake-exe-bytes")
	os.WriteFile(filepath.Join(src, exeName), content, 0644)
	os.WriteFile(filepath.Join(src, "LICENSE"), []byte("Apache License 2.0"), 0644)
	os.WriteFile(filepath.Join(src, "README.md"), []byte("DBX readme"), 0644)
	os.WriteFile(filepath.Join(src, portableMarkName), nil, 0644)
	os.WriteFile(filepath.Join(src, portableUpdateFileName), []byte(`{"version":"1.5.3"}`), 0644)
	os.WriteFile(filepath.Join(src, "dbx-data.db"), []byte("noise"), 0644)

	// 非便携形态（exe 在而 portable.dbx 缺）先拒——导入必须落在 ListInstalled
	// 认可的形状里，不留隐形安装
	noMark := t.TempDir()
	os.WriteFile(filepath.Join(noMark, exeName), content, 0644)
	if _, err := m.ImportLocal(noMark); err == nil || !strings.Contains(err.Error(), portableMarkName) {
		t.Fatalf("缺便携标记的源应拒导入并点名标记, got %v", err)
	}

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
	for _, name := range []string{exeName, "LICENSE", "README.md", portableMarkName, portableUpdateFileName} {
		if _, err := os.Stat(filepath.Join(info.Dir, name)); err != nil {
			t.Errorf("导入后缺少 %s: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(info.Dir, "dbx-data.db")); !os.IsNotExist(err) {
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
	seedRemote(t, DBXRelease{Version: "v1.5.3", AssetName: "DBX_1.5.3_x64-portable.zip", Size: 10})

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
