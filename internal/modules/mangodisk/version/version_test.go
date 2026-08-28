package version

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseReleasesBody(t *testing.T) {
	body, _ := json.Marshal([]map[string]any{
		{
			"tag_name": "v1.0.7", "published_at": "2026-08-26T00:00:00Z", "draft": false, "prerelease": false,
			"assets": []map[string]any{
				{"name": "MangoDisk-1.0.7-windows.exe", "size": 1, "digest": "sha256:" + strings.Repeat("1", 64)},
				{"name": "MangoDisk-1.0.7-windows-cli.exe", "size": 2, "digest": "sha256:" + strings.Repeat("2", 64)},
				{"name": "MangoDisk-1.0.7-windows-portable.exe", "size": 23, "digest": "sha256:" + strings.Repeat("a", 64), "url": "asset"},
			},
		},
		{"tag_name": "nightly", "draft": false, "assets": []map[string]any{}},
	})
	list, err := parseReleasesBody(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("len = %d, want 1", len(list))
	}
	if list[0].AssetName != "MangoDisk-1.0.7-windows-portable.exe" || list[0].Size != 23 {
		t.Fatalf("asset = %+v", list[0])
	}
}

func TestParseReleasesRejectsMissingDigestAndSize(t *testing.T) {
	body := []byte(`[
		{"tag_name":"v1.0.6","draft":false,"assets":[{"name":"MangoDisk-1.0.6-windows-portable.exe","size":0,"digest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}]},
		{"tag_name":"v1.0.5","draft":false,"assets":[{"name":"MangoDisk-1.0.5-windows-portable.exe","size":1,"digest":""}]}
	]`)
	list, err := parseReleasesBody(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("应全部过滤: %+v", list)
	}
}

func TestNormalizeFileVersion(t *testing.T) {
	cases := map[string]string{
		"1.0.7":   "1.0.7",
		"1.0.7.0": "1.0.7",
		"v1.0.7":  "1.0.7",
		"1.0.7,0": "1.0.7",
	}
	for input, want := range cases {
		if got := normalizeFileVersion(input); got != want {
			t.Errorf("normalizeFileVersion(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestExpectedAssetName(t *testing.T) {
	if got := expectedAssetName("v1.0.7"); got != "MangoDisk-1.0.7-windows-portable.exe" {
		t.Fatal(got)
	}
}
