package dirstats

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// mkTree 构造：<root>/a/x.txt(10B) a/y.txt(20B)、b/z.txt(30B)、top.txt(5B)、空目录 c/
func mkTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write := func(rel string, n int) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, make([]byte, n), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join("a", "x.txt"), 10)
	write(filepath.Join("a", "y.txt"), 20)
	write(filepath.Join("b", "z.txt"), 30)
	write("top.txt", 5)
	if err := os.MkdirAll(filepath.Join(root, "c"), 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestMeasureTotals(t *testing.T) {
	root := mkTree(t)
	s := Measure(root, Options{})
	if s.Err != nil {
		t.Fatal(s.Err)
	}
	if s.Bytes != 65 || s.Files != 4 {
		t.Fatalf("合计失真: %+v", s)
	}
	if s.Dirs < 4 { // root + a + b + c（Windows TempDir 解析可能并入更多层）
		t.Fatalf("目录计数异常: %+v", s)
	}
	if s.Partial || s.ErrorCount != 0 {
		t.Fatalf("全量遍历不应带截断/错误: %+v", s)
	}
}

func TestMeasureMissingAndFile(t *testing.T) {
	root := mkTree(t)
	if s := Measure(filepath.Join(root, "nope"), Options{}); !os.IsNotExist(s.Err) {
		t.Fatalf("缺失目录应报 Err: %+v", s.Err)
	}
	if s := Measure(filepath.Join(root, "top.txt"), Options{}); s.Err == nil {
		t.Fatal("文件入参应报根级错误")
	}
}

func TestMeasureSkipsSymlinks(t *testing.T) {
	root := mkTree(t)
	link := filepath.Join(root, "link-to-a")
	if err := os.Symlink(filepath.Join(root, "a"), link); err != nil {
		t.Skipf("平台不支持无特权符号链接: %v", err) // Windows CI 常态
	}
	s := Measure(root, Options{})
	if s.Bytes != 65 || s.Files != 4 {
		t.Fatalf("符号链接被双计: %+v", s)
	}
}

// TestMeasureBudgetTruncation 用"预取消 ctx"确定性地驱动截断路径（不赌调度器：
// WithTimeout(1ns) 的计时器唤醒可能晚于整场遍历，那类断言是伪装的可靠性）。
func TestMeasureBudgetTruncation(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "many")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	const total = 2000
	for i := 0; i < total; i++ {
		if err := os.WriteFile(filepath.Join(dir, "f"+tempName(i)), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s := measure(ctx, root, Options{CheckEvery: 1})
	if !s.Partial {
		t.Fatalf("已取消 ctx 必须在首检查点截断: %+v", s)
	}
	if s.Err != nil {
		t.Fatalf("预算截断不是错误，Err 应为空: %+v", s.Err)
	}
	if s.Files >= total {
		t.Fatalf("Partial 却走完全场（截断未生效）: %+v", s)
	}
}

func tempName(i int) string { return "n" + itoa(i) }

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}

func TestMeasureChildrenSortAndMix(t *testing.T) {
	root := mkTree(t)
	children, err := MeasureChildren(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]Child{}
	for _, c := range children {
		byName[c.Name] = c
	}
	if len(children) != 4 {
		t.Fatalf("一级子项数: %d", len(children))
	}
	if byName["a"].Bytes != 30 || byName["b"].Bytes != 30 || byName["c"].Bytes != 0 {
		t.Fatalf("子目录度量失真: %+v", children)
	}
	if byName["top.txt"].IsDir || byName["top.txt"].Bytes != 5 {
		t.Fatalf("顶层文件度量失真: %+v", byName["top.txt"])
	}
	// 降序：a(30) b(30) 并列按名序 → top.txt(5) → c(0，目录垫底)
	if children[0].Name != "a" || children[1].Name != "b" {
		t.Fatalf("并列段排序异常: %+v", children)
	}
	if children[2].Name != "top.txt" || children[3].Name != "c" {
		t.Fatalf("非零/零值段排序异常: %+v", children)
	}
}

// TestMeasureChildrenSharedDeadline 预取消 ctx 下：所有目录子项短路为 Partial、
// 文件子项照常计量、整场不报错——"一屏总览不因巨目录失踪"的确定性证明。
func TestMeasureChildrenSharedDeadline(t *testing.T) {
	root := mkTree(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	children, err := measureChildren(ctx, root, Options{CheckEvery: 1, MaxWorkers: 1})
	if err != nil {
		t.Fatal(err)
	}
	var dirs int
	for _, c := range children {
		if c.IsDir {
			dirs++
			if !c.Partial {
				t.Fatalf("预算耗尽后目录子项必须 Partial: %+v", c)
			}
		}
	}
	if dirs != 3 {
		t.Fatalf("目录子项数: %d", dirs)
	}
	if top := childNamed(t, children, "top.txt"); top.Bytes != 5 || top.Partial {
		t.Fatalf("文件子项不应受预算短路影响: %+v", top)
	}
}

func childNamed(t *testing.T, children []Child, name string) Child {
	t.Helper()
	for _, c := range children {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("缺少子项 %s（实际 %+v）", name, children)
	return Child{}
}

func TestMeasureChildrenUnreadableDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 目录 ACL 语义不同，chmod 不生效")
	}
	if os.Geteuid() == 0 {
		t.Skip("root 无视目录权限位（容器常态），chmod 造不出不可读场景")
	}
	root := t.TempDir()
	bad := filepath.Join(root, "locked")
	if err := os.Mkdir(bad, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(bad, "inner"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(bad, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(bad, 0o755) })

	s := Measure(root, Options{})
	if s.Err != nil {
		t.Fatalf("局部不可读不应升级为根级错误: %+v", s.Err)
	}
	if s.ErrorCount == 0 {
		t.Fatalf("不可读子目录必须计入 ErrorCount: %+v", s)
	}
}

func TestMeasureChildrenMissingRoot(t *testing.T) {
	if _, err := MeasureChildren(filepath.Join(t.TempDir(), "nope"), Options{}); !os.IsNotExist(err) {
		t.Fatalf("缺失根应直接报错: %v", err)
	}
	if _, err := MeasureChildren(mkTree(t), Options{}); err != nil {
		t.Fatalf("正常根不应报错: %v", err)
	}
}

// MeasureBudgeted：托管版本目录度量专用入口（budget<=0 不限时）。
// 小树下应给出精确全量（与 Measure 同账），非法根如实带 Err——调用方
// （各托管模块 dirSize 回退口径）依赖这两个基本保证。
func TestMeasureBudgeted(t *testing.T) {
	root := mkTree(t)
	want := Measure(root, Options{})
	got := MeasureBudgeted(root, 0)
	if got.Err != nil || got.Partial || got.Bytes != want.Bytes || got.Files != want.Files {
		t.Fatalf("budget=0 应与 Measure 同账: got %+v want %+v", got, want)
	}
	if got := MeasureBudgeted(root, time.Second); got.Err != nil || got.Partial || got.Bytes != want.Bytes {
		t.Fatalf("宽裕预算不应截断小树: %+v", got)
	}
	if got := MeasureBudgeted(filepath.Join(root, "nope"), time.Second); got.Err == nil {
		t.Fatalf("缺失根应报 Err: %+v", got)
	}
}
