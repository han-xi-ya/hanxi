package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"unicode/utf8"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"hanxi/internal/product"
)

// maxPayloadBytes 单工具文本载荷上限（PLAN_MCP §2.4 尺寸预算）：超限截断并置
// "truncated":true——学 MooTool「预算耗尽 ≠ 无结果」语义，绝不清空误导模型。
const maxPayloadBytes = 1 << 20

// 工具英文名（决策 7：英文名 + 中文 description，PLAN §8-7 示例形态 hanxi_xxx）。
const (
	toolEnvCheck = "hanxi_envcheck_detect"
	toolSearch   = "hanxi_file_search"
	toolOCR      = "hanxi_ocr_recognize"
	toolMemo     = "hanxi_memo_search"
)

// Deps 是 MCP server 的全部外联依赖（构造注入，单测以假件驱动全链路）。
// 真装配见 Run()；各字段允许为 nil——对应工具在调用时报"后端不可用"而非 panic。
type Deps struct {
	Access   *Access    // access.json 授权引擎（每次调用重读，fail-closed）
	Gate     ModuleGate // 模块启用门禁（真 = registry+settings 组合）
	EnvCheck EnvChecker // hanxi_envcheck_detect 后端
	Search   Searcher   // hanxi_file_search 后端（严格只读档）
	// C4/C5 依次追加：OCR Recognizer / Memo MemoSource。
}

// NewMCPServer 按工具面全量组表并挂授权/门禁中间件。
//
// 工具表设计（PLAN_MCP §3 两条口径的统一实现）：
//   - tools/list 恒为全量工具面（可发现性：未授权工具也在列表中，description 说明授权要求）；
//   - tools/call 逐次过 access.json ∩ 模块启用 交集门，未授权返回指引错误。
func NewMCPServer(deps Deps) *server.MCPServer {
	s := server.NewMCPServer(
		"hanxi",
		product.Version,
		server.WithInstructions("hanxi 工具箱的只读 MCP 接入：所有工具均严格只读，"+
			"调用前需在 hanxi「设置 → AI 接入」中逐项授权（access.json）。"+
			"未授权/被停用的工具调用会返回指引错误，不会执行。"),
		server.WithToolCapabilities(false),
		server.WithRecovery(),
		server.WithToolHandlerMiddleware(gateMiddleware(deps)),
	)
	for _, def := range toolDefs {
		tool, handler := def.Build(deps)
		s.AddTool(tool, handler)
	}
	return s
}

// toolDef 一个 MCP 工具的静态定义：名称/授权模块键 + 构建器（工具声明与 handler 成对产出）。
type toolDef struct {
	Name     string
	ModuleID string
	Build    func(deps Deps) (mcp.Tool, server.ToolHandlerFunc)
}

// knownModuleIDs 授权文件允许出现的工具键集合（出现集合外键 = access.json 非法 = 全拒绝）。
var knownModuleIDs = map[string]bool{
	"envcheck":   true,
	"everything": true,
	"ocr":        true,
	"memo":       true,
}

// toolDefs 全量工具面（C3-C5 逐提交挂载 everything/ocr/memo）。注册顺序即 tools/list
// 展示顺序，保持稳定；任何新增工具必须先过"会进云端模型上下文"红线审（包注释纪律 2）。
var toolDefs = []toolDef{
	{Name: toolEnvCheck, ModuleID: "envcheck", Build: buildEnvCheckTool},
	{Name: toolSearch, ModuleID: "everything", Build: buildEverythingTool},
}

// gateMiddleware 是所有工具调用的统一闸门：授权（每次重读 access.json）→ 模块启用 →
// EnsureActive 懒激活，三道门全过才进 handler。任何拒绝都以工具错误返回并给指引
// （deny-by-default + 可发现性）；绝不 panic、绝不静默空结果。
func gateMiddleware(deps Deps) server.ToolHandlerMiddleware {
	byName := make(map[string]string, len(toolDefs))
	for _, def := range toolDefs {
		byName[def.Name] = def.ModuleID
	}
	return func(next server.ToolHandlerFunc) server.ToolHandlerFunc {
		return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			moduleID, ok := byName[req.Params.Name]
			if !ok {
				return mcp.NewToolResultError(fmt.Sprintf("未知工具 %q", req.Params.Name)), nil
			}
			if deps.Access == nil || !deps.Access.Allowed(moduleID) {
				return mcp.NewToolResultError(
					fmt.Sprintf("工具 %s 未获授权（授权模块 %s）。请用户在 hanxi 主窗口「设置 → AI 接入」中开启后重试；"+
						"授权即时生效，无需重启本服务。", req.Params.Name, moduleID)), nil
			}
			if deps.Gate != nil {
				if err := deps.Gate.Check(moduleID); err != nil {
					return mcp.NewToolResultError(fmt.Sprintf("工具 %s 暂不可用：%v", req.Params.Name, err)), nil
				}
			}
			return next(ctx, req)
		}
	}
}

// ---------- 结果封装公共件 ----------

// resultPayload 统一外层结构：工具结果一律为单条紧凑 JSON 文本（省 token）。
type resultPayload map[string]any

// listResult 把条目列表装进 {count, truncated, results:[...]} 信封并执行 1MB 预算：
// 装不下就对半砍条目（预算耗尽以 truncated=true 显式表达，≠ 无结果）；
// cappedByLimit=true 表示后端按上限取数、大概率还有更多（同样置 truncated）。
func listResult(items []any, cappedByLimit bool) (*mcp.CallToolResult, error) {
	total := len(items)
	payload := resultPayload{}
	n := total
	for {
		kept := items
		if n < total {
			kept = items[:n]
		}
		payload["count"] = len(kept)
		payload["truncated"] = n < total || (cappedByLimit && n == total && total > 0)
		payload["results"] = kept
		data, err := json.Marshal(payload)
		if err != nil {
			return mcp.NewToolResultErrorFromErr("结果序列化失败", err), nil
		}
		if len(data) <= maxPayloadBytes || n <= 0 {
			return mcp.NewToolResultText(string(data)), nil
		}
		n /= 2
	}
}

// textResult 直接序列化一个对象载荷（调用方保证无列表或列表已经 listResult 处理过）。
func textResult(payload any) (*mcp.CallToolResult, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return mcp.NewToolResultErrorFromErr("结果序列化失败", err), nil
	}
	if len(data) > maxPayloadBytes {
		// 非列表形态无法逐条裁剪：按 rune 边界裁字节并附截断标记文本。
		s := string(data)
		cut := maxPayloadBytes - 64
		for cut > 0 && !utf8.RuneStart(s[cut]) {
			cut--
		}
		return mcp.NewToolResultText(s[:cut] + `…(payload truncated)}`), nil
	}
	return mcp.NewToolResultText(string(data)), nil
}

// truncateUTF8 按字节上限在 rune 边界截断文本，返回（截断文, 是否截断）。
func truncateUTF8(s string, limit int) (string, bool) {
	if len(s) <= limit {
		return s, false
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut], true
}
