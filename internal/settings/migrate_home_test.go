package settings

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mustWrite 落一个小文件（含父目录）。
func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestLegacyAppDataHome(t *testing.T) {
	if got := legacyAppDataHome(""); got != "" {
		t.Fatalf("无 APPDATA 应返回空, got %q", got)
	}
	if got := legacyAppDataHome("   "); got != "" {
		t.Fatalf("空白 APPDATA 应返回空, got %q", got)
	}
	want := filepath.Join(`C:\Users\me\AppData\Roaming`, "Hanxi")
	if got := legacyAppDataHome(`C:\Users\me\AppData\Roaming`); got != want {
		t.Fatalf("旧家定位错误: got %q want %q", got, want)
	}
}

// TestShouldMigrateLegacyHome 搬迁闸门矩阵：只有"旧家有效、新家全新"才放行。
func TestShouldMigrateLegacyHome(t *testing.T) {
	root := t.TempDir()
	oldHome := filepath.Join(root, "appdata", "Hanxi")
	newHome := filepath.Join(root, "app", "hanxidata")
	if err := os.MkdirAll(filepath.Join(oldHome, "versions"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(newHome, 0755); err != nil {
		t.Fatal(err)
	}

	if !shouldMigrateLegacyHome(oldHome, newHome) {
		t.Fatal("旧家有特征、新家全新应放行")
	}
	if shouldMigrateLegacyHome("", newHome) {
		t.Fatal("旧家不可定位不得搬")
	}
	if shouldMigrateLegacyHome(oldHome, oldHome) {
		t.Fatal("新旧同家不得搬")
	}
	// 新家恰好被绑定成旧家本身（仅大小写写法不同）：Windows 路径口径必须判同家
	if shouldMigrateLegacyHome(oldHome, strings.ToUpper(oldHome)) {
		t.Fatal("新旧同家的大小写变体不得自我搬迁")
	}
	// 新家已有 config.json = 已安身
	mustWrite(t, filepath.Join(newHome, "config.json"), "{}")
	if shouldMigrateLegacyHome(oldHome, newHome) {
		t.Fatal("新家已有数据根特征不得再搬（防回滚反向污染）")
	}
	// 旧家无特征 = 空壳
	emptyOld := filepath.Join(root, "empty-old")
	if err := os.MkdirAll(emptyOld, 0755); err != nil {
		t.Fatal(err)
	}
	freshNew := filepath.Join(root, "fresh-new")
	if err := os.MkdirAll(freshNew, 0755); err != nil {
		t.Fatal(err)
	}
	if shouldMigrateLegacyHome(emptyOld, freshNew) {
		t.Fatal("空壳旧家不值得动手")
	}
	// 新家嵌在旧家之内（exe 直接住在 %APPDATA%\Hanxi 下的极端场景）
	nested := filepath.Join(oldHome, "app", "hanxidata")
	if err := os.MkdirAll(nested, 0755); err != nil {
		t.Fatal(err)
	}
	if shouldMigrateLegacyHome(oldHome, nested) {
		t.Fatal("新家位于旧家内部时必须放弃自动搬迁")
	}
}

// TestMigrateFromLegacyHome 完整搬迁：顶层文件与版本子树整体挪进新家，
// 旧家搬光后清理；新家同名条目跳过不覆盖且不删旧家原件。
func TestMigrateFromLegacyHome(t *testing.T) {
	root := t.TempDir()
	oldHome := filepath.Join(root, "old")
	newHome := filepath.Join(root, "new")
	if err := os.MkdirAll(oldHome, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(newHome, 0755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(oldHome, "config.json"), `{"theme":"dark"}`)
	mustWrite(t, filepath.Join(oldHome, "state", "ocr.json"), `{"a":1}`)
	mustWrite(t, filepath.Join(oldHome, "versions", "everything", "1.0", "es.exe"), "binary-ish")

	if err := migrateFromLegacyHome(oldHome, newHome); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{
		"config.json",
		filepath.Join("state", "ocr.json"),
		filepath.Join("versions", "everything", "1.0", "es.exe"),
	} {
		if _, err := os.Stat(filepath.Join(newHome, rel)); err != nil {
			t.Errorf("条目未搬进新家: %s (%v)", rel, err)
		}
	}
	if _, err := os.Stat(filepath.Join(oldHome, "config.json")); !os.IsNotExist(err) {
		t.Error("搬迁成功后旧家原件必须消失（move 语义）")
	}
	if _, err := os.Stat(oldHome); !os.IsNotExist(err) {
		t.Error("旧家搬光后应被清理（退出用户目录的最后一米）")
	}

	// 第二轮：新家同名条目跳过不覆盖，旧家残留保留
	secondOld := filepath.Join(root, "old2")
	mustWrite(t, filepath.Join(secondOld, "config.json"), "LEGACY")
	mustWrite(t, filepath.Join(secondOld, "keepme.json"), "keep")
	mustWrite(t, filepath.Join(newHome, "keepme.json"), "NEWER")
	if err := migrateFromLegacyHome(secondOld, newHome); err == nil {
		t.Fatal("存在同名冲突时必须报告迁移未完成")
	}
	if got, err := os.ReadFile(filepath.Join(newHome, "keepme.json")); err != nil || string(got) != "NEWER" {
		t.Errorf("新家同名条目被覆盖: %q (%v)", got, err)
	}
	if _, err := os.Stat(filepath.Join(secondOld, "keepme.json")); err != nil {
		t.Error("存在冲突条目时旧家原件不得被删除")
	}
	if _, err := os.Stat(filepath.Join(newHome, migrationJournalName)); err != nil {
		t.Error("存在冲突时 journal 必须保留，不得宣布迁移完成")
	}
}

func TestMigrateFromLegacyHomeResumesPendingAfterFailure(t *testing.T) {
	root := t.TempDir()
	oldHome := filepath.Join(root, "old")
	newHome := filepath.Join(root, "new")
	mustWrite(t, filepath.Join(oldHome, "a.json"), "a")
	mustWrite(t, filepath.Join(oldHome, "b.json"), "b")

	original := moveLegacyPath
	t.Cleanup(func() { moveLegacyPath = original })
	failed := false
	moveLegacyPath = func(src, dst string) error {
		if filepath.Base(src) == "b.json" && !failed {
			failed = true
			return errors.New("injected move failure")
		}
		return movePath(src, dst)
	}
	if err := migrateFromLegacyHome(oldHome, newHome); err == nil {
		t.Fatal("故障注入后首轮应失败")
	}
	if got, err := os.ReadFile(filepath.Join(newHome, "a.json")); err != nil || string(got) != "a" {
		t.Fatalf("首项应已提交: %q %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(oldHome, "b.json")); err != nil {
		t.Fatal("失败项源文件必须保留")
	}

	moveLegacyPath = original
	if err := migrateFromLegacyHome(oldHome, newHome); err != nil {
		t.Fatalf("续跑应完成剩余项: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(newHome, "b.json")); err != nil || string(got) != "b" {
		t.Fatalf("续跑未提交失败项: %q %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(newHome, migrationJournalName)); !os.IsNotExist(err) {
		t.Fatal("全部完成后 journal 应删除")
	}
	if _, err := os.Stat(oldHome); !os.IsNotExist(err) {
		t.Fatal("全部完成后旧家应清理")
	}
}

func TestMigrateLegacyHomeResumesDespiteNewRootFeature(t *testing.T) {
	root := t.TempDir()
	appData := filepath.Join(root, "appdata")
	oldHome := legacyAppDataHome(appData)
	newHome := filepath.Join(root, "new")
	mustWrite(t, filepath.Join(oldHome, "config.json"), "{}")
	mustWrite(t, filepath.Join(oldHome, "later.json"), "later")
	if err := os.MkdirAll(newHome, 0755); err != nil {
		t.Fatal(err)
	}
	if err := saveMigrationJournal(filepath.Join(newHome, migrationJournalName), []string{"later.json"}); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(newHome, "config.json"), "{}")
	t.Setenv("APPDATA", appData)

	migrateLegacyHome(newHome)
	if got, err := os.ReadFile(filepath.Join(newHome, "later.json")); err != nil || string(got) != "later" {
		t.Fatalf("已有 journal 时不得被新家特征挡住续跑: %q %v", got, err)
	}
}

// TestMovePathCopyFallback copy+delete 退路（跨卷场景的纯逻辑面）：
// 子树整体复制后删源；复制中途失败时旧家原件必须仍在。
func TestMovePathCopyFallback(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	mustWrite(t, filepath.Join(src, "a", "b.txt"), "hello")
	mustWrite(t, filepath.Join(src, "c.txt"), "world")

	if err := movePath(src, dst); err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(dst, "a", "b.txt")); err != nil || string(got) != "hello" {
		t.Fatalf("子树复制不完整: %q (%v)", got, err)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatal("复制全成后源应被删除")
	}

	// 目标被文件挡住 → copyPath 失败 → 源原件保留
	blocker := filepath.Join(root, "blocked")
	mustWrite(t, blocker, "x")
	src2 := filepath.Join(root, "src2")
	mustWrite(t, filepath.Join(src2, "k.json"), "k")
	if err := movePath(src2, filepath.Join(blocker, "sub")); err == nil {
		t.Fatal("目标不可写应报错")
	}
	if _, err := os.Stat(filepath.Join(src2, "k.json")); err != nil {
		t.Fatal("复制失败时旧家原件必须原样保留（先复制后删源铁律）")
	}
}
