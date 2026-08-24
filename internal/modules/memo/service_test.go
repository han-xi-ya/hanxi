package memo

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMemoStoreAndCRUD(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "memo_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	dataDir := filepath.Join(tempDir, "data")
	_ = os.MkdirAll(dataDir, 0755)

	memoFile := filepath.Join(dataDir, "memo.json")
	store, err := NewStore(memoFile)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}

	// 1. 测试初始加载空数据
	items, err := store.Load()
	if err != nil {
		t.Fatalf("failed to load initial store: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("expected empty items, got %d", len(items))
	}

	// 2. 测试通过 Service 操作
	svc := &MemoService{
		store: store,
		items: items,
	}

	created, err := svc.Create("测试 SQL 片段", "SELECT * FROM users WHERE active = 1;", []string{"SQL", "Database"}, "blue")
	if err != nil {
		t.Fatalf("failed to create memo: %v", err)
	}
	if created.Title != "测试 SQL 片段" || len(created.Tags) != 2 {
		t.Errorf("unexpected created memo: %+v", created)
	}

	// 3. 测试查询与标签过滤
	listRes := svc.List(MemoFilter{Tag: "SQL"})
	if len(listRes) != 1 {
		t.Fatalf("expected 1 item with tag SQL, got %d", len(listRes))
	}

	// 4. 测试置顶切换
	pinned, err := svc.TogglePin(created.ID)
	if err != nil || !pinned {
		t.Errorf("expected pinned to be true, got %v, err: %v", pinned, err)
	}

	// 5. 测试脱敏遮罩切换
	masked, err := svc.ToggleMask(created.ID)
	if err != nil || !masked {
		t.Errorf("expected masked to be true, got %v, err: %v", masked, err)
	}

	// 6. 测试统计数据
	stats := svc.GetStats()
	if stats.TotalCount != 1 || stats.PinnedCount != 1 {
		t.Errorf("unexpected stats: %+v", stats)
	}
	if stats.TagCloud["#SQL"] != 1 {
		t.Errorf("tag cloud missing #SQL: %+v", stats.TagCloud)
	}

	// 7. 测试删除
	if err := svc.Delete(created.ID); err != nil {
		t.Fatalf("failed to delete memo: %v", err)
	}
	if len(svc.List(MemoFilter{})) != 0 {
		t.Errorf("expected 0 items after delete")
	}
}
