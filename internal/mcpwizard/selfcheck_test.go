package mcpwizard

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// —— probeStdio 帧级用例：假「服务端」经 io.Pipe 应答 ——
//
// #63 教训的客户端镜像：probeStdio 与假服务端各持一端，测试结束前两侧
// 写端必须关（假服务端见到 stdin EOF 即关 stdout），否则扫描器永挂。
// 所有用例都套 watchdog select，卡死给出明确失败而非包级超时。

type fakeFrame struct {
	ID     json.Number `json:"id"`
	Method string      `json:"method"`
}

// startFakeServer 起一个脚本化的握手应答协程。handle 收到每一帧请求行，
// 自行决定写回什么（或不写）；stdin EOF 后自动关 stdout 收尾。
func startFakeServer(in io.Reader, out *io.PipeWriter, t *testing.T, handle func(line string)) {
	t.Helper()
	go func() {
		defer out.Close()
		sc := bufio.NewScanner(in)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" {
				continue
			}
			var f fakeFrame
			if err := json.Unmarshal([]byte(line), &f); err != nil {
				continue // 假服务端同样只在被要求污染剧本时吐垃圾
			}
			if f.Method == "notifications/initialized" {
				continue // 通知无应答
			}
			handle(line)
		}
	}()
}

func initOKFrame(name string) string {
	res, _ := json.Marshal(map[string]any{
		"protocolVersion": probeProtocolVersion,
		"capabilities":    map[string]any{},
		"serverInfo":      map[string]any{"name": name, "version": "test"},
	})
	return `{"jsonrpc":"2.0","id":1,"result":` + string(res) + `}`
}

func toolsOKFrame(names ...string) string {
	tools := make([]any, 0, len(names))
	for _, n := range names {
		tools = append(tools, map[string]any{"name": n, "description": "x"})
	}
	res, _ := json.Marshal(map[string]any{"tools": tools})
	return `{"jsonrpc":"2.0","id":2,"result":` + string(res) + `}`
}

// runProbe 在 io.Pipe 对上跑 probeStdio，带 watchdog（管道环节卡死即明确失败）。
func runProbe(t *testing.T, stdinW io.Writer, stdoutR io.Reader) ([]string, error) {
	t.Helper()
	type out struct {
		tools []string
		err   error
	}
	ch := make(chan out, 1)
	go func() { tools, err := probeStdio(stdinW, stdoutR); ch <- out{tools, err} }()
	select {
	case res := <-ch:
		return res.tools, res.err
	case <-time.After(15 * time.Second):
		t.Fatal("probeStdio 未在 15s 内返回（管道死锁？）")
		return nil, nil
	}
}

func TestProbeStdioHealthy(t *testing.T) {
	clientInR, clientInW := io.Pipe()   // 我们写 → 假服务端读
	serverOutR, serverOutW := io.Pipe() // 假服务端写 → 我们读
	startFakeServer(clientInR, serverOutW, t, func(line string) {
		var f fakeFrame
		_ = json.Unmarshal([]byte(line), &f)
		switch f.ID.String() {
		case "1":
			_, _ = io.WriteString(serverOutW, initOKFrame("hanxi")+"\n")
		case "2":
			_, _ = io.WriteString(serverOutW, toolsOKFrame("hanxi_envcheck_detect", "hanxi_file_search", "hanxi_ocr_recognize", "hanxi_memo_search")+"\n")
			_ = clientInW.Close() // 答完即收线：假服务端见 stdin EOF 关 stdout
		}
	})
	tools, err := runProbe(t, clientInW, serverOutR)
	if err != nil {
		t.Fatalf("健康握手应成功: %v", err)
	}
	if len(tools) != 4 || tools[0] != "hanxi_envcheck_detect" || tools[3] != "hanxi_memo_search" {
		t.Fatalf("工具清单异常: %v", tools)
	}
}

func TestProbeStdioBranches(t *testing.T) {
	cases := []struct {
		name    string
		handle  func(line string, out *io.PipeWriter, closeIn func())
		wantErr string
	}{
		{
			name: "垃圾即污染",
			handle: func(line string, out *io.PipeWriter, closeIn func()) {
				_, _ = io.WriteString(out, " hanxi mcp: 启动中……\n") // stdout 混入日志行（#63 纪律的反面）
			},
			wantErr: "非协议行",
		},
		{
			name: "早退EOF",
			handle: func(line string, out *io.PipeWriter, closeIn func()) {
				closeIn() // 服务不答话直接收线
			},
			wantErr: "stdout EOF",
		},
		{
			name: "JSON-RPC错误应答",
			handle: func(line string, out *io.PipeWriter, closeIn func()) {
				var f fakeFrame
				_ = json.Unmarshal([]byte(line), &f)
				if f.ID.String() == "1" {
					_, _ = io.WriteString(out, `{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"服务初始化失败"}}`+"\n")
				}
			},
			wantErr: "服务初始化失败",
		},
		{
			name: "李鬼serverInfo",
			handle: func(line string, out *io.PipeWriter, closeIn func()) {
				var f fakeFrame
				_ = json.Unmarshal([]byte(line), &f)
				if f.ID.String() == "1" {
					_, _ = io.WriteString(out, initOKFrame("evil-twin")+"\n")
				}
			},
			wantErr: "并非 hanxi mcp 本尊",
		},
		{
			name: "tools字段缺失",
			handle: func(line string, out *io.PipeWriter, closeIn func()) {
				var f fakeFrame
				_ = json.Unmarshal([]byte(line), &f)
				switch f.ID.String() {
				case "1":
					_, _ = io.WriteString(out, initOKFrame("hanxi")+"\n")
				case "2":
					_, _ = io.WriteString(out, `{"jsonrpc":"2.0","id":2,"result":{}}`+"\n")
					closeIn()
				}
			},
			wantErr: "空清单", // tools/list 缺省 tools → probeStdio 返回空列表，由 SelfCheck 定性
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clientInR, clientInW := io.Pipe()
			serverOutR, serverOutW := io.Pipe()
			var closeOnce sync.Once
			startFakeServer(clientInR, serverOutW, t, func(line string) {
				tc.handle(line, serverOutW, func() { closeOnce.Do(func() { _ = clientInW.Close() }) })
			})
			tools, err := runProbe(t, clientInW, serverOutR)
			if tc.name == "tools字段缺失" {
				// probeStdio 层空列表不是错误（协议合法），定性发生在 SelfCheck
				if err != nil {
					t.Fatalf("空 tools 应返回 nil error: %v", err)
				}
				if len(tools) != 0 {
					t.Fatalf("应空清单: %v", tools)
				}
				svc, _ := newTestService(t, nil)
				svc.probe = func(ctx context.Context) ([]string, error) { return nil, nil }
				info, _ := svc.SelfCheck(false)
				if info.State != checkStateFailed || !strings.Contains(info.Message, "空清单") {
					t.Fatalf("SelfCheck 应把空清单定性为失败: %+v", info)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("应报 %q，得 %v", tc.wantErr, err)
			}
		})
	}
}

// —— SelfCheck 三分支 + 缓存语义（注入假 probe，不真 spawn）——

func TestSelfCheckHealthyAndCache(t *testing.T) {
	svc, _ := newTestService(t, nil)
	var spawns int32
	cur := time.Date(2026, 9, 17, 12, 0, 0, 0, time.Local)
	svc.now = func() time.Time { return cur }
	svc.probe = func(ctx context.Context) ([]string, error) {
		atomic.AddInt32(&spawns, 1)
		return []string{"a", "b", "c", "d"}, nil
	}

	info, err := svc.SelfCheck(false)
	if err != nil {
		t.Fatal(err)
	}
	if info.State != checkStateOK || info.ToolCount != 4 || !info.Fresh || len(info.Tools) != 4 {
		t.Fatalf("健康态异常: %+v", info)
	}

	// 同会话（TTL 内）连开预览：吃缓存不再 spawn；Tools 副本隔离缓存本体
	info2, _ := svc.SelfCheck(false)
	if atomic.LoadInt32(&spawns) != 1 || info2.Fresh || info2.State != checkStateOK || info2.ToolCount != 4 {
		t.Fatalf("TTL 内应复用缓存: %+v spawns=%d", info2, spawns)
	}

	// 强制刷新：再 spawn
	if info3, _ := svc.SelfCheck(true); !info3.Fresh || atomic.LoadInt32(&spawns) != 2 {
		t.Fatalf("refresh 应强制重 spawn: %+v spawns=%d", info3, spawns)
	}

	// TTL 过期：自动重 spawn
	cur = cur.Add(selfCheckTTL + time.Second)
	if info4, _ := svc.SelfCheck(false); !info4.Fresh || atomic.LoadInt32(&spawns) != 3 {
		t.Fatalf("TTL 过期应重 spawn: %+v spawns=%d", info4, spawns)
	}
}

func TestSelfCheckTimeoutBranch(t *testing.T) {
	svc, _ := newTestService(t, nil)
	var spawns int32
	svc.probeTimeout = 60 * time.Millisecond
	svc.probe = func(ctx context.Context) ([]string, error) {
		atomic.AddInt32(&spawns, 1)
		<-ctx.Done() // 模拟子进程挂死：只有预算能救场
		return nil, fmt.Errorf("握手超时（%v 内未完成 initialize→tools/list，子进程已终止）", svc.probeBudget())
	}
	start := time.Now()
	info, err := svc.SelfCheck(false)
	if err != nil {
		t.Fatal(err)
	}
	if info.State != checkStateFailed || !strings.Contains(info.Message, "超时") {
		t.Fatalf("超时应定性失败: %+v", info)
	}
	if el := time.Since(start); el > 5*time.Second {
		t.Fatalf("超时未被预算约束: %v", el)
	}
	// 失败结果同样缓存（refresh 可越）
	if info2, _ := svc.SelfCheck(false); info2.Fresh || atomic.LoadInt32(&spawns) != 1 {
		t.Fatalf("失败也应缓存: %+v spawns=%d", info2, spawns)
	}
	if info3, _ := svc.SelfCheck(true); !info3.Fresh || atomic.LoadInt32(&spawns) != 2 {
		t.Fatalf("失败后 refresh 应重试: spawns=%d", spawns)
	}
}

func TestSelfCheckBadProtocolBranch(t *testing.T) {
	svc, _ := newTestService(t, nil)
	svc.probe = func(ctx context.Context) ([]string, error) {
		return nil, errors.New(`initialize 握手失败: stdout 出现非协议行（污染）: "hanxi mcp: 启动中……"`)
	}
	info, _ := svc.SelfCheck(false)
	if info.State != checkStateFailed || !strings.Contains(info.Message, "非协议行") || info.ToolCount != 0 {
		t.Fatalf("坏协议态异常: %+v", info)
	}
}

func TestSelfCheckSerializesConcurrentCallers(t *testing.T) {
	svc, _ := newTestService(t, nil)
	var spawns int32
	svc.probe = func(ctx context.Context) ([]string, error) {
		atomic.AddInt32(&spawns, 1)
		time.Sleep(80 * time.Millisecond)
		return []string{"a"}, nil
	}
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			info, err := svc.SelfCheck(false)
			if err != nil || info.State != checkStateOK {
				t.Errorf("并发自检异常: %+v %v", info, err)
			}
		}()
	}
	wg.Wait()
	if n := atomic.LoadInt32(&spawns); n != 1 {
		t.Fatalf("并发预览应只 spawn 一次，得 %d", n)
	}
}

// —— spawnProbe 真实 exec 面（测试进程 helper 覆盖正常预算、晚归与超时强杀；
// cmd.exe 冒充坏协议子进程继续兜住真实早退路径）——

func TestSelfCheckHelperProcess(t *testing.T) {
	if os.Getenv("HANXI_SELFCHECK_HELPER") != "1" {
		return
	}
	switch os.Getenv("HANXI_SELFCHECK_MODE") {
	case "healthy":
		helperServeMCP(0)
	case "late":
		helperServeMCP(250 * time.Millisecond)
	case "late-exit":
		if pidFile := os.Getenv("HANXI_SELFCHECK_PID_FILE"); pidFile != "" {
			_ = os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())), 0o600)
		}
		time.Sleep(250 * time.Millisecond)
		os.Exit(0)
	case "hang":
		if pidFile := os.Getenv("HANXI_SELFCHECK_PID_FILE"); pidFile != "" {
			_ = os.WriteFile(pidFile, []byte(strconv.Itoa(os.Getpid())), 0o600)
		}
		for {
			time.Sleep(time.Hour)
		}
	}
	os.Exit(2)
}

func helperServeMCP(delay time.Duration) {
	sc := bufio.NewScanner(os.Stdin)
	enc := json.NewEncoder(os.Stdout)
	for sc.Scan() {
		var f fakeFrame
		if json.Unmarshal(sc.Bytes(), &f) != nil {
			continue
		}
		if delay > 0 {
			time.Sleep(delay)
			delay = 0
		}
		switch f.ID.String() {
		case "1":
			var res any
			_ = json.Unmarshal([]byte(initOKFrame("hanxi")), &res)
			_ = enc.Encode(res)
		case "2":
			var res any
			_ = json.Unmarshal([]byte(toolsOKFrame("a", "b", "c", "d")), &res)
			_ = enc.Encode(res)
			_ = os.Stdout.Close()
			os.Exit(0)
		}
	}
}

func helperProbeService(t *testing.T, mode string) *McpWizardService {
	t.Helper()
	old := probeCommand
	probeCommand = func(ctx context.Context, command string) *exec.Cmd {
		cmd := exec.CommandContext(ctx, command, "-test.run=^TestSelfCheckHelperProcess$")
		cmd.Env = append(os.Environ(), "HANXI_SELFCHECK_HELPER=1", "HANXI_SELFCHECK_MODE="+mode)
		return cmd
	}
	t.Cleanup(func() { probeCommand = old })
	svc, _ := newTestService(t, nil)
	svc.command = os.Args[0]
	return svc
}

func TestSpawnProbeHelperBudgetAndLateReturn(t *testing.T) {
	for _, tc := range []struct {
		name    string
		mode    string
		budget  time.Duration
		wantErr bool
	}{
		{name: "预算内完成", mode: "healthy", budget: 2 * time.Second},
		{name: "晚归越界", mode: "late", budget: 50 * time.Millisecond, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := helperProbeService(t, tc.mode)
			ctx, cancel := context.WithTimeout(context.Background(), tc.budget)
			defer cancel()
			tools, err := svc.spawnProbe(ctx)
			if tc.wantErr {
				if err == nil || !strings.Contains(err.Error(), "握手超时") {
					t.Fatalf("应按预算超时，得 tools=%v err=%v", tools, err)
				}
				return
			}
			if err != nil || len(tools) != 4 {
				t.Fatalf("预算内 helper 应成功，得 tools=%v err=%v", tools, err)
			}
		})
	}
}

func TestSpawnProbeHelperTimeoutLeavesNoProcess(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("PID 残留探测仅 Windows")
	}
	for _, mode := range []string{"late-exit", "hang"} {
		t.Run(mode, func(t *testing.T) {
			pidFile := filepath.Join(t.TempDir(), "pid")
			old := probeCommand
			probeCommand = func(ctx context.Context, command string) *exec.Cmd {
				cmd := exec.CommandContext(ctx, command, "-test.run=^TestSelfCheckHelperProcess$")
				cmd.Env = append(os.Environ(), "HANXI_SELFCHECK_HELPER=1", "HANXI_SELFCHECK_MODE="+mode, "HANXI_SELFCHECK_PID_FILE="+pidFile)
				return cmd
			}
			t.Cleanup(func() { probeCommand = old })
			svc, _ := newTestService(t, nil)
			svc.command = os.Args[0]
			ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
			defer cancel()
			start := time.Now()
			_, err := svc.spawnProbe(ctx)
			if err == nil || !strings.Contains(err.Error(), "握手超时") {
				t.Fatalf("%s helper 应超时: %v", mode, err)
			}
			if elapsed := time.Since(start); elapsed > probeReapTimeout+2*time.Second {
				t.Fatalf("超时返回超过固定收尸窗口: %v", elapsed)
			}
			data, readErr := os.ReadFile(pidFile)
			if readErr != nil {
				t.Fatalf("读取 helper PID: %v", readErr)
			}
			pid, convErr := strconv.Atoi(strings.TrimSpace(string(data)))
			if convErr != nil {
				t.Fatalf("解析 helper PID: %v", convErr)
			}
			if processExists(pid) {
				t.Fatalf("超时返回后 helper 仍存活: pid=%d", pid)
			}
		})
	}
}

func TestSpawnProbeRealExeBadChild(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("cmd.exe 冒烟仅 Windows")
	}
	svc, _ := newTestService(t, nil)
	svc.probeTimeout = 10 * time.Second
	svc.command = `C:\Windows\System32\cmd.exe` // 塞 "mcp" 参数 → 吐非 JSON 行/早退
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	type out struct {
		tools []string
		err   error
	}
	ch := make(chan out, 1)
	go func() { tools, err := svc.spawnProbe(ctx); ch <- out{tools, err} }()
	select {
	case res := <-ch:
		if res.err == nil {
			t.Fatalf("cmd.exe 不是 MCP server，应失败: tools=%v", res.tools)
		}
		t.Logf("坏子进程如实报告: %v", res.err)
	case <-time.After(30 * time.Second):
		t.Fatal("spawnProbe 未在 30s 内收敛（杀/关/Wait 链有洞）")
	}
}

// TestSpawnProbeKillsOnDeadline 用真实进程走超时强杀支（管杀纪律）：选
// powershell.exe——冷启动必 >80ms 且 stdout 在报错前完全静默（脚本名不合法
// 的错误走 stderr），deadline 到点时它必然还活着，只能被 ctx 杀掉收尾。
func TestSpawnProbeKillsOnDeadline(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("powershell.exe 冒烟仅 Windows")
	}
	ps, err := exec.LookPath("powershell.exe")
	if err != nil {
		// 开发 shell 常剥 PATH 里的 WindowsPowerShell 目录，走 SystemRoot 绝对路径兜底
		ps = filepath.Join(os.Getenv("SystemRoot"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
		if _, serr := os.Stat(ps); serr != nil {
			t.Skipf("无 powershell.exe: %v / %v", err, serr)
		}
	}
	svc, _ := newTestService(t, nil)
	svc.command = ps
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err = svc.spawnProbe(ctx)
	el := time.Since(start)
	if err == nil || !strings.Contains(err.Error(), "握手超时") {
		t.Fatalf("应定性为握手超时: %v", err)
	}
	if el > 10*time.Second { // 含 3s 收尾窗口的预算上限
		t.Fatalf("超时收敛太慢: %v", el)
	}
}

func TestSpawnProbeNoExecutable(t *testing.T) {
	svc, _ := newTestService(t, nil)
	svc.command = ""
	if _, err := svc.spawnProbe(context.Background()); err == nil || !strings.Contains(err.Error(), "无法解析") {
		t.Fatalf("command 为空应明确报错: %v", err)
	}
}

func TestSpawnProbeRealHanxiMcp(t *testing.T) {
	// 真身端到端（有则跑、无则跳）：若恰好存在可执行的 hanxi.exe（环境变量
	// HANXI_EXE 显式指路，供人工/CI 实弹验收），spawnProbe 应报健康且工具数 ≥4。
	exe := os.Getenv("HANXI_EXE")
	if exe == "" {
		t.Skip("未设 HANXI_EXE，跳过实弹自检（#63 已证 stdio 可用）")
	}
	if _, err := exec.LookPath(exe); err != nil {
		t.Skipf("HANXI_EXE 不可执行: %v", err)
	}
	svc, _ := newTestService(t, nil)
	svc.command = exe
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	tools, err := svc.spawnProbe(ctx)
	if err != nil {
		t.Fatalf("真 hanxi mcp 自检失败: %v", err)
	}
	if len(tools) < 4 {
		t.Fatalf("首版工具面应 ≥4，得 %v", tools)
	}
}
