package mcp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	evinstance "hanxi/internal/modules/everything/instance"
	evsearch "hanxi/internal/modules/everything/search"
	"hanxi/internal/platform"
	"hanxi/internal/settings"
)

// 严格只读档（PLAN_MCP §8 决策 3-B）两类前置缺失的指引错误（S2 随批落地：
// 文案如实说明 ES 依赖运行中实例与按需装机组件，绝不承诺代为启动/下载）。
var (
	errNoInstance = errors.New("Everything 未在运行。本工具严格只读，不会代为启动实例——" +
		"请先在 hanxi 主程序（或系统托盘）启动 Everything 后台实例后重试")
	errNoSearchTool = errors.New("Everything 搜索组件（es.exe）尚未安装。本工具不会代为下载组件——" +
		"请先在 hanxi 的 Everything 页面完成一次搜索（或安装搜索组件）后重试")
)

// Searcher 是 hanxi_file_search 的后端能力面（真 = strictSearcher；单测注入假件）。
type Searcher interface {
	// Search 严格只读：无实例/缺组件返回上面两类指引错误，绝不产生启动/下载副作用。
	Search(query string, limit int) ([]evsearch.Result, error)
}

// strictSearcher 独立于 EverythingService 的只读查询通道：自建探测引擎（纯查
// 进程在位状态，从不 Start）+ es.exe 在场校验。不复用 service.Search 正是为了
// 绕开它的两段编排副作用（懒拉起实例、缺组件联网下载）——决策 3-B 的落点。
//
// 状态探测与执行器都留了字段切点（snapshotFn/runFn），单测不碰真实数据根与进程。
type strictSearcher struct {
	engine     *evinstance.Engine
	esExe      string
	snapshotFn func() evinstance.State // nil = 走 engine 真实探测
	runFn      func(esExe, query string, limit int) ([]evsearch.Result, error)
}

func newStrictSearcher(plat platform.Platform) *strictSearcher {
	paths := settings.GetPaths()
	return &strictSearcher{
		engine: evinstance.NewEngine(plat.Job(), evinstance.NewEverythingProbe(), evinstance.Callbacks{}),
		// esExe 与 EverythingService.esDir 同谱（数据根/everything/es，版本无关）。
		esExe: evsearch.ESExePath(filepath.Join(paths.DataDir(), "everything", "es")),
	}
}

func (s *strictSearcher) state() evinstance.State {
	if s.snapshotFn != nil {
		return s.snapshotFn()
	}
	s.engine.RefreshExternal()
	return s.engine.Snapshot().State
}

func (s *strictSearcher) Search(query string, limit int) ([]evsearch.Result, error) {
	switch st := s.state(); st {
	case evinstance.StateRunning, evinstance.StateExternal:
		// 实例在场（自有托管或外部自启均可查——ES 只认默认实例）
	default:
		return nil, errNoInstance
	}
	if fi, err := os.Stat(s.esExe); err != nil || fi.IsDir() {
		return nil, errNoSearchTool
	}
	run := s.runFn
	if run == nil {
		run = evsearch.Search
	}
	return run(s.esExe, query, limit)
}

// maxSearchLimit 单次搜索上限（沿用 everything service 的 searchResultLimit=300）。
const maxSearchLimit = 300

// buildEverythingTool 全盘文件搜索（只读）。
func buildEverythingTool(deps Deps) (mcp.Tool, server.ToolHandlerFunc) {
	tool := mcp.NewTool(toolSearch,
		mcp.WithDescription("按关键词搜索本机全部磁盘的文件/文件夹（经 Everything 索引，严格只读，返回路径/大小/修改时间）。"+
			"前置条件：① 有正在运行的 Everything 实例（本工具绝不代为启动，缺失时报错给指引）；"+
			"② hanxi 的 es.exe 搜索组件已安装（缺失时同样只报错指引，绝不代为下载）。"+
			"支持 Everything 查询语法（如 ext:go、大小/日期过滤）；单次上限 300 条，"+
			"结果可能被 limit/预算截断（以 truncated 标志如实表达，截断≠无结果）。"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithString("query", mcp.Required(), mcp.Description("搜索关键词或 Everything 查询表达式（如 \"report ext:pdf\"）")),
		mcp.WithNumber("limit", mcp.Description("返回条数上限（1-300，默认 50）")),
	)
	handler := func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if deps.Search == nil {
			return mcp.NewToolResultError("everything 后端未装配：请确认 hanxi 已启用「Everything 搜索」模块"), nil
		}
		query, err := req.RequireString("query")
		if err != nil {
			return mcp.NewToolResultError("缺少参数 query（搜索关键词不能为空）"), nil
		}
		limit := req.GetInt("limit", 50)
		if limit < 1 || limit > maxSearchLimit {
			limit = maxSearchLimit
		}
		results, err := deps.Search.Search(query, limit)
		if err != nil {
			if errors.Is(err, errNoInstance) || errors.Is(err, errNoSearchTool) {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultError(fmt.Sprintf("文件搜索失败: %v", err)), nil
		}
		items := make([]any, 0, len(results))
		for _, r := range results {
			items = append(items, r)
		}
		// 拿满 limit：索引里大概率还有更多，如实置 truncated。
		return listResult(items, len(results) == limit)
	}
	return tool, handler
}
