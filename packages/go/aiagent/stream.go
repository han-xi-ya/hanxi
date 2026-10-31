package aiagent

// 常驻 stream-json 会话。本文件的每一个设计决定都钉在 2026-10-30 真机探测的
// 实测记录上（probe 原始帧已归档 docs/evidence/aichat-stream-probe-2026-10-30/，
// 易失原址 C:/tmp/aichat-probe/ 仅历史脚注；帧样本另见册 #137–#141 与提交说明）：
//
//   - argv（实测命中 claude.cmd 直传，Go 无需包 cmd /C）：
//     claude --input-format stream-json --output-format stream-json --verbose
//     --permission-mode default --disallowedTools <清单> --session-id <uuid4>
//     续聊换 --resume <同一个uuid>。实测单轮 2.5~8s（一次性 -p 形态是 4.4~66s，
//     常驻赢在免冷启动）；result 后进程**存活**等下一帧，实测同进程多轮连续。
//   - 一条坏 stdin 行 = 进程 38ms 内暴死、stdout 不报错（只 stderr 一行
//     "Error parsing streaming input line"）。所以用户文本**永远只经
//     encoding/json 整帧序列化**后写出，不存在任何手拼路径。
//   - system/init 不在启动期出现，而是**第一条 user 帧写入后才发、每回合重发**；
//     想"等 init 就绪再放行输入"是错的——应当立刻可写。
//   - result 帧之后还有 command_lifecycle(completed) 与 session_state_changed
//     (idle) 尾帧；"还在收帧"不等于卡住。
//   - hook_response 单帧实测 17KB+（SessionStart 插件喷的），行缓冲上限必须
//     给足，默认 64KB 的 Scanner 会当场炸。
//   - 会话档案挂在 **cwd 派生的项目目录**（init.memory_paths 实证）：WorkDir
//     必须固定且由调用方持久化，换目录复进大概率找不到会话。
//   - 回合对账键：Send 返回的 uuid 会原样出现在 result.user_message_uuid
//     （实测省略 uuid 则该对账字段整个消失，故 uuid 必发）。
//
// **明确不做**（未实测不进主链，与包注释同一教条）：逐 token 增量
// （--include-partial-messages 未验证，文本粒度是"每个 content 块完成时一条
// 快照帧"）、stdin 控制帧（秒级 interrupt、can_use_tool 授权应答）——这些都
// 留给带实测的后续批次；本版"停止"的语义就是 Close() 杀树 + 下次 --resume 复进。

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
)

// ErrSessionDead 会话进程已终止（被杀、崩溃或坏输入暴死）后继续 Send 的错误。
// 恢复姿势是**新 Dial 同一 SessionID 且 Resume=true**（实测跨进程记忆成立）。
var ErrSessionDead = errors.New("AI 常驻会话已终止")

const (
	// chatLineMax stdin 之外唯一的巨型帧来源是 hook 噪音（实测 17KB+ 起步），
	// 4MB 既容得下实测样本，也是内存护栏——与 stdoutCapHint 分列同理：
	// 拒收判据与缓冲上限不是同一件事。
	chatLineMax = 4 << 20
	// eventsQueue 事件队列深度。满了会反压到读循环（读循环阻塞在 send 上），
	// 极端滞留时管道里堆的是 CLI 自己的输出——宁可让上游变慢，不丢帧不谎报。
	eventsQueue = 64
)

// DefaultChatDisallowedTools 是 v1"纯文字问答档"的实测组合（探测 argv 原样）。
// 关掉工具执行面就绕开了整个权限系统（default 模式下无工具可批，就不会有
// 需要人批的时刻）；清单外还有哪些工具、逐条禁用后果 [未验证]，扩清单前先测。
var DefaultChatDisallowedTools = []string{"Bash", "Edit", "Write", "MultiEdit", "NotebookEdit", "WebFetch"}

// EventKind 会话事件三分类。
type EventKind int

const (
	// EventText 一个 assistant **文本块**的完整快照（非 token 增量，见头注）。
	// 归属哪一轮按到达序判：调用方必须守住"同一会话一轮在途"（见 Send）。
	EventText EventKind = iota
	// EventTurnDone 该轮 result 帧——回合终态的唯一权威（实测 result 之后还有
	// lifecycle 尾帧，别把 Text 收完当结束）。
	EventTurnDone
	// EventDead 进程退出（Close/崩溃/坏输入暴死）。此后不再有事件，通道关闭。
	// Err==nil 表示经 Close 的体面收场。
	EventDead
)

// TurnResult result 帧里实测存在的统计全集（探测 E2 逐字对过）。
// 字段缺失时为零值——CLI 加字段不破坏我们，我们也不赌没见过的字段。
type TurnResult struct {
	Subtype      string
	IsError      bool
	Text         string // 本轮最终文本；与 EventText 快照内容一致（实测），UI 以其为准
	SessionID    string
	TurnUUID     string // = 我方 Send 的 uuid（user_message_uuid 原样回带）
	DurationMs   int64
	TTFTMs       int64
	InputTokens  int64
	OutputTokens int64
	TotalCostUSD float64 // 用户通道口径的回包数，与我方计费无关；展示可以，推断不行
}

// Event 一条会话事件。
type Event struct {
	Kind   EventKind
	Text   string      // 仅 EventText：一个文本块的完整快照
	Result *TurnResult // 仅 EventTurnDone
	Err    error       // 仅 EventDead（nil=Close 体面退出）
}

// SessionInfo init 帧里我们消费的三件套（每回合重发，取首见即可）。
type SessionInfo struct {
	SessionID string
	Model     string
	Version   string
}

// SessionOptions Dial 参数。SessionID/WorkDir 必填。
type SessionOptions struct {
	// SessionID 自造 uuid4（用 NewSessionID），首轮走 --session-id 起名、
	// 后续轮/复进走 --resume。非 uuid4 字符串的后果 [未验证]，别赌。
	SessionID string
	// Resume true 用 --resume 续既有会话；false 用 --session-id 开新会话。
	Resume bool
	// WorkDir 会话档案按 cwd 派生目录归档（实测），必须固定并持久化。
	WorkDir string
	// PermissionMode 留空 = "default"。v1 只测过 default；其它模式 [未验证]。
	PermissionMode string
	// DisallowedTools 留空 = DefaultChatDisallowedTools。
	DisallowedTools []string
}

// Session 一个常驻 claude 进程加它的双向流。并发模型：Send 可多 goroutine
// （写锁串行），Events 只许**一个**消费者。
type Session struct {
	w         io.WriteCloser
	wmu       sync.Mutex
	events    chan Event
	info      atomic.Pointer[SessionInfo]
	dead      atomic.Bool
	dropMu    sync.Mutex
	dropped   int64 // 未识别帧计数（不静默吞数，可读出来对账）
	stderr    *boundedBuffer
	cmd       *exec.Cmd
	closeOnce sync.Once
}

// Dial 起一个常驻会话进程。找不到 CLI 返回 ErrNotInstalled（与 New 同口径）。
func Dial(ctx context.Context, opts SessionOptions) (*Session, error) {
	if strings.TrimSpace(opts.SessionID) == "" {
		return nil, errors.New("会话必须带 SessionID（uuid4，用 NewSessionID 生成）")
	}
	// WorkDir 强制：会话档案挂在 cwd 派生的项目目录（init.memory_paths 实证），
	// 随手换个目录 --resume 就找不到历史——这条错在源头拦，不在复进时坑人。
	if strings.TrimSpace(opts.WorkDir) == "" {
		return nil, errors.New("会话必须固定 WorkDir：resume 依赖 cwd 派生的项目目录（实测）")
	}
	bin, err := exec.LookPath("claude")
	if err != nil || strings.TrimSpace(bin) == "" {
		return nil, ErrNotInstalled
	}

	cmd := exec.CommandContext(ctx, bin, streamArgs(opts)...)
	cmd.Dir = opts.WorkDir
	hideChildConsole(cmd) // GUI 本体拉控制台程序不得弹黑框（沿用 child_windows.go）

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("接管会话 stdin 失败: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("接管会话 stdout 失败: %w", err)
	}
	s := &Session{
		w:      stdin,
		events: make(chan Event, eventsQueue),
		stderr: &boundedBuffer{limit: stderrCapBytes},
		cmd:    cmd,
	}
	// 一次性指针（实测每回合重发 init，内容同）：CompareAndSwap 首见即锁。
	seed := &SessionInfo{SessionID: opts.SessionID}
	s.info.Store(seed)
	cmd.Stderr = s.stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("会话进程启动失败: %w", err)
	}
	waitCh := make(chan error, 1)
	go func() { waitCh <- cmd.Wait() }()
	go s.readLoop(stdout, waitCh)
	return s, nil
}

// streamArgs 组装实测过的 argv。任务内容**不进 argv**（全走 stdin 帧），
// 与 Ask 同一理由：命令行塞用户内容会撞长度上限并暴露在本机进程列表。
func streamArgs(opts SessionOptions) []string {
	args := []string{"--input-format", "stream-json", "--output-format", "stream-json", "--verbose"}
	mode := opts.PermissionMode
	if mode == "" {
		mode = "default"
	}
	args = append(args, "--permission-mode", mode)
	tools := opts.DisallowedTools
	if tools == nil {
		tools = DefaultChatDisallowedTools
	}
	if len(tools) > 0 {
		args = append(args, "--disallowedTools")
		args = append(args, tools...)
	}
	if opts.Resume {
		args = append(args, "--resume", opts.SessionID)
	} else {
		args = append(args, "--session-id", opts.SessionID)
	}
	return args
}

// Send 投递一条用户消息，返回回合对账 uuid。
//
// **调用方必须守住同一会话一轮在途**（收到该 TurnUUID 的 EventTurnDone 或
// EventDead 之前不再 Send）：实测 CLI 会排队并逐轮回 result，但 EventText
// 快照不带回合标记，回合归属靠到达序推——并发两发就把归属推不出来了。
func (s *Session) Send(text string) (string, error) {
	if s.dead.Load() {
		return "", ErrSessionDead
	}
	if strings.TrimSpace(text) == "" {
		return "", ErrNoInput
	}
	uuid := NewTurnID()
	// 整帧序列化，永不手拼：坏 stdin 行 = 进程 38ms 暴死（实测），
	// 用户文本里的换行/引号只可能以 JSON 转义形态出现。
	frame := userFrame{
		Type: "user",
		Message: userContent{
			Role:    "user",
			Content: []textBlock{{Type: "text", Text: text}},
		},
		UUID: uuid,
	}
	b, err := json.Marshal(frame)
	if err != nil { // text 是 string，marshal 理论上不会失败；真失败就是编程错误，宁可拒也不写出半帧
		return "", fmt.Errorf("用户帧序列化失败: %w", err)
	}
	b = append(b, '\n')
	s.wmu.Lock()
	defer s.wmu.Unlock()
	if _, err := s.w.Write(b); err != nil {
		return "", fmt.Errorf("写入会话 stdin 失败（进程多半已暴死，读 Events 拿死因）: %w", err)
	}
	return uuid, nil
}

// Events 事件通道。只许一个消费者；EOF/Close 后以 EventDead 收尾并关闭。
func (s *Session) Events() <-chan Event { return s.events }

// Info init 帧的镜像（model/version 在首条 init 到达前为零值）。
func (s *Session) Info() SessionInfo { return *s.info.Load() }

// DroppedFrames 未识别类型的帧数（诚实计数，不静默）。
func (s *Session) DroppedFrames() int64 {
	s.dropMu.Lock()
	defer s.dropMu.Unlock()
	return s.dropped
}

// StderrNote stderr 尾部诊断（实测正常会话仅 84 字节的模型名告警；坏输入暴死时
// 死因**只**出现在这里——Events 里只有 exit status）。
func (s *Session) StderrNote() string { return tail(s.stderr.Bytes(), stderrNoteBytes) }

// Close 体面收场：关 stdin（实测进程不会因此自行退出）→ 杀进程树 → 幂等。
// Windows 走 taskkill /T /F：实测 .cmd→cmd.exe→node 三 pid 全收，
// Process.Kill 只杀头，孙会残留。
func (s *Session) Close() error {
	var err error
	s.closeOnce.Do(func() {
		_ = s.w.Close()
		if s.cmd != nil && s.cmd.Process != nil {
			err = killProcessTree(s.cmd)
		}
	})
	return err
}

// readLoop 收帧主循环。EOF 后取 Wait 结果定性死因，发 EventDead 关通道。
func (s *Session) readLoop(r io.Reader, waitCh <-chan error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64<<10), chatLineMax) // 巨型 hook 帧实测 17KB+，默认 64KB 会炸
	for sc.Scan() {
		s.handleLine(sc.Bytes())
	}
	err := <-waitCh
	// 读取侧自己的失败要说得出名字：超过 chatLineMax 的巨帧会把 Scan 掐断，
	// 此时 Wait 可能还是 0（进程活着但会话已瞎）——不并进去就是一次静默半死。
	if scErr := sc.Err(); scErr != nil {
		if err == nil {
			err = fmt.Errorf("会话输出流读取失败: %w", scErr)
		} else {
			err = fmt.Errorf("会话输出流读取失败: %w（进程退出: %v）", scErr, err)
		}
	}
	// exit status 1 是本 CLI 死后 Wait 的实测常态（Close 杀树同样走这条），
	// 不冒充异常；真正的坏消息形态是"没 Send 过坏帧却突然 Dead"，死因去
	// StderrNote 读，这里只如实转交。
	s.dead.Store(true)
	s.events <- Event{Kind: EventDead, Err: err}
	close(s.events)
}

// 帧结构只声明实测存在的字段；CLI 加字段不破 our parse。
type (
	textBlock struct {
		Type string `json:"type"`
		Text string `json:"text,omitempty"`
	}
	userContent struct {
		Role    string      `json:"role"`
		Content []textBlock `json:"content"`
	}
	userFrame struct {
		Type    string      `json:"type"`
		Message userContent `json:"message"`
		UUID    string      `json:"uuid"`
	}
	frameHead struct {
		Type    string `json:"type"`
		Subtype string `json:"subtype"`
	}
	assistantFrame struct {
		Message struct {
			Content []textBlock `json:"content"`
		} `json:"message"`
	}
	initFrame struct {
		SessionID         string `json:"session_id"`
		Model             string `json:"model"`
		ClaudeCodeVersion string `json:"claude_code_version"`
	}
	resultFrame struct {
		Subtype         string  `json:"subtype"`
		IsError         bool    `json:"is_error"`
		Result          string  `json:"result"`
		SessionID       string  `json:"session_id"`
		UserMessageUUID string  `json:"user_message_uuid"`
		DurationMs      int64   `json:"duration_ms"`
		TTFTMs          int64   `json:"ttft_ms"`
		TotalCostUSD    float64 `json:"total_cost_usd"`
		Usage           struct {
			InputTokens  int64 `json:"input_tokens"`
			OutputTokens int64 `json:"output_tokens"`
		} `json:"usage"`
	}
)

func (s *Session) handleLine(line []byte) {
	if len(strings.TrimSpace(string(line))) == 0 {
		return
	}
	var head frameHead
	if err := json.Unmarshal(line, &head); err != nil {
		// 非 JSON 输出行（CLI 偶发裸文本）：计数不崩、不猜。
		s.countDropped()
		return
	}
	switch head.Type {
	case "system":
		if head.Subtype == "init" {
			var f initFrame
			if err := json.Unmarshal(line, &f); err == nil {
				s.info.Store(&SessionInfo{
					SessionID: pick(f.SessionID, s.info.Load().SessionID),
					Model:     f.Model,
					Version:   f.ClaudeCodeVersion,
				})
			}
		}
		// 其余 system（hook_started/hook_response/thinking_tokens×45/session_state_changed…）
		// 全部为实测噪音帧，丢弃但可经 DroppedFrames 之外的口径不谎称"处理过"——
		// 它们是**已识别、按设计忽略**，不计 dropped。
	case "assistant":
		var f assistantFrame
		if err := json.Unmarshal(line, &f); err != nil {
			s.countDropped()
			return
		}
		for _, blk := range f.Message.Content {
			if blk.Type == "text" && blk.Text != "" {
				s.events <- Event{Kind: EventText, Text: blk.Text}
			}
		}
	case "result":
		var f resultFrame
		if err := json.Unmarshal(line, &f); err != nil {
			s.countDropped()
			return
		}
		s.events <- Event{Kind: EventTurnDone, Result: &TurnResult{
			Subtype: f.Subtype, IsError: f.IsError, Text: f.Result,
			SessionID: f.SessionID, TurnUUID: f.UserMessageUUID,
			DurationMs: f.DurationMs, TTFTMs: f.TTFTMs,
			InputTokens: f.Usage.InputTokens, OutputTokens: f.Usage.OutputTokens,
			TotalCostUSD: f.TotalCostUSD,
		}}
	default:
		// user（tool_result 回填帧，v1 无工具故实测未见过）与未知类型：计数。
		s.countDropped()
	}
}

func (s *Session) countDropped() {
	s.dropMu.Lock()
	s.dropped++
	s.dropMu.Unlock()
}

func pick(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// NewSessionID 生成会话/回合用 uuid4（crypto/rand，形状与实测 --session-id 一致）。
func NewSessionID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand 失败 = 系统熵源坏了，这不是"降级用时间戳"的场合：
		// 会话 id 撞车的代价是串会话，宁可炸。
		panic("aiagent: 生成会话 id 失败: " + err.Error())
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10xx
	h := hex.EncodeToString(b)
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}

var NewTurnID = NewSessionID // 同形制：回合 uuid 与会话 uuid 都是 uuid4（实测对账键）
