package memo

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"hanxi/internal/extapi"
)

// denyGate 恒拒调用门（停用态模拟，测试本地件）。
type denyGate struct{}

func (denyGate) Acquire(string) (func(), error) { return nil, errors.New("模块已停用") }

// newFullService 组装带数据根/旧库路径的 MemoService（与 NewMemoService 布线后同形，
// ClearAll 残片收口依赖这两字段）。
func newFullService(t *testing.T) (*MemoService, string, string) {
	t.Helper()
	dataDir := t.TempDir()
	memoDir := filepath.Join(dataDir, "memo")
	legacyPath := filepath.Join(dataDir, "state", "memo.json")
	return &MemoService{
		files: NewFileStore(memoDir), useFiles: true, items: []MemoItem{},
		holder: extapi.NewLeaseHolder(ID), dataDir: dataDir, legacyPath: legacyPath,
	}, memoDir, legacyPath
}

func dirFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// TestClearAllFiles 文件库模式一键全删：普通/置顶/敏感遮罩条目一律清光，
// 盘上目录收口为空（含隔离取证副本与原子写半程 tmp），事件照常广播。
func TestClearAllFiles(t *testing.T) {
	svc, memoDir, _ := newFullService(t)

	a, err := svc.Create("SQL 片段", "SELECT 1;", []string{"SQL"}, "blue")
	if err != nil {
		t.Fatal(err)
	}
	b, err := svc.Create("密钥", "sk-secret", []string{"Token"}, "rose")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.TogglePin(a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ToggleMask(b.ID); err != nil { // 敏感遮罩条目也在必删之列
		t.Fatal(err)
	}

	// 目录内混入数据-bearing 残骸：坏条隔离副本 + 原子写半程 tmp
	if err := os.WriteFile(filepath.Join(memoDir, "ghost.md.bad-20260926-120000"), []byte("---\nid: ghost\n---\n旧正文"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(memoDir, a.ID+".md.tmp.999"), []byte("半程"), 0644); err != nil {
		t.Fatal(err)
	}

	// 内存幽灵条（盘上文件已被外部删走）：全删以盘为准一并收口
	svc.items = append(svc.items, MemoItem{ID: "ghosty", Title: "盘上无此条"})

	events := 0
	svc.onChanged = func() { events++ }

	deleted, err := svc.ClearAll()
	if err != nil {
		t.Fatalf("ClearAll: %v", err)
	}
	if deleted != 3 {
		t.Errorf("删除条数 = %d, want 3", deleted)
	}
	if events != 1 {
		t.Errorf("onChanged 事件数 = %d, want 1", events)
	}
	items, _ := svc.List(MemoFilter{})
	if len(items) != 0 {
		t.Errorf("内存清单未清空: %+v", items)
	}
	if stats, _ := svc.GetStats(); stats.TotalCount != 0 || stats.PinnedCount != 0 || len(stats.TagCloud) != 0 {
		t.Errorf("统计未归零: %+v", stats)
	}
	if left := dirFiles(t, memoDir); len(left) != 0 {
		t.Errorf("memo/ 目录未收口: %v", left)
	}

	// 再点一次：空库幂等，返回 0 且不再广播
	deleted, err = svc.ClearAll()
	if err != nil || deleted != 0 || events != 1 {
		t.Errorf("空库重复全删 = %d/%v，事件数 = %d", deleted, err, events)
	}
}

// TestClearAllSweepsMigrationArtifacts 不留残片：旧库本体（文件库模式下的回滚
// 残留）、.migrated 迁移留底（内含全量旧便签正文）、.corrupt- 取证副本、半程暂存
// 目录全部销毁；不越界误删数据根其它内容。
func TestClearAllSweepsMigrationArtifacts(t *testing.T) {
	svc, memoDir, legacyPath := newFullService(t)

	if _, err := svc.Create("正常条目", "body", nil, ""); err != nil {
		t.Fatal(err)
	}
	stateDir := filepath.Dir(legacyPath)
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		t.Fatal(err)
	}
	write := func(path, content string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	write(legacyPath, `[{"id":"old1","title":"旧库残留"}]`)
	write(legacyPath+migratedSuffix, `[{"id":"old2","title":"迁移留底里的旧便签"}]`)
	write(legacyPath+corruptInfix+"20260926-000000", `[{坏库`)
	staging := filepath.Join(filepath.Dir(memoDir), stagingPrefix+"424242")
	if err := os.MkdirAll(staging, 0755); err != nil {
		t.Fatal(err)
	}
	write(filepath.Join(staging, "x.md"), "---\nid: x\n---\n暂存正文")
	unrelated := filepath.Join(filepath.Dir(memoDir), "settings.json")
	write(unrelated, `{"keep":true}`)

	deleted, err := svc.ClearAll()
	if err != nil {
		t.Fatalf("ClearAll: %v", err)
	}
	if deleted != 1 {
		t.Errorf("删除条数 = %d, want 1", deleted)
	}
	for _, p := range []string{
		legacyPath,
		legacyPath + migratedSuffix,
		legacyPath + corruptInfix + "20260926-000000",
		staging,
	} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("残片未销毁: %s (%v)", p, err)
		}
	}
	if _, err := os.Stat(unrelated); err != nil {
		t.Errorf("越界误删数据根其它内容: %v", err)
	}
	if left := dirFiles(t, memoDir); len(left) != 0 {
		t.Errorf("memo/ 目录未收口: %v", left)
	}
}

// TestClearAllLegacyFallback 回落旧库模式：memo.json 是读写权威，全删=原子写回空表
// （不删本体，避免下次启动无意义复活空库文件），同源派生物照常清。
func TestClearAllLegacyFallback(t *testing.T) {
	dir := t.TempDir()
	legacyPath := filepath.Join(dir, "memo.json")
	seedLegacy(t, legacyPath, []MemoItem{sampleItem(), {ID: "memo_2", Title: "二", Content: "x", IsMasked: true}})
	store, err := NewStore(legacyPath)
	if err != nil {
		t.Fatal(err)
	}
	items, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	svc := &MemoService{store: store, items: items, holder: extapi.NewLeaseHolder(ID), legacyPath: legacyPath}

	deleted, err := svc.ClearAll()
	if err != nil {
		t.Fatalf("ClearAll: %v", err)
	}
	if deleted != 2 {
		t.Errorf("删除条数 = %d, want 2", deleted)
	}
	restored, err := store.Load()
	if err != nil || len(restored) != 0 {
		t.Errorf("旧库未写回空表: %+v %v", restored, err)
	}
	if got, _ := svc.List(MemoFilter{}); len(got) != 0 {
		t.Errorf("内存清单未清空: %+v", got)
	}
}

// TestClearAllPartialFailureKeepsUndeleted 单点删除失败（Windows 文件占用）：
// 已确认删除的条目从内存移除，失败条目留在内存（内存与盘不分叉），错误如实上浮。
func TestClearAllPartialFailureKeepsUndeleted(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("以 Windows 文件占用模拟删除失败，非 Windows 平台该失败不复现")
	}
	svc, memoDir, _ := newFullService(t)
	gone, err := svc.Create("会被删掉", "x", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	stuck, err := svc.Create("删不掉", "y", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	// 持打开句柄让删除吃 sharing violation（Windows 原生失败面；项目目标平台即 Windows）
	hd, oerr := os.OpenFile(filepath.Join(memoDir, stuck.ID+".md"), os.O_RDWR, 0644)
	if oerr != nil {
		t.Fatal(oerr)
	}
	t.Cleanup(func() { _ = hd.Close() })

	deleted, err := svc.ClearAll()
	if err == nil {
		t.Fatal("部分失败必须上浮错误")
	}
	if !strings.Contains(err.Error(), stuck.ID) {
		t.Errorf("错误未点名失败文件: %v", err)
	}
	if deleted != 1 {
		t.Errorf("确认删除条数 = %d, want 1", deleted)
	}
	items, _ := svc.List(MemoFilter{})
	if len(items) != 1 || items[0].ID != stuck.ID {
		t.Errorf("失败条目应留在内存（与盘不分叉）: %+v", items)
	}
	// 成功那条的盘上文件确已销毁
	if _, err := os.Stat(filepath.Join(memoDir, gone.ID+".md")); !os.IsNotExist(err) {
		t.Error("已删条目文件未销毁")
	}
}

// TestClearAllGated memo 停用（门关闭）时全删被拒，数据原样在位。
func TestClearAllGated(t *testing.T) {
	svc, memoDir, _ := newFullService(t)
	if _, err := svc.Create("受门保护", "x", nil, ""); err != nil {
		t.Fatal(err)
	}
	svc.holder.SetGate(denyGate{})
	if _, err := svc.ClearAll(); err == nil {
		t.Fatal("停用态应拒绝全删")
	}
	if left := dirFiles(t, memoDir); len(left) != 1 {
		t.Errorf("门拒绝后不得先删数据: %v", left)
	}
}

// TestMemoIDFromFile 文件名→本尊 ID 的三种形态与拒斥形态。
func TestMemoIDFromFile(t *testing.T) {
	cases := []struct {
		name string
		id   string
		ok   bool
	}{
		{"memo_1.md", "memo_1", true},
		{"memo_1.md.bad-20260926-000000", "memo_1", true},
		{"memo_1.md.tmp.4242", "memo_1", true},
		{"README.txt", "", false},
		{"weird.bad-1", "", false},
	}
	for _, c := range cases {
		id, ok := memoIDFromFile(c.name)
		if id != c.id || ok != c.ok {
			t.Errorf("memoIDFromFile(%q) = %q,%v want %q,%v", c.name, id, ok, c.id, c.ok)
		}
	}
}
