package mcpwizard

// 安装前自检（PLAN_MCP §2.5「安装前自检」，R2 偿 F4b 刻意留的口子）：
// 写入客户端配置之前，Go 侧 spawn 自家 `hanxi mcp`（os.Executable + serverArgs）
// 走一遍 initialize → tools/list，拿到非空工具清单即判健康；失败（超时/早退/
// 协议污染）红字警示 + 指引。
//
// 阻断性裁定（本包对 §2.5「先验证后写配置」的落地口径）：自检失败**不阻断**
// 写入链——写进客户端配置的条目在客户端真正拉起子进程之前完全惰性，且工具面
// 另有 access.json fail-closed 门（未授权什么也不通）；§8/包注释的三条 fail-closed
// 红线全部是"文件安全"性质，运行时健康不属其列。硬阻断会把杀毒误拦截等瞬时
// 环境问题变成"想写也写不进去"的死胡同，故预览页呈现醒目红字与排查指引，
// 由用户知情后决定继续或放弃。
//
// 依赖边界不变：本包不 import internal/mcp 也不 import mcp-go——握手帧按线上
// 协议（newline-delimited JSON-RPC）自写自发，与 F4a 只共享"字面协议"不共享
// 编译期符号（F4a/F4b 拆分纪律）。协议版本号是唯一软耦合点，升级 mcp-go 时
// 需同步 probeProtocolVersion（v0.41.1 的 LATEST_PROTOCOL_VERSION 即此值）。

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// 握手协议版本（对齐 mcp-go v0.41.1 的 LATEST_PROTOCOL_VERSION）。
const probeProtocolVersion = "2025-06-18"

// 自检结论两态（前端按字面量映射徽章；"进行中"是前端等待 promise 的本地态）。
const (
	checkStateOK     = "ok"
	checkStateFailed = "failed"
)

// 缓存与超时口径：TTL 覆盖一次向导会话（三客户端连开预览只 spawn 一次）；
// 超时是整场握手（spawn + initialize + tools/list）的硬预算——hanxi mcp 冷启动
// 要过 settings/windows.New/registry 构造链，正常 1~3s，给 15s 富余。
const (
	selfCheckTTL     = 90 * time.Second
	selfCheckTimeout = 15 * time.Second

	// WaitDelay 限制子进程退出后仍被后代继承的 stderr 管道；reapTimeout 则只给
	// deadline 强杀后的 cmd.Wait 协程一段固定收尸窗口，窗口耗尽后调用方直接返回。
	probeWaitDelay   = time.Second
	probeReapTimeout = 3 * time.Second
)

// SelfCheckInfo 安装前自检结果（Wails 绑定 DTO）。Message 恒非空：
// 成功=结论一句话，失败=原因（指引文案由前端固定补充）。
type SelfCheckInfo struct {
	State     string   `json:"state"`     // ok / failed
	ToolCount int      `json:"toolCount"` // tools/list 工具数（失败为 0）
	Tools     []string `json:"tools"`     // 工具名清单（可发现性展示）
	Message   string   `json:"message"`
	CheckedAt string   `json:"checkedAt"` // 真实 spawn 时刻（RFC3339；缓存复用仍是原时刻）
	Fresh     bool     `json:"fresh"`     // true=本次调用真 spawn；false=TTL 内缓存复用
}

// probeBudget 握手超时预算（字段未注入时取默认；测试缩短用）。
func (s *McpWizardService) probeBudget() time.Duration {
	if s.probeTimeout > 0 {
		return s.probeTimeout
	}
	return selfCheckTimeout
}

// probeRunner 是「起子进程走一遍握手拿工具清单」的注入缝：
// 生产接线 = (*McpWizardService).spawnProbe（真实 exec，管杀管埋）；
// 单测注入健康/超时/坏协议剧本假件（真 spawn 另有一发 cmd.exe 早退冒烟兜底）。
type probeRunner func(ctx context.Context) ([]string, error)

// SelfCheck 执行（或复用）一次安装前自检。refresh=true 忽略缓存强制重 spawn
// （前端「重新自检」按钮）。返回的 error 仅代表编程错误，业务失败在
// State=failed + Message 里表达——绑定面友好，前端无须 try/catch 双轨。
// 全程互斥串行：同刻至多一个自检子进程（并发预览共用结果）。
func (s *McpWizardService) SelfCheck(refresh bool) (SelfCheckInfo, error) {
	s.checkMu.Lock()
	defer s.checkMu.Unlock()
	if !refresh && s.check != nil && s.now().Sub(s.checkAt) < selfCheckTTL {
		c := *s.check
		c.Fresh = false
		c.Tools = append([]string(nil), s.check.Tools...)
		return c, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), s.probeBudget())
	defer cancel()
	tools, err := s.probe(ctx)
	info := SelfCheckInfo{CheckedAt: s.now().Format(time.RFC3339), Fresh: true}
	switch {
	case err != nil:
		info.State = checkStateFailed
		info.Message = err.Error()
	case len(tools) == 0:
		info.State = checkStateFailed
		info.Message = "握手成功但 tools/list 返回空清单——server 版本异常，请确认 hanxi.exe 未被半路替换"
	default:
		info.State = checkStateOK
		info.Tools = tools
		info.ToolCount = len(tools)
		info.Message = fmt.Sprintf("hanxi mcp 握手成功，%d 件工具就位", len(tools))
	}
	s.check = &info
	s.checkAt = s.now()
	out := info
	return out, nil
}

type probeOut struct {
	tools []string
	err   error
}

var probeCommand = func(ctx context.Context, command string) *exec.Cmd {
	return exec.CommandContext(ctx, command, serverArgs...)
}

// spawnProbe 生产真接线：exec.CommandContext 拉起当前 exe + ["mcp"]，
// stdin/stdout 双管道；probe 与进程退出共同参与协调。cmd.Wait 只由唯一 wait
// goroutine 调用，任何分支都不再同步 Wait。ctx 截止后显式 Kill 并关闭双管道，
// 只在固定窗口内等 wait goroutine 收尸；到期立即返回，避免病态子进程拖死向导。
// WaitDelay 兜住子进程已退、后代却继承 stderr 管道的场景。子进程日志截尾附进
// 失败原因供排障（#63：stdout 必须纯协议）。
func (s *McpWizardService) spawnProbe(ctx context.Context) ([]string, error) {
	if s.command == "" {
		return nil, errors.New("无法解析 hanxi 可执行文件路径（os.Executable 失败）")
	}
	cmd := probeCommand(ctx, s.command)
	cmd.SysProcAttr = hideChildWindow()
	cmd.WaitDelay = probeWaitDelay
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("建立 stdin 管道失败: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("建立 stdout 管道失败: %w", err)
	}
	var stderr syncBuffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, fmt.Errorf("拉起 hanxi mcp 子进程失败: %w", err)
	}

	probeDone := make(chan probeOut, 1)
	go func() {
		tools, perr := probeStdio(stdin, stdout)
		probeDone <- probeOut{tools: tools, err: perr}
	}()
	waitDone := make(chan error, 1)
	go func() { waitDone <- cmd.Wait() }()

	var res probeOut
	select {
	case res = <-probeDone:
		_ = stdin.Close()
		select {
		case exitErr := <-waitDone:
			return finishProbe(res, exitErr, &stderr)
		case <-ctx.Done():
			return finishTimedOutProbe(cmd, stdin, stdout, waitDone, &stderr, s.probeBudget())
		}
	case exitErr := <-waitDone:
		_ = stdin.Close()
		_ = stdout.Close()
		select {
		case res = <-probeDone:
		default:
			res.err = errors.New("握手完成前服务退出（stdout EOF）")
		}
		return finishProbe(res, exitErr, &stderr)
	case <-ctx.Done():
		return finishTimedOutProbe(cmd, stdin, stdout, waitDone, &stderr, s.probeBudget())
	}
}

func finishTimedOutProbe(cmd *exec.Cmd, stdin io.Closer, stdout io.Closer, waitDone <-chan error, stderr *syncBuffer, budget time.Duration) ([]string, error) {
	// CommandContext 也会发 Kill；这里显式补发并主动关管道，让仍阻塞在
	// read/write 的 probe 立刻松开。收尸只等固定窗口，绝不再同步 Wait。
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	_ = stdin.Close()
	_ = stdout.Close()
	timer := time.NewTimer(probeReapTimeout)
	defer timer.Stop()
	select {
	case <-waitDone:
	case <-timer.C:
	}
	res := probeOut{err: fmt.Errorf("握手超时（%s 内未完成 initialize→tools/list，子进程已终止）", budget)}
	return finishProbe(res, nil, stderr)
}

func finishProbe(res probeOut, exitErr error, stderr *syncBuffer) ([]string, error) {
	if res.err != nil {
		if tail := stderr.Tail(200); tail != "" {
			return nil, fmt.Errorf("%w；子进程 stderr: %s", res.err, tail)
		}
		if exitErr != nil {
			return nil, fmt.Errorf("%w；子进程异常退出（%v）", res.err, exitErr)
		}
		return nil, res.err
	}
	if exitErr != nil {
		// 应答齐全但退出码非零：会话收尾崩了，诚实报失败（工具清单照常附上）
		return res.tools, fmt.Errorf("握手完成但 hanxi mcp 非零退出（%v）", exitErr)
	}
	return res.tools, nil
}

// probeStdio 在已建立的管道对上当 MCP 客户端跑最小握手：
// initialize → notifications/initialized → tools/list，返回工具名清单。
// 纯 io 抽象（帧级测试用 io.Pipe 假应答器驱动，角色与 internal/mcp 的
// TestStdioFrameLoop 恰好互补）。写端绝不 linger：每步 awaitResult 读完
// 对应 id 应答就前进，避免 io.Pipe 无缓冲写阻塞（#63 教训的客户端版）。
func probeStdio(stdin io.Writer, stdout io.Reader) ([]string, error) {
	in := bufio.NewScanner(stdout)
	in.Buffer(make([]byte, 0, 64*1024), 4*1024*1024) // 工具清单帧很小，上限防垃圾刷屏

	initReq := fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":%q,"capabilities":{},"clientInfo":{"name":"hanxi-wizard-selfcheck","version":"0"}}}`, probeProtocolVersion)
	if err := writeFrame(stdin, initReq); err != nil {
		return nil, fmt.Errorf("发送 initialize 失败: %w", err)
	}
	raw, err := awaitResult(in, 1)
	if err != nil {
		return nil, fmt.Errorf("initialize 握手失败: %w", err)
	}
	var init struct {
		ServerInfo struct {
			Name string `json:"name"`
		} `json:"serverInfo"`
	}
	if err := json.Unmarshal(raw, &init); err != nil {
		return nil, fmt.Errorf("initialize 应答异常: %w", err)
	}
	if init.ServerInfo.Name != entryName {
		return nil, fmt.Errorf("应答方 serverInfo.name=%q，并非 hanxi mcp 本尊（端口/管道被第三方占用？）", init.ServerInfo.Name)
	}
	if err := writeFrame(stdin, `{"jsonrpc":"2.0","method":"notifications/initialized"}`); err != nil {
		return nil, fmt.Errorf("发送 initialized 通知失败: %w", err)
	}
	if err := writeFrame(stdin, `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`); err != nil {
		return nil, fmt.Errorf("发送 tools/list 失败: %w", err)
	}
	raw, err = awaitResult(in, 2)
	if err != nil {
		return nil, fmt.Errorf("tools/list 失败: %w", err)
	}
	var list struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, fmt.Errorf("tools/list 应答异常: %w", err)
	}
	names := make([]string, 0, len(list.Tools))
	for _, t := range list.Tools {
		if t.Name != "" {
			names = append(names, t.Name)
		}
	}
	return names, nil
}

// rpcFrame 客户端视角的最小帧形态：只关心 id/result/error，通知与其他 id 跳过。
type rpcFrame struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.Number     `json:"id"`
	Method  string          `json:"method"`
	Result  json.RawMessage `json:"result"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// awaitResult 逐行读 stdout 直到 id==want 的应答帧。非 JSON 行即协议污染
// （"stdout 即协议"纪律的客户端执法——GUI 宿主绝不会被垃圾文本糊脸）；
// EOF 先于应答 = 服务早退。
func awaitResult(in *bufio.Scanner, want int64) (json.RawMessage, error) {
	for in.Scan() {
		line := bytes.TrimSpace(in.Bytes())
		if len(line) == 0 {
			continue
		}
		var f rpcFrame
		if err := json.Unmarshal(line, &f); err != nil {
			return nil, fmt.Errorf("stdout 出现非协议行（污染）: %q", snippet(string(line), 120))
		}
		if f.JSONRPC != "2.0" {
			return nil, fmt.Errorf("非 JSON-RPC 2.0 帧: %q", snippet(string(line), 120))
		}
		n, err := f.ID.Int64()
		if err != nil || n != want {
			continue // 无 id（通知）或别的 id（乱序/多路）——等我要的那条
		}
		if f.Error != nil {
			return nil, fmt.Errorf("JSON-RPC 错误 %d: %s", f.Error.Code, f.Error.Message)
		}
		if len(f.Result) == 0 {
			return nil, errors.New("应答帧缺少 result 字段")
		}
		return f.Result, nil
	}
	if err := in.Err(); err != nil {
		return nil, fmt.Errorf("读取 stdout 失败: %w", err)
	}
	return nil, errors.New("握手完成前服务退出（stdout EOF）")
}

func writeFrame(w io.Writer, frame string) error {
	_, err := io.WriteString(w, frame+"\n")
	return err
}

func snippet(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}

// syncBuffer 并发安全地收集子进程 stderr 尾部（exec 泵协程与结果组装并发读写）。
type syncBuffer struct {
	mu  sync.Mutex
	buf []byte
}

const syncBufferKeep = 4096

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf = append(b.buf, p...)
	if len(b.buf) > syncBufferKeep {
		b.buf = append(b.buf[:0], b.buf[len(b.buf)-syncBufferKeep:]...)
	}
	return len(p), nil
}

// Tail 返回最后 n 个 rune 的文本（已去首尾空白），空缓冲返回 ""。
func (b *syncBuffer) Tail(n int) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.TrimSpace(snippet(string(b.buf), n))
}
