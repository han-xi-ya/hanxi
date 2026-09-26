package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"hanxi/internal/jsonstore"
	"hanxi/internal/logging"
	"hanxi/internal/modules/memo"
	"hanxi/internal/settings"
)

// maxMemoLimit 单次返回条目上限（沿用"单结果 ≤300 条"红线的量级）。
const maxMemoLimit = 300

// maxMemoTags 统计工具标签云条数上限（标签基数远小于条目数，200 足够覆盖
// 个人便签库实况；超限按计数降序保留头部并置 tagsTruncated）。
const maxMemoTags = 200

// memoFileNameRe 与 memo 包 filestore.go 同名同规（那里未导出，此处复制并保持同步）：
// 便签 id 仅安全字符集，防目录里混入的无关 md 被误读。
var memoFileNameRe = regexp.MustCompile(`^[A-Za-z0-9_.-]+\.md$`)

// MemoSource 是 hanxi_memo_search 的后端取数面（真 = memoDiskReader；单测注入假件）。
type MemoSource interface {
	Load() ([]memo.MemoItem, error)
}

// memoDiskReader 零落盘直读通道（包注释决策 3 的 memo 落点）：
// 不复用 memo.MemoService——其构造携带旧库迁移/隔离改名写盘副作用（sweepStaleStaging、
// migrateMemoToFiles、LoadAll 坏条隔离），无头进程一概不碰。
//
// 读策略：文件库 <DataDir>/memo/<id>.md 优先（F3-b 后的权威形态）；库目录不存在或
// 无有效条目时回落旧整库 <StateDir>/memo.json（只读 Load，不创建不隔离——迁移是
// GUI 的职责，回落覆盖"迁移未跑"的过渡态）。解析失败的单条只跳过并告警，不连坐。
type memoDiskReader struct {
	dir        string
	legacyPath string
}

func newMemoDiskReader() *memoDiskReader {
	paths := settings.GetPaths()
	return &memoDiskReader{
		dir:        filepath.Join(paths.DataDir(), "memo"),
		legacyPath: filepath.Join(paths.StateDir(), "memo.json"),
	}
}

func (r *memoDiskReader) Load() ([]memo.MemoItem, error) {
	items, hasFiles, err := r.loadDir()
	if err != nil {
		return nil, err
	}
	if hasFiles {
		return items, nil
	}
	var legacy []memo.MemoItem
	ok, lerr := jsonstore.Load(r.legacyPath, &legacy)
	if lerr != nil {
		// 损坏旧库 fail-loud：修复（隔离取证副本）是 GUI 通道的职责，无头只报不修。
		return nil, fmt.Errorf("便签旧库读取失败（请在 hanxi 主程序中完成修复/迁移）: %w", lerr)
	}
	if !ok {
		return []memo.MemoItem{}, nil // 两态皆无 = 真空库
	}
	return legacy, nil
}

// loadDir 扫描文件库。hasFiles=目录里存在过候选 .md（无论解析成败），用于区分
// "空库"与"全部解析失败"，避免后者误触发旧库回落读出陈旧数据。
func (r *memoDiskReader) loadDir() ([]memo.MemoItem, bool, error) {
	entries, err := os.ReadDir(r.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("便签文件库目录不可读: %w", err)
	}
	var out []memo.MemoItem
	var hasFiles bool
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") || !memoFileNameRe.MatchString(e.Name()) {
			continue
		}
		hasFiles = true
		data, rerr := os.ReadFile(filepath.Join(r.dir, e.Name()))
		if rerr != nil {
			slog.Warn("mcp memo: read failed, item skipped", "file", e.Name(), "err", rerr)
			continue
		}
		item, derr := memo.DecodeMemo(e.Name(), data)
		if derr != nil {
			// 只跳过不隔离（零落盘纪律）；GUI 下次装载会正常隔离取证。
			slog.Warn("mcp memo: decode failed, item skipped", "file", e.Name(), "err", derr)
			continue
		}
		out = append(out, item)
	}
	return out, hasFiles, nil
}

// parseMemoTimeBound 解析 since/until 时间窗参数：接受 YYYY-MM-DD 与 RFC3339
// 两种形态（与 log 工具日期口径同谱，模型两种都会填）。纯日期按本机时区解释
// （便签时间戳是 GUI 在本机写入的墙钟时间，"上周三记的"按机主历法对齐）；
// RFC3339 自带偏移量，按字面时刻。since 取当日零点、until 取当日末尾（含全天）。
// 格式非法 fail-loud 报指引——静默忽略参数比报错更糟（模型会误信"已按时间窗过滤"）。
func parseMemoTimeBound(raw string, isUntil bool) (time.Time, bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false, nil
	}
	if d, err := time.ParseInLocation("2006-01-02", raw, time.Local); err == nil {
		if isUntil {
			return d.AddDate(0, 0, 1).Add(-time.Nanosecond), true, nil
		}
		return d, true, nil
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t, true, nil
	}
	return time.Time{}, false, fmt.Errorf("时间参数 %q 格式非法：须为 YYYY-MM-DD 或 RFC3339（如 2026-09-01T00:00:00Z）", raw)
}

// memoInWindow 更新时间窗判定（两侧闭区间，边界未设侧恒通过）。
func memoInWindow(it memo.MemoItem, since, until time.Time, hasSince, hasUntil bool) bool {
	if hasSince && it.UpdatedAt.Before(since) {
		return false
	}
	if hasUntil && it.UpdatedAt.After(until) {
		return false
	}
	return true
}

// buildMemoTool 便签只读检索（决策 2 落点：整库总开关 + IsMasked 条目不下发——
// 遮罩条目整条不进结果，连标题与命中事实都不外泄；非遮罩条目正文再过 Redact）。
// N16 C 批深化：since/until 更新时间窗参数（"上周记的那条 SQL 在哪"式找回）。
func buildMemoTool(deps Deps) (mcp.Tool, server.ToolHandlerFunc) {
	tool := mcp.NewTool(toolMemo,
		mcp.WithDescription("按关键词/标签/时间窗检索 hanxi「极客随手记」本地便签库（只读，不改动任何条目）。"+
			"安全约定：标记为敏感遮罩（IsMasked，通常为 API Key/Token 类条目）的便签整条不下发，"+
			"不会出现在结果中（命中数因此可能少于库内实况）；其余条目的标题/正文也经过敏感串脱敏。"+
			"keyword 为空时按更新时间倒序返回最近条目；since/until 按更新时间过滤，"+
			"接受 YYYY-MM-DD 或 RFC3339（纯日期时 until 含当日全天）。"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithString("keyword", mcp.Description("关键词（子串匹配标题/正文/标签，忽略大小写；留空=列最近）")),
		mcp.WithString("tag", mcp.Description("精确标签过滤（如 #SQL，# 前缀可选）")),
		mcp.WithString("since", mcp.Description("更新时间窗下界（YYYY-MM-DD 或 RFC3339，含当日零点/该时刻起）")),
		mcp.WithString("until", mcp.Description("更新时间窗上界（YYYY-MM-DD 或 RFC3339，纯日期含当日全天）")),
		mcp.WithNumber("limit", mcp.Description("返回条数上限（1-300，默认 50）")),
	)
	handler := func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if deps.Memo == nil {
			return mcp.NewToolResultError("memo 后端未装配：请确认 hanxi 已启用「极客随手记」模块"), nil
		}
		since, hasSince, serr := parseMemoTimeBound(req.GetString("since", ""), false)
		if serr != nil {
			return mcp.NewToolResultError(fmt.Sprintf("since 参数非法: %v", serr)), nil
		}
		until, hasUntil, uerr := parseMemoTimeBound(req.GetString("until", ""), true)
		if uerr != nil {
			return mcp.NewToolResultError(fmt.Sprintf("until 参数非法: %v", uerr)), nil
		}
		items, err := deps.Memo.Load()
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("读取便签库失败: %v", err)), nil
		}
		kw := strings.ToLower(strings.TrimSpace(req.GetString("keyword", "")))
		tag := strings.TrimSpace(req.GetString("tag", ""))
		limit := req.GetInt("limit", 50)
		if limit < 1 || limit > maxMemoLimit {
			limit = maxMemoLimit
		}

		var matched []memo.MemoItem
		for _, it := range items {
			if it.IsMasked {
				continue // 决策 2：遮罩条目不下发（整条剔除，非正文打码）
			}
			if !memoInWindow(it, since, until, hasSince, hasUntil) {
				continue
			}
			if tag != "" && !memoTagMatched(it.Tags, tag) {
				continue
			}
			if kw != "" && !memoKeywordHit(it, kw) {
				continue
			}
			matched = append(matched, it)
		}
		sort.SliceStable(matched, func(i, j int) bool {
			return matched[i].UpdatedAt.After(matched[j].UpdatedAt)
		})
		capped := len(matched) > limit
		if capped {
			matched = matched[:limit]
		}
		itemsOut := make([]any, 0, len(matched))
		for _, it := range matched {
			itemsOut = append(itemsOut, resultPayload{
				"id":        it.ID,
				"title":     logging.Redact(it.Title),
				"content":   logging.Redact(it.Content),
				"tags":      it.Tags,
				"isPinned":  it.IsPinned,
				"colorTag":  it.ColorTag,
				"createdAt": it.CreatedAt.Format(time.RFC3339),
				"updatedAt": it.UpdatedAt.Format(time.RFC3339),
			})
		}
		res, rerr := listResult(itemsOut, capped)
		if rerr != nil {
			return res, rerr
		}
		return res, nil
	}
	return tool, handler
}

// memoKeywordHit 与 MemoService.List 同谱的分词忽略版：整串小写子串匹配
// （title/content/任一 tag）。MCP 面保持简单口径，文档即口径。
func memoKeywordHit(it memo.MemoItem, kwLower string) bool {
	if strings.Contains(strings.ToLower(it.Title), kwLower) ||
		strings.Contains(strings.ToLower(it.Content), kwLower) {
		return true
	}
	for _, t := range it.Tags {
		if strings.Contains(strings.ToLower(t), kwLower) {
			return true
		}
	}
	return false
}

// memoTagMatched 精确标签（容忍 # 前缀差异），对齐 MemoService.List 的 tag 过滤语义。
func memoTagMatched(tags []string, want string) bool {
	trimWant := strings.TrimPrefix(want, "#")
	for _, t := range tags {
		if strings.EqualFold(t, want) ||
			strings.EqualFold(strings.TrimPrefix(t, "#"), trimWant) {
			return true
		}
	}
	return false
}

// buildMemoStatsTool 便签库只读统计（N16 C 批"用不上"痛点：AI 问答侧的
// "我的库有多大/都记了哪些主题/最近一次更新是什么时候"式概览，不逐条取数）。
//
// 遮罩纪律与本族一切工具同谱且在统计口径上更严：IsMasked 条目在聚合之前就
// 整条剔除——不计数、不进标签云、不影响时间范围，也不输出"遮罩条目数"这类
// 间接泄露字段（存在性本身即敏感）；载荷只有计数与标签，无标题无正文，
// 不存在正文出机面。授权与检索同键（access.json memo 键控整个便签工具族）。
func buildMemoStatsTool(deps Deps) (mcp.Tool, server.ToolHandlerFunc) {
	tool := mcp.NewTool(toolMemoStats,
		mcp.WithDescription("统计 hanxi「极客随手记」本地便签库概况（只读，零改动）："+
			"非遮罩条目总数、置顶数、标签云（计数降序，≤200 个）、最早/最近创建与更新时间。"+
			"支持 since/until 更新时间窗（YYYY-MM-DD 或 RFC3339）圈定「这段时间记了多少」。"+
			"安全约定：敏感遮罩（IsMasked）条目在计数与标签云之前即整条剔除，"+
			"统计口径仅覆盖非遮罩条目（库内实况可能更多）；本工具只返回聚合计数，不含任何标题/正文。"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithString("since", mcp.Description("更新时间窗下界（YYYY-MM-DD 或 RFC3339，留空=不限）")),
		mcp.WithString("until", mcp.Description("更新时间窗上界（YYYY-MM-DD 或 RFC3339，纯日期含当日全天，留空=不限）")),
	)
	handler := func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if deps.Memo == nil {
			return mcp.NewToolResultError("memo 后端未装配：请确认 hanxi 已启用「极客随手记」模块"), nil
		}
		since, hasSince, serr := parseMemoTimeBound(req.GetString("since", ""), false)
		if serr != nil {
			return mcp.NewToolResultError(fmt.Sprintf("since 参数非法: %v", serr)), nil
		}
		until, hasUntil, uerr := parseMemoTimeBound(req.GetString("until", ""), true)
		if uerr != nil {
			return mcp.NewToolResultError(fmt.Sprintf("until 参数非法: %v", uerr)), nil
		}
		items, err := deps.Memo.Load()
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("读取便签库失败: %v", err)), nil
		}

		var total, pinned int
		var firstCreated, lastCreated, oldestUpdated, newestUpdated time.Time
		tagCounts := map[string]int{}
		for _, it := range items {
			if it.IsMasked {
				continue // 聚合前整条剔除：遮罩条目的存在性也不经统计外泄
			}
			if !memoInWindow(it, since, until, hasSince, hasUntil) {
				continue
			}
			total++
			if it.IsPinned {
				pinned++
			}
			if firstCreated.IsZero() || it.CreatedAt.Before(firstCreated) {
				firstCreated = it.CreatedAt
			}
			if lastCreated.IsZero() || it.CreatedAt.After(lastCreated) {
				lastCreated = it.CreatedAt
			}
			if oldestUpdated.IsZero() || it.UpdatedAt.Before(oldestUpdated) {
				oldestUpdated = it.UpdatedAt
			}
			if newestUpdated.IsZero() || it.UpdatedAt.After(newestUpdated) {
				newestUpdated = it.UpdatedAt
			}
			for _, t := range it.Tags {
				// 归一化口径对齐 MemoService.GetStats：补 # 前缀、大小写敏感（不发明第二套语义）。
				cleaned := strings.TrimSpace(t)
				if cleaned == "" {
					continue
				}
				if !strings.HasPrefix(cleaned, "#") {
					cleaned = "#" + cleaned
				}
				tagCounts[cleaned]++
			}
		}

		// 标签云确定性输出：计数降序、同计数按标签名升序（Go map 遍历无序，
		// 不排序则同一库两次调用字节不同）。
		type tagStat struct {
			Tag   string `json:"tag"`
			Count int    `json:"count"`
		}
		tags := make([]tagStat, 0, len(tagCounts))
		for t, c := range tagCounts {
			tags = append(tags, tagStat{Tag: t, Count: c})
		}
		sort.Slice(tags, func(i, j int) bool {
			if tags[i].Count != tags[j].Count {
				return tags[i].Count > tags[j].Count
			}
			if li, lj := strings.ToLower(tags[i].Tag), strings.ToLower(tags[j].Tag); li != lj {
				return li < lj
			}
			return tags[i].Tag < tags[j].Tag // 大小写仅异的标签再按字节序，保证全序确定
		})
		tagsCapped := len(tags) > maxMemoTags
		if tagsCapped {
			tags = tags[:maxMemoTags]
		}

		payload := resultPayload{
			"totalCount":       total,
			"pinnedCount":      pinned,
			"distinctTags":     len(tagCounts),
			"tagsTruncated":    tagsCapped,
			"tagCloud":         tags,
			"timeWindowActive": hasSince || hasUntil,
		}
		strOf := func(t time.Time) string {
			if t.IsZero() {
				return ""
			}
			return t.Format(time.RFC3339)
		}
		payload["firstCreatedAt"] = strOf(firstCreated)
		payload["lastCreatedAt"] = strOf(lastCreated)
		payload["oldestUpdatedAt"] = strOf(oldestUpdated)
		payload["newestUpdatedAt"] = strOf(newestUpdated)
		return textResult(payload)
	}
	return tool, handler
}
