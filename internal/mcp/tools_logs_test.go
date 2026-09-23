package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ---------- hanxi_log_read（N34） ----------

type fakeLogTailer struct {
	tail  LogTail
	err   error
	calls int

	lastDate     string
	lastMax      int
	lastLevel    string
	lastContains string
}

func (f *fakeLogTailer) Tail(date string, maxLines int, level, contains string) (LogTail, error) {
	f.calls++
	f.lastDate, f.lastMax, f.lastLevel, f.lastContains = date, maxLines, level, contains
	return f.tail, f.err
}

func slogLine(ts, level, msg string, kv ...string) string {
	parts := []string{fmt.Sprintf(`"time":"%s"`, ts), fmt.Sprintf(`"level":"%s"`, level), fmt.Sprintf(`"msg":"%s"`, msg)}
	for i := 0; i+1 < len(kv); i += 2 {
		parts = append(parts, fmt.Sprintf(`"%s":"%s"`, kv[i], kv[i+1]))
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func logSetup(t *testing.T, tailer LogTailer) (deps Deps) {
	t.Helper()
	d, access, _ := newTestServer(t)
	grant(t, access, map[string]bool{"logs": true})
	d.Logs = tailer
	return d
}

func TestLogTailParsesAndRedacts(t *testing.T) {
	ft := &fakeLogTailer{tail: LogTail{
		File:  "app-2026-09-24.log",
		Found: true,
		Lines: []string{
			slogLine("2026-09-24T10:00:00+08:00", "INFO", "frpc starting password=hunter2"),
			slogLine("2026-09-24T10:00:05+08:00", "ERROR", "dial 192.168.10.7:7000 refused", "module", "frpc", "user", "zhang.san@example.com"),
			"这一行不是 JSON（半截坏行也要出机，降级 raw）",
		},
		More: true,
	}}
	c := inProcClient(t, logSetup(t, ft))
	res, text := callText(t, c, toolLogs, nil)
	if res.IsError {
		t.Fatalf("默认查询应成功: %s", text)
	}
	var env struct {
		Count     int `json:"count"`
		Truncated bool
		Results   []map[string]any
	}
	if err := json.Unmarshal([]byte(text), &env); err != nil {
		t.Fatalf("输出须为合法 JSON 信封: %v", err)
	}
	if env.Count != 3 || !env.Truncated {
		t.Fatalf("信封字段错: %s", text)
	}
	// 敏感串绝不出机（Redact 窄口径 + PII 层联合回归锚）
	for _, banned := range []string{"hunter2", "192.168.10.7", "zhang.san@example.com"} {
		if strings.Contains(text, banned) {
			t.Errorf("敏感串 %q 泄露进模型可见输出: %s", banned, text)
		}
	}
	for _, want := range []string{"[ipv4]:7000", "[email]", `password=\"******\"`, "10:00:05", "ERROR", "dial"} {
		if !strings.Contains(text, want) {
			t.Errorf("打码后应保行结构（缺 %q）: %s", want, text)
		}
	}
	// 结构化拆分：前两条有 time/level/msg，坏行走 raw 降级
	if env.Results[0]["level"] != "INFO" || env.Results[1]["level"] != "ERROR" {
		t.Errorf("级别应拆出独立字段: %+v", env.Results)
	}
	if env.Results[1]["ctx"] != `{"module":"frpc","user":"[email]"}` {
		t.Errorf("附加属性应归入 ctx 并脱敏: %v", env.Results[1]["ctx"])
	}
	if env.Results[2]["parse"] != "raw" {
		t.Errorf("非 JSON 行须 raw 降级: %+v", env.Results[2])
	}
	// 参数缺省口径：100 行、无过滤、日期透传空（后端补今天）
	if ft.calls != 1 || ft.lastMax != 100 || ft.lastLevel != "" || ft.lastContains != "" || ft.lastDate != "" {
		t.Fatalf("缺省参数口径错: %+v", ft)
	}
}

func TestLogTailParamsNormalized(t *testing.T) {
	ft := &fakeLogTailer{tail: LogTail{File: "app-2026-09-23.log", Found: true}}
	c := inProcClient(t, logSetup(t, ft))
	callText(t, c, toolLogs, map[string]any{
		"date": "2026-09-23", "level": "warn", "contains": "FrPc", "lines": float64(9999),
	})
	if ft.lastDate != "2026-09-23" || ft.lastLevel != "warn" || ft.lastContains != "FrPc" {
		t.Fatalf("handler 应原样透传（大小写归一由 disk reader 内部完成）: %+v", ft)
	}
	if ft.lastMax != maxLogLines {
		t.Fatalf("lines 超限须硬顶到 %d，得 %d", maxLogLines, ft.lastMax)
	}
}

func TestLogTailMissingDateGuides(t *testing.T) {
	ft := &fakeLogTailer{tail: LogTail{File: "app-2020-01-01.log", Dates: []string{"2026-09-22", "2026-09-24"}}}
	c := inProcClient(t, logSetup(t, ft))
	res, text := callText(t, c, toolLogs, map[string]any{"date": "2020-01-01"})
	if !res.IsError || !strings.Contains(text, "2026-09-24") || !strings.Contains(text, "没有日志文件") {
		t.Fatalf("缺档应指引现存日期: %s", text)
	}
}

func TestLogDescriptionHonesty(t *testing.T) {
	tool, _ := buildLogsTool(Deps{})
	for _, phrase := range []string{"[ipv4]", "只读", "500", "INFO/WARN/ERROR"} {
		if !strings.Contains(tool.Description, phrase) {
			t.Errorf("description must contain %q: %s", phrase, tool.Description)
		}
	}
}

// ---------- logDiskReader / tailFile 真档面 ----------

func writeLogDir(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestLogDiskReaderTailBasics(t *testing.T) {
	var lines []string
	for i := 1; i <= 300; i++ {
		lvl := "INFO"
		if i%100 == 0 {
			lvl = "ERROR"
		}
		lines = append(lines, slogLine(fmt.Sprintf("2026-09-24T00:%02d:%02d+08:00", i/60, i%60), lvl, fmt.Sprintf("event %d", i)))
	}
	dir := writeLogDir(t, map[string]string{
		"app-2026-09-24.log": strings.Join(lines, "\n") + "\n",
		"app-2026-09-23.log": "旧档\n",
		"stray.txt":          "无关文件",
	})
	r := &logDiskReader{dir: dir, now: func() time.Time { return time.Date(2026, 9, 24, 12, 0, 0, 0, time.Local) }}

	// 缺省取今天、尾部 5 行、时间正序
	tail, err := r.Tail("", 5, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if !tail.Found || tail.File != "app-2026-09-24.log" || len(tail.Lines) != 5 {
		t.Fatalf("缺省 tail 错: %+v", tail)
	}
	if !strings.Contains(tail.Lines[4], "event 300") || !strings.Contains(tail.Lines[0], "event 296") {
		t.Fatalf("尾部窗口/正序错: %v", tail.Lines)
	}
	if !tail.More {
		t.Error("更早历史在场必须 truncated")
	}
	if strings.Join(tail.Dates, ",") != "2026-09-23,2026-09-24" {
		t.Errorf("Dates 应列 app-*.log 日期升序且忽略杂项: %v", tail.Dates)
	}

	// 级别过滤 + 满量截断
	tail, _ = r.Tail("2026-09-24", 500, "ERROR", "")
	if len(tail.Lines) != 3 {
		t.Fatalf("ERROR 过滤错: %d 条", len(tail.Lines))
	}
	if tail.More {
		t.Error("到文件头取尽不得置截断")
	}

	// contains 过滤（大小写不敏感）
	tail, _ = r.Tail("2026-09-24", 10, "", "EVENT 7")
	if len(tail.Lines) != 10 || !strings.Contains(tail.Lines[9], "event 79") {
		t.Fatalf("contains 过滤错: %v", tail.Lines)
	}
	if !tail.More {
		t.Error("过滤截断（100-7x/100-8x 更早）须如实置 truncated")
	}

	// 缺档日期：Found=false + 指路
	tail, err = r.Tail("2026-09-01", 10, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if tail.Found || len(tail.Dates) != 2 {
		t.Fatalf("缺档形态错: %+v", tail)
	}

	// 非法日期拒收（路径注入面钉死）
	if _, err := r.Tail("../../config", 10, "", ""); err == nil || !strings.Contains(err.Error(), "YYYY-MM-DD") {
		t.Fatalf("非法日期必须拒绝: %v", err)
	}
}

func TestLogDiskReaderEdgeShapes(t *testing.T) {
	// 空文件 / 无末换行半行 / 纯半行文件
	dir := writeLogDir(t, map[string]string{
		"app-2026-09-24.log": "partial trailing line without newline",
		"app-2026-09-23.log": "",
	})
	r := &logDiskReader{dir: dir, now: time.Now}
	tail, err := r.Tail("2026-09-24", 10, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if !tail.Found || len(tail.Lines) != 0 || tail.More {
		t.Fatalf("未终结半行不得出机: %+v", tail)
	}
	tail, _ = r.Tail("2026-09-23", 10, "", "")
	if !tail.Found || len(tail.Lines) != 0 {
		t.Fatalf("空档合法: %+v", tail)
	}
}

func TestTailFileWindowGrowth(t *testing.T) {
	// 窗口扩张回归：needle 埋在文件头，之后灌超过初始窗口（256 KiB）的 filler——
	// 单窗取不到命中行，必须加倍扩张到全文件才收口，且不得误报截断。
	dir := writeLogDir(t, nil)
	path := filepath.Join(dir, "app.log")
	var sb strings.Builder
	sb.WriteString(slogLine("2026-09-24T00:00:01+08:00", "INFO", "needle at the top") + "\n")
	filler := slogLine("2026-09-24T00:00:02+08:00", "INFO", strings.Repeat("x", 200))
	for sb.Len() < 400<<10 {
		sb.WriteString(filler + "\n")
	}
	sb.WriteString(slogLine("2026-09-24T00:00:03+08:00", "INFO", "needle #2") + "\n")
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	lines, more, err := tailFile(path, 2, "", "needle")
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 2 || !strings.Contains(lines[0], "needle at the top") || !strings.Contains(lines[1], "needle #2") {
		t.Fatalf("跨窗口收集错: %d 条 %v", len(lines), lines)
	}
	if more {
		t.Error("窗口已扩至文件头且两条命中全部送达——历史全量已给，不得虚报截断")
	}
}
