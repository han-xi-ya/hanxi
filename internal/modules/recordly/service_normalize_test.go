package recordly

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizeVersion(t *testing.T) {
	cases := []struct{ in, want string }{
		{"1.3.5", "v1.3.5"},
		{"v1.3.5", "v1.3.5"},
		{"V1.3.5", "v1.3.5"},
		{"  v1.3.5 ", "v1.3.5"},
		{" V1.3.5\t", "v1.3.5"},
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

// TestDownloadVersionInstalledDedupCaseInsensitive 已装 v1.3.5 时裸号/v号/
// V号/带空白四种入参都回 already-installed（CompareCore 数值核心互认），
// 且托管安装目录不被下载链触碰。本模块无下载函数接缝：修复路径在 go 语句
// 之前同步返回，若归一退回只剥小写 v 则返回值先红（行序保证无赛跑假绿）。
func TestDownloadVersionInstalledDedupCaseInsensitive(t *testing.T) {
	f := newRecordlyFixture(t, false)
	dir := mkInstall(t, f.versionsDir, "v1.3.5")

	for _, in := range []string{"1.3.5", "v1.3.5", "V1.3.5", " 1.3.5 "} {
		got, err := f.svc.DownloadVersion(in)
		if err != nil {
			t.Fatalf("DownloadVersion(%q): %v", in, err)
		}
		if got != "already-installed" {
			t.Fatalf("DownloadVersion(%q) = %q，期望 already-installed", in, got)
		}
	}
	fi, err := os.Stat(filepath.Join(dir, "Recordly.exe"))
	if err != nil || fi.Size() == 0 {
		t.Fatalf("去重路径不应动盘: err=%v size=%v", err, fi)
	}
	installed, err := f.svc.manager.ListInstalled()
	if err != nil || len(installed) != 1 {
		t.Fatalf("去重路径不得新增/损坏安装账目，实得 %+v err=%v", installed, err)
	}
}
