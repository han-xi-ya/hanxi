package mcp

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"hanxi/internal/platform"
)

// guarded_test.go 钉破坏性基建四件套的硬语义：总闸 fail-closed、token
// 单次消费与过期、黑名单永久拒杀、族登记不变量。假时钟注入（TokenStore.now），
// 真实文件用 t.TempDir——与 access_test.go 同风格。

// ---------- 公共假件 ----------

// captureHandler 收集 slog 记录，审计必落断言的唯一手段。
type captureHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *captureHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *captureHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r.Clone())
	return nil
}
func (h *captureHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *captureHandler) WithGroup(string) slog.Handler      { return h }

// auditOutcomes 取 mcp.portkill.audit 前缀记录的 outcome 字段序列
// （stage 非空时再按 stage 过滤）。
func (h *captureHandler) auditOutcomes(stage string) []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []string
	for _, r := range h.records {
		if r.Message != auditMsgPrefix {
			continue
		}
		if stage != "" {
			gotStage := attrString(r, "stage")
			if gotStage != stage {
				continue
			}
		}
		out = append(out, attrString(r, "outcome"))
	}
	return out
}

// auditRecord 返回最近一条匹配 (stage, outcome) 的审计记录（无则 nil）。
func (h *captureHandler) auditRecord(stage, outcome string) *slog.Record {
	h.mu.Lock()
	defer h.mu.Unlock()
	for i := len(h.records) - 1; i >= 0; i-- {
		r := h.records[i]
		if r.Message != auditMsgPrefix {
			continue
		}
		if attrString(r, "stage") == stage && attrString(r, "outcome") == outcome {
			return &r
		}
	}
	return nil
}

func attrString(r slog.Record, key string) string {
	var v string
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == key {
			v = a.Value.String()
			return false
		}
		return true
	})
	return v
}

// installAuditCapture 临时接管 slog.Default（t.Cleanup 复位）。
func installAuditCapture(t *testing.T) *captureHandler {
	t.Helper()
	h := &captureHandler{}
	prev := slog.Default()
	slog.SetDefault(slog.New(h))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return h
}

// testClock 可控时钟。
type testClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *testClock) Time() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *testClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// fakeDestructivePolicy 内存总闸（真形态由 destructiveFilePolicy 测试把关）。
type fakeDestructivePolicy struct {
	enabled bool
	calls   int
}

func (f *fakeDestructivePolicy) Allowed(string) bool {
	f.calls++
	return f.enabled
}
func (f *fakeDestructivePolicy) LastReason() string { return "fake policy off" }

// ---------- A2：destructive.json 严格 fail-closed ----------

func TestGuardedDestructiveFilePolicyFailClosed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, DestructiveFileName)
	p := NewDestructiveFilePolicy(path)

	// 缺文件：合法态，全关。
	if p.Allowed(portkillAccessKey) {
		t.Fatal("missing destructive.json must mean disabled (fail-closed)")
	}
	write := func(body string) {
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	write(`{"version":1,"enabled":{"portkill":true}}`)
	if !p.Allowed(portkillAccessKey) {
		t.Fatalf("explicit enable must pass, reason=%s", p.LastReason())
	}

	// 显式 false 同样全关。
	write(`{"version":1,"enabled":{"portkill":false}}`)
	if p.Allowed(portkillAccessKey) {
		t.Fatal("explicit false must deny")
	}

	// 未知 op 键 / version≠1 / 未知顶层字段 / 坏 JSON / 尾随垃圾：整体拒读。
	for name, body := range map[string]string{
		"unknown op key":   `{"version":1,"enabled":{"format_disk":true,"portkill":true}}`,
		"version 2":        `{"version":2,"enabled":{"portkill":true}}`,
		"unknown field":    `{"version":1,"enabled":{"portkill":true},"yolo":true}`,
		"broken json":      `{"version":1,`,
		"trailing garbage": `{"version":1,"enabled":{"portkill":true}} {}`,
		"missing enabled":  `{"version":1}`,
	} {
		write(body)
		if p.Allowed(portkillAccessKey) {
			t.Errorf("%s: must deny wholesale (fail-closed)", name)
		}
	}

	// 撤闸即时生效（每次调用重读盘，不养缓存）。
	write(`{"version":1,"enabled":{"portkill":true}}`)
	if !p.Allowed(portkillAccessKey) {
		t.Fatal("grant must take effect immediately")
	}
	write(`{"version":1,"enabled":{"portkill":false}}`)
	if p.Allowed(portkillAccessKey) {
		t.Fatal("revoke must take effect immediately")
	}
}

// ---------- A3：token 单次消费 / 过期 / 上限 ----------

func TestGuardedTokenStoreSingleUseExpiryAndCap(t *testing.T) {
	clk := &testClock{t: time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)}
	s := NewKillTokenStore(portkillTokenTTL, clk.Time)

	id, hash, err := s.Issue(8080, 1234, `C:\app\svc.exe`, clk.Time())
	if err != nil {
		t.Fatal(err)
	}
	if id == "" || hash == "" {
		t.Fatal("issue must return token id and audit hash")
	}
	if strings.Contains(hash, id) {
		t.Fatal("audit hash must not contain the plaintext token")
	}

	// 首次消费成功。
	con := s.Consume(id)
	if !con.Valid || con.Token.pid != 1234 {
		t.Fatalf("first consume must be valid: %+v", con)
	}
	// 复用被拒（一次性硬保证）。
	con = s.Consume(id)
	if con.Valid || !strings.Contains(con.Why, "已被使用") {
		t.Fatalf("reuse must be rejected, got %+v", con)
	}
	// 未知 token。
	con = s.Consume("deadbeef")
	if con.Valid || !strings.Contains(con.Why, "不存在") {
		t.Fatalf("unknown token must be rejected, got %+v", con)
	}
	// 过期：发一枚后推进时钟超 TTL。
	id2, _, err := s.Issue(9090, 4321, `C:\app\other.exe`, clk.Time())
	if err != nil {
		t.Fatal(err)
	}
	clk.Advance(portkillTokenTTL + time.Second)
	con = s.Consume(id2)
	if con.Valid || !strings.Contains(con.Why, "过期") {
		t.Fatalf("expired token must be rejected, got %+v", con)
	}
	// 过期即销毁：第二次归因为"不存在"而非可重试。
	con = s.Consume(id2)
	if con.Valid || !strings.Contains(con.Why, "不存在") {
		t.Fatalf("expired token must be destroyed, got %+v", con)
	}

	// 在途上限：塞满 max 枚未消费 token 后拒发。
	s2 := NewKillTokenStore(time.Hour, clk.Time)
	for i := 0; i < portkillMaxTokens; i++ {
		if _, _, err := s2.Issue(1, uint32(i+10), "x", clk.Time()); err != nil {
			t.Fatalf("issue %d must fit: %v", i, err)
		}
	}
	if _, _, err := s2.Issue(1, 99999, "x", clk.Time()); err == nil {
		t.Fatal("issuing beyond cap must fail loudly")
	}
	if got := s2.Pending(); got != portkillMaxTokens {
		t.Fatalf("pending = %d, want %d", got, portkillMaxTokens)
	}
}

// ---------- A4：永久拒杀黑名单（独立于授权态） ----------

func TestGuardedHardDenyList(t *testing.T) {
	be := &fakePortkillBackend{} // isProt=false
	cases := []struct {
		name string
		pid  uint32
		info platform.ProcInfo
		want string // 非空=必须拒
	}{
		{"pid 0", 0, platform.ProcInfo{PID: 0}, "系统核心进程"},
		{"pid 4 system", 4, platform.ProcInfo{PID: 4, Name: "System"}, "系统核心进程"},
		{"self", uint32(os.Getpid()), platform.ProcInfo{PID: uint32(os.Getpid())}, "自身"},
		{"hanxi.exe", 1000, platform.ProcInfo{ExePath: `D:\tools\hanxi.exe`}, "黑名单"},
		{"hanxi.exe case-insensitive", 1000, platform.ProcInfo{ExePath: `D:\HANXI.EXE`}, "黑名单"},
		{"dwm.exe", 1001, platform.ProcInfo{ExePath: `C:\Windows\System32\dwm.exe`}, "黑名单"},
		{"explorer.exe", 1002, platform.ProcInfo{ExePath: `C:\Windows\explorer.exe`}, "黑名单"},
		{"platform protected", 1003, platform.ProcInfo{ExePath: `C:\a\normal.exe`}, ""},
		{"ordinary target", 1004, platform.ProcInfo{ExePath: `C:\dev\myapp.exe`, Name: "myapp.exe"}, ""},
	}
	for _, tc := range cases {
		got := portkillHardDeny(be, tc.pid, tc.info)
		if tc.want == "" && got != "" {
			t.Errorf("%s: must allow but denied: %s", tc.name, got)
		}
		if tc.want != "" && !strings.Contains(got, tc.want) {
			t.Errorf("%s: deny reason %q must contain %q", tc.name, got, tc.want)
		}
	}
	// 平台红线判定命中同样拒。
	be.isProt = true
	if got := portkillHardDeny(be, 2000, platform.ProcInfo{ExePath: `C:\a\x.exe`}); !strings.Contains(got, "红线") {
		t.Errorf("platform IsProtected must deny, got %q", got)
	}
}

// ---------- 族登记不变量（static 审豁免面的自我约束） ----------

func TestGuardedDestructiveFamilyInvariants(t *testing.T) {
	if len(destructiveFamilies) == 0 {
		t.Fatal("族表为空——豁免条款退化为无源之水")
	}
	for _, f := range destructiveFamilies {
		if !strings.HasSuffix(f.Prepare, "_prepare") || !strings.HasSuffix(f.Execute, "_execute") {
			t.Errorf("family %v: 二段式命名契约（*_prepare/*_execute）破裂", f)
		}
		if f.AccessKey == "" {
			t.Errorf("family %v: 缺 access 授权键", f)
		}
		if !isDestructiveToolName(f.Prepare) || !isDestructiveToolName(f.Execute) {
			t.Errorf("family %v: 成员未入豁免名册", f)
		}
		if !isKnownDestructiveOp(f.AccessKey) {
			t.Errorf("family %v: access 键未登记 destructive op", f)
		}
		if isDestructiveExecuteName(f.Prepare) {
			t.Errorf("prepare 件 %s 不得被认作 execute", f.Prepare)
		}
	}
	// 非族成员绝不可被认作破坏性工具（防豁免面扩大）。
	for _, name := range []string{toolEnvCheck, toolSearch, toolLogs, "hanxi_portscan_start"} {
		if isDestructiveToolName(name) {
			t.Errorf("%q 不得进入破坏性豁免名册", name)
		}
	}
}

// ---------- 接线形态自检（hook 未设=全关，非 panic） ----------

func TestGuardedUnwiredHookIsFailClosed(t *testing.T) {
	SetPortkillWiring(nil)
	if w := portkillWiringOf(); w != nil {
		t.Fatal("hook 复位后必须为 nil")
	}
	h := installAuditCapture(t)
	_, handler := buildPortkillExecuteTool(Deps{})
	req := mcpCallReq(toolPortkillExecute, map[string]any{"token": "whatever"})
	res, err := handler(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatal("未接线时 execute 必须报错而非放行")
	}
	if got := h.auditOutcomes("execute"); len(got) != 1 || got[0] != "unwired" {
		t.Fatalf("未接线也必须留审计痕: %v", got)
	}
}
