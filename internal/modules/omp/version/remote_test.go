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

func rel(tag string, pre, draft bool, assets ...map[string]any) map[string]any {
	return map[string]any{"tag_name": tag, "published_at": "2026-11-02T19:49:20Z", "prerelease": pre, "draft": draft, "assets": assets}
}

func TestParseReleasesBody(t *testing.T) {
	// 样本族：正常入选 / 缺 digest 丢弃 / 非纯 semver tag 丢弃 / draft 丢弃 /
	// 仅 arm64 资产丢弃 / prerelease 入列带标记 / 兜底名匹配（omp-win-x64 漂移形态）。
	releases := []any{
		rel("v18.16.0", false, false,
			a("omp-darwin-arm64", validDigest(), 215000000),
			a("omp-linux-x64", validDigest(), 277000000),
			a("omp-windows-x64.exe", validDigest(), 243000000),
			a("omp-windows-arm64.exe", validDigest(), 226000000),
			a("SHA256SUMS.txt", validDigest(), 400),
		),
		rel("v18.15.0", false, false, a("omp-windows-x64.exe", "", 243000000)),
		rel("v18.15.1-canary", false, false, a("omp-windows-x64.exe", validDigest(), 1)),
		rel("v18.14.0", false, true, a("omp-windows-x64.exe", validDigest(), 1)),
		rel("v18.13.0", false, false, a("omp-windows-arm64.exe", validDigest(), 1)),
		rel("v18.12.0", true, false, a("omp-windows-x64.exe", validDigest(), 1)),
		rel("v18.11.0", false, false, a("oh-my-pi-windows-x64.exe", validDigest(), 1)),
	}
	body, err := json.Marshal(releases)
	if err != nil {
		t.Fatalf("marshal sample: %v", err)
	}

	list, err := parseReleasesBody(body)
	if err != nil {
		t.Fatalf("parseReleasesBody error: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("期望 3 条可用版本（正常/pre/兜底名），实际 %d 条：%+v", len(list), list)
	}
	got := list[0]
	if got.Version != "v18.16.0" || got.Tag != "v18.16.0" {
		t.Errorf("Version/Tag 期望 v18.16.0/v18.16.0，实际 %q/%q", got.Version, got.Tag)
	}
	if got.AssetName != "omp-windows-x64.exe" {
		t.Errorf("AssetName 期望精确名 omp-windows-x64.exe，实际 %q", got.AssetName)
	}
	if got.SHA256 == "" || len(got.SHA256) != 64 {
		t.Errorf("SHA256 期望 64 hex，实际 %q", got.SHA256)
	}
	if list[1].Version != "v18.12.0" || !list[1].IsPre {
		t.Errorf("prerelease 条目期望入列且 IsPre=true，实际 %+v", list[1])
	}
	if list[2].Version != "v18.11.0" || list[2].AssetName != "oh-my-pi-windows-x64.exe" {
		t.Errorf("windows-x64 兜底名匹配失败，实际 %+v", list[2])
	}
}

func TestFindWindowsAssetExcludesArm64(t *testing.T) {
	// 只有 arm64 与带 blockmap 噪声时不得误吞；windows-x64 存在时优先精确名。
	if _, ok := findWindowsAsset([]asset{{Name: "omp-windows-arm64.exe"}}); ok {
		t.Errorf("arm64 变体不应入选")
	}
	got, ok := findWindowsAsset([]asset{
		{Name: "omp-windows-x64.exe.sig"},
		{Name: "OMP-WINDOWS-X64.EXE"},
	})
	if !ok || got.Name != "OMP-WINDOWS-X64.EXE" {
		t.Errorf("大小写不敏感精确名匹配失败：%+v ok=%v", got, ok)
	}
}

func TestDigestHex(t *testing.T) {
	if digestHex("sha256:"+strings.Repeat("0f", 32)) == "" {
		t.Errorf("合法 digest 应剥前缀返回 hex")
	}
	if digestHex("md5:abc") != "" || digestHex("sha256:short") != "" || digestHex("") != "" {
		t.Errorf("畸形 digest 必须返回空串")
	}
}
