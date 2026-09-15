package version

import (
	"encoding/json"
	"strings"
	"testing"
)

// validDigest 长度合法的 sha256 digest（64 hex，仅测解析不测真伪）。
func validDigest() string { return "sha256:" + strings.Repeat("ab", 32) }

func a(name, digest string, size int) map[string]any {
	return map[string]any{"name": name, "url": "https://x/" + name, "size": size, "digest": digest}
}

func rel(tag string, draft bool, assets ...map[string]any) map[string]any {
	return map[string]any{"tag_name": tag, "published_at": "2026-09-10T07:35:13Z", "prerelease": false, "draft": draft, "assets": assets}
}

func TestParseReleasesBody(t *testing.T) {
	// 五类样本：正常入选 / 缺 digest 丢弃 / 非 desktop tag 丢弃 / draft 丢弃 / 无安装包丢弃
	releases := []any{
		rel("desktop-v0.11.5", false,
			a("Douzy-Setup-0.11.5.exe", validDigest(), 147000000),
			a("Douzy-Setup-0.11.5.exe.blockmap", validDigest(), 200000),
			a("Douzy-0.11.5.dmg", validDigest(), 180000000),
			a("latest.yml", validDigest(), 400),
		),
		rel("desktop-v0.11.4", false, a("Douzy-Setup-0.11.4.exe", "", 147000000)),
		rel("v1.2.3", false, a("Douzy-Setup-1.2.3.exe", validDigest(), 1)),
		rel("desktop-v9.9.9", true, a("Douzy-Setup-9.9.9.exe", validDigest(), 1)),
		rel("desktop-v0.11.3", false, a("Douzy-0.11.3-mac.zip", validDigest(), 1)),
	}
	body, err := json.Marshal(releases)
	if err != nil {
		t.Fatalf("marshal sample: %v", err)
	}

	list, err := parseReleasesBody(body)
	if err != nil {
		t.Fatalf("parseReleasesBody error: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("期望仅 1 条可用版本，实际 %d 条：%+v", len(list), list)
	}
	got := list[0]
	if got.Version != "v0.11.5" {
		t.Errorf("Version 期望 v0.11.5，实际 %q", got.Version)
	}
	if got.Tag != "desktop-v0.11.5" {
		t.Errorf("Tag 期望保留原始 desktop-v0.11.5，实际 %q", got.Tag)
	}
	if got.AssetName != "Douzy-Setup-0.11.5.exe" {
		t.Errorf("AssetName 期望选中 Setup.exe，实际 %q", got.AssetName)
	}
	if got.SHA256 == "" {
		t.Errorf("SHA256 不应为空")
	}
}

func TestFindInstallerAssetExcludesBlockmap(t *testing.T) {
	assets := []asset{
		{Name: "Douzy-0.11.5-arm64-mac.zip"},
		{Name: "Douzy-Setup-0.11.5.exe.blockmap"},
		{Name: "Douzy-Setup-0.11.5.exe"},
		{Name: "SHA256SUMS.txt"},
	}
	got, ok := findInstallerAsset(assets, "0.11.5")
	if !ok {
		t.Fatal("应能挑中 Douzy-Setup-0.11.5.exe")
	}
	if got.Name != "Douzy-Setup-0.11.5.exe" {
		t.Fatalf("错误命中 %q（应排除 .blockmap）", got.Name)
	}
	if _, ok := findInstallerAsset(assets, "0.9.9"); ok {
		t.Fatal("不匹配版本号的资产不应入选")
	}
}

func TestDigestHex(t *testing.T) {
	if got := digestHex(validDigest()); len(got) != 64 {
		t.Errorf("合法 digest 应剥离前缀得 64 hex，实际长度 %d", len(got))
	}
	for _, bad := range []string{"", "sha256:", "sha256:tooshort", "md5:" + strings.Repeat("ab", 32)} {
		if got := digestHex(bad); got != "" {
			t.Errorf("digestHex(%q) 期望空串，实际 %q", bad, got)
		}
	}
}
