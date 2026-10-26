package litemonitor

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"hanxi/internal/extapi"
	"hanxi/internal/modules/litemonitor/version"
)

func TestNormalizeVersion(t *testing.T) {
	cases := []struct{ in, want string }{
		{"1.3.6", "v1.3.6"},
		{"v1.3.6", "v1.3.6"},
		{"V1.3.6", "v1.3.6"},
		{"  v1.3.6 ", "v1.3.6"},
		{" V1.3.6\t", "v1.3.6"},
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

// newNormalizeTestLiteMonitorService 带具名 versionsDir 的最小装配（harness 同法，
// engine 不在下载路径上留空）。
func newNormalizeTestLiteMonitorService(t *testing.T) (*LiteMonitorService, string) {
	t.Helper()
	versionsDir := t.TempDir()
	return &LiteMonitorService{
		manager: version.NewManager(versionsDir),
		store:   newLiteMonitorStore(t.TempDir()),
		holder:  extapi.NewLeaseHolder(ID),
	}, versionsDir
}

// seedLiteMonitorImported 伪造 imported- 兜底导入目录（exe 非空 + 语言包锚点）。
// 数字版本目录在单测里无法伪造：ListInstalled 对非 imported 令牌强制校验
// PE FileVersion 与目录名一致，而测试环境无任何真实 PE 样本（软探针造不出
// 版本资源）；imported- 令牌免 PE 闸，恰是 windterm 用例钉过的透传形态，
// 故行为面用它，数字形态由 normalizeVersion 词表钉死。
func seedLiteMonitorImported(t *testing.T, versionsDir string) {
	t.Helper()
	dir := filepath.Join(versionsDir, "litemonitor_imported-0123456789ab")
	if err := os.MkdirAll(filepath.Join(dir, "resources", "lang"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "LiteMonitor.exe"), []byte("MZ伪造载荷"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "resources", "lang", "zh.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestDownloadVersionInstalledDedupCaseInsensitive 已装导入件在裸令牌/v号/V号/
// 带空白四种入参下都回 already-installed，且下载接缝零调用（修复路径在 go
// 语句之前同步返回，判据无赛跑；V 形态旧归一产出 vVimported-… 去重落空）。
func TestDownloadVersionInstalledDedupCaseInsensitive(t *testing.T) {
	svc, versionsDir := newNormalizeTestLiteMonitorService(t)
	seedLiteMonitorImported(t, versionsDir)
	var calls atomic.Int32
	svc.download = func(context.Context, string, string, func(version.DownloadProgress)) error {
		calls.Add(1)
		return nil
	}

	const token = "imported-0123456789ab"
	for _, in := range []string{token, "v" + token, "V" + token, " " + "v" + token + " "} {
		got, err := svc.DownloadVersion(in)
		if err != nil {
			t.Fatalf("DownloadVersion(%q): %v", in, err)
		}
		if got != "already-installed" {
			t.Fatalf("DownloadVersion(%q) = %q，期望 already-installed", in, got)
		}
	}
	if n := calls.Load(); n != 0 {
		t.Fatalf("去重路径不得启动下载，接缝被调 %d 次", n)
	}
	ents, err := os.ReadDir(versionsDir)
	if err != nil || len(ents) != 1 {
		t.Fatalf("去重路径不得新增版本目录，实得 %+v err=%v", ents, err)
	}
}
