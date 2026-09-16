package logging

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeLog 在 dir 下创建名为 name 的日志文件并把 mtime 拨到 ageDays 天前。
func writeLog(t *testing.T, dir, name string, ageDays int) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("x\n"), 0644); err != nil {
		t.Fatal(err)
	}
	stamp := time.Now().AddDate(0, 0, -ageDays)
	if err := os.Chtimes(p, stamp, stamp); err != nil {
		t.Fatal(err)
	}
}

func TestPruneOldLogs(t *testing.T) {
	dir := t.TempDir()
	writeLog(t, dir, "app-2020-01-01.log", 30) // 过期：应删
	writeLog(t, dir, "app-yesterday.log", 1)   // 未过期：应留
	writeLog(t, dir, "other-2020-01-01.log", 30)
	writeLog(t, dir, "app-notalog.txt", 30)

	pruneOldLogs(dir, 7)

	for _, tc := range []struct {
		name string
		want bool // true = 应仍存在
	}{
		{"app-2020-01-01.log", false},
		{"app-yesterday.log", true},
		{"other-2020-01-01.log", true}, // 非 app-*.log 命名不触碰
		{"app-notalog.txt", true},      // 非 .log 后缀不触碰
	} {
		_, err := os.Stat(filepath.Join(dir, tc.name))
		if tc.want && err != nil {
			t.Errorf("%s 应保留，实际被删除或不可读: %v", tc.name, err)
		}
		if !tc.want && err == nil {
			t.Errorf("%s 应被清理，实际仍存在", tc.name)
		}
	}
}

// retainDays<=0 视为不清理（与 GetGeneralSettings 的"0=永久保留"口径一致）。
func TestPruneOldLogsDisabled(t *testing.T) {
	dir := t.TempDir()
	writeLog(t, dir, "app-2020-01-01.log", 365)

	pruneOldLogs(dir, 0)
	pruneOldLogs(dir, -1)

	if _, err := os.Stat(filepath.Join(dir, "app-2020-01-01.log")); err != nil {
		t.Fatalf("retainDays<=0 不应清理任何文件: %v", err)
	}
}
