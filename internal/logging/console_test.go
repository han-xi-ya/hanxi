package logging

import (
	"bufio"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// failWriter 模拟 windowsgui 双击启动时无效句柄的 os.Stderr：写入必错。
type failWriter struct{}

func (failWriter) Write([]byte) (int, error) {
	return 0, errors.New("write /dev/stderr: The handle is invalid.")
}

var _ io.Writer = failWriter{}

func TestConsoleWriterSwallowsError(t *testing.T) {
	n, err := consoleWriter{failWriter{}}.Write([]byte("hello"))
	if err != nil {
		t.Fatalf("consoleWriter 应吞掉底层错误并谎报成功, got %v", err)
	}
	if n != len("hello") {
		t.Errorf("应谎报写入全部字节: got %d want %d", n, len("hello"))
	}
}

// 回归护栏：控制台写失败（GUI 子系统无 stderr）时，磁盘日志文件必须收全量记录。
// 修复前 io.MultiWriter(os.Stderr, f) 首路报错即中断，文件恒 0 字节。
func TestInitLoggerFileWritesSurviveBadConsole(t *testing.T) {
	old := consoleOut
	consoleOut = failWriter{}
	t.Cleanup(func() { consoleOut = old })

	dir := t.TempDir()
	logger, cleanup, err := InitLogger(dir, 7)
	if err != nil {
		t.Fatalf("InitLogger: %v", err)
	}

	logger.Info("first line")
	logger.Warn("second line")
	logger.Error("third line")
	cleanup()

	matches, err := filepath.Glob(filepath.Join(dir, "app-*.log"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("期望恰好生成一个当天日志文件, got %v (err=%v)", matches, err)
	}
	f, err := os.Open(matches[0])
	if err != nil {
		t.Fatalf("打开当天日志文件失败: %v", err)
	}
	defer f.Close()

	var lines []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if s := strings.TrimSpace(sc.Text()); s != "" {
			lines = append(lines, s)
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if len(lines) != 3 {
		t.Fatalf("文件应收到全部 3 条记录, got %d: %q", len(lines), lines)
	}
	for _, want := range []string{"first line", "second line", "third line"} {
		found := false
		for _, l := range lines {
			if strings.Contains(l, want) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("日志文件缺少记录 %q", want)
		}
	}
}
