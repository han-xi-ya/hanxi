package rustdesk

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"hanxi/internal/modules/rustdesk/version"
)

// TestNormalizeVersion 版本入参归一词表：裸号/v号/V号/带空白同落一个 v 规范形；
// imported-/vimported- 兜底令牌恒透传为 vimported-（绝不产出 vvimported-），与
// ListInstalled 展示口径（"v" + 目录后缀，导入兜底目录即 vimported-<哈希>）逐字节一致。
func TestNormalizeVersion(t *testing.T) {
	cases := []struct{ in, want string }{
		{"1.10.0", "v1.10.0"},
		{"v1.10.0", "v1.10.0"},
		{"V1.10.0", "v1.10.0"},
		{"  v1.10.0 ", "v1.10.0"},
		{" V1.10.0\t", "v1.10.0"},
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

// TestDownloadVersionInstalledDedupCaseInsensitive 已装 1.10.0 时含大写 V 的四种
// 入参都回 already-installed，且下载驱动被调 0 次、在飞名册零登记、versions 根目录
// 零新增。判据取"驱动调用次数"这一外部事实，不只看返回值字符串——归一退回只剥小写
// v 时 V 形态先去重落空、再登记名册起后台下载，四形态断言链当场变红。
func TestDownloadVersionInstalledDedupCaseInsensitive(t *testing.T) {
	svc, versionsDir, _ := newTestRustDeskService(t)
	seedRDInstalled(t, versionsDir, "1.10.0")

	var driverCalls int
	svc.downloadDriver = func(_ context.Context, targetVersion string, _ func(version.DownloadProgress)) error {
		driverCalls++
		t.Errorf("已装版本不得放行下载驱动，实得入参 %q", targetVersion)
		return nil
	}

	for _, in := range []string{"1.10.0", "v1.10.0", "V1.10.0", " V1.10.0\t"} {
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
		if driverCalls != 0 {
			t.Fatalf("DownloadVersion(%q) 不得调用下载驱动，实得 %d 次", in, driverCalls)
		}
	}

	ents, err := os.ReadDir(versionsDir)
	if err != nil || len(ents) != 1 {
		t.Fatalf("去重路径不得新增版本目录，实得 %+v err=%v", ents, err)
	}
	if got, _ := svc.store.GetActive(); got != "" {
		t.Fatalf("去重路径不得改 active，实得 %q", got)
	}
}

// TestThreeEntriesSameValueConsistent 三入口同输入一致（审查批次在 papertodo/
// rufus/recordly 复现面的正解）：V 形态设定便携版本不再"非法版本号"，返回值与
// 落账同为一个 v 规范形；随后换裸号形态卸载，仍认账并清 active——证明归一在
// Download/SetActive/Remove 三点贯通同一口径。
func TestThreeEntriesSameValueConsistent(t *testing.T) {
	svc, versionsDir, _ := newTestRustDeskService(t)
	dir := seedRDInstalled(t, versionsDir, "1.10.0")

	got, err := svc.SetActiveVersion("V1.10.0", version.FormPortable)
	if err != nil {
		t.Fatalf("SetActiveVersion(V 形态): %v", err)
	}
	if got != "v1.10.0" {
		t.Fatalf("SetActiveVersion(V 形态) = %q，期望 v1.10.0", got)
	}
	if v, f := svc.store.GetActive(); v != "v1.10.0" || f != version.FormPortable {
		t.Fatalf("落账须同为 v1.10.0/portable，实得 %q/%q", v, f)
	}

	if err := svc.RemoveVersion(" 1.10.0 "); err != nil {
		t.Fatalf("RemoveVersion(裸号形态): %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("卸载应移除版本目录，实得 err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(versionsDir)); err != nil {
		t.Fatalf("versions 根目录应可复读: %v", err)
	}
	if v, f := svc.store.GetActive(); v != "" || f != "" {
		t.Fatalf("卸掉在用版本应成对清空 active/form，实得 %q/%q", v, f)
	}
}
