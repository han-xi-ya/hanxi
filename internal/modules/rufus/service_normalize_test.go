package rufus

import (
	"os"
	"testing"
)

func TestNormalizeVersion(t *testing.T) {
	cases := []struct{ in, want string }{
		{"4.15", "v4.15"},
		{"v4.15", "v4.15"},
		{"V4.15", "v4.15"},
		{"  v4.15 ", "v4.15"},
		{" V4.15\t", "v4.15"},
		{"imported-0123456789ab", "vimported-0123456789ab"},
		{"vimported-0123456789ab", "vimported-0123456789ab"},
		{"Vimported-0123456789ab", "vimported-0123456789ab"},
		{"", "v"},
	}
	for _, c := range cases {
		if got := normalizeVersion(c.in); got != c.want {
			t.Errorf("normalizeVersion(%q) = %q, 期望 %q", c.in, got, c.want)
		}
	}
}

// TestDownloadVersionInstalledDedupCaseInsensitive 已装 4.15 时含大写 V 的
// 四种入参都回 already-installed，且版本树零新增（修复路径在 go 语句之前
// 同步返回；若归一退回只剥小写 v，返回值先红——行序保证无赛跑假绿）。
func TestDownloadVersionInstalledDedupCaseInsensitive(t *testing.T) {
	f := newRufusFixture(t, false)
	mkInstalled(t, f.versionsDir, "rufus_4.15")

	for _, in := range []string{"4.15", "v4.15", "V4.15", " 4.15 "} {
		got, err := f.svc.DownloadVersion(in)
		if err != nil {
			t.Fatalf("DownloadVersion(%q): %v", in, err)
		}
		if got != "already-installed" {
			t.Fatalf("DownloadVersion(%q) = %q，期望 already-installed", in, got)
		}
	}
	ents, err := os.ReadDir(f.versionsDir)
	if err != nil || len(ents) != 1 {
		t.Fatalf("去重路径不得新增版本目录，实得 %+v err=%v", ents, err)
	}
}

// TestThreeEntriesSameValueConsistent 三入口同输入一致（T3 复现面的正解）：
// V 形态设版本不再"非法版本号"，与裸号同 token 落账；随后换一种形态卸载，
// 仍认账并清 active——证明归一在 Download/SetActive/Remove 三点贯通同一口径。
func TestThreeEntriesSameValueConsistent(t *testing.T) {
	f := newRufusFixture(t, false)
	dir := mkInstalled(t, f.versionsDir, "rufus_4.15")

	got, err := f.svc.SetActiveVersion("V4.15")
	if err != nil {
		t.Fatalf("SetActiveVersion(V 形态): %v", err)
	}
	if got != "v4.15" || f.svc.store.GetActive() != "v4.15" {
		t.Fatalf("SetActiveVersion = %q active=%q，应同落 v4.15", got, f.svc.store.GetActive())
	}
	if err := f.svc.RemoveVersion(" 4.15 "); err != nil {
		t.Fatalf("RemoveVersion(裸号形态): %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("卸载应移除版本目录，实得 err=%v", err)
	}
	if got := f.svc.store.GetActive(); got != "" {
		t.Fatalf("卸掉在用版本应清 active，实得 %q", got)
	}
}
