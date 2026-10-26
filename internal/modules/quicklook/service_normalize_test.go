package quicklook

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"hanxi/internal/extapi"
	"hanxi/internal/modules/quicklook/instance"
	"hanxi/internal/modules/quicklook/version"
)

// TestNormalizeVersion 版本入参归一词表：本模块名册与目录令牌口径是裸号
// （上游 tag 无 v 前缀），故四种写法同落同一个裸号；imported- 兜底令牌同落裸
// imported-——与 ListInstalled 展示逐字节一致，绝不产出带 v 前缀的名册外形状
// （本家族的"绝不产出 vvimported-"形态）。
func TestNormalizeVersion(t *testing.T) {
	cases := []struct{ in, want string }{
		{"4.5.0", "4.5.0"},
		{"v4.5.0", "4.5.0"},
		{"V4.5.0", "4.5.0"},
		{"  v4.5.0 ", "4.5.0"},
		{" V4.5.0\t", "4.5.0"},
		{"imported-0123456789ab", "imported-0123456789ab"},
		{"vimported-0123456789ab", "imported-0123456789ab"},
		{"Vimported-0123456789ab", "imported-0123456789ab"},
		{"", ""}, // 空串旧口径即回空（下游形状闸拒），本修复不改此行为
	}
	for _, c := range cases {
		if got := normalizeVersion(c.in); got != c.want {
			t.Errorf("normalizeVersion(%q) = %q, 期望 %q", c.in, got, c.want)
		}
	}
}

// newNormalizeFixture 装配归一用例的 service：versions 根目录可控（手工植数字
// 版本目录）、engine 非 nil 静止态（RemoveVersion 要取快照）、下载缝计数——
// 去重落空放行后台下载时，计数就是"白起一路注定失败下载"的直接证据。
func newNormalizeFixture(t *testing.T) (*QuickLookService, string, *atomic.Int32) {
	t.Helper()
	dir := t.TempDir()
	versionsDir := filepath.Join(dir, "versions")
	if err := os.MkdirAll(versionsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	calls := &atomic.Int32{}
	svc := &QuickLookService{
		manager:   version.NewManager(versionsDir),
		store:     newQuicklookStore(dir),
		engine:    instance.NewEngine(nil, nil, instance.Callbacks{}),
		downloads: map[string]struct{}{},
		holder:    extapi.NewLeaseHolder(ID),
	}
	svc.download = func(_ context.Context, _, targetVersion string, _ func(version.DownloadProgress)) error {
		calls.Add(1)
		return nil
	}
	return svc, versionsDir, calls
}

// plantQuickLookVersion 按 ListInstalled 收纳口径手工植一个数字版本目录
// （quicklook_<x.y.z>：主程序非空 + 便携标记 portable.lock 双锚点），返回目录路径。
func plantQuickLookVersion(t *testing.T, versionsDir, ver string) string {
	t.Helper()
	dir := filepath.Join(versionsDir, "quicklook_"+ver)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "QuickLook.exe"), []byte("MZ fake payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "portable.lock"), []byte("portable"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// TestDownloadVersionInstalledDedupCaseInsensitive 已装 4.5.0 时含大写 V 的四种
// 入参都回 already-installed。判据是外部事实：在飞名册零登记 + versions 根目录
// 零新增 + 下载缝调用 0 次（名册登记发生在同步段，判据无赛跑）。归一退回只剥小写
// v 时 V 形态去重落空、登记名册并起后台下载，断言链当场变红。
func TestDownloadVersionInstalledDedupCaseInsensitive(t *testing.T) {
	svc, versionsDir, calls := newNormalizeFixture(t)
	plantQuickLookVersion(t, versionsDir, "4.5.0")

	for _, in := range []string{"4.5.0", "v4.5.0", "V4.5.0", " V4.5.0\t"} {
		state, err := svc.DownloadVersion(in)
		if err != nil {
			t.Fatalf("DownloadVersion(%q): %v", in, err)
		}
		if state != "already-installed" {
			t.Fatalf("DownloadVersion(%q) = %q，期望 already-installed（已装版本不得起后台下载）", in, state)
		}
		if len(svc.downloads) != 0 {
			t.Fatalf("DownloadVersion(%q) 已装早退不得登记在飞名册，实得 %v", in, svc.downloads)
		}
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("已装版本不得调用下载缝，实得 %d 次", got)
	}
	ents, err := os.ReadDir(versionsDir)
	if err != nil || len(ents) != 1 {
		t.Fatalf("去重路径不得新增版本目录，实得 %+v err=%v", ents, err)
	}
	if got := svc.store.GetActive(); got != "" {
		t.Fatalf("去重路径不得改 active，实得 %q", got)
	}
}

// TestThreeEntriesSameValueConsistent 三入口同输入一致：V 形态设定使用版本不再
// "非法版本号"，返回值与落账同为裸号规范形；随后换另一种形态卸载仍认账（目录从
// 磁盘消失）并清 active——证明归一在 Download/SetActive/Remove 三点贯通同一口径。
func TestThreeEntriesSameValueConsistent(t *testing.T) {
	svc, versionsDir, calls := newNormalizeFixture(t)
	dir := plantQuickLookVersion(t, versionsDir, "4.5.0")

	got, err := svc.SetActiveVersion("V4.5.0")
	if err != nil {
		t.Fatalf("SetActiveVersion(V 形态): %v", err)
	}
	if got != "4.5.0" || svc.store.GetActive() != "4.5.0" {
		t.Fatalf("SetActiveVersion = %q active=%q，应同落裸号 4.5.0", got, svc.store.GetActive())
	}
	if err := svc.RemoveVersion(" 4.5.0 "); err != nil {
		t.Fatalf("RemoveVersion(裸号形态): %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("卸载应移除版本目录，实得 err=%v", err)
	}
	if got := svc.store.GetActive(); got != "" {
		t.Fatalf("卸掉在用版本应清 active，实得 %q", got)
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("激活/卸载路径均不得调用下载缝，实得 %d 次", got)
	}
}
