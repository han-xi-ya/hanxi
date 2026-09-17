package memo

import (
	"os"
	"path/filepath"
	"testing"
)

// newFilesService 直接组装文件库模式的 MemoService（绕过 settings.Paths 全局解析，
// 与既有测试同款构造路径）。
func newFilesService(t *testing.T) (*MemoService, string) {
	t.Helper()
	memoDir := filepath.Join(t.TempDir(), "memo")
	return &MemoService{files: NewFileStore(memoDir), useFiles: true, items: []MemoItem{}}, memoDir
}

// TestMemoFilesCRUD 文件库模式端到端：创建/更新/置顶/删除只动单条文件，
// "重启"（重新装载）后内容一致。
func TestMemoFilesCRUD(t *testing.T) {
	svc, memoDir := newFilesService(t)

	created, err := svc.Create("片段", "SELECT 1;", []string{"SQL"}, "blue")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(memoDir, created.ID+".md")); err != nil {
		t.Fatalf("Create 未落单条文件: %v", err)
	}
	// 只应有一个文件
	if entries, _ := os.ReadDir(memoDir); len(entries) != 1 {
		t.Fatalf("Create 波及其它文件: %v", entries)
	}

	updated, err := svc.Update(created.ID, "片段改名", "SELECT 2;", []string{"SQL", "Db"}, "")
	if err != nil || updated.Title != "片段改名" {
		t.Fatalf("Update: %+v %v", updated, err)
	}
	if _, err := svc.TogglePin(created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ToggleMask(created.ID); err != nil {
		t.Fatal(err)
	}

	// QuickCreate（fileshare 投递联动回归位）
	if err := svc.QuickCreate("投递", "text", []string{"share"}); err != nil {
		t.Fatalf("QuickCreate: %v", err)
	}

	// "重启"：从盘上重新装载
	restarted := &MemoService{files: NewFileStore(memoDir), useFiles: true}
	items, lerr := restarted.files.LoadAll()
	if lerr != nil || len(items) != 2 {
		t.Fatalf("重启装载 = %+v %v", items, lerr)
	}

	// 删除第二条
	if err := svc.Delete(updated.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(memoDir, updated.ID+".md")); !os.IsNotExist(err) {
		t.Error("Delete 未删单条文件")
	}
	if len(svc.List(MemoFilter{})) != 1 {
		t.Error("内存清单未同步删除")
	}
}

// TestMemoFilesMigrationIntegration 旧库 → 迁移 → 文件库读写一条龙。
func TestMemoFilesMigrationIntegration(t *testing.T) {
	dataDir := t.TempDir()
	state := filepath.Join(dataDir, "state")
	if err := os.MkdirAll(state, 0755); err != nil {
		t.Fatal(err)
	}
	legacy := filepath.Join(state, "memo.json")
	seedLegacy(t, legacy, []MemoItem{sampleItem(), {ID: "memo_2", Title: "二", Content: "x"}})

	memoDir := filepath.Join(dataDir, "memo")
	if need, err := memoNeedsMigration(legacy, memoDir); err != nil || !need {
		t.Fatalf("判据: %v %v", need, err)
	}
	if committed, err := migrateMemoToFiles(legacy, memoDir); !committed || err != nil {
		t.Fatalf("迁移: %v %v", committed, err)
	}
	svc := &MemoService{files: NewFileStore(memoDir), useFiles: true}
	svc.items, _ = svc.files.LoadAll()
	if len(svc.items) != 2 {
		t.Fatalf("迁移后装载 = %d", len(svc.items))
	}
	if _, err := svc.Create("迁移后新建", "c", nil, ""); err != nil {
		t.Fatal(err)
	}
	if len(svc.List(MemoFilter{})) != 3 {
		t.Error("迁移后新增异常")
	}
}

// TestMemoRestoreFile 快照热恢复钩子语义：校验、落盘、内存换装。
func TestMemoRestoreFile(t *testing.T) {
	svc, memoDir := newFilesService(t)
	created, err := svc.Create("旧标题", "旧内容", nil, "blue")
	if err != nil {
		t.Fatal(err)
	}
	// 伪造"历史版本"字节（该条的旧形态）
	old := created
	old.Title = "历史标题"
	old.Content = "历史内容"
	data, err := EncodeMemo(old)
	if err != nil {
		t.Fatal(err)
	}

	if err := svc.RestoreFile(created.ID, string(data)); err != nil {
		t.Fatal(err)
	}
	items := svc.List(MemoFilter{})
	if len(items) != 1 || items[0].Title != "历史标题" {
		t.Fatalf("内存未换装: %+v", items)
	}
	onDisk, _ := os.ReadFile(filepath.Join(memoDir, created.ID+".md"))
	if string(onDisk) != string(data) {
		t.Error("盘上非原样字节")
	}

	// 守卫：非法内容 / ID 不符 / 回落模式
	if err := svc.RestoreFile(created.ID, "garbage"); err == nil {
		t.Error("非 frontmatter 内容应拒")
	}
	if err := svc.RestoreFile("other_id", string(data)); err == nil {
		t.Error("ID 不符应拒")
	}
	legacy := &MemoService{items: []MemoItem{}}
	if err := legacy.RestoreFile("x", string(data)); err == nil {
		t.Error("回落旧库模式应拒绝热恢复")
	}
}

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
