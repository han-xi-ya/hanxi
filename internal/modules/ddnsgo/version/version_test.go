package version

import (
	"strings"
	"testing"
)

// fakeReleasesJSON 构造与真实 GitHub API 同构的样例响应：
// 覆盖 windows x64/arm64/i386 zip、跨平台 tar.gz、预发布、非规范 tag、缺失 digest 的 release。
func fakeReleasesJSON(t *testing.T) []byte {
	t.Helper()
	h := func(ch byte) string { return "sha256:" + strings.Repeat(string(ch), 64) }
	body := `[
  {
    "tag_name": "v6.17.6",
    "published_at": "2026-08-19T12:36:25Z",
    "prerelease": false,
    "draft": false,
    "assets": [
      {"name": "checksums.txt", "url": "https://api.github.com/x/0", "size": 2804, "digest": "` + h('0') + `"},
      {"name": "ddns-go_6.17.6_windows_arm64.zip", "url": "https://api.github.com/x/1", "size": 4317887, "digest": "` + h('a') + `"},
      {"name": "ddns-go_6.17.6_windows_i386.zip", "url": "https://api.github.com/x/2", "size": 4659482, "digest": "` + h('b') + `"},
      {"name": "ddns-go_6.17.6_linux_x86_64.tar.gz", "url": "https://api.github.com/x/3", "size": 4700000, "digest": "` + h('c') + `"},
      {"name": "ddns-go_6.17.6_windows_x86_64.zip", "url": "https://api.github.com/x/4", "size": 4799520, "digest": "sha256:9d33056f2efff0bbe51987a40c4aa67e1ed2186f8da8e9f7a8236817603f92ff"}
    ]
  },
  {
    "tag_name": "v6.17.5",
    "published_at": "2026-08-10T08:00:00Z",
    "prerelease": false,
    "draft": false,
    "assets": [
      {"name": "ddns-go_6.17.5_windows_x86_64.zip", "url": "https://api.github.com/x/5", "size": 4794312, "digest": "` + h('d') + `"}
    ]
  },
  {
    "tag_name": "v6.18.0-beta1",
    "published_at": "2026-08-15T08:00:00Z",
    "prerelease": true,
    "draft": false,
    "assets": [
      {"name": "ddns-go_6.18.0-beta1_windows_x86_64.zip", "url": "https://api.github.com/x/6", "size": 4800000, "digest": "` + h('e') + `"}
    ]
  },
  {
    "tag_name": "v6.17.4",
    "published_at": "2026-08-01T08:00:00Z",
    "prerelease": false,
    "draft": false,
    "assets": [
      {"name": "ddns-go_6.17.4_windows_x86_64.zip", "url": "https://api.github.com/x/7", "size": 4794324}
    ]
  }
]`
	return []byte(body)
}

// TestParseReleasesBody 解析过滤：6.17.6/6.17.5 入列表，
// beta tag 丢弃（非纯语义版本），6.17.4 丢弃（缺 digest）。
func TestParseReleasesBody(t *testing.T) {
	list, err := parseReleasesBody(fakeReleasesJSON(t))
	if err != nil {
		t.Fatalf("parseReleasesBody: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("期望 2 个版本，实际 %d: %+v", len(list), list)
	}
	byVer := map[string]DdnsRelease{}
	for _, r := range list {
		byVer[r.Version] = r
	}
	if v := byVer["v6.17.6"]; v.SHA256 != "9d33056f2efff0bbe51987a40c4aa67e1ed2186f8da8e9f7a8236817603f92ff" ||
		v.Size != 4799520 || v.AssetName != "ddns-go_6.17.6_windows_x86_64.zip" || v.IsPre {
		t.Errorf("v6.17.6 解析错误: %+v", v)
	}
	for _, gone := range []string{"v6.18.0-beta1", "v6.17.4"} {
		if _, ok := byVer[gone]; ok {
			t.Errorf("%s 不应入列表", gone)
		}
	}
	for _, r := range list {
		if strings.Contains(r.AssetName, "arm64") || strings.Contains(r.AssetName, "i386") ||
			strings.Contains(r.AssetName, "tar.gz") {
			t.Errorf("混入非 windows-x64 zip 资产: %s", r.AssetName)
		}
		if len(r.SHA256) != 64 {
			t.Errorf("sha256 格式异常: %q", r.SHA256)
		}
	}
}

// TestFindPortableAsset 资产筛选：x64 zip 命中、arm64/i386/tar.gz/checksums 绝不混入。
func TestFindPortableAsset(t *testing.T) {
	assets := []asset{
		{Name: "checksums.txt", Size: 1},
		{Name: "ddns-go_6.17.6_windows_arm64.zip", Size: 1},
		{Name: "ddns-go_6.17.6_windows_i386.zip", Size: 1},
		{Name: "ddns-go_6.17.6_linux_x86_64.tar.gz", Size: 1},
		{Name: "ddns-go_6.17.6_windows_x86_64.zip", Size: 4799520},
	}
	got, ok := findPortableAsset(assets, "v6.17.6")
	if !ok {
		t.Fatal("应命中 windows x64 zip 资产")
	}
	if got.Name != "ddns-go_6.17.6_windows_x86_64.zip" || got.Size != 4799520 {
		t.Errorf("命中错误资产: %+v", got)
	}

	// 无 windows zip 的 release 不命中
	if _, ok := findPortableAsset([]asset{{Name: "ddns-go_6.17.5_linux_x86_64.tar.gz"}}, "v6.17.5"); ok {
		t.Error("仅有 linux tar.gz 的 release 不应命中")
	}
}

// TestVersionFromToken 版本令牌形状（承迁移前 dirNameRe 口径）：语义版本与
// imported- 时间戳收纳并规范化为 v 前缀；外来/畸形令牌拒绝。
// （注：宽数字起头分支与 ccswitch 模板同构，"6.17.6.bak" 这类手工杂物目录会被
// 收纳扫描但 exe 缺失即跳过，不构成损坏误判。）
func TestVersionFromToken(t *testing.T) {
	ok := map[string]string{"6.17.6": "v6.17.6", "10.0.1": "v10.0.1", "6.17.6.bak": "v6.17.6.bak",
		"imported-20260819-121314": "vimported-20260819-121314"}
	for token, want := range ok {
		got, accepted := versionFromToken(token)
		if !accepted || got != want {
			t.Errorf("versionFromToken(%q) = (%q, %v), want (%q, true)", token, got, accepted, want)
		}
	}
	for _, token := range []string{"v6.17.6", "x", "", "6", "imported-1"} {
		if _, accepted := versionFromToken(token); accepted {
			t.Errorf("应拒绝版本令牌 %q", token)
		}
	}
}

// TestResolveVersionDirRejection 非法版本号（路径穿越尝试）必须被拒绝。
func TestResolveVersionDirRejection(t *testing.T) {
	m := NewManager(t.TempDir())
	for _, v := range []string{"../../etc", "v6.17", "6.17.6-extra", ""} {
		if _, _, err := m.resolveVersionDir(v); err == nil {
			t.Errorf("非法版本号 %q 应被拒绝", v)
		}
	}
}

// TestResolveVersionDirNotInstalledGuidance 合法但未安装的版本号给出引导文案
// （与迁移前逐字一致，RPC 错误面零漂移）。
func TestResolveVersionDirNotInstalledGuidance(t *testing.T) {
	m := NewManager(t.TempDir())
	_, _, err := m.resolveVersionDir("v6.17.6")
	if err == nil || err.Error() != "版本 v6.17.6 未安装，请先在下方版本管理下载或导入" {
		t.Fatalf("未安装引导文案漂移: %v", err)
	}
}
