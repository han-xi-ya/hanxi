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

// buildMemoTool 便签只读检索（决策 2 落点：整库总开关 + IsMasked 条目不下发——
// 遮罩条目整条不进结果，连标题与命中事实都不外泄；非遮罩条目正文再过 Redact）。
func buildMemoTool(deps Deps) (mcp.Tool, server.ToolHandlerFunc) {
	tool := mcp.NewTool(toolMemo,
		mcp.WithDescription("按关键词/标签检索 hanxi「极客随手记」本地便签库（只读，不改动任何条目）。"+
			"安全约定：标记为敏感遮罩（IsMasked，通常为 API Key/Token 类条目）的便签整条不下发，"+
			"不会出现在结果中（命中数因此可能少于库内实况）；其余条目的标题/正文也经过敏感串脱敏。"+
			"keyword 为空时按更新时间倒序返回最近条目。"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithString("keyword", mcp.Description("关键词（子串匹配标题/正文/标签，忽略大小写；留空=列最近）")),
		mcp.WithString("tag", mcp.Description("精确标签过滤（如 #SQL，# 前缀可选）")),
		mcp.WithNumber("limit", mcp.Description("返回条数上限（1-300，默认 50）")),
	)
	handler := func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if deps.Memo == nil {
			return mcp.NewToolResultError("memo 后端未装配：请确认 hanxi 已启用「极客随手记」模块"), nil
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
