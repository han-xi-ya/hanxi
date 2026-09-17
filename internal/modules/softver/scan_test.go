package softver

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// writeFileTree 构造固定形状的小目录树：100B 文件 + 子目录内 250B 文件（共 350B / 2 文件）。
func writeFileTree(t *testing.T, root string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), make([]byte, 100), 0o644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(root, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "b.bin"), make([]byte, 250), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestWalkDirSizeCounts(t *testing.T) {
	dir := t.TempDir()
	writeFileTree(t, dir)
	var last scanStat
	var lastPath string
	stat, err := walkDirSize(context.Background(), dir, func(s scanStat, cur string) {
		last, lastPath = s, cur
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if stat.Bytes != 350 || stat.Files != 2 {
		t.Errorf("stat = %+v, 期望 Bytes=350 Files=2", stat)
	}
	if stat.Dirs < 1 {
		t.Errorf("子目录未计数: %+v", stat)
	}
	if lastPath == "" || last.Bytes != stat.Bytes {
		t.Errorf("进度回调缺失: lastPath=%q last=%+v", lastPath, last)
	}
}

func TestWalkDirSizeMissingRootHardFails(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope")
	if _, err := walkDirSize(context.Background(), missing, nil); err == nil {
		t.Fatal("根不存在应硬失败（不装「扫完了」）")
	}
}

func TestWalkDirSizePreCanceled(t *testing.T) {
	dir := t.TempDir()
	writeFileTree(t, dir)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := walkDirSize(ctx, dir, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, 期望 context.Canceled", err)
	}
}
