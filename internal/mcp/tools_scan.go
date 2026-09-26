package mcp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync/atomic"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"hanxi/internal/logging"
	"hanxi/internal/modules/lan"
	"hanxi/internal/modules/portscan"
)

// tools_scan.go 是扫描工具族（机主指令"AI 能力接入增加端口扫描、局域网扫描"）：
// hanxi_portscan_scan（授权键 portscan）与 hanxi_lan_scan（授权键 lan）。
//
// 语义定性（诚实口径，勿再套"纯查询"话术）：扫描是**主动网络探测**，不是本机
// 状态读取。它不修改本机任何状态、不写入任何数据，对外也只是 connect/echo 级
// 探测（与 GUI 同一引擎）——所以注解仍是 readOnlyHint=true / destructiveHint=
// false，但 openWorldHint=true（与外部网络交互）且 idempotentHint=false
// （同一请求两次结果可能不同）。"重 IO 且耗时不定"的边界靠**有界参数**收口：
//
//   - portscan：仅单目标（IP/域名，拒绝 CIDR——目标×端口二维相乘是"全子网
//     分钟级"的真正来源，砍掉目标维度后规模可控）；端口去重后 ≤256 个，超限
//     **拒绝而非静默截断**（端口探测缺漏会让"开放 N 个"结论失真，诚实报错让
//     模型换小端口集重调）；单端口超时钳 100~3000ms；整轮硬预算 60s，预算耗尽
//     返回部分结果并置 completed=false 如实标注"结论不完整"。
//   - lan：地址数经 lan.CountTargets 同一解析面预检，≤1024（/22），超限拒绝并
//     指引；lan 引擎自带 10/25s 动态超时与单飞闸，忙冲突映射为中文"忙"错误。
//   - 两工具都是 **MCP 面单飞**（同一无头进程同时只跑一轮扫描，后来者收忙错误
//     而非顶掉前一轮——portscan 直构 Scanner 而不走 StartScan，正是为了绕开其
//     "新任务取消旧任务"的 GUI last-wins 语义：模型并发调用下那会产出被误标
//     完整的半截结果）。
//   - 不放开代理/并发/限速参数：扫描语义恒定本机直连（N25 豁免口径同源），
//     参数面越小，可被误用的面越小。
//
// 输出纪律：自由文本（banner/备注/主机名）逐字段 logging.RedactPII 家族口径
// 打码后才出机；结构化的 ip/mac 是工具核心结论、原样保留（与 sysinfo full 档
// 披露本机 IP/MAC 同谱），工具描述与 GUI 风险行如实声明"结果含网络拓扑"。
// 行数上限：LAN 设备 ≤256 行，超出 truncated=true（预算耗尽 ≠ 无结果）。

const (
	maxPortscanPorts   = 256                     // 单轮端口数硬上限（超限拒绝）
	minPortTimeout     = 100 * time.Millisecond  // 单端口超时钳制下界
	maxPortTimeout     = 3000 * time.Millisecond // 单端口超时钳制上界
	defaultPortTimeout = 600 * time.Millisecond  // 与引擎 DefaultScanTimeout 同源
	portscanBudget     = 60 * time.Second        // 整轮墙钟预算（超时=部分结果+completed=false）
	maxLanTargets      = 1024                    // 局域网单轮地址数上限（约 /22，严于引擎的 /20）
	maxLanDeviceRows   = 256                     // 下发设备行数上限
)

// errScanBusy 是两扫描工具共用的"MCP 面已有扫描在途"忙错误（如实报忙，不排队
// 不顶替），handler 原样把 Error() 文案作为指引错误返回。
var errScanBusy = errors.New("已有上一轮扫描在途（本工具的 MCP 面单飞互斥），请等待其结束后重试")

// portScanOutcome 一轮有界端口探测的结果载荷。
type portScanOutcome struct {
	Target     string                // 实际探测目标（回显）
	TotalPorts int                   // 本轮端口总数（去重后）
	OpenPorts  []portscan.PortResult // 开放端口明细（升序）
	DurationMs int64                 // 引擎自报耗时
	Completed  bool                  // false=预算/取消打断，只拿到部分结论（必须如实标注）
}

// PortProber 是 hanxi_portscan_scan 的后端能力面（真 = portScanBackend；单测注入假件）。
// ctx 承载整轮墙钟预算；忙时返回 errScanBusy。
type PortProber interface {
	Probe(ctx context.Context, target string, ports []int, perPortTimeout time.Duration, deepDetect bool) (portScanOutcome, error)
}

// portScanBackend 直构 portscan.Scanner（与 GUI 同一探测引擎、零状态分歧），
// 自带 MCP 面单飞闸。刻意不走 PortScanService.StartScan：其"新一轮顶掉旧任务"
// 语义属 GUI 交互（用户在界面里改主意），无头模型并发调用下会把前一轮扫描取消
// 成被误标完整的半截结果；notify/事件推送亦非无头所需。无头门禁已由
// registryGate.Acquire("portscan") 在中间件层承担（envcheck 直构同款口径），
// 本实例不属 registry 租约账，不构成旁路。
type portScanBackend struct {
	scanner *portscan.Scanner
	busy    atomic.Bool
}

func newPortScanBackend() *portScanBackend {
	return &portScanBackend{scanner: portscan.NewScanner()}
}

func (b *portScanBackend) Probe(ctx context.Context, target string, ports []int, perPortTimeout time.Duration, deepDetect bool) (portScanOutcome, error) {
	if !b.busy.CompareAndSwap(false, true) {
		return portScanOutcome{}, errScanBusy
	}
	defer b.busy.Store(false)
	// 参数固定口径：不走代理（""=直连，N25 豁免登记同谱）、并发用引擎默认 30、
	// 无微限速（单目标小端口集无须防封语义）。progressCallback 传 nil：无头无事件面。
	summary, err := b.scanner.ExecuteScan(ctx, "mcp", target, ports, "", perPortTimeout,
		portscan.DefaultScanConcurrency, 0, deepDetect, nil)
	if err != nil {
		return portScanOutcome{}, err
	}
	out := portScanOutcome{
		Target:     target,
		TotalPorts: len(ports),
		OpenPorts:  summary.OpenPorts,
		DurationMs: summary.DurationMs,
		Completed:  true,
	}
	// ctx 已死（预算/客户端取消）= 部分结论：引擎把未探测端口计为 closed 静默收尾，
	// 绝不可对外宣称"扫描完成"。
	if ctx.Err() != nil {
		out.Completed = false
	}
	return out, nil
}

// LanProber 是 hanxi_lan_scan 的后端能力面（真 = lanProbe；单测注入假件）。
// 两方法都对接同一 LanService：CountTargets 借 lan 包同一解析面做有界预检
// （不在 MCP 侧重复网段数学），ScanDevices 复用模块单飞闸与动态超时。
type LanProber interface {
	CountTargets(target string) (int, error)
	ScanDevices(target string) ([]lan.DeviceInfo, error)
}

// lanProbe 真接线：与 GUI 同一 LanService 实例（registry 内模块经 Service()
// 取用），单飞、动态超时、ARP 补全全部同谱；模块调用门已由 registryGate 在
// 中间件层完成，service 内部 holder.Enter 的二次取租约有 sysinfo 同款先例。
type lanProbe struct{ svc *lan.LanService }

func (p lanProbe) CountTargets(target string) (int, error) { return lan.CountTargets(target) }

func (p lanProbe) ScanDevices(target string) ([]lan.DeviceInfo, error) {
	devices, err := p.svc.Scan(target)
	if errors.Is(err, lan.ErrScanInProgress) {
		// 引擎忙错误是英文定式；对模型换成中文指引（含 GUI 在途冲突语义：
		// 无头与 GUI 是不同进程、各自单飞，但同一无头进程内两路模型调用互斥）。
		return nil, errScanBusy
	}
	return devices, err
}

// looksLikeIPRange 识别局域网式 IP 范围（两端均可解析为 IP，如
// "10.0.0.1-10.0.0.9"），拒绝进端口扫描的 target（应走 hanxi_lan_scan）。
// 域名中的连字符（nas-home）不误伤：两端都不是 IP 即放行。
func looksLikeIPRange(target string) bool {
	parts := strings.Split(target, "-")
	if len(parts) != 2 {
		return false
	}
	return net.ParseIP(strings.TrimSpace(parts[0])) != nil && net.ParseIP(strings.TrimSpace(parts[1])) != nil
}

// buildPortScanTool 有界端口扫描（授权键 portscan）。
func buildPortScanTool(deps Deps) (mcp.Tool, server.ToolHandlerFunc) {
	tool := mcp.NewTool(toolPortScan,
		mcp.WithDescription("对单个目标（IP 或域名）做一轮有界的主动端口探测（只读语义：不修改本机与目标任何状态，"+
			"仅 connect/横幅级网络探测，但属于出网动作）。有界口径：单目标（不支持网段）、端口去重后最多 256 个、"+
			"单端口超时 100~3000ms（越界自动钳制）、整轮 60 秒预算（超时返回部分结果并置 completed=false，"+
			"此时结论不完整，请缩小端口集重调）。同一时刻 MCP 面只允许一轮扫描在途，忙时返回指引错误而非排队。"+
			"deep_detect=true 时对开放端口追抓服务横幅（banner 属自由文本，已逐行打码：token/密码/IPv4/邮箱/密钥前缀）。"+
			"注意：开放端口的端口号/服务名/延迟会原样进入云端模型上下文。"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithString("target", mcp.Required(), mcp.Description("单个目标：IPv4/IPv6 地址或域名（如 192.168.1.1、nas.local；不接受 CIDR/网段，网段探测请用 hanxi_lan_scan）")),
		mcp.WithString("ports", mcp.Required(), mcp.Description(`端口表达式："22,80,443,8000-8010" 形态（1-65535，去重后 ≤256 个，超限整体拒绝并给指引）`)),
		mcp.WithNumber("timeout_ms", mcp.Description("单端口连接超时 ms（自动钳制到 100-3000，默认 600）")),
		mcp.WithBoolean("deep_detect", mcp.Description("是否对开放端口做服务横幅指纹探测（默认 false；结果更多但更慢）")),
	)
	handler := func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if deps.PortScan == nil {
			return mcp.NewToolResultError("portscan 后端未装配：请确认以 hanxi mcp 无头模式运行本服务"), nil
		}
		target, err := req.RequireString("target")
		if err != nil {
			return mcp.NewToolResultError("缺少参数 target（目标不能为空）"), nil
		}
		target = strings.TrimSpace(target)
		if target == "" || strings.ContainsAny(target, "/ \t") || looksLikeIPRange(target) {
			return mcp.NewToolResultError("target 必须是单个 IP 或域名（本工具仅支持单目标，网段/IP 范围扫描请用 hanxi_lan_scan）"), nil
		}
		portsRaw, err := req.RequireString("ports")
		if err != nil {
			return mcp.NewToolResultError("缺少参数 ports（端口表达式不能为空，如 \"22,80,443\"）"), nil
		}
		ports, perr := portscan.ParsePortRange(portsRaw)
		if perr != nil {
			return mcp.NewToolResultError(fmt.Sprintf("端口表达式无效：%v", perr)), nil
		}
		if len(ports) > maxPortscanPorts {
			return mcp.NewToolResultError(fmt.Sprintf(
				"端口集过大：去重后 %d 个，本工具上限 %d 个。为保证『开放端口』结论完整，超限整体拒绝（不静默截断）；"+
					"请拆成多个 ≤%d 端口的批次分批调用", len(ports), maxPortscanPorts, maxPortscanPorts)), nil
		}
		perPort := defaultPortTimeout
		if v := req.GetInt("timeout_ms", 0); v > 0 {
			perPort = time.Duration(v) * time.Millisecond
			if perPort < minPortTimeout {
				perPort = minPortTimeout
			}
			if perPort > maxPortTimeout {
				perPort = maxPortTimeout
			}
		}
		deep := req.GetBool("deep_detect", false)

		scanCtx, cancel := context.WithTimeout(ctx, portscanBudget)
		defer cancel()
		out, err := deps.PortScan.Probe(scanCtx, target, ports, perPort, deep)
		if err != nil {
			if errors.Is(err, errScanBusy) {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultError(fmt.Sprintf("端口扫描失败: %v", err)), nil
		}
		items := make([]any, 0, len(out.OpenPorts))
		for _, p := range out.OpenPorts {
			// banner 是外部自由文本（最不可控来源），逐字段过 RedactPII 才出机；
			// service 亦可能来自 HTTP Server 头等外部输入，同样打码。
			entry := resultPayload{
				"port":      p.Port,
				"status":    string(p.Status),
				"latencyMs": p.LatencyMs,
			}
			if s := logging.RedactPII(p.Service); s != "" {
				entry["service"] = s
			}
			if b := logging.RedactPII(p.Banner); b != "" {
				entry["banner"] = b
			}
			items = append(items, entry)
		}
		payload := resultPayload{
			"target":     out.Target,
			"totalPorts": out.TotalPorts,
			"openCount":  len(out.OpenPorts),
			"durationMs": out.DurationMs,
			"completed":  out.Completed,
			"truncated":  false,
			"openPorts":  items,
		}
		if !out.Completed {
			payload["note"] = fmt.Sprintf(
				"本轮在 %v 墙钟预算内未探完全部端口，以上仅为已确认的开放端口，结论不完整（不要把未列出的端口判定为关闭）；"+
					"建议缩小端口集或降低 timeout_ms 后重调", portscanBudget)
		}
		return textResult(payload)
	}
	return tool, handler
}

// buildLanScanTool 有界局域网扫描（授权键 lan）。
func buildLanScanTool(deps Deps) (mcp.Tool, server.ToolHandlerFunc) {
	tool := mcp.NewTool(toolLanScan,
		mcp.WithDescription("对指定 IPv4 网段/IP 范围做一轮有界的在线设备探测（只读语义：ping/ARP 式可达性探测，"+
			"不修改本机与任何设备状态，但属于局域网出网动作）。有界口径：单轮地址数 ≤1024（约 /22，超限拒绝并指引；"+
			"CIDR 掩码不得粗于 /20 的引擎硬限仍在）；引擎自带 10~25 秒动态超时；同一时刻仅允许一轮扫描，"+
			"忙时返回指引错误而非排队。结果含每台在线设备的 IP 与 MAC 地址（工具核心结论、原样下发）及 RTT，"+
			"主机名/用户备注属自由文本已打码——网络拓扑信息会进入云端模型上下文，请确认后再授权。"+
			"设备行数上限 256，超出置 truncated=true（截断 ≠ 无更多设备）。"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithOpenWorldHintAnnotation(true),
		mcp.WithString("target", mcp.Required(), mcp.Description("扫描范围：CIDR（如 192.168.1.0/24）、IP 范围（如 10.0.0.1-10.0.0.50）或单个 IP（自动补全所在 /24）")),
	)
	handler := func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if deps.Lan == nil {
			return mcp.NewToolResultError("lan 后端未装配：请确认以 hanxi mcp 无头模式运行本服务"), nil
		}
		target, err := req.RequireString("target")
		if err != nil {
			return mcp.NewToolResultError("缺少参数 target（扫描范围不能为空）"), nil
		}
		target = strings.TrimSpace(target)
		count, err := deps.Lan.CountTargets(target)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("扫描范围无效：%v", err)), nil
		}
		if count > maxLanTargets {
			return mcp.NewToolResultError(fmt.Sprintf(
				"目标地址数 %d 超出本工具上限 %d（约 /22）。为避免对局域网的大规模并发探测，超限整体拒绝；"+
					"请拆成更小的网段/IP 范围分批扫描", count, maxLanTargets)), nil
		}
		// 引擎自限：>512 地址内部超时 25s、否则 10s，无须 MCP 侧再叠墙钟；
		// ctx 仅作客户端断开的传导上限（handler 返回即释放）。
		devices, err := deps.Lan.ScanDevices(target)
		if err != nil {
			if errors.Is(err, errScanBusy) {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return mcp.NewToolResultError(fmt.Sprintf("局域网扫描失败: %v", err)), nil
		}
		rows := devices
		capped := len(devices) >= maxLanDeviceRows
		if capped {
			rows = devices[:maxLanDeviceRows]
		}
		items := make([]any, 0, len(rows))
		for _, d := range rows {
			// IP/MAC 是工具核心结论（授权本键即知情放行，描述已声明），原样结构化；
			// 主机名/备注属自由文本，逐字段 RedactPII。
			entry := resultPayload{"ip": d.IP, "mac": d.MAC, "rttMs": d.RTTMs}
			if h := logging.RedactPII(d.Hostname); h != "" {
				entry["hostname"] = h
			}
			if r := logging.RedactPII(d.Remark); r != "" {
				entry["remark"] = r
			}
			if d.IsSelf {
				entry["isSelf"] = true
			}
			if d.IsGateway {
				entry["isGateway"] = true
			}
			items = append(items, entry)
		}
		// 拿满行数上限：大概率还有更多设备，如实置 truncated。
		return listResult(items, capped)
	}
	return tool, handler
}
