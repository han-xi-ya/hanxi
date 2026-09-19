package artifact

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stageContent 建一个带内容的中转目录并返回路径。
func stageContent(t *testing.T, tree *Tree, txn, fileName, content string) string {
	t.Helper()
	staging, discard, err := tree.StageDir(txn)
	if err != nil {
		t.Fatalf("StageDir(%q) 失败: %v", txn, err)
	}
	t.Cleanup(discard)
	if err := os.WriteFile(filepath.Join(staging, fileName), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return staging
}

func TestTreeLifecycle(t *testing.T) {
	root := filepath.Join(t.TempDir(), "versions")
	tree := OpenTree(root, "demo")

	shaA := strings.Repeat("aa", 32)
	staging := stageContent(t, tree, "tx1", "app.exe", "v1")
	if err := tree.Commit(staging, "1.0.0", Meta{ZipSHA256: shaA}); err != nil {
		t.Fatalf("首次落位失败: %v", err)
	}
	dir, err := tree.Resolve("1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(dir) != "demo_1.0.0" {
		t.Fatalf("目录命名不符: %s", dir)
	}
	if _, err := os.Stat(filepath.Join(dir, "app.exe")); err != nil {
		t.Fatalf("落位内容丢失: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "meta.json")); err != nil {
		t.Fatalf("meta.json 未写入: %v", err)
	}
	// staging 已被 rename 搬空，discard 应为幂等 no-op
	if _, err := os.Stat(staging); !os.IsNotExist(err) {
		t.Fatal("staging 应已不存在")
	}

	// 再装一版：Versions 降序 + Resolve("") 取最新可用
	staging2 := stageContent(t, tree, "tx2", "app.exe", "v2")
	if err := tree.Commit(staging2, "1.10.0", Meta{ZipSHA256: strings.Repeat("bb", 32)}); err != nil {
		t.Fatal(err)
	}
	list, err := tree.Versions()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].Version != "1.10.0" || list[1].Version != "1.0.0" {
		t.Fatalf("Versions 排序异常: %+v", list)
	}
	latest, err := tree.Resolve("")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(latest) != "demo_1.10.0" {
		t.Fatalf("Resolve('') 应取最新: %s", latest)
	}

	// 卸载
	if err := tree.Remove("1.0.0", nil); err != nil {
		t.Fatalf("卸载失败: %v", err)
	}
	if _, err := tree.Resolve("1.0.0"); err == nil {
		t.Fatal("已卸载版本不应可解析")
	}
	if err := tree.Remove("1.0.0", nil); err == nil || !strings.Contains(err.Error(), "未安装") {
		t.Fatalf("重复卸载报错异常: %v", err)
	}
}

func TestTreeCommitIdempotentAndDrift(t *testing.T) {
	root := filepath.Join(t.TempDir(), "versions")
	tree := OpenTree(root, "demo")
	sha := strings.Repeat("cc", 32)

	s1 := stageContent(t, tree, "tx1", "a.bin", "one")
	if err := tree.Commit(s1, "2.0.0", Meta{ZipSHA256: sha}); err != nil {
		t.Fatal(err)
	}
	// 同版本同摘要：幂等复用，返回成功且不触碰既有目录
	s2 := stageContent(t, tree, "tx2", "a.bin", "two")
	if err := tree.Commit(s2, "2.0.0", Meta{ZipSHA256: strings.ToUpper(sha)}); err != nil {
		t.Fatalf("同摘要应幂等: %v", err)
	}
	dir, _ := tree.Resolve("2.0.0")
	got, err := os.ReadFile(filepath.Join(dir, "a.bin"))
	if err != nil || string(got) != "one" {
		t.Fatalf("幂等落位竟改写了既有内容: %q %v", got, err)
	}
	if _, err := os.Stat(s2); !os.IsNotExist(err) {
		t.Fatal("幂等路径应丢弃 staging")
	}
	// 同版本异摘要：拒绝防漂移
	s3 := stageContent(t, tree, "tx3", "a.bin", "three")
	if err := tree.Commit(s3, "2.0.0", Meta{ZipSHA256: strings.Repeat("dd", 32)}); err == nil ||
		!strings.Contains(err.Error(), "漂移") {
		t.Fatalf("异摘要应拒绝防漂移: %v", err)
	}
}

func TestTreeCommitRejectsResidue(t *testing.T) {
	root := filepath.Join(t.TempDir(), "versions")
	tree := OpenTree(root, "demo")

	// 落位中断模拟：目标名已被"无账本非空目录"占用
	target := filepath.Join(root, "demo_9.9.9")
	if err := os.MkdirAll(target, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "junk.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	staging := stageContent(t, tree, "tx1", "app.exe", "x")
	if err := tree.Commit(staging, "9.9.9", Meta{ZipSHA256: strings.Repeat("ee", 32)}); err == nil ||
		!strings.Contains(err.Error(), "缺少可信元信息") {
		t.Fatalf("疑似残留目录必须拒绝覆盖: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "junk.txt")); err != nil {
		t.Fatal("拒绝路径不得破坏既有残留目录")
	}

	// staging 越界（根目录外）必须拒绝
	elsewhere := filepath.Join(t.TempDir(), "sneaky")
	if err := os.MkdirAll(filepath.Join(elsewhere, "payload"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := tree.Commit(elsewhere, "3.0.0", Meta{}); err == nil ||
		!strings.Contains(err.Error(), "不在版本树根目录内") {
		t.Fatalf("越界 staging 必须拒绝: %v", err)
	}

	// 空中转目录拒绝
	emptyStaging, discard, err := tree.StageDir("tx-empty")
	if err != nil {
		t.Fatal(err)
	}
	defer discard()
	if err := tree.Commit(emptyStaging, "3.0.1", Meta{}); err == nil ||
		!strings.Contains(err.Error(), "为空") {
		t.Fatalf("空 staging 必须拒绝: %v", err)
	}
}

func TestTreeRemoveInUse(t *testing.T) {
	root := filepath.Join(t.TempDir(), "versions")
	tree := OpenTree(root, "demo")
	s := stageContent(t, tree, "tx1", "app.exe", "x")
	if err := tree.Commit(s, "1.0.0", Meta{ZipSHA256: strings.Repeat("ff", 32)}); err != nil {
		t.Fatal(err)
	}
	dir, _ := tree.Resolve("1.0.0")

	// 假 inUse：进程占用探测先行拒卸
	busy := func(got string) error {
		if got != dir {
			t.Fatalf("inUse 收到目录异常: %s", got)
		}
		return errors.New("pid 4242 running")
	}
	err := tree.Remove("1.0.0", busy)
	if err == nil || !strings.Contains(err.Error(), "正在使用中") || !strings.Contains(err.Error(), "先退出") {
		t.Fatalf("占用拒卸应给人话错误: %v", err)
	}
	if _, statErr := os.Stat(dir); statErr != nil {
		t.Fatal("拒卸后目录必须完好")
	}
	// inUse 放行后正常卸载
	if err := tree.Remove("1.0.0", func(string) error { return nil }); err != nil {
		t.Fatalf("空闲卸载失败: %v", err)
	}
}

func TestTreeStageDirExclusionAndTokens(t *testing.T) {
	root := filepath.Join(t.TempDir(), "versions")
	tree := OpenTree(root, "demo")

	s1, discard1, err := tree.StageDir("tx-dup")
	if err != nil {
		t.Fatal(err)
	}
	defer discard1()
	if filepath.Base(s1) != ".tmp-tx-dup" {
		t.Fatalf("staging 命名异常: %s", s1)
	}
	if _, _, err := tree.StageDir("tx-dup"); err == nil {
		t.Fatal("同名事务必须独占失败")
	}

	for _, bad := range []string{"", "../esc", "a/b", `a\b`} {
		if _, _, err := tree.StageDir(bad); err == nil {
			t.Fatalf("非法 txnID %q 必须被拒", bad)
		}
	}
	// 非法版本令牌注入 Commit
	s := stageContent(t, tree, "tx-ok", "f", "x")
	if err := tree.Commit(s, "../pwn", Meta{}); err == nil {
		t.Fatal("非法版本必须被拒")
	}
	if err := tree.Commit("missing-dir", "4.0.0", Meta{}); err == nil {
		t.Fatal("不存在 staging 必须被拒")
	}
	// 非法摘要形状
	s2 := stageContent(t, tree, "tx-sha", "f", "x")
	if err := tree.Commit(s2, "4.0.1", Meta{ZipSHA256: "not-hex"}); err == nil {
		t.Fatal("非法 ZipSHA256 必须被拒")
	}
}

func TestTreeVersionsIgnoresForeignAndResolveMissing(t *testing.T) {
	root := filepath.Join(t.TempDir(), "versions")
	tree := OpenTree(root, "demo")
	// 外来目录 / 其他族目录 / 未装版本查询
	for _, name := range []string{"other_1.0.0", "not-a-version-dir", "readme.txt"} {
		p := filepath.Join(root, name)
		var err error
		if name == "readme.txt" {
			err = os.WriteFile(p, []byte("x"), 0644)
		} else {
			err = os.MkdirAll(p, 0755)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	list, err := tree.Versions()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("扫描不得混入非本族条目: %+v", list)
	}
	if _, err := tree.Resolve(""); err == nil {
		t.Fatal("空树取最新应报错")
	}
	if _, err := tree.Resolve("3.3.3"); err == nil || !strings.Contains(err.Error(), "未安装") {
		t.Fatalf("未装版本解析异常: %v", err)
	}
}

func TestTreeCleanupAbandoned(t *testing.T) {
	root := filepath.Join(t.TempDir(), "versions")
	tree := OpenTree(root, "demo")
	s := stageContent(t, tree, "tx1", "app.exe", "x")
	if err := tree.Commit(s, "1.0.0", Meta{ZipSHA256: strings.Repeat("11", 32)}); err != nil {
		t.Fatal(err)
	}

	// 模拟崩溃遗留：孤儿 .tmp- 与 .removing- 目录
	orphanTmp := filepath.Join(root, ".tmp-crashed")
	orphanRemoving := filepath.Join(root, "demo_2.0.0.removing-123")
	for _, d := range []string{orphanTmp, orphanRemoving} {
		if err := os.MkdirAll(filepath.Join(d, "inner"), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "inner", "f"), []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	leftovers := tree.CleanupAbandoned()
	if len(leftovers) != 0 {
		t.Fatalf("应全部清理，实际残留: %v", leftovers)
	}
	for _, d := range []string{orphanTmp, orphanRemoving} {
		if _, err := os.Stat(d); !os.IsNotExist(err) {
			t.Fatalf("孤儿目录未清理: %s", d)
		}
	}
	// 正式版本不受波及
	if _, err := tree.Resolve("1.0.0"); err != nil {
		t.Fatalf("清理误伤正式版本: %v", err)
	}
}

func TestTreeInvalidEntryNameDefersError(t *testing.T) {
	root := filepath.Join(t.TempDir(), "versions")
	tree := OpenTree(root, `bad name/..`)
	if _, _, err := tree.StageDir("tx1"); err == nil {
		t.Fatal("非法入口名前缀必须让方法返回错误而非 panic")
	}
	if _, err := tree.Versions(); err == nil {
		t.Fatal("Versions 同样应报错")
	}
	if leftovers := tree.CleanupAbandoned(); leftovers != nil {
		t.Fatal("CleanupAbandoned 对坏树应静默返回空")
	}
}
