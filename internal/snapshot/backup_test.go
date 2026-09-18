package snapshot

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestBackupEngine(t *testing.T) (*backupEngine, string) {
	t.Helper()
	dataDir := t.TempDir()
	eng := newBackupEngine(dataDir, filepath.Join(dataDir, snapshotsDirName))
	return eng, dataDir
}

func TestBackupEngineChangesAndCommit(t *testing.T) {
	eng, dataDir := newTestBackupEngine(t)
	ctx := context.Background()

	// 空数据：无变更
	files, err := eng.changes(ctx)
	if err != nil || len(files) != 0 {
		t.Fatalf("empty: %v %v", files, err)
	}

	write(t, dataDir, "config.json", `{"a":1}`)
	write(t, dataDir, "state/memo.json", `[]`)
	write(t, dataDir, "runtime/frpc/x.toml", "secret")            // 白名单外
	write(t, dataDir, "state/memo.json.tmp.999", "debris")        // 残骸
	write(t, dataDir, "config.json.corrupt-20260101-000000", "q") // 取证副本

	files, err = eng.changes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(files, ",") != "config.json,state/memo.json" {
		t.Fatalf("changes = %v", files)
	}

	if err := eng.commit(ctx, files); err != nil {
		t.Fatal(err)
	}
	// 提交后无变更（内容未动，mtime 跳也不算）
	if again, _ := eng.changes(ctx); len(again) != 0 {
		t.Fatalf("commit 后仍有变更: %v", again)
	}

	// 改写 + 删除
	write(t, dataDir, "config.json", `{"a":2}`)
	os.Remove(filepath.Join(dataDir, "state", "memo.json"))
	files, err = eng.changes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(files, ",") != "config.json,state/memo.json" {
		t.Fatalf("changes2 = %v", files)
	}
	if err := eng.commit(ctx, files); err != nil {
		t.Fatal(err)
	}

	revs, err := eng.revisions(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(revs) != 2 {
		t.Fatalf("revisions = %+v", revs)
	}
	if revs[0].Summary != "1 个文件" {
		t.Errorf("rev0 summary = %q", revs[0].Summary)
	}

	// 首份备份里 memo.json 原样可读回
	details, err := eng.revisionFiles(ctx, revs[1].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(details) != 2 {
		t.Errorf("rev1 files = %+v", details)
	}
	data, err := eng.file(ctx, revs[1].ID, "config.json")
	if err != nil || string(data) != `{"a":1}` {
		t.Fatalf("rev1 config = %q err=%v", data, err)
	}

	// 备份目录不得把 .snapshots 自身卷进拷贝（自嵌套坑）
	leak := filepath.Join(dataDir, snapshotsDirName, backupDirName, revs[0].ID, snapshotsDirName)
	if _, err := os.Stat(leak); err == nil {
		t.Fatal("备份混入了 .snapshots 自身")
	}
}

func TestBackupEnginePrune(t *testing.T) {
	eng, dataDir := newTestBackupEngine(t)
	ctx := context.Background()
	write(t, dataDir, "config.json", `{"n":0}`)

	for i := 0; i < backupKeepCount+5; i++ {
		write(t, dataDir, "config.json", fmt.Sprintf(`{"n":%d}`, i))
		if _, err := eng.changes(ctx); err != nil {
			t.Fatal(err)
		}
		if err := eng.commit(ctx, nil); err != nil {
			t.Fatal(err)
		}
		// 同秒连拍靠 -n 序号避让；修剪按目录计
		revs, err := eng.revisions(ctx, 100)
		if err != nil {
			t.Fatal(err)
		}
		if len(revs) > backupKeepCount {
			t.Fatalf("修剪未生效: %d > %d", len(revs), backupKeepCount)
		}
	}
	dirs, _ := eng.backupDirs()
	if len(dirs) != backupKeepCount {
		t.Fatalf("最终份数 %d", len(dirs))
	}
}

func TestBackupEngineFileGuards(t *testing.T) {
	eng, dataDir := newTestBackupEngine(t)
	ctx := context.Background()
	write(t, dataDir, "config.json", `{}`)
	if err := eng.commit(ctx, []string{"config.json"}); err != nil {
		t.Fatal(err)
	}
	revs, _ := eng.revisions(ctx, 1)
	if _, err := eng.file(ctx, revs[0].ID, "../config.json"); err == nil {
		t.Error("穿越应拒")
	}
	if _, err := eng.file(ctx, revs[0].ID, "runtime/frpc/x.toml"); err == nil {
		t.Error("白名单外应拒")
	}
	if _, err := eng.file(ctx, "deadbeef", "config.json"); err == nil {
		t.Error("git hash 形态标识在备份模式应拒")
	}
}

func TestBackupEngineIgnoresUnpublishedAndInvalidBackups(t *testing.T) {
	eng, dataDir := newTestBackupEngine(t)
	ctx := context.Background()
	write(t, dataDir, rootConfig, `{}`)
	if err := eng.commit(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(eng.backupRoot, ".pending-debris"), 0755); err != nil {
		t.Fatal(err)
	}
	invalid := filepath.Join(eng.backupRoot, "20990101-000000")
	if err := os.MkdirAll(invalid, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(invalid, manifestName), []byte(`{"../evil":"bad"}`), 0644); err != nil {
		t.Fatal(err)
	}
	revs, err := eng.revisions(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(revs) != 1 {
		t.Fatalf("列表只应承认合法已发布 manifest，got %+v", revs)
	}
}

func TestBackupEnginePublishFailureLeavesNoVisibleRevision(t *testing.T) {
	eng, dataDir := newTestBackupEngine(t)
	ctx := context.Background()
	write(t, dataDir, rootConfig, `{}`)
	eng.ops.rename = func(string, string) error { return fmt.Errorf("injected rename failure") }
	if err := eng.commit(ctx, nil); err == nil {
		t.Fatal("故障注入应使发布失败")
	}
	revs, err := eng.revisions(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(revs) != 0 {
		t.Fatalf("未原子发布的备份不得可见: %+v", revs)
	}
	entries, err := os.ReadDir(eng.backupRoot)
	if err != nil {
		t.Fatal(err)
	}
	foundPending := false
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), pendingBackupPrefix) {
			foundPending = true
		}
	}
	if !foundPending {
		t.Fatal("失败暂存目录应保留供诊断")
	}
}

func TestParseBackupDirTime(t *testing.T) {
	ts := parseBackupDirTime("20260917-143000")
	if ts.Format(backupTimeLayout) != "20260917-143000" {
		t.Errorf("plain = %v", ts)
	}
	ts2 := parseBackupDirTime("20260917-143000-3")
	if ts2.Format(backupTimeLayout) != "20260917-143000" {
		t.Errorf("suffixed = %v", ts2)
	}
}
