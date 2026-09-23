package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"hanxi/internal/logging"
	"hanxi/internal/settings"
)

// tools_logs.go 是 N34 运行日志只读工具（hanxi_log_read）：问答式排障
// （"昨晚 frpc 怎么断的"）经 MCP 读 hanxi 自己的落盘日志。三条纪律：
//
//   - 只读 tail：按天文件 app-YYYY-MM-DD.log（logging.InitLogger 落盘形态），
//     窗口回读不整库吞入；os.Open 在 Windows 走共享读打开，与 GUI 写者句柄
//     互不干扰（N34 ④"不抢写者锁"）。
//   - 行级脱敏后才出机：日志行会带机器名/IP/路径级信息，落盘窄口径
//     （logging.Redact）不够——本工具逐行走 logging.RedactPII（Redact 全量 +
//     IPv4/邮箱/供应商前缀密钥），脱敏口径写进工具描述（模型知情）。
//   - 硬顶截断：输出行数上限 500（默认 100），窗口耗尽仍有更早历史时
//     truncated=true——"预算耗尽 ≠ 无结果"（PLAN_MCP 尺寸纪律同款）。

const (
	defaultLogLines = 100
	maxLogLines     = 500
)

// logDateRe 只认 InitLogger 的按天命名字符集，杜绝 "../config" 之类路径注入。
var logDateRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// logLevelRe 从 slog JSON 行提取级别（解析失败按无级别归入"仅 contains 过滤"档）。
var logLevelRe = regexp.MustCompile(`"level":"([A-Z]+)"`)

// LogTail 一次 tail 取数结果（原始行，未脱敏——脱敏是 handler 出机前的职责）。
type LogTail struct {
	File  string   // 实际读取的文件名
	Found bool     // 该日日志在位（缺档时 Dates 给指路）
	Lines []string // 命中过滤的原始行，时间正序，≤ MaxLines
	More  bool     // 窗口之前还有被截断的更早行（truncated 语义源）
	Dates []string // 目录内现存日志日期（缺档指引用）
}

// LogTailer 是 hanxi_log_read 的后端取数面（真 = logDiskReader；单测注入假件）。
type LogTailer interface {
	Tail(date string, maxLines int, level, contains string) (LogTail, error)
}

// logDiskReader 直读 <DataDir>/logs 下的按天日志（零落盘：只 Open 只读，不创建
// 目录/文件；logs 目录由 GUI 或 headless InitLogger 建立，缺目录=尚无日志）。
type logDiskReader struct {
	dir string
	now func() time.Time // 单测钉"今天"
}

func newLogDiskReader() *logDiskReader {
	return &logDiskReader{dir: settings.GetPaths().LogsDir(), now: time.Now}
}

func (r *logDiskReader) Tail(date string, maxLines int, level, contains string) (LogTail, error) {
	if date == "" {
		date = r.now().Format("2006-01-02")
	}
	if !logDateRe.MatchString(date) {
		return LogTail{}, fmt.Errorf("日期格式须为 YYYY-MM-DD，收到 %q", date)
	}
	res := LogTail{File: "app-" + date + ".log", Dates: r.dates()}
	path := filepath.Join(r.dir, res.File)
	lines, more, err := tailFile(path, maxLines, strings.ToUpper(level), strings.ToLower(contains))
	if os.IsNotExist(err) {
		return res, nil // 缺档合法态：Found=false，指路信息在 Dates
	}
	if err != nil {
		return LogTail{}, fmt.Errorf("日志尾部读取失败: %w", err)
	}
	res.Found = true
	res.Lines = lines
	res.More = more
	return res, nil
}

// dates 列目录内现存日志日期（升序）。目录缺失是合法态（返回空）。
func (r *logDiskReader) dates() []string {
	entries, err := os.ReadDir(r.dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(name, "app-") || !strings.HasSuffix(name, ".log") {
			continue
		}
		d := strings.TrimSuffix(strings.TrimPrefix(name, "app-"), ".log")
		if logDateRe.MatchString(d) {
			out = append(out, d)
		}
	}
	sort.Strings(out)
	return out
}

// tailFile 回读窗口法取 path 尾部过滤后的最后 maxLines 行：窗口从 256 KiB 起
// 4 倍扩张（上限 64 MiB，防病态巨档拖死无头进程），够数/触顶/到文件头即停。
// 返回 more=窗口预算内还有更早原始行被截走（含"过滤吃掉候选行"的诚实表达）。
func tailFile(path string, maxLines int, levelUpper, containsLower string) (lines []string, more bool, err error) {
	f, err := os.Open(path) // Windows 共享读打开，不抢 GUI 写者锁
	if err != nil {
		return nil, false, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, false, err
	}
	const (
		minWin int64 = 256 << 10
		maxWin int64 = 64 << 20
	)
	size := st.Size()
	for win := minWin; ; win *= 4 {
		start := size - win
		if start < 0 {
			start = 0
		}
		b := make([]byte, size-start)
		if _, rerr := f.ReadAt(b, start); rerr != nil {
			return nil, false, fmt.Errorf("读取日志尾部失败: %w", rerr)
		}
		kept, full := scanTailWindow(b, start == 0, maxLines, levelUpper, containsLower)
		if full || start == 0 || win >= maxWin {
			// full 且仍 !atStart：更早处必然还有内容（窗口外），截断如实为 true。
			return kept, full || start > 0, nil
		}
		// 未取满且没到文件头 → 窗口加倍再找更早的命中行。
	}
}

// scanTailWindow 在一个"截至文件尾"的缓冲上取最后 maxLines 条命中行。
// 完整性规则：末字节无 '\n' 时丢末段（写者可能正追加半行，不猜内容）；
// 未触到文件头（!atStart）时首段是截断的半行，同样丢。
// full=true 表示行数已取满且更早处还有非空原始行（或窗口外仍有内容）——
// 调用方据此置截断语义；full=false 且 atStart 才是"历史全量已给"。
func scanTailWindow(b []byte, atStart bool, maxLines int, levelUpper, containsLower string) (lines []string, full bool) {
	if len(b) == 0 {
		return nil, false
	}
	seg := strings.Split(string(b), "\n")
	if !atStart && len(seg) > 0 {
		seg = seg[1:] // 首段是截断的半行
	}
	if len(b) > 0 && b[len(b)-1] != '\n' && len(seg) > 0 {
		seg = seg[:len(seg)-1] // 末段是写者未终结的半行
	}
	var kept []string
	for i := len(seg) - 1; i >= 0; i-- {
		line := strings.TrimSpace(seg[i])
		if line == "" {
			continue
		}
		if levelUpper != "" {
			m := logLevelRe.FindStringSubmatch(line)
			if m == nil || m[1] != levelUpper {
				continue
			}
		}
		if containsLower != "" && !strings.Contains(strings.ToLower(line), containsLower) {
			continue
		}
		kept = append(kept, line)
		if len(kept) >= maxLines {
			older := !atStart // 窗口外必然还有更早内容
			for j := i - 1; j >= 0 && !older; j-- {
				older = strings.TrimSpace(seg[j]) != ""
			}
			return reverseStrings(kept), older
		}
	}
	return reverseStrings(kept), false
}

func reverseStrings(s []string) []string {
	out := make([]string, 0, len(s))
	for i := len(s) - 1; i >= 0; i-- {
		out = append(out, s[i])
	}
	return out
}

// logLinePayload 把一行 slog JSON 拆成模型友好的四字段（出机前逐字段 RedactPII）。
// 解析失败不丢行（排障现场坏行本身是线索），降级为整行进 msg + parse:"raw" 标记。
func logLinePayload(line string) resultPayload {
	var doc map[string]json.RawMessage
	if err := json.Unmarshal([]byte(line), &doc); err != nil {
		return resultPayload{"msg": logging.RedactPII(line), "parse": "raw"}
	}
	p := resultPayload{}
	pick := func(dst, src string) {
		var s string
		if v, ok := doc[src]; ok && json.Unmarshal(v, &s) == nil && s != "" {
			p[dst] = logging.RedactPII(s)
			delete(doc, src)
		}
	}
	pick("time", "time")
	pick("level", "level")
	pick("msg", "msg")
	if len(doc) > 0 {
		if rest, err := json.Marshal(doc); err == nil {
			p["ctx"] = logging.RedactPII(string(rest))
		}
	}
	return p
}

// buildLogsTool 运行日志只读 tail（N34）。门禁注记：本工具背后没有业务模块，
// access.json 的 logs 键即唯一授权门（registryGate 对 "logs" 走空操作通道）。
func buildLogsTool(deps Deps) (mcp.Tool, server.ToolHandlerFunc) {
	tool := mcp.NewTool(toolLogs,
		mcp.WithDescription("按日期 tail hanxi 自身运行日志（只读 app-YYYY-MM-DD.log，不改变任何文件），"+
			"支持级别与关键字过滤，时间正序返回，适合『昨晚 frpc 怎么断的』式排障问答。"+
			"脱敏口径：每行走敏感信息打码（token/password/Bearer + IPv4→[ipv4] + 邮箱→[email] + "+
			"sk-/ghp_ 类前缀密钥→[redacted-key]），四段点分版本号会被 IPv4 规则一并打码；"+
			"落盘日志本身按更窄口径脱敏，未打码内容不会经本工具出机。"+
			"注意：级别只有 INFO/WARN/ERROR（DEBUG 不落盘）；行数硬顶 500，更早历史被截时 truncated=true。"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithString("date", mcp.Description("日志日期 YYYY-MM-DD（留空=今天；hanxi 按保留天数滚动清理，过期日期可能已不存在）")),
		mcp.WithString("level", mcp.Description("级别过滤 INFO/WARN/ERROR（大小写不敏感，留空=全部）")),
		mcp.WithString("contains", mcp.Description("子串关键字过滤（匹配整行 JSON，模块名/错误词均可，忽略大小写）")),
		mcp.WithNumber("lines", mcp.Description("返回行数上限（1-500，默认 100）")),
	)
	handler := func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if deps.Logs == nil {
			return mcp.NewToolResultError("logs 后端未装配：请确认以 hanxi mcp 无头模式运行本服务"), nil
		}
		lines := req.GetInt("lines", defaultLogLines)
		if lines < 1 || lines > maxLogLines {
			lines = maxLogLines
		}
		tail, err := deps.Logs.Tail(
			strings.TrimSpace(req.GetString("date", "")),
			lines,
			strings.TrimSpace(req.GetString("level", "")),
			strings.TrimSpace(req.GetString("contains", "")),
		)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("日志读取失败: %v", err)), nil
		}
		if !tail.Found {
			guide := "该日期没有日志文件（hanxi 按保留天数清理旧日志）"
			if len(tail.Dates) > 0 {
				guide += "；现存日志日期：" + strings.Join(tail.Dates, ", ")
			}
			return mcp.NewToolResultError(guide), nil
		}
		items := make([]any, 0, len(tail.Lines))
		for _, l := range tail.Lines {
			items = append(items, logLinePayload(l))
		}
		res, rerr := listResult(items, tail.More)
		if rerr != nil {
			return res, rerr
		}
		return res, nil
	}
	return tool, handler
}
