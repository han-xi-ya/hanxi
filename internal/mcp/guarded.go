package mcp

// guarded.go 是破坏性工具（destructive tool）安全基建——包注释决策 1
// 「所有工具严格只读、写操作永不注册为工具」红线显式升版的执行层。
//
// 升版论证（详见 docs/adr/ADR-0006 提案，机主指令"AI 能力接入增加端口查杀"）：
// 只读红线保护的是"云端模型上下文能对本机造成什么"。端口查杀天然是破坏性操作，
// 开这个口子而不毁掉红线的做法不是撤销红线，而是把 GUI 侧人工确认框的等价物
// 编译进工具通道，形成四道独立闸门（任何一道失守都不足以造成损害）：
//
//	A1 授权门（复用既有）：access.json ∩ 模块启用门，与只读工具同口径、fail-closed；
//	A2 机主总闸（本文件）：destructive.json 独立文件开关，默认不存在=全关——
//	   与 access.json 物理分离，防止「AI 接入」六/八键 UI 误操作顺带放行写工具；
//	A3 二段式确认（本文件 TokenStore）：prepare 只发一次性短 TTL token，
//	   execute 校验 token 且复查进程指纹（pid+exe+start time）后才动手——
//	   AI 无法用"一个调用"完成杀伤，中间必然经过模型把 token 原样带回的往返；
//	A4 红线黑名单（tools_portkill.go）：系统关键进程与 hanxi 自身永久拒杀，
//	   独立于 A1-A3 的授权态，任何 token/开关都绕不过。
//
// 每次 execute（无论成败）落一条 slog 结构化审计（msg="mcp.portkill.audit"，
// 单独可 grep 前缀），字段含通道、目标指纹、结果与归因——留痕本身不参与判定，
// 但保证事后"谁经 MCP 通道杀了什么"可复盘。

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
	"time"
)

// ---------- A2：机主破坏性总闸（destructive.json，fail-closed） ----------

const (
	// DestructiveFileName 破坏性授权文件名：<DataDir>/mcp/destructive.json。
	// 刻意与 access.json 分离建档：AI 接入面板逐键扩表（六键→八键→九键）时，
	// 写工具的放行必须是独立、显式、带"这是破坏性操作"心智负担的一次单独授权。
	DestructiveFileName = "destructive.json"
	destructiveDirName  = "mcp" // 与 access.json 同目录（读方永不建目录，写方补位纪律同 access）
	maxDestructiveSize  = 16 << 10
	destructiveSchemaV  = 1
)

// DestructivePolicy 是破坏性操作总闸的判定面（真 = destructiveFilePolicy；
// 单测注入假件）。Allowed 每次调用都必须呈现实时授权态（撤闸即时生效，不养缓存），
// 任何异常一律收敛为 false（fail-closed，与 access.go 同一判定哲学）。
type DestructivePolicy interface {
	Allowed(op string) bool
	// LastReason 返回最近一次拒绝原因快照（诊断/日志用，不参与判定）。
	LastReason() string
}

// destructiveFile 是 destructive.json 的落盘结构：
//
//	{"version":1,"enabled":{"portkill":true}}
type destructiveFile struct {
	Version int             `json:"version"`
	Enabled map[string]bool `json:"enabled"`
}

// destructiveFilePolicy 读盘判定破坏性总闸。文件缺失是合法态（默认全关）；
// 损坏/超限/version≠1/未知字段/未知 op 键 = 非法授权态 = 全关（宁可错杀）。
type destructiveFilePolicy struct {
	path string

	mu     sync.Mutex
	reason string
}

// NewDestructiveFilePolicy 指向 <DataDir>/mcp/destructive.json。构造无 IO。
func NewDestructiveFilePolicy(path string) *destructiveFilePolicy {
	return &destructiveFilePolicy{path: path}
}

// Path 返回总闸文件绝对路径（错误指引文案与诊断用）。
func (p *destructiveFilePolicy) Path() string { return p.path }

// Allowed 报告破坏性操作 op 是否被机主显式放行。每次调用重读盘（撤闸即时生效）。
func (p *destructiveFilePolicy) Allowed(op string) bool {
	enabled, err := p.load()
	if err != nil {
		p.setReason(err.Error())
		return false
	}
	allowed := enabled[op] // 缺键=零值 false（默认全关）
	if !allowed {
		p.setReason(fmt.Sprintf("destructive op %q not enabled in %s", op, p.path))
	}
	return allowed
}

// LastReason 返回最近一次拒绝判定的原因快照。
func (p *destructiveFilePolicy) LastReason() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.reason
}

func (p *destructiveFilePolicy) setReason(s string) {
	p.mu.Lock()
	p.reason = s
	p.mu.Unlock()
}

func (p *destructiveFilePolicy) load() (map[string]bool, error) {
	fi, err := os.Stat(p.path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]bool{}, nil // 缺文件=合法的全关默认态
		}
		return nil, fmt.Errorf("destructive.json stat failed: %w", err)
	}
	if fi.IsDir() || fi.Size() > maxDestructiveSize {
		return nil, fmt.Errorf("destructive.json size/type invalid (%d bytes, limit %d)", fi.Size(), maxDestructiveSize)
	}
	data, err := os.ReadFile(p.path)
	if err != nil {
		return nil, fmt.Errorf("destructive.json read failed: %w", err)
	}
	var doc destructiveFile
	dec := json.NewDecoder(bytes.NewReader(data))
	// DisallowUnknownFields 与 access.go 同款：契约外字段即整体拒绝，防授权语义漂移。
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("destructive.json parse failed: %w", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, fmt.Errorf("destructive.json has trailing garbage")
	}
	if doc.Version != destructiveSchemaV {
		return nil, fmt.Errorf("destructive.json version %d unsupported (want %d)", doc.Version, destructiveSchemaV)
	}
	if doc.Enabled == nil {
		return nil, fmt.Errorf("destructive.json missing \"enabled\" object")
	}
	for key := range doc.Enabled {
		if !isKnownDestructiveOp(key) {
			return nil, fmt.Errorf("destructive.json unknown op key %q", key)
		}
	}
	return doc.Enabled, nil
}

// ---------- A3：一次性确认 token 库（prepare→execute 二段式的载体） ----------

// portkillTokenTTL 是确认 token 的存活窗口。120 秒的取法：要覆盖一次真实的
// "模型读完占用清单 → 决策 → 回发 execute" 往返（秒级到十几秒级），又要短到
// 泄出的 token 在下一轮对话前必然失效；GUI 侧确认框等价物的"当场点头"语义。
const portkillTokenTTL = 120 * time.Second

// portkillMaxTokens 是在途 token 硬上限：防备好的会话堆积（超限时先清扫过期，
// 仍满则拒绝新发——宁可让模型重新 prepare，不静默挤掉旧 token）。
const portkillMaxTokens = 64

// killToken 一枚一次性查杀授权：绑定完整进程指纹（pid+exe+start time），
// execute 消费时必须对当前系统实况复查同款指纹（防 TOCTOU 杀错/PID 复用）。
type killToken struct {
	id        string // 明文随机 ID（只出现在 prepare 回执，绝不明文进日志）
	hash      string // sha256(id) 前 8 字节十六进制——审计留痕用它关联而不泄 token
	port      int    // 授权时的端口（审计上下文，不是复核对象）
	pid       uint32
	exePath   string
	startedAt time.Time // 零值表示 prepare 时系统查不到启动时间（复核降级为仅比对 exe 路径）
	issuedAt  time.Time
	used      bool // 单次消费标记（消费后保留条目一小段时间，用于区分"没用过/已用过"）
}

// consumeOutcome 是一次消费裁决的四态结果（区分 unknown/used/expired 供审计归因）。
type consumeOutcome struct {
	Token *killToken
	Valid bool   // true 时 Token 可进入复核阶段
	Why   string // 拒绝归因（中文，直接进模型错误文案）
}

// TokenStore 一次性 token 发放/消费库（内存态，进程生命周期内有效；
// MCP 会话即进程生命周期——进程重启所有 token 自然作废，无落盘泄露面）。
type TokenStore struct {
	mu     sync.Mutex
	byID   map[string]*killToken
	ttl    time.Duration
	max    int
	now    func() time.Time
	seqLen int // 随机 ID 字节数
}

// NewKillTokenStore 构造确认 token 库（真装配 ttl=portkillTokenTTL）。
func NewKillTokenStore(ttl time.Duration, now func() time.Time) *TokenStore {
	if now == nil {
		now = time.Now
	}
	return &TokenStore{byID: map[string]*killToken{}, ttl: ttl, max: portkillMaxTokens, now: now, seqLen: 16}
}

// Issue 发放一枚绑定目标指纹的一次性 token，返回明文 ID 与审计哈希。
func (s *TokenStore) Issue(port int, pid uint32, exePath string, startedAt time.Time) (string, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepLocked()
	if len(s.byID) >= s.max {
		return "", "", fmt.Errorf("在途确认令牌已满（%d 枚上限），旧令牌请等待过期后重试", s.max)
	}
	buf := make([]byte, s.seqLen)
	if _, err := rand.Read(buf); err != nil {
		return "", "", fmt.Errorf("随机令牌生成失败: %w", err)
	}
	id := hex.EncodeToString(buf)
	h := sha256.Sum256(buf)
	t := &killToken{
		id:        id,
		hash:      hex.EncodeToString(h[:4]),
		port:      port,
		pid:       pid,
		exePath:   exePath,
		startedAt: startedAt,
		issuedAt:  s.now(),
	}
	s.byID[id] = t
	return id, t.hash, nil
}

// Consume 原子消费一枚 token：单次使用是硬保证——无论后续复核成败，
// 一经尝试即置 used，杜绝"失败重试"变成无限重放。
func (s *TokenStore) Consume(id string) consumeOutcome {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepLocked()
	t, ok := s.byID[id]
	if !ok {
		return consumeOutcome{Why: "令牌不存在或已随会话结束作废"}
	}
	if t.used {
		return consumeOutcome{Why: "令牌已被使用（一次性令牌不可复用，请重新 prepare 获取新令牌）"}
	}
	if s.now().Sub(t.issuedAt) > s.ttl {
		// 过期即销毁：不给"猜时间差复用"留窗口。
		delete(s.byID, id)
		return consumeOutcome{Why: fmt.Sprintf("令牌已过期（有效期 %d 秒），请重新 prepare 获取新令牌", int(s.ttl.Seconds()))}
	}
	t.used = true
	return consumeOutcome{Token: t, Valid: true}
}

// sweepLocked 清掉"过期且已消费"的条目；仅过期未消费的保留到 Consume 归因后
// 删除（保留 unknown/expired/used 三态可辨的审计价值），但受 max 约束先扫这里。
func (s *TokenStore) sweepLocked() {
	deadline := s.now().Add(-2 * s.ttl)
	for id, t := range s.byID {
		if t.used && t.issuedAt.Before(deadline) {
			delete(s.byID, id)
		}
	}
}

// Pending 返回在途（未消费）token 数（测试与诊断用）。
func (s *TokenStore) Pending() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, t := range s.byID {
		if !t.used {
			n++
		}
	}
	return n
}

// ---------- A1+A4 的装配载体：破坏性工具接线（不改 server.go 的即插通道） ----------

// portkillWiring 是端口查杀工具的后端依赖包（真装配在 Run() 接线行构造）。
// 之所以走包级 hook 而非 Deps 字段：本组件交付时不触碰 server.go（两路并行
// 领地纪律），接线行报告里给出把 hook 替换为 Deps 字段的等价改法（更优，
// 主会话拍板时二选一）。nil hook = 工具报"后端未接线"而非 panic——fail-closed。
type portkillWiring struct {
	Backend PortkillBackend
	Policy  DestructivePolicy
	Tokens  *TokenStore
}

var (
	portkillHookMu sync.Mutex
	portkillHook   *portkillWiring
)

// NewPortkillWiring 组装接线依赖（Run() 接线行的构造函数形态，参数即三闸门
// 的后端：service adapter / destructive.json 总闸 / token 库）。
func NewPortkillWiring(b PortkillBackend, p DestructivePolicy, ts *TokenStore) *portkillWiring {
	return &portkillWiring{Backend: b, Policy: p, Tokens: ts}
}

// SetPortkillWiring 注入端口查杀后端依赖（Run() 启动时调用一次；测试经
// t.Cleanup 复位传 nil）。传 nil 复位为未接线态——工具面存在但一律拒绝，
// 与"未注册"的区别仅在可发现性，杀伤面相同（全关）。
func SetPortkillWiring(w *portkillWiring) {
	portkillHookMu.Lock()
	defer portkillHookMu.Unlock()
	portkillHook = w
}

// portkillWiringOf 读取当前接线快照（每次调用重读，无缓存分歧）。
func portkillWiringOf() *portkillWiring {
	portkillHookMu.Lock()
	defer portkillHookMu.Unlock()
	return portkillHook
}

// ---------- 审计（每次 execute 无论成败必落一条；prepare 同样留痕） ----------

// auditMsgPrefix 是 MCP 破坏性通道审计的固定 msg 前缀——日志文件里
// grep '"msg":"mcp.portkill.' 即可拉出全量通道行为账（机主验收剧本第 5 步）。
const auditMsgPrefix = "mcp.portkill.audit"

// portkillAudit 落一条结构化审计。tokenHash 只入哈希不入明文；outcome 是
// 封闭词表（success/fail/denied_policy/denied_protected/aborted_identity/
// expired/used/unknown_token/need_elevate/unwired/error/issued），
// reason 为人读归因。level 按后果分级：成功 Info、拒绝/中止 Warn。
func portkillAudit(stage, outcome, reason string, t *killToken, extra ...any) {
	attrs := []any{
		"channel", "mcp",
		"stage", stage,
		"outcome", outcome,
	}
	if t != nil {
		attrs = append(attrs,
			"token", t.hash,
			"port", t.port,
			"pid", t.pid,
			"exe", t.exePath,
		)
	}
	attrs = append(attrs, extra...)
	if reason != "" {
		attrs = append(attrs, "reason", reason)
	}
	level := slog.LevelInfo
	switch outcome {
	case "fail", "denied_policy", "denied_protected", "aborted_identity", "expired", "used", "unknown_token", "need_elevate", "unwired", "error":
		level = slog.LevelWarn
	}
	slog.LogAttrs(context.Background(), level, auditMsgPrefix, slogAttrs(attrs)...)
}

// slogAttrs 把 k,v 交替切片转 slog.Attr（值一律 any，保持调用处书写轻便）。
func slogAttrs(kv []any) []slog.Attr {
	attrs := make([]slog.Attr, 0, len(kv)/2)
	for i := 0; i+1 < len(kv); i += 2 {
		key, ok := kv[i].(string)
		if !ok {
			continue
		}
		attrs = append(attrs, slog.Any(key, kv[i+1]))
	}
	return attrs
}
