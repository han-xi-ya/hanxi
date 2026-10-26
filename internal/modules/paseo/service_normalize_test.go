package paseo

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"hanxi/internal/extapi"
	"hanxi/internal/modules/paseo/instance"
	"hanxi/internal/modules/paseo/version"
)

// TestNormalizeVersion 版本入参归一词表：本模块名册与目录令牌口径是裸号
// （paseo_0.8.0 / paseo_0.8.0-beta.1 / paseo_imported-兜底），故四种写法同落同一个
// 裸号；imported-/vimported-/Vimported- 同落裸 imported-——与 ListInstalled 展示逐
// 字节一致，绝不产出带 v 前缀的名册外形状（本家族的"绝不产出 vvimported-"形态）。
func TestNormalizeVersion(t *testing.T) {
	cases := []struct{ in, want string }{
		{"0.8.0", "0.8.0"},
		{"v0.8.0", "0.8.0"},
		{"V0.8.0", "0.8.0"},
		{"  v0.8.0 ", "0.8.0"},
		{" V0.8.0\t", "0.8.0"},
		{"V0.8.0-beta.1", "0.8.0-beta.1"},
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

// newNormalizeFixture 装配归一用例的 service：versions 根目录可控、engine 非 nil
// 静止态（RemoveVersion 取快照）、下载缝计数。
func newNormalizeFixture(t *testing.T) (*PaseoService, string, *atomic.Int32) {
	t.Helper()
	versionsDir := t.TempDir()
	calls := &atomic.Int32{}
	svc := &PaseoService{
		manager:   version.NewManager(versionsDir),
		store:     newPaseoStore(t.TempDir()),
		holder:    extapi.NewLeaseHolder(ID),
		downloads: map[string]struct{}{},
	}
	svc.engine = instance.NewEngine(nil, nil, instance.Callbacks{})
	svc.download = func(_ context.Context, _, targetVersion string, _ func(version.DownloadProgress)) error {
		calls.Add(1)
		return nil
	}
	return svc, versionsDir, calls
}

// plantPaseoVersion 按 ListInstalled 收纳口径手工植一个数字版本目录
// （paseo_<x.y.z>：Electron 双锚点 Paseo.exe 非空 + resources/app.asar），返回目录路径。
func plantPaseoVersion(t *testing.T, versionsDir, ver string) string {
	t.Helper()
	dir := filepath.Join(versionsDir, "paseo_"+ver)
	if err := os.MkdirAll(filepath.Join(dir, "resources"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Paseo.exe"), []byte("not-a-real-exe"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "resources", "app.asar"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// TestDownloadVersionInstalledDedupCaseInsensitive 已装 0.8.0 时含大写 V 的四种
// 入参都回 already-installed。判据是外部事实：在飞名册零登记 + versions 根目录
// 零新增 + 下载缝调用 0 次（名册登记在同步段，判据无赛跑）。归一退回只剥小写 v 时
// V 形态去重落空、登记名册并起后台下载，断言链当场变红。
func TestDownloadVersionInstalledDedupCaseInsensitive(t *testing.T) {
	svc, versionsDir, calls := newNormalizeFixture(t)
	plantPaseoVersion(t, versionsDir, "0.8.0")

	for _, in := range []string{"0.8.0", "v0.8.0", "V0.8.0", " V0.8.0\t"} {
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
// 磁盘消失）并清 active。未装版本另断两种形态话术逐字相同（旧形制下 V 形态过不了
// tokenVersionRe 报"非法版本号"、裸号形态报"未安装"，即三入口互异）。
func TestThreeEntriesSameValueConsistent(t *testing.T) {
	svc, versionsDir, calls := newNormalizeFixture(t)
	dir := plantPaseoVersion(t, versionsDir, "0.8.0")

	got, err := svc.SetActiveVersion("V0.8.0")
	if err != nil {
		t.Fatalf("SetActiveVersion(V 形态): %v", err)
	}
	if got != "0.8.0" || svc.store.GetActive() != "0.8.0" {
		t.Fatalf("SetActiveVersion = %q active=%q，应同落裸号 0.8.0", got, svc.store.GetActive())
	}

	errV := setErr(svc, "V9.9.9")
	errPlain := setErr(svc, "9.9.9")
	if errV == nil || errPlain == nil {
		t.Fatalf("前置：未装版本不得可激活，V=%v 裸=%v", errV, errPlain)
	}
	if errV.Error() != errPlain.Error() {
		t.Fatalf("未装版本两种形态话术须一致，实得 V=%q 裸=%q", errV, errPlain)
	}

	if err := svc.RemoveVersion(" 0.8.0 "); err != nil {
		t.Fatalf("RemoveVersion(裸号形态): %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("卸载应移除版本目录，实得 err=%v", err)
	}
	if got := svc.store.GetActive(); got != "" {
		t.Fatalf("卸掉在用版本应清 active，实得 %q", got)
	}
	ents, rerr := os.ReadDir(versionsDir)
	if rerr != nil || len(ents) != 0 {
		t.Fatalf("versions 根目录应零残骸，实得 %+v err=%v", ents, rerr)
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("激活/卸载路径均不得调用下载缝，实得 %d 次", got)
	}
}

func setErr(svc *PaseoService, v string) error {
	_, err := svc.SetActiveVersion(v)
	return err
}
