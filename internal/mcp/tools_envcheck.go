package mcp

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"hanxi/internal/logging"
	"hanxi/internal/modules/envcheck/detect"
)

// EnvChecker 是 hanxi_envcheck_detect 的后端能力面（真 = *envcheck.EnvCheckService，
// 单测注入假件避免真实 spawn 版本命令）。
type EnvChecker interface {
	DetectAll() []detect.ToolInfo
}

// buildEnvCheckTool 开发环境体检只读查询（PLAN_MCP §2.2 envcheck 行：
// DetectAll 纯本机 LookPath+版本命令，零出网；在线版本对比通道首版不暴露）。
func buildEnvCheckTool(deps Deps) (mcp.Tool, server.ToolHandlerFunc) {
	tool := mcp.NewTool(toolEnvCheck,
		mcp.WithDescription("检测本机开发工具链环境（Git、Go、Node.js、Java、Python、.NET 等）："+
			"返回每个工具的安装路径与版本号，只读且纯本机探测（不联网、不执行除版本查询外的任何命令）。"+
			"适合回答“这台机器装了什么版本的 git/go/node”类问题。"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
	)
	handler := func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if deps.EnvCheck == nil {
			return mcp.NewToolResultError("envcheck 后端未装配：请在 hanxi 主程序中确认「开发环境检测」模块可用"), nil
		}
		tools := deps.EnvCheck.DetectAll()
		items := make([]any, 0, len(tools))
		for _, t := range tools {
			// 红线：hint 可能携带版本命令的失败输出（环境回声），统一过 Redact 口径。
			t.Hint = logging.Redact(t.Hint)
			items = append(items, t)
		}
		return listResult(items, false)
	}
	return tool, handler
}
