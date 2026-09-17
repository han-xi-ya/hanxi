package memo

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// 测试夹具目录形态：<tmp>/state/memo.json + <tmp>/memo/（与真实数据根布局一致，
// 暂存目录落 <tmp>/.memo-migrating-<pid>）。

func newLayout(t *testing.T) (dataDir, legacyPath, memoDir string) {
	t.Helper()
	dataDir = t.TempDir()
	state := filepath.Join(dataDir, "state")
	if err := os.MkdirAll(state, 0755); err != nil {
		t.Fatal(err)
	}
	return dataDir, filepath.Join(state, "memo.json"), filepath.Join(dataDir, "memo")
}

func seedLegacy(t *testing.T, legacyPath string, items []MemoItem) {
	t.Helper()
	st, err := NewStore(legacyPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Save(items); err != nil {
		t.Fatal(err)
	}
}

func TestMigrateHappyPath(t *testing.T) {
	dataDir, legacy, memoDir := newLayout(t)
	items := []MemoItem{sampleItem(), {ID: "memo_2", Title: "第二条", Content: "b", CreatedAt: sampleItem().UpdatedAt, UpdatedAt: sampleItem().UpdatedAt}}
	seedLegacy(t, legacy, items)

	need, err := memoNeedsMigration(legacy, memoDir)
	if err != nil || !need {
		t.Fatalf("判据应为需迁移: %v %v", need, err)
	}
	committed, err := migrateMemoToFiles(legacy, memoDir)
	if err != nil || !committed {
		t.Fatalf("migrate: committed=%v err=%v", committed, err)
	}

	// 提交点后：旧库改名留底、memo/ 出全条目、暂存已清
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Error("memo.json 应已被改名让位")
	}
	if _, err := os.Stat(legacy + migratedSuffix); err != nil {
		t.Errorf("留底缺失: %v", err)
	}
	fs := NewFileStore(memoDir)
	got, err := fs.LoadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("文件库条目 = %d", len(got))
	}
	staging := filepath.Join(dataDir, stagingPrefix+fmt.Sprint(os.Getpid()))
	if _, err := os.Stat(staging); !os.IsNotExist(err) {
		t.Error("暂存目录应已收掉")
	}

	// 判据幂等：迁移后不再触发；再调迁移无事可做
	need, _ = memoNeedsMigration(legacy, memoDir)
	if need {
		t.Fatal("二次判据仍要求迁移（不幂等）")
	}
	committed, err = migrateMemoToFiles(legacy, memoDir)
	if committed || err != nil {
		t.Fatalf("重入应 no-op: %v %v", committed, err)
	}
}

func TestMigrateAbortKeepsLegacyBytes(t *testing.T) {
	dataDir, legacy, memoDir := newLayout(t)
	items := []MemoItem{sampleItem(), {ID: "blocked", Title: "t"}}
	seedLegacy(t, legacy, items)
	before, err := os.ReadFile(legacy)
	if err != nil {
		t.Fatal(err)
	}

	// 构造第 3 步（暂存写文件）中途失败：把"blocked.md"预置成目录，rename 必炸
	staging := filepath.Join(dataDir, stagingPrefix+fmt.Sprint(os.Getpid()))
	if err := os.MkdirAll(filepath.Join(staging, "blocked.md"), 0755); err != nil {
		t.Fatal(err)
	}

	committed, err := migrateMemoWithLoader(legacy, memoDir, func() ([]MemoItem, error) {
		return items, nil
	})
	if committed || err == nil {
		t.Fatalf("应中止且未过提交点: committed=%v err=%v", committed, err)
	}
	// 半途中止后 memo.json 字节不动（PLAN 必含回退断言）
	after, rerr := os.ReadFile(legacy)
	if rerr != nil || string(after) != string(before) {
		t.Fatalf("旧库字节被改动: %v", rerr)
	}
	// 暂存被清、memo/ 无已提交文件
	if _, serr := os.Stat(staging); !os.IsNotExist(serr) {
		t.Error("暂存应被整体回退清理")
	}
	if has, _ := memoDirHasFiles(memoDir); has {
		t.Error("中止路径不得留下已提交文件")
	}

	// 重入跳过后再来一次真实迁移必须成功（判据幂等 + 失败可重试）
	need, _ := memoNeedsMigration(legacy, memoDir)
	if !need {
		t.Fatal("中止后判据应仍要求迁移")
	}
	committed, err = migrateMemoToFiles(legacy, memoDir)
	if !committed || err != nil {
		t.Fatalf("重试迁移应成功: %v %v", committed, err)
	}
}

func TestMigrateCrashAfterCommitResumes(t *testing.T) {
	dataDir, legacy, memoDir := newLayout(t)
	items := []MemoItem{sampleItem()}
	seedLegacy(t, legacy, items)

	// 构造"提交点已过、搬运失败"现场：memoDir 预置成文件 → MkdirAll 必炸
	if err := os.WriteFile(memoDir, []byte("not a dir"), 0644); err != nil {
		t.Fatal(err)
	}
	committed, err := migrateMemoToFiles(legacy, memoDir)
	if !committed || err == nil {
		t.Fatalf("应报 committed=true + 搬运失败: %v %v", committed, err)
	}
	if _, lerr := os.Stat(legacy); !os.IsNotExist(lerr) {
		t.Error("提交点已过：旧库应已让位")
	}

	// 修复现场后直接再走迁移：续跑分支收掉暂存
	if err := os.Remove(memoDir); err != nil {
		t.Fatal(err)
	}
	committed, err = migrateMemoToFiles(legacy, memoDir)
	if err != nil {
		t.Fatalf("续跑失败: %v", err)
	}
	got, lerr := NewFileStore(memoDir).LoadAll()
	if lerr != nil || len(got) != 1 || got[0].ID != sampleItem().ID {
		t.Fatalf("续跑结果 = %+v %v", got, lerr)
	}
	staging := filepath.Join(dataDir, stagingPrefix+fmt.Sprint(os.Getpid()))
	if _, serr := os.Stat(staging); !os.IsNotExist(serr) {
		t.Error("续跑完成后暂存应被收掉")
	}
}

func TestMigrateDivergencePrefersFiles(t *testing.T) {
	_, legacy, memoDir := newLayout(t)
	seedLegacy(t, legacy, []MemoItem{sampleItem()})
	if err := NewFileStore(memoDir).SaveItem(sampleItem()); err != nil {
		t.Fatal(err)
	}
	need, err := memoNeedsMigration(legacy, memoDir)
	if err != nil {
		t.Fatal(err)
	}
	if need {
		t.Fatal("文件库已提交时不得再迁移（以文件库为权威）")
	}
	if _, err := os.Stat(legacy); err != nil {
		t.Error("异常现场不得动旧库（留给人工比对）")
	}
}

func TestMigrateEmptyLegacyStillCommits(t *testing.T) {
	_, legacy, memoDir := newLayout(t)
	seedLegacy(t, legacy, []MemoItem{})
	committed, err := migrateMemoToFiles(legacy, memoDir)
	if !committed || err != nil {
		t.Fatalf("空旧库也要过提交点否则永远空转: %v %v", committed, err)
	}
	need, _ := memoNeedsMigration(legacy, memoDir)
	if need {
		t.Fatal("提交后判据应闭合")
	}
}

func TestSweepStaleStaging(t *testing.T) {
	t.Run("旧库在位删残骸", func(t *testing.T) {
		dataDir, legacy, memoDir := newLayout(t)
		seedLegacy(t, legacy, []MemoItem{sampleItem()})
		staging := filepath.Join(dataDir, stagingPrefix+"999999999")
		if err := os.MkdirAll(staging, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(staging, "x.md"), []byte("half"), 0644); err != nil {
			t.Fatal(err)
		}
		sweepStaleStaging(dataDir, memoDir, legacy)
		if _, err := os.Stat(staging); !os.IsNotExist(err) {
			t.Error("失败残骸应被清扫")
		}
		if _, err := os.Stat(legacy); err != nil {
			t.Error("旧库必须原样在位")
		}
	})
	t.Run("提交点后暂存续跑", func(t *testing.T) {
		dataDir, legacy, memoDir := newLayout(t)
		// 模拟上次进程：旧库已改名、暂存里有成品文件
		if err := os.MkdirAll(memoDir, 0755); err != nil {
			t.Fatal(err)
		}
		staging := filepath.Join(dataDir, stagingPrefix+"999999998")
		if err := os.MkdirAll(staging, 0755); err != nil {
			t.Fatal(err)
		}
		data, _ := EncodeMemo(sampleItem())
		if err := os.WriteFile(filepath.Join(staging, sampleItem().ID+".md"), data, 0644); err != nil {
			t.Fatal(err)
		}
		sweepStaleStaging(dataDir, memoDir, legacy)
		got, err := NewFileStore(memoDir).LoadAll()
		if err != nil || len(got) != 1 {
			t.Fatalf("续跑应收编暂存条目: %+v %v", got, err)
		}
		if _, err := os.Stat(staging); !os.IsNotExist(err) {
			t.Error("续跑完成后暂存应被收掉")
		}
	})
}
