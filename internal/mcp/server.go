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
// 契约扩充批（N32/N34 2026-09-24）加至六件；N16 C 批（2026-09-26）便签族加
// hanxi_memo_stats 至七件——新工具挂既有 memo 键：授权粒度=模块，memo 开关
// 同时放行检索与统计，撤权同样一并生效（fail-closed 语义不因工具族扩充而稀释）。
// AI 接入批（2026-09-26）加扫描族两件（hanxi_portscan_scan / hanxi_lan_scan）至
// 九件：扫描是主动网络探测而非纯查询，**必须各立授权键**（不挂既有键），
// access.json 六键契约扩至八键（portscan/lan 两键默认 false=不放开触发扫描，
// 机主要逐项授权才可用；读写对拍+GUI 呈现+白名单三处联动同步）。
// 端口查杀批（2026-09-26，红线升版执行层见 guarded.go）加破坏性族两件
// （hanxi_portkill_prepare / hanxi_portkill_execute）至十一件：族共用 portkill
// 一键（授权粒度=模块，照 memo 键先例），access.json 扩至九键——但 portkill 键
// 刻意不进「设置 → AI 接入」面板（破坏性键不给误操作台阶，机主手动双文件开启），
// 且工具生效还需 destructive.json 机主总闸 + 一次性令牌二段确认（四道闸全说见
// guarded.go 文件头）；白名单/knownModuleIDs/写侧 accessToolKeys 三处同批扩键。
const (
	toolEnvCheck  = "hanxi_envcheck_detect"
	toolSearch    = "hanxi_file_search"
	toolOCR       = "hanxi_ocr_recognize"
	toolMemo      = "hanxi_memo_search"
	toolMemoStats = "hanxi_memo_stats"
	toolSysInfo   = "hanxi_sysinfo_report"
	toolLogs      = "hanxi_log_read"
	toolPortScan  = "hanxi_portscan_scan"
	toolLanScan   = "hanxi_lan_scan"
)

// accessKeyLogs 是 logs 工具的授权键（无同名业务模块，registryGate 据此放行空门）。
const accessKeyLogs = "logs"

// Deps 是 MCP server 的全部外联依赖（构造注入，单测以假件驱动全链路）。
// 真装配见 Run()；各字段允许为 nil——对应工具在调用时报"后端不可用"而非 panic。
type Deps struct {
	Access   *Access      // access.json 授权引擎（每次调用重读，fail-closed）
	Gate     ModuleGate   // 模块启用门禁（真 = registry+settings 组合）
	EnvCheck EnvChecker   // hanxi_envcheck_detect 后端
	Search   Searcher     // hanxi_file_search 后端（严格只读档）
	OCR      Recognizer   // hanxi_ocr_recognize 后端
	Memo     MemoSource   // 便签族后端（零落盘直读，memo_search 与 memo_stats 共用）
	SysInfo  ReportSource // hanxi_sysinfo_report 后端（N32，与 GUI 同一 service）
	Logs     LogTailer    // hanxi_log_read 后端（N34，只读 tail 按天日志）
	PortScan PortProber   // hanxi_portscan_scan 后端（有界主动探测，MCP 面单飞）
	Lan      LanProber    // hanxi_lan_scan 后端（有界主动探测，与 GUI 同一 service）
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
		server.WithInstructions("hanxi 工具箱的 MCP 接入：查询类严格只读（环境体检/文件搜索/便签/系统档案/运行日志/OCR）；"+
			"扫描类（端口扫描/局域网扫描）仅对网络做可达性探测，不修改本机或任何设备状态，但属于主动出网动作且结果含网络信息；"+
			"破坏性工具族（端口查杀 prepare/execute）双授权默认关、二段确认方生效——access.json 的 portkill 键与"+
			" mcp/destructive.json 机主总闸齐开才放行，prepare 只发放一次性令牌（120 秒），execute 复核进程指纹后才结束进程，"+
			"系统关键进程与 hanxi 自身永久拒杀。"+
			"所有工具调用前需授权（access.json，九键，默认全关；portkill 键刻意不进「设置 → AI 接入」面板，机主手动两文件开启）。"+
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
// 六键契约扩充批（N32/N34）与写方 mcpwizard/access_write.go 的 accessToolKeys 同步演进，
// 一致性由 access_readmatch_test.go 对拍矩阵把关。
// AI 接入批（2026-09-26）加 portscan/lan 两键至八键——扫描类是主动出网动作，
// 必须与纯查询工具分键授权（机主可只放行扫描而不放行文件搜索等，反之亦然）。
// 端口查杀批加 portkill 键至九键：**读方（本表）/写方（accessToolKeys）/名称白名单
// （guards_test.go）三处必须同批**——漏一处，含 portkill 键的整档 access.json 会被
// fail-closed 拒读、连坐封死全部工具。键名取自 portkillAccessKey（=portkill.ID），
// 杜绝两处手打字符串漂移；destructive.json 侧的同名 op 键由 isKnownDestructiveOp
// 独立把关（两文件两契约，见 guarded.go A2）。
var knownModuleIDs = map[string]bool{
	"envcheck":        true,
	"everything":      true,
	"ocr":             true,
	"memo":            true,
	"sysinfo":         true,
	"logs":            true,
	"portscan":        true,
	"lan":             true,
	portkillAccessKey: true,
}

// toolDefs 全量工具面（首版四件 PLAN_MCP C1-C5 收口；扩充批 +sysinfo/logs 至六件；
// N16 C 批便签族 +memo_stats 至七件；AI 接入批 +portscan/lan 扫描族至九件；
// 端口查杀批 +portkill 破坏族两件至十一件，置于列表末——只读面在前、破坏面垫后，
// tools/list 展示顺序即风险次序）。
// 授权键九枚：memo 键下两件只读工具；portscan/lan 各立一键（主动探测与纯查询
// 分键授权，见 knownModuleIDs 注记）；portkill 一键下挂 prepare/execute 族两件
// （二段式共键授权，生效另需 destructive.json 总闸，见 guarded.go A1/A2）。
// 注册顺序即 tools/list 展示顺序，保持稳定；任何新增工具必须先过"会进云端模型上下文"
// 红线审（包注释纪律 1），并同步 knownModuleIDs 与 guards_test.go 的名称白名单。
var toolDefs = []toolDef{
	{Name: toolEnvCheck, ModuleID: "envcheck", Build: buildEnvCheckTool},
	{Name: toolSearch, ModuleID: "everything", Build: buildEverythingTool},
	{Name: toolOCR, ModuleID: "ocr", Build: buildOcrTool},
	{Name: toolMemo, ModuleID: "memo", Build: buildMemoTool},
	{Name: toolMemoStats, ModuleID: "memo", Build: buildMemoStatsTool},
	{Name: toolSysInfo, ModuleID: "sysinfo", Build: buildSysInfoTool},
	{Name: toolLogs, ModuleID: accessKeyLogs, Build: buildLogsTool},
	{Name: toolPortScan, ModuleID: "portscan", Build: buildPortScanTool},
	{Name: toolLanScan, ModuleID: "lan", Build: buildLanScanTool},
	{Name: toolPortkillPrepare, ModuleID: portkillAccessKey, Build: buildPortkillPrepareTool},
	{Name: toolPortkillExecute, ModuleID: portkillAccessKey, Build: buildPortkillExecuteTool},
}

// isDestructiveAccessKey 报告模块键是否属已登记破坏族（豁免/文案裁决唯一来源是
// destructiveFamilies 族登记表，散落字面量即红线漂移——同 guarded.go 族登记纪律）。
func isDestructiveAccessKey(key string) bool {
	for _, f := range destructiveFamilies {
		if f.AccessKey == key {
			return true
		}
	}
	return false
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
				if isDestructiveAccessKey(moduleID) {
					// 破坏族（A1 门）刻意不给"面板开启"指引——portkill 键不进「AI 接入」
					// 面板是基建既定决策（guarded.go A2：防逐键 UI 误操作顺带放行写工具），
					// 指引必须如实指向两文件的机主手动开启路径。
					return mcp.NewToolResultError(
						fmt.Sprintf("工具 %s 未获授权（破坏性工具族 %s，四道闸之 A1）。该键刻意不在「设置 → AI 接入」面板呈现，"+
							"需机主手动改 hanxi 数据目录 mcp/ 下两文件：access.json 置 \"%s\": true，并新建 %s "+
							"写入 {\"version\":1,\"enabled\":{\"%s\":true}}（缺一律拒，另见工具描述）；授权即时生效，无需重启本服务。",
							req.Params.Name, moduleID, moduleID, DestructiveFileName, moduleID)), nil
				}
				return mcp.NewToolResultError(
					fmt.Sprintf("工具 %s 未获授权（授权模块 %s）。请用户在 hanxi 主窗口「设置 → AI 接入」中开启后重试；"+
						"授权即时生效，无需重启本服务。", req.Params.Name, moduleID)), nil
			}
			if deps.Gate != nil {
				// 门禁同时取得 operation lease：无头工具调用在途期间，
				// GUI 停用/退出的 drain 会等待本次调用收口（Wave 3 统一门）。
				release, err := deps.Gate.Check(moduleID)
				if err != nil {
					return mcp.NewToolResultError(fmt.Sprintf("工具 %s 暂不可用：%v", req.Params.Name, err)), nil
				}
				if release != nil {
					defer release()
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
// 超过预算时返回固定的合法 JSON 错误信封；绝不在序列化后的 JSON 字节上硬截断，
// 因为截断可能切进转义序列、字符串或对象结构，产出无效 JSON。
func textResult(payload any) (*mcp.CallToolResult, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return mcp.NewToolResultErrorFromErr("结果序列化失败", err), nil
	}
	if len(data) > maxPayloadBytes {
		data, err = json.Marshal(resultPayload{
			"ok":        false,
			"error":     fmt.Sprintf("结果超过 %d 字节上限，已省略；请缩小查询范围后重试", maxPayloadBytes),
			"truncated": true,
		})
		if err != nil {
			return mcp.NewToolResultErrorFromErr("超限结果信封序列化失败", err), nil
		}
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
