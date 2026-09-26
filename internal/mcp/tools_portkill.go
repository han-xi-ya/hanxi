package mcp

// tools_portkill.go 是机主指令"AI 能力接入增加端口查杀"的破坏性工具对：
//
//	hanxi_portkill_prepare(port)   只读侦察 + 发放一次性确认 token（无杀伤力）
//	hanxi_portkill_execute(token)  消费 token + 复查进程指纹后才真正结束进程
//
// 本文件自含 handler 与后端 seam，不注册进 server.go 的 toolDefs——接线行
// （toolDefs 追加两行、knownModuleIDs 加 "portkill" 键、mcpModules 挂
// portkill 模块、Run() 调 SetPortkillWiring、static 审扩 destructive 条款）
// 以报告交付，由主会话拍板合入。红线升版论证见 guarded.go 文件头与报告。

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"hanxi/internal/logging"
	"hanxi/internal/modules/portkill"
	"hanxi/internal/platform"
)

// 破坏性工具名（对外契约：接线后不可改名，同 TestToolNamesStable 纪律）。
const (
	toolPortkillPrepare = "hanxi_portkill_prepare"
	toolPortkillExecute = "hanxi_portkill_execute"
)

// portkillAccessKey 是查杀工具族共用的 access.json 授权键（模块粒度，
// 照 memo 键下挂两件工具的既有先例）。另一路扫描扩键（六键→八键）时本键
// 追加为第九键；读写两侧（knownModuleIDs / mcpwizard accessToolKeys）必须
// 同步扩，否则未知键整体拒读会把全部工具连坐封死——协调点在报告列明。
const portkillAccessKey = portkill.ID

// destructiveFamilies 登记包内全部破坏性工具族（红线升版后 static 审的
// 唯一豁免来源）：一个族必须恰好两件、名字为 *_prepare + *_execute 二段式，
// 族键即 access 授权键。server_test.go 的扩版条款据此裁决豁免范围。
type destructiveFamily struct {
	Prepare   string
	Execute   string
	AccessKey string
}

var destructiveFamilies = []destructiveFamily{
	{Prepare: toolPortkillPrepare, Execute: toolPortkillExecute, AccessKey: portkillAccessKey},
}

// isKnownDestructiveOp 报告 op 键是否为 destructive.json 允许出现的集合
// （集合外键=契约外授权请求，整文件拒读，镜像 access.go 的 knownModuleIDs 纪律）。
func isKnownDestructiveOp(op string) bool {
	for _, f := range destructiveFamilies {
		if op == f.AccessKey {
			return true
		}
	}
	return false
}

// isDestructiveToolName 报告名称是否属已登记的破坏性工具族成员。
func isDestructiveToolName(name string) bool {
	for _, f := range destructiveFamilies {
		if name == f.Prepare || name == f.Execute {
			return true
		}
	}
	return false
}

// isDestructiveExecuteName 报告名称是否为族内的"扣扳机"件（execute 段）。
func isDestructiveExecuteName(name string) bool {
	for _, f := range destructiveFamilies {
		if name == f.Execute {
			return true
		}
	}
	return false
}

// ---------- 后端 seam（真 = portkillBackendAdapter；单测注入假件） ----------

// PortkillBackend 是端口查杀工具依赖的最小后端面，形状刻意与
// *portkill.PortKillService + platform.ProcessAPI 的既有方法一一对应，
// 真装配 adapter 只做转发不做第二套语义（GUI 与 MCP 同一 service 契约，
// 平台层 KillVerified 的指纹复核在 seam 之后仍然生效——双重复核）。
type PortkillBackend interface {
	// QueryPort 查端口占用（TCP+UDP 快照），只读。
	QueryPort(port int) ([]portkill.PortOccupant, error)
	// QueryProcess 按 PID 取当前实况（execute 前 TOCTOU 复核用）。
	QueryProcess(pid uint32) (platform.ProcInfo, error)
	// IsProtected 平台红线判定（csrss/lsass/services/svchost、pid 0/4、自身）。
	IsProtected(pid uint32, info platform.ProcInfo) bool
	// KillProcess 按指纹（pid+exe+start）复核后结束进程。
	KillProcess(pid uint32, exePath string, startedAtUnix int64) (portkill.KillResult, error)
}

// portkillBackendAdapter 把 GUI 同谱的 portkill service 与平台进程 API
// 缝合成 PortkillBackend（Run() 接线行构造，注入 SetPortkillWiring）。
type portkillBackendAdapter struct {
	svc  *portkill.PortKillService
	proc platform.ProcessAPI
}

// NewPortkillAdapter 组装真后端。svc 取 portkill 模块实例的 Service()
// （与 GUI 同谱启用门禁/生命周期），proc 取 plat.Process()。
func NewPortkillAdapter(svc *portkill.PortKillService, plat platform.Platform) PortkillBackend {
	return &portkillBackendAdapter{svc: svc, proc: plat.Process()}
}

func (a *portkillBackendAdapter) QueryPort(port int) ([]portkill.PortOccupant, error) {
	return a.svc.QueryPort(port)
}

func (a *portkillBackendAdapter) QueryProcess(pid uint32) (platform.ProcInfo, error) {
	return a.proc.Query(pid)
}

func (a *portkillBackendAdapter) IsProtected(pid uint32, info platform.ProcInfo) bool {
	return a.proc.IsProtected(pid, info)
}

func (a *portkillBackendAdapter) KillProcess(pid uint32, exePath string, startedAtUnix int64) (portkill.KillResult, error) {
	return a.svc.KillProcess(pid, exePath, startedAtUnix)
}

// ---------- A4：永久拒杀黑名单（独立于一切授权态） ----------

// portkillHardDenyNames 是 MCP 通道附加的进程名黑名单（小写基名匹配）。
// 平台 IsProtected 已覆盖 csrss/wininit/services/lsass/smss/svchost 与
// pid 0/4/自身；这里补的是"对 hanxi 机主场景不可杀"的余集，清单可扩展——
// 扩展只加不减：黑名单收窄属于红线回撤，必须走与本片同级的显式决策。
var portkillHardDenyNames = map[string]bool{
	"hanxi.exe":    true, // 自身（GUI 与无头同二进制；杀 GUI 毁掉撤闸通道本身）
	"dwm.exe":      true, // 桌面合成器，杀掉即黑屏
	"explorer.exe": true,
}

// portkillHardDeny 给出 MCP 通道对目标的永久拒杀判定（含平台红线之外的
// 黑名单与 pid 下界）；返回拒杀原因，nil 表示可放行到复核/查杀段。
// 判定顺序无关紧要但必须全查：pid<=4、当前进程、黑名单名、平台 IsProtected。
func portkillHardDeny(b PortkillBackend, pid uint32, info platform.ProcInfo) string {
	if pid <= 4 {
		return "PID ≤ 4 为系统核心进程，永久拒杀"
	}
	if pid == uint32(os.Getpid()) {
		return "目标即 hanxi 无头进程自身，永久拒杀"
	}
	name := strings.ToLower(filepath.Base(info.ExePath))
	if name == "" {
		name = strings.ToLower(info.Name)
	}
	if portkillHardDenyNames[name] {
		return fmt.Sprintf("进程 %s 在 MCP 通道永久拒杀黑名单内", name)
	}
	if b != nil && b.IsProtected(pid, info) {
		return "目标属系统红线保护进程（平台判定），不可查杀"
	}
	return ""
}

// ---------- hanxi_portkill_prepare（只读侦察 + 发 token，无杀伤力） ----------

// occupantPayload 把一条占用记录装进模型可见载荷；出机前逐文本字段过
// logging.RedactPII（包注释决策 1：一切工具输出按"会进云端模型上下文"审视）。
func occupantPayload(o portkill.PortOccupant, token, blockReason string) resultPayload {
	p := resultPayload{
		"pid":         o.PID,
		"protocol":    logging.RedactPII(o.Protocol),
		"localIp":     logging.RedactPII(o.LocalIP),
		"state":       logging.RedactPII(o.State),
		"processName": logging.RedactPII(o.ProcessName),
		"exePath":     logging.RedactPII(o.ExePath),
		"killable":    token != "",
	}
	if !o.StartedAt.IsZero() {
		p["startedAt"] = o.StartedAt.Format(time.RFC3339)
	}
	if token != "" {
		p["confirmToken"] = token
	}
	if blockReason != "" {
		p["blockReason"] = logging.RedactPII(blockReason)
	}
	return p
}

// buildPortkillPrepareTool 端口占用侦察 + 一次性确认 token 发放。
// 声明形态：readOnlyHint=true / destructiveHint=false——prepare 本身不改
// 系统任何状态（发 token 只在无头进程内存里）；杀伤面全部收在 execute。
func buildPortkillPrepareTool(deps Deps) (mcp.Tool, server.ToolHandlerFunc) {
	tool := mcp.NewTool(toolPortkillPrepare,
		mcp.WithDescription("破坏性工具二段式·第一段（侦察，只读）：查询指定端口的占用进程明细"+
			"（PID/进程名/可执行路径/启动时间），并为每个可查杀目标发放一次性确认 token（有效期 120 秒）。"+
			"本工具不结束任何进程；token 须交 hanxi_portkill_execute 二次确认后才生效。"+
			"系统关键进程与 hanxi 自身在黑名单内，标记 killable=false 且不发 token。"+
			"调用前需机主在 access.json 授权 portkill 键，并在 destructive.json 打开破坏性总闸（默认关）。"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(false), // 每次调用新发 token，重复调用非无害空转
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithNumber("port", mcp.Required(), mcp.Description("要侦察的端口号（1-65535）")),
	)
	handler := func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		w := portkillWiringOf()
		if w == nil || w.Backend == nil {
			portkillAudit("prepare", "unwired", "portkill 后端未接线（需 Run() 装配 SetPortkillWiring）", nil)
			return mcp.NewToolResultError("端口查杀后端未接线：本 hanxi 无头版本未装配该能力"), nil
		}
		port, perr := req.RequireInt("port")
		if perr != nil {
			return mcp.NewToolResultError(fmt.Sprintf("端口参数非法：%v（须为 1-65535 整数）", perr)), nil
		}
		if port < 1 || port > 65535 {
			return mcp.NewToolResultError(fmt.Sprintf("端口号非法: %d（须为 1-65535 整数）", port)), nil
		}
		// A2 机主总闸：与 execute 同闸同判——闸关时连 token 都不发放，
		// 模型拿不到任何"看似可用"的确认物，指引错误即完整交代。
		if w.Policy == nil || !w.Policy.Allowed(portkillAccessKey) {
			reason := "破坏性总闸未开（默认关）"
			if w.Policy != nil {
				reason = w.Policy.LastReason()
			}
			portkillAudit("prepare", "denied_policy", reason, nil, "port", port)
			return mcp.NewToolResultError(
				fmt.Sprintf("端口查杀未获机主授权：destructive.json 总闸未开（fail-closed 默认全关）。"+
					"请机主在 hanxi 数据目录 mcp/%s 显式建档 {\"version\":1,\"enabled\":{\"portkill\":true}} 后重试；"+
					"授权即时生效，无需重启本服务。", DestructiveFileName)), nil
		}
		occupants, qerr := w.Backend.QueryPort(port)
		if qerr != nil {
			portkillAudit("prepare", "error", fmt.Sprintf("端口查询失败: %v", qerr), nil, "port", port)
			return mcp.NewToolResultError(fmt.Sprintf("端口 %d 查询失败: %v", port, qerr)), nil
		}
		if len(occupants) == 0 {
			return textResult(resultPayload{
				"port": port, "count": 0, "occupants": []any{},
				"message": fmt.Sprintf("端口 %d 当前无进程占用，无需查杀", port),
			})
		}
		items := make([]any, 0, len(occupants))
		issued := 0
		for _, o := range occupants {
			reason := portkillHardDeny(w.Backend, o.PID, platform.ProcInfo{
				PID: o.PID, Name: o.ProcessName, ExePath: o.ExePath, StartedAt: o.StartedAt,
			})
			if reason == "" && o.IsProtected {
				reason = "目标属系统红线保护进程（平台判定），不可查杀"
			}
			if reason != "" {
				items = append(items, occupantPayload(o, "", reason))
				continue
			}
			tok, hash, ierr := w.Tokens.Issue(port, o.PID, o.ExePath, o.StartedAt)
			if ierr != nil {
				items = append(items, occupantPayload(o, "", "确认令牌发放失败，请稍后重试"))
				continue
			}
			issued++
			items = append(items, occupantPayload(o, tok, ""))
			portkillAudit("prepare", "issued", "", &killToken{
				hash: hash, port: port, pid: o.PID, exePath: o.ExePath,
			})
		}
		res, rerr := textResult(resultPayload{
			"port": port, "count": len(items), "occupants": items, "tokensIssued": issued,
			"tokenTtlSeconds": int(portkillTokenTTL.Seconds()),
			"nextStep":        "对携带 confirmToken 的目标调用 hanxi_portkill_execute(token)；一次调用只杀一个目标",
		})
		if rerr != nil {
			return res, rerr
		}
		return res, nil
	}
	return tool, handler
}

// ---------- hanxi_portkill_execute（消费 token + 指纹复查 + 真正查杀） ----------

// buildPortkillExecuteTool 二段式第二段：唯一具备杀伤力的入口。
// 声明形态：readOnlyHint=false / destructiveHint=true——工具注解如实申报，
// 客户端确认 UI 据此呈现（红线升版后的诚实标注，而非伪装只读）。
func buildPortkillExecuteTool(deps Deps) (mcp.Tool, server.ToolHandlerFunc) {
	tool := mcp.NewTool(toolPortkillExecute,
		mcp.WithDescription("破坏性工具二段式·第二段（执行，有杀伤力）：消费 hanxi_portkill_prepare "+
			"发放的一次性确认 token，结束该 token 绑定的进程（pid+可执行路径+启动时间三重指纹，"+
			"执行前对系统实况复查，指纹已变即中止并消耗令牌，防 PID 复用杀错）。"+
			"令牌单次使用、120 秒过期；每次调用至多结束一个进程。"+
			"需 access.json 授权 portkill 键与 destructive.json 机主总闸，两闸缺一拒绝且不消耗调用方之外的任何状态。"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithOpenWorldHintAnnotation(false),
		mcp.WithString("token", mcp.Required(), mcp.Description("hanxi_portkill_prepare 回执中该目标的一次性确认令牌")),
	)
	handler := func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		tokenID, err := req.RequireString("token")
		if err != nil {
			portkillAudit("execute", "error", "缺少 token 参数", nil)
			return mcp.NewToolResultError("缺少 token 参数：请先调用 hanxi_portkill_prepare 获取一次性确认令牌"), nil
		}
		tokenID = strings.TrimSpace(tokenID)
		w := portkillWiringOf()
		if w == nil || w.Backend == nil || w.Tokens == nil {
			portkillAudit("execute", "unwired", "portkill 后端未接线", nil)
			return mcp.NewToolResultError("端口查杀后端未接线：本 hanxi 无头版本未装配该能力"), nil
		}
		// A2 机主总闸先于 token 消费：闸关时不烧 token（撤闸是机主即时权利，
		// 不应产生"令牌被无权通道消耗"的副作用）。
		if w.Policy == nil || !w.Policy.Allowed(portkillAccessKey) {
			reason := "破坏性总闸未开（默认关）"
			if w.Policy != nil {
				reason = w.Policy.LastReason()
			}
			portkillAudit("execute", "denied_policy", reason, nil)
			return mcp.NewToolResultError(
				fmt.Sprintf("端口查杀未获机主授权：destructive.json 总闸未开（fail-closed 默认全关），"+
					"令牌未被消耗。请机主显式建档 {\"version\":1,\"enabled\":{\"portkill\":true}} 于 mcp/%s 后重试。",
					DestructiveFileName)), nil
		}
		// A3 token 消费：单次使用硬保证——此后任何失败都不返还令牌。
		con := w.Tokens.Consume(tokenID)
		t := con.Token
		if !con.Valid {
			outcome := "unknown_token"
			switch {
			case strings.Contains(con.Why, "过期"):
				outcome = "expired"
			case strings.Contains(con.Why, "已被使用"):
				outcome = "used"
			}
			portkillAudit("execute", outcome, con.Why, t)
			return mcp.NewToolResultError(fmt.Sprintf("确认令牌校验失败：%s", con.Why)), nil
		}
		// TOCTOU 复查：prepare 与 execute 之间目标可能已退出、PID 可能被复用。
		// 复查不过一律中止且如实告知（令牌已消耗，必须重新 prepare——防重放）。
		current, qerr := w.Backend.QueryProcess(t.pid)
		if qerr != nil {
			portkillAudit("execute", "aborted_identity", fmt.Sprintf("目标进程已不存在: %v", qerr), t)
			return mcp.NewToolResultError(fmt.Sprintf(
				"已安全中止：prepare 时的目标进程 PID %d 现已不存在（可能已退出或 PID 被回收）。"+
					"本次令牌已消耗，如需继续请重新 prepare。", t.pid)), nil
		}
		if msg := fingerprintMismatch(t, current); msg != "" {
			portkillAudit("execute", "aborted_identity", msg, t, "currentExe", current.ExePath)
			return mcp.NewToolResultError(fmt.Sprintf(
				"已安全中止（防杀错）：%s。本次令牌已消耗，请重新 prepare 确认最新占用者。", msg)), nil
		}
		// A4 黑名单对"实况"复查一遍：令牌签发后清单不会变，但判定必须建立在
		// 当前指纹上（比如 prepare 时拿不到路径、现在拿到了）。
		if deny := portkillHardDeny(w.Backend, t.pid, current); deny != "" {
			portkillAudit("execute", "denied_protected", deny, t)
			return mcp.NewToolResultError(fmt.Sprintf("拒绝查杀：%s（令牌已消耗）", deny)), nil
		}
		// 真查杀。portkill service 内部经 platform.KillVerified 还会做第三重
		// 指纹复核（双保险，语义与上一段一致）；统一历史/notify 由 service 自带。
		res, kerr := w.Backend.KillProcess(t.pid, t.exePath, unixOrZero(t.startedAt))
		if kerr != nil {
			portkillAudit("execute", "error", fmt.Sprintf("查杀调用失败: %v", kerr), t)
			return mcp.NewToolResultError(fmt.Sprintf("查杀执行失败：%v", kerr)), nil
		}
		switch {
		case res.Success:
			portkillAudit("execute", "success", "", t, "processName", current.Name)
			return textResult(resultPayload{
				"ok": true, "outcome": "success", "pid": t.pid, "port": t.port,
				"message": fmt.Sprintf("已结束进程 PID %d（%s），端口 %d 占用应已释放", t.pid, current.Name, t.port),
			})
		case res.NeedElevate:
			// MCP 通道刻意不触发 UAC：无头进程弹提权对话框对不在电脑前的
			// 机主不可见亦不可控，等于绕过了"人工点头"。指引回 GUI 完成。
			portkillAudit("execute", "need_elevate", res.ErrorMessage, t)
			return textResult(resultPayload{
				"ok": false, "outcome": "need_elevate", "pid": t.pid, "port": t.port,
				"message": "目标进程权限等级高于本会话，MCP 通道不代发 UAC 提权；" +
					"请机主在 hanxi 主窗口「释放端口」页面完成提权查杀（那里有人工确认框）。",
			})
		default:
			portkillAudit("execute", "fail", res.ErrorMessage, t)
			return textResult(resultPayload{
				"ok": false, "outcome": "fail", "pid": t.pid, "port": t.port,
				"message": fmt.Sprintf("查杀未成功：%s", logging.RedactPII(res.ErrorMessage)),
			})
		}
	}
	return tool, handler
}

// fingerprintMismatch 比对待查杀进程"签发时指纹"与"当前实况"：
// exe 路径大小写不敏感全等（Windows 语义），启动时间允许 1 秒误差
// （与 platform.KillVerified 同口径）；任一侧为零值则该维度降级跳过
// ——路径与时间至少要有一个可核，全空视为异常直接拒。
func fingerprintMismatch(t *killToken, current platform.ProcInfo) string {
	if t.exePath != "" && current.ExePath != "" && !strings.EqualFold(t.exePath, current.ExePath) {
		return fmt.Sprintf("PID %d 的可执行路径已变化（签发 %s → 现状 %s），疑似 PID 被复用", t.pid, t.exePath, current.ExePath)
	}
	if !t.startedAt.IsZero() && !current.StartedAt.IsZero() {
		diff := t.startedAt.Sub(current.StartedAt)
		if diff < -time.Second || diff > time.Second {
			return fmt.Sprintf("PID %d 的启动时间已变化（签发 %s → 现状 %s），疑似 PID 被复用",
				t.pid, t.startedAt.Format(time.RFC3339Nano), current.StartedAt.Format(time.RFC3339Nano))
		}
	}
	if t.exePath == "" && t.startedAt.IsZero() {
		return "签发时未获得任何可比对指纹（路径与启动时间均缺失），拒绝仅凭 PID 查杀"
	}
	return ""
}

// unixOrZero 把签发时间戳换算成 portkill service 期望的 unix 秒（零值传 0，
// 与 GUI 调用方 KillProcess(pid, exe, startedAt.Unix()) 的口径一致）。
func unixOrZero(ts time.Time) int64 {
	if ts.IsZero() {
		return 0
	}
	return ts.Unix()
}

// ---------- 接线行报告用导出件（不影响未接线时的行为，全部 fail-closed） ----------

// PortkillAccessKey 供接线方（server.go toolDefs / knownModuleIDs /
// mcpwizard 写侧）引用同一个键名，杜绝两处手打字符串漂移。
func PortkillAccessKey() string { return portkillAccessKey }

// DestructivePolicyPath 供 Run() 接线行拼总闸文件路径（与 access.json 同目录）。
func DestructivePolicyPath(dataDir string) string {
	return filepath.Join(dataDir, destructiveDirName, DestructiveFileName)
}

// NewDefaultKillTokenStore 按生产 TTL 构造 token 库（Run() 接线行用）。
func NewDefaultKillTokenStore() *TokenStore {
	return NewKillTokenStore(portkillTokenTTL, time.Now)
}
