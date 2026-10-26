package bili23

import (
	"os"
	"path/filepath"
	"testing"

	"hanxi/internal/extapi"
	"hanxi/internal/modules/bili23/version"
)

// TestNormalizeVersion 版本入参归一词表：裸号/v号/V号/带空白同落一个 v 规范形；
// imported-/vimported- 兜底令牌恒透传为 vimported-（绝不产出 vvimported-）。
func TestNormalizeVersion(t *testing.T) {
	cases := []struct{ in, want string }{
		{"2.15.0", "v2.15.0"},
		{"v2.15.0", "v2.15.0"},
		{"V2.15.0", "v2.15.0"},
		{"  v2.15.0 ", "v2.15.0"},
		{" V2.15.0\t", "v2.15.0"},
		{"imported-0123456789ab", "vimported-0123456789ab"},
		{"vimported-0123456789ab", "vimported-0123456789ab"},
		{"Vimported-0123456789ab", "vimported-0123456789ab"},
		{"", "v"}, // 空串旧口径即补成 "v"（下游按非法形状拒），本修复不改此行为
	}
	for _, c := range cases {
		if got := normalizeVersion(c.in); got != c.want {
			t.Errorf("normalizeVersion(%q) = %q, 期望 %q", c.in, got, c.want)
		}
	}
}

// newNormalizeTestService 装配归一用例的最小 service（无引擎：只走下载去重与
// 设定版本路径，两者均不触碰 engine）。
func newNormalizeTestService(t *testing.T) (*Service, string) {
	t.Helper()
	versionsDir := t.TempDir()
	return &Service{
		manager: version.NewManager(versionsDir),
		store:   newBili23Store(t.TempDir()),
		holder:  extapi.NewLeaseHolder(ID),
	}, versionsDir
}

// seedBili23Installed 伪造三锚点齐备（exe / _pystand_static.int / script/main.py）
// 的已落位版本目录，ListInstalled 收纳口径。
func seedBili23Installed(t *testing.T, versionsDir, ver string) {
	t.Helper()
	dir := filepath.Join(versionsDir, "bili23_"+ver)
	if err := os.MkdirAll(filepath.Join(dir, "script"), 0o755); err != nil {
		t.Fatal(err)
	}
	for p, body := range map[string]string{
		"Bili23.exe":          "MZ伪造载荷",
		"_pystand_static.int": "int",
		"script/main.py":      "main",
	} {
		if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(p)), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// TestDownloadVersionInstalledDedupCaseInsensitive 已装前提下同一版本的
// 裸号/v号/V号/带空白四种入参都回 already-installed。后台下载不启动的判据：
// 修复路径在 go 语句之前同步返回，versions 树零新增且 active 未漂移
// （若去重再次落空放行下载，返回值先红——变异行序保证无赛跑假绿）。
func TestDownloadVersionInstalledDedupCaseInsensitive(t *testing.T) {
	svc, versionsDir := newNormalizeTestService(t)
	seedBili23Installed(t, versionsDir, "2.15.0")

	for _, in := range []string{"2.15.0", "v2.15.0", "V2.15.0", " 2.15.0 "} {
		got, err := svc.DownloadVersion(in)
		if err != nil {
			t.Fatalf("DownloadVersion(%q): %v", in, err)
		}
		if got != "already-installed" {
			t.Fatalf("DownloadVersion(%q) = %q，期望 already-installed（已装版本不得起后台下载）", in, got)
		}
	}
	ents, err := os.ReadDir(versionsDir)
	if err != nil || len(ents) != 1 {
		t.Fatalf("去重路径不得新增版本目录，实得 %+v err=%v", ents, err)
	}
	if got := svc.store.GetActive(); got != "" {
		t.Fatalf("去重路径不得改 active，实得 %q", got)
	}
}

// TestSetActiveVersionCaseInsensitiveSameToken 三入口同输入一致：V 形态设定
// 版本不再报"非法版本号"，返回值与落账同为一个 v 规范形；未装版本三种形态
// 同样都拒（口径一致，不再大小写互异）。
func TestSetActiveVersionCaseInsensitiveSameToken(t *testing.T) {
	svc, versionsDir := newNormalizeTestService(t)
	seedBili23Installed(t, versionsDir, "2.15.0")

	for _, in := range []string{"2.15.0", "v2.15.0", "V2.15.0", " 2.15.0 "} {
		got, err := svc.SetActiveVersion(in)
		if err != nil {
			t.Fatalf("SetActiveVersion(%q): %v", in, err)
		}
		if got != "v2.15.0" || svc.store.GetActive() != "v2.15.0" {
			t.Fatalf("SetActiveVersion(%q) = %q active=%q，应同落 v2.15.0", in, got, svc.store.GetActive())
		}
	}
	for _, in := range []string{"9.9.9", "v9.9.9", "V9.9.9"} {
		if _, err := svc.SetActiveVersion(in); err == nil {
			t.Fatalf("未装版本 %q 应被拒（三种形态口径一致）", in)
		}
	}
}
