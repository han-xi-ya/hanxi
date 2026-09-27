package logging

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// rotate_test.go 锁死 dayRotateWriter 的按天轮转契约（修复"启动定名"缺陷）：
// 跨天换档落新文件、换档触发 prune、并发写 race 干净、Close 后不复活文件句柄。
// 时钟一律经 initLogger 的 now 注入（仓内 fake clock 惯例），全部用例零 sleep。

// fakeClock 可注入时钟：读写各自加锁，供并发用例在写负载中途拨表而不触 race。
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = t
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

// muteConsole 把 consoleOut 换成黑洞，避免测试日志刷爆 go test 输出。
func muteConsole(t *testing.T) {
	t.Helper()
	old := consoleOut
	consoleOut = io.Discard
	t.Cleanup(func() { consoleOut = old })
}

// fileLines 读取 dir 下 name 的全部非空行；文件不存在返回 nil。
func fileLines(t *testing.T, dir, name string) []string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("读取 %s 失败: %v", name, err)
	}
	var out []string
	for _, l := range strings.Split(string(b), "\n") {
		if s := strings.TrimSpace(l); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// logFileNames 列 dir 下 app-*.log 文件名（排序后返回，断言可比对）。
func logFileNames(t *testing.T, dir string) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, "app-*.log"))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, m := range matches {
		names = append(names, filepath.Base(m))
	}
	sort.Strings(names)
	return names
}

// TestDayRolloverWritesNewFile 主用例：注入时钟跨过午夜后，新记录必须落进
// 新一天的 app-YYYY-MM-DD.log，旧文件不得再被追加——hanxi_log_read 的
// "按日期 tail 当天文件"契约的成立前提。
func TestDayRolloverWritesNewFile(t *testing.T) {
	muteConsole(t)
	clock := &fakeClock{t: time.Date(2026, 9, 26, 23, 58, 0, 0, time.Local)}

	dir := t.TempDir()
	logger, cleanup, err := initLogger(dir, 7, clock.Now)
	if err != nil {
		t.Fatalf("initLogger: %v", err)
	}

	logger.Info("before-midnight")
	clock.Set(time.Date(2026, 9, 27, 0, 1, 0, 0, time.Local))
	logger.Info("after-midnight")
	cleanup()

	want := []string{"app-2026-09-26.log", "app-2026-09-27.log"}
	if got := logFileNames(t, dir); !equalStrings(got, want) {
		t.Fatalf("期望恰好生成两天文件 %v, got %v", want, got)
	}
	d26 := strings.Join(fileLines(t, dir, "app-2026-09-26.log"), "\n")
	d27 := strings.Join(fileLines(t, dir, "app-2026-09-27.log"), "\n")
	if !strings.Contains(d26, "before-midnight") {
		t.Errorf("当天记录应留在 09-26 文件, got %q", d26)
	}
	if strings.Contains(d26, "after-midnight") {
		t.Errorf("跨天记录不得续写启动当天文件, got %q", d26)
	}
	if !strings.Contains(d27, "after-midnight") {
		t.Errorf("跨天记录应落进 09-27 文件, got %q", d27)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestRolloverTriggersPrune retainDays 清理此前只在启动跑一次，常驻进程存活期
// 内形同虚设；现在跨转换档必须顺带 prune 一次。过期文件在 init 之后才布置，
// 确保被删是轮转触发而非启动清理的功劳。
func TestRolloverTriggersPrune(t *testing.T) {
	muteConsole(t)
	clock := &fakeClock{t: time.Date(2026, 9, 26, 23, 0, 0, 0, time.Local)}

	dir := t.TempDir()
	logger, cleanup, err := initLogger(dir, 7, clock.Now)
	if err != nil {
		t.Fatalf("initLogger: %v", err)
	}

	writeLog(t, dir, "app-2020-01-01.log", 30) // 启动清理之后布置的过期档
	if _, err := os.Stat(filepath.Join(dir, "app-2020-01-01.log")); err != nil {
		t.Fatalf("布置过期档失败: %v", err)
	}

	logger.Info("line-day26")
	clock.Set(time.Date(2026, 9, 27, 0, 0, 1, 0, time.Local))
	logger.Info("line-day27") // 这次写入触发换档 + prune

	if _, err := os.Stat(filepath.Join(dir, "app-2020-01-01.log")); !os.IsNotExist(err) {
		t.Errorf("轮转应顺带清理过期日志, stat err=%v", err)
	}
	cleanup()
}

// TestConcurrentWritesAcrossRollover 多 goroutine 并发写 + 中途拨表换天：
// go test -race 必须干净（轮转/写入/cleanup 统一持锁），且记录一条不少、
// 一条不多地分布在换档前后的两个文件里。
func TestConcurrentWritesAcrossRollover(t *testing.T) {
	muteConsole(t)
	clock := &fakeClock{t: time.Date(2026, 9, 26, 23, 59, 59, 0, time.Local)}

	dir := t.TempDir()
	logger, cleanup, err := initLogger(dir, 7, clock.Now)
	if err != nil {
		t.Fatalf("initLogger: %v", err)
	}

	const goroutines, perG = 8, 50
	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < perG; i++ {
				// g0 写到中段把时钟拨过午夜，制造写入与换档的真并发窗口
				if g == 0 && i == perG/2 {
					clock.Set(time.Date(2026, 9, 27, 0, 0, 1, 0, time.Local))
				}
				logger.Info(fmt.Sprintf("g%d-l%d", g, i))
			}
		}(g)
	}
	wg.Wait()
	cleanup()

	total := len(fileLines(t, dir, "app-2026-09-26.log")) + len(fileLines(t, dir, "app-2026-09-27.log"))
	if want := goroutines * perG; total != want {
		t.Errorf("并发写+轮转不得丢行/重行: got %d want %d", total, want)
	}
	// 每行恰好出现一次（JSON handler 串行化下不应交错撕裂，粗验行数即可）
	if len(logFileNames(t, dir)) != 2 {
		t.Errorf("期望两天文件恰好 2 个, got %v", logFileNames(t, dir))
	}
}

// TestCleanupNoReopenAfterClose 退出语义：cleanup 之后即使再次跨天也不得复活
// 新文件句柄（防止关后写把半死的全局 logger 拽回磁盘、与 app 退出竞态）。
func TestCleanupNoReopenAfterClose(t *testing.T) {
	muteConsole(t)
	clock := &fakeClock{t: time.Date(2026, 9, 26, 23, 0, 0, 0, time.Local)}

	dir := t.TempDir()
	logger, cleanup, err := initLogger(dir, 7, clock.Now)
	if err != nil {
		t.Fatalf("initLogger: %v", err)
	}
	logger.Info("before-close")
	cleanup()

	clock.Set(time.Date(2026, 9, 27, 0, 0, 0, 0, time.Local))
	logger.Info("after-close") // handler 对已关句柄写报错即静默丢弃，与修复前语义一致

	if got := logFileNames(t, dir); len(got) != 1 || got[0] != "app-2026-09-26.log" {
		t.Errorf("cleanup 后不得再开新档, got %v", got)
	}
	if strings.Contains(strings.Join(fileLines(t, dir, "app-2026-09-26.log"), "\n"), "after-close") {
		t.Errorf("cleanup 后记录不得写回旧档")
	}
}
