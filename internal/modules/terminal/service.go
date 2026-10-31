package terminal

// TerminalService：内嵌终端的调用面（v0 竖切）。
//
// 与 aichat 的分野说清楚：aichat 是"协议对话"（hanxi 懂内容），这里是
// "字节管道"（hanxi 完全不懂内容，也不装懂）。画面归前端的 xterm.js，
// 本层只做四件事：起伪控制台、双向搬字节、resize、收尸。
//
// 字节通道用 Wails v3 beta.10 内置 GoStream（本仓首例，注册点在 module.go
// OnInit；选型理由：事件通道载荷只能 JSON 且 >8KB 每帧多一跳 fetch，终端
// 刷屏场景不可用——调研在案，勿改回 events）。**每帧首字节是路由标签**：
//
//	0x00 = JSON 控制帧（open/resize/ready/exit）
//	0x01 = 原始终端字节（双向都是，混帧不错序）
//
// 标签制而不是"首帧 JSON 之后裸流"的隐式状态机，是为了两端任何时刻都能
// 单看一帧自证类型——调试时猜状态是最贵的。
//
// 审计批注（2026-10-31 两路只读审计后的定稿形态，条条有出处）：
//   - **停用/退出收口不得过自家调用门**：stopping 态下 Enter 被 registry
//     拒绝（registry.go:813），收口若走带门的 KillTerminal 就会"一个都不杀、
//     账一项不清"却显示已停用——收口走无门的 killSession（B1）。
//   - **数据面入口同样过门**：GoStream 端点不过 rpc 面，serveConn 的 open
//     帧单独经 holder.Enter 准入；否则停用后残留页与新页都能继续喂字节
//     （B2——矩阵只扫导出方法，包内 handler 是它的盲区，别把"降为包内"
//     当成"免于接门"的理由）。
//   - **抢主必须真退泵**：前任泵发现 owner≠己时，把手中帧转交现任后**退出**
//     ——只丢帧不退泵 = 双读同一管道，现任永远缺 ~50%（"接管"名存实亡）。
//   - **owner 清空用 CAS**：Load/Store 非原子，先查后清之间 B 上位的话会
//     擦掉刚上位的现任（黑屏但输入仍通的经典雷）。
//   - **无主期间挂丢弃式 drain**：断线后没人读 pty，OS 管道（审计实证默认
//     4KB）+conhost 缓冲写满会**反压冻住子进程**——drain 保持字节流动，
//     "断线进程仍活着"不是靠嘴说的，是靠有人消费。
//   - **resize 前置 exited 闸**：与 kill 交叉的窗口由 conpty 层 RWMutex 护栏
//     兜底，这里再查一次账，双层各管各的语义。
//
// v0 语义两条，如实写在这、也如实进 UI 文案：
//   - 断线不杀进程（窗口刷新后重连即续，drain 保证进程不冻），但**应用
//     重启进程即清账**——v0 不做会话账本持久化；
//   - 一个会话同一时刻只有一路在管输出（后 attach 抢主、前任转交后退场）。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hanxi/internal/extapi"
	"hanxi/packages/go/aiagent"
	"hanxi/packages/go/conpty"
)

// StreamName 前端 Stream() 使用的通道名。改这里必须同步前端 useTerminal 与
// 帧协议注释（两处注释互相指认，防单边漂移）。
const StreamName = "terminal:io"

var ErrSessionNotFound = errors.New("终端会话不存在")

// 帧标签。
const (
	frameControl byte = 0x00
	frameRaw     byte = 0x01
)

// streamIO 是对 application.StreamConn 的窄面切片——owner 存它而不是具体
// 类型，为的是"抢主/转交/CAS/drain"这些真出过事故的语义能进单测（审计
// 口径告：不带门的 holder 测出来的绿，恰好全是生产路径不会发生的那条）。
type streamIO interface {
	Send(b []byte) error
	Close() error
	Context() context.Context
	Receive() ([]byte, error)
}

// TermProfile v0 固定三档（返回前端用于入口列表）。
type TermProfile struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

var v0Profiles = []TermProfile{
	{ID: "cmd", Title: "命令行"},
	{ID: "powershell", Title: "PowerShell"},
	{ID: "claude", Title: "Claude Code"},
}

// TermSessionRow 会话账目（内存态，见头注 v0 语义）。
type TermSessionRow struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	ProfileID string `json:"profileId"`
	Pid       uint32 `json:"pid"`
	CreatedAt int64  `json:"createdAt"`
	Alive     bool   `json:"alive"`
}

// ptyLike 是 conpty.Pty 的窄接口——与 aichat.agentStream、jsonfmt.Asker 同一条
// 理由：不注入就只能真起伪控制台才能测状态机（CI 与并行门禁都赔不起）。
type ptyLike interface {
	Read(b []byte) (int, error)
	Write(b []byte) (int, error)
	Resize(cols, rows uint16) error
	Close() error
	Pid() uint32
	Done() <-chan struct{}
	Err() error
}

// session 一个伪控制台会话的运行态。
type session struct {
	row    TermSessionRow
	pty    ptyLike
	exited atomic.Bool
	owner  atomic.Pointer[streamIO] // 当前在管输出的连接（抢主语义见头注）
	// draining：无主期间的丢弃式读泵在场标记（同会话至多一个）。
	draining atomic.Bool
}

// TerminalService 暴露给前端的终端服务。
type TerminalService struct {
	holder *extapi.LeaseHolder
	// spawnFn 伪控制台起爆点注入位（默认真 conpty）。
	spawnFn func(command string, args []string, opts conpty.Options) (ptyLike, error)

	mu       sync.Mutex
	sessions map[string]*session
	seq      int
}

func NewService(holder *extapi.LeaseHolder) *TerminalService {
	return &TerminalService{
		holder:   holder,
		sessions: map[string]*session{},
		spawnFn: func(command string, args []string, opts conpty.Options) (ptyLike, error) {
			return conpty.Spawn(command, args, opts)
		},
	}
}

// Profiles 只读列表；claude 档是否可用由前端点开时经 OpenTerminal 如实报错
// （可选依赖缺失不该让入口列表缺项——没装的机器上入口在、点了才说明白，
// 与 jsonfmt AI 按钮同一口径）。
func (s *TerminalService) Profiles() ([]TermProfile, error) {
	release, err := s.holder.Enter()
	if err != nil {
		return nil, err
	}
	release()
	return append([]TermProfile(nil), v0Profiles...), nil
}

// OpenTerminal 起一个伪控制台会话（RPC 快返回；刷屏全在字节通道里）。
func (s *TerminalService) OpenTerminal(profileID string, cols, rows int) (*TermSessionRow, error) {
	release, err := s.holder.Enter()
	if err != nil {
		return nil, err
	}
	release()

	command, args, title, err := resolveProfile(profileID)
	if err != nil {
		return nil, err
	}
	if cols < 2 || cols > 999 || rows < 2 || rows > 999 {
		return nil, fmt.Errorf("终端尺寸 %dx%d 不在 2..999 内", cols, rows)
	}

	pty, err := s.spawnFn(command, args, conpty.Options{
		Cols: uint16(cols), Rows: uint16(rows),
	})
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	s.seq++
	row := TermSessionRow{
		ID:        fmt.Sprintf("term-%d-%d", time.Now().UnixMilli(), s.seq),
		Title:     fmt.Sprintf("%s #%d", title, s.seq),
		ProfileID: profileID,
		Pid:       pty.Pid(),
		CreatedAt: time.Now().UnixMilli(),
		Alive:     true,
	}
	sess := &session{row: row, pty: pty}
	s.sessions[row.ID] = sess
	s.mu.Unlock()

	// 外部退场观察：pty.Done 关闭（子进程自然死/被击杀）即记账并通知现任。
	go func() {
		<-pty.Done()
		sess.exited.Store(true)
		if cur := sess.owner.Load(); cur != nil {
			_ = s.sendControl(*cur, controlFrame{Op: "exit"})
		}
	}()
	return &row, nil
}

func (s *TerminalService) ListTerminals() ([]TermSessionRow, error) {
	release, err := s.holder.Enter()
	if err != nil {
		return nil, err
	}
	release()
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]TermSessionRow, 0, len(s.sessions))
	for _, sess := range s.sessions {
		r := sess.row
		r.Alive = !sess.exited.Load()
		out = append(out, r)
	}
	return out, nil
}

// RenameTerminal 只动内存账（v0 无持久化，改名不留盘是语义的一部分）。
func (s *TerminalService) RenameTerminal(id, title string) error {
	release, err := s.holder.Enter()
	if err != nil {
		return err
	}
	release()
	title = strings.TrimSpace(title)
	if title == "" {
		return errors.New("会话名不能为空")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[id]
	if !ok {
		return ErrSessionNotFound
	}
	sess.row.Title = title
	return nil
}

// KillTerminal 杀会话（进程树收口在 conpty.Close 内）并除账——带门的 RPC 面。
func (s *TerminalService) KillTerminal(id string) error {
	release, err := s.holder.Enter()
	if err != nil {
		return err
	}
	release()
	s.mu.Lock()
	_, ok := s.sessions[id]
	s.mu.Unlock()
	if !ok {
		return ErrSessionNotFound
	}
	s.killSession(id)
	return nil
}

// killSession 收口的**无门**内部路径：摘账 → 标记退场 → 关当前连接 → 杀
// 伪控制台树。停用/退出链必须走这里而不是带 Enter 的 KillTerminal——
// stopping 态下门会拒绝 Acquire（registry.go:813），带门的收口等于没写（B1）。
// 幂等：摘账在锁内一次完成，重复调用第二跳查不到。
func (s *TerminalService) killSession(id string) {
	s.mu.Lock()
	sess, ok := s.sessions[id]
	if ok {
		delete(s.sessions, id)
	}
	s.mu.Unlock()
	if !ok {
		return
	}
	sess.exited.Store(true)
	if cur := sess.owner.Load(); cur != nil {
		_ = (*cur).Close() // 流先断：连接活着继续喂字节就是幽灵会话
	}
	_ = sess.pty.Close()
}

// killAll 模块停用/应用退出收口（逐会话走无门路径；并发安全靠 killSession 幂等）。
func (s *TerminalService) killAll() {
	s.mu.Lock()
	ids := make([]string, 0, len(s.sessions))
	for id := range s.sessions {
		ids = append(ids, id)
	}
	s.mu.Unlock()
	for _, id := range ids {
		s.killSession(id)
	}
}

// controlFrame 控制帧形状（前端 useTerminal 的对偶定义）。
type controlFrame struct {
	Op        string `json:"op"` // open | resize | ready | exit
	SessionID string `json:"sessionId,omitempty"`
	Cols      int    `json:"cols,omitempty"`
	Rows      int    `json:"rows,omitempty"`
	Code      int    `json:"code,omitempty"`
	Error     string `json:"error,omitempty"`
}

// encodeControl 组一帧控制帧（纯函数，serveConn 与测试共用——编解码只有一份）。
func encodeControl(ctl controlFrame) ([]byte, error) {
	b, err := json.Marshal(ctl)
	if err != nil {
		return nil, err
	}
	frame := make([]byte, 0, len(b)+1)
	frame = append(frame, frameControl)
	frame = append(frame, b...)
	return frame, nil
}

func (s *TerminalService) sendControl(c streamIO, ctl controlFrame) error {
	frame, err := encodeControl(ctl)
	if err != nil {
		return err
	}
	return c.Send(frame)
}

// handleStream GoStream 入口（包内方法——曾以导出名被 bindings 生成成前端
// API，污染契约面，降为不可导出；module.go OnInit 注册）。窄壳一层：
// 具体 StreamConn 转交 serveConn 的接口面，状态机全家可测。
func (s *TerminalService) handleStream(c *application.StreamConn) {
	s.serveConn(c)
}

// serveConn 一个连接的帧循环。生命周期由 c.Context() 兜住（页面刷新/窗口
// 关闭都会到这里取消）。
func (s *TerminalService) serveConn(c streamIO) {
	defer c.Close()

	var sess *session
	var my streamIO = c

	defer func() {
		if sess != nil {
			// CAS 而非 Load+Store：先查后清之间 B 上位的话会擦掉现任（黑屏但
			// 输入仍通——审计 M2 的窄窗）。
			sess.owner.CompareAndSwap(&my, nil)
		}
	}()

	for {
		select {
		case <-c.Context().Done():
			return
		default:
		}
		frame, err := c.Receive()
		if err != nil {
			return // 断线/刷新
		}
		if len(frame) == 0 {
			continue
		}
		switch frame[0] {
		case frameControl:
			var ctl controlFrame
			if err := json.Unmarshal(frame[1:], &ctl); err != nil {
				// 控制帧坏 = 协议破坏，当场说明并收场（不猜状态）。
				_ = s.sendControl(c, controlFrame{Op: "ready", Error: "控制帧 JSON 解析失败，连接作废"})
				return
			}
			switch ctl.Op {
			case "open":
				if sess != nil { // 重复 open：拒绝，不让同一连接挂两只输出泵
					_ = s.sendControl(c, controlFrame{Op: "ready", Error: "该连接已打开会话，重复 open 被拒"})
					continue
				}
				// 数据面准入（B2）：停用/退出/未启用状态下 open 直接拒——
				// GoStream 端点在 rpc 门外，这道 Enter 是它唯一的门。
				release, gerr := s.holder.Enter()
				if gerr != nil {
					_ = s.sendControl(c, controlFrame{Op: "ready", Error: "模块未启用或正在停用，拒绝接入: " + gerr.Error()})
					continue
				}
				release()
				s.mu.Lock()
				target := s.sessions[ctl.SessionID]
				s.mu.Unlock()
				if target == nil || target.exited.Load() {
					_ = s.sendControl(c, controlFrame{Op: "ready", Error: "会话不存在或已终止（已杀死或从未存在）"})
					continue
				}
				sess = target
				// 抢主：Store 上位 + 起泵；前任泵见到 owner≠己会把手中帧转交后退出。
				sess.owner.Store(&my)
				_ = s.sendControl(c, controlFrame{Op: "ready", SessionID: sess.row.ID})
				go s.pumpOutput(sess, &my)
			case "resize":
				if sess == nil {
					_ = s.sendControl(c, controlFrame{Op: "ready", Error: "resize 在 open 之前到达，忽略"})
					continue
				}
				if sess.exited.Load() {
					_ = s.sendControl(c, controlFrame{Op: "exit"})
					continue
				}
				if ctl.Cols >= 2 && ctl.Rows >= 2 {
					if err := sess.pty.Resize(uint16(ctl.Cols), uint16(ctl.Rows)); err != nil {
						// conpty 层护栏（closed 位）兜住与击杀交叉的窗口，这里如实上报错误。
						_ = s.sendControl(c, controlFrame{Op: "ready", Error: "resize 失败: " + err.Error()})
					}
				}
			default:
				_ = s.sendControl(c, controlFrame{Op: "ready", Error: "未知控制 op: " + ctl.Op})
			}
		case frameRaw:
			if sess == nil || sess.exited.Load() {
				continue // 未 open 或已死的输入：丢弃不猜（正常前端时序不会走到，防违约客户端）
			}
			if _, err := sess.pty.Write(frame[1:]); err != nil {
				if sess.exited.Load() {
					// 击杀竞态：只丢这一帧，不断整条连接（审计 R5）。
					continue
				}
				return
			}
		default:
			// 未知标签：协议破坏，明说不静默。
			_ = s.sendControl(c, controlFrame{Op: "ready", Error: fmt.Sprintf("未知帧标签 0x%02x", frame[0])})
		}
	}
}

// routeAction 是"手里这块字节该给谁、泵还继不继续"的决策枚举。决策抽成
// 纯函数：抢主/无主/退场三条分支在 goroutine 竞态里没法稳定地测（测出来
// 的绿要赌时序），决策表本身必须逐行可断言。
type routeAction int

const (
	routeKeep    routeAction = iota // 现任就是自己：发出，继续
	routeHandoff                    // 被抢主：手中帧转交现任，退场
	routeDrain                      // 无主：帧弃、起 drain，退场
	routeExit                       // 读结束（EOF/错）：通知在场者 exit，退场
)

// routeChunk 决策表。gotData=本轮 Read 是否拿到字节；cur=读出后的 owner 快照。
func routeChunk(cur, my *streamIO, gotData bool, readErr error) (routeAction, *streamIO) {
	switch {
	case gotData && cur == my:
		return routeKeep, my
	case gotData && cur != nil:
		return routeHandoff, cur
	case gotData:
		return routeDrain, nil
	case readErr != nil:
		return routeExit, cur // cur 可能为 nil（无主可通知）
	default:
		return routeKeep, my // n==0 且无错（os 层罕见，保持循环）
	}
}

// pumpOutput pty 输出 → 现任连接。三种收场都写明：被抢主（转交手中帧后退）、
// 无主（交棒 drain 后退）、pty 死（exit 帧后退）。绝不"只丢帧不退泵"——
// 双读同一管道会让现任永久缺约一半输出（审计 M1）。
func (s *TerminalService) pumpOutput(sess *session, my *streamIO) {
	buf := make([]byte, 16*1024)
	for {
		n, err := sess.pty.Read(buf)
		cur := sess.owner.Load()
		var frame []byte
		if n > 0 {
			frame = make([]byte, 0, n+1)
			frame = append(frame, frameRaw)
			frame = append(frame, buf[:n]...)
		}
		act, target := routeChunk(cur, my, n > 0, err)
		switch act {
		case routeKeep:
			if len(frame) == 0 {
				continue // n==0 无错的罕见轮：不发空帧
			}
			if serr := (*target).Send(frame); serr != nil {
				// 发不出去多半是连接已死：尽力补一条错误说明，泵绝不静默死（审计 M4）。
				_ = s.sendControl(*target, controlFrame{Op: "ready", Error: "输出通道写入失败，泵已收场（画面可能截断）"})
				return
			}
		case routeHandoff:
			// 手中帧转交现任（OS 管道到达序保证不缺不重），随即退场。
			_ = (*target).Send(frame)
			return
		case routeDrain:
			// 无主：这一帧丢弃（无人认领），起 drain 保证子进程不被反压冻住后退场。
			s.startDrain(sess)
			return
		case routeExit:
			if target != nil {
				_ = s.sendControl(*target, controlFrame{Op: "exit"})
			}
			return
		}
	}
}

// startDrain 无主期间的丢弃式读泵：读出来就扔。存在的意义是**保持子进程
// 能写**——OS 管道默认 4KB（审计实证），断线后不 drain，洪峰输出会把
// claude/powershell 反压冻死，"断线不杀进程"就成了假话。同一会话至多一只
// （draining 位 CAS）；新主上位后 drain 自查离场（循环顶检查 owner），
// 至多一次在途 Read 的字节归 drain——窗口一帧，注释即账。
func (s *TerminalService) startDrain(sess *session) {
	if sess.exited.Load() {
		return
	}
	if !sess.draining.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer sess.draining.Store(false)
		buf := make([]byte, 16*1024)
		for {
			if sess.owner.Load() != nil || sess.exited.Load() {
				return // 新主上位或会话终了，drain 让位
			}
			// 真 Pty.Read 无字节即阻塞、直到 EOF——drain 也必须"边等边可离场"，
			// 否则新主上位时我们还挂在一口永远没来的 Read 上（单测第一跑就是
			// 这样卡住 exit 断言的）。窄读接口把"无限等"切成 200ms 轮次：
			// 空轮继续、有字节吞掉、EOF/错收场。真 conpty 无数据时 Read 返回
			// (0,nil) 还是阻塞到 n>0 属 [未验证]，轮次对两种形态都成立。
			if _, err := readPtyChunked(sess.pty, buf, 200*time.Millisecond); err != nil {
				return
			}
		}
	}()
}

// readPtyChunked 带单轮时限地读一块字节；超时返回 (0, nil) 表示"这轮没字节"。
// 生产 pty 读永不返回 0,nil——该形态仅假件用于离场时序，调用方（drain）
// 两种都当"继续"处理，语义安全。
func readPtyChunked(p ptyLike, buf []byte, wait time.Duration) (int, error) {
	type rn struct {
		n   int
		err error
	}
	ch := make(chan rn, 1)
	go func() {
		n, err := p.Read(buf)
		ch <- rn{n, err}
	}()
	select {
	case r := <-ch:
		return r.n, r.err
	case <-time.After(wait):
		return 0, nil
	}
}

// resolveProfile 三档 profile → 命令行。claude 档经 LookPath 解析 .cmd 绝对路径
// 再借 cmd.exe /c 拉起（CreateProcess 只自动补 .exe，不给 .cmd——探针 run3 的
// 实证姿势；PATH 里没有 claude 时报 aiagent 同款"未安装"人话）。
func resolveProfile(id string) (command string, args []string, title string, err error) {
	switch id {
	case "cmd":
		return "cmd.exe", nil, "命令行", nil
	case "powershell":
		return "powershell.exe", []string{"-NoProfile"}, "PowerShell", nil
	case "claude":
		bin, lerr := exec.LookPath("claude")
		if lerr != nil || strings.TrimSpace(bin) == "" {
			return "", nil, "", aiagent.ErrNotInstalled
		}
		return "cmd.exe", []string{"/c", bin}, "Claude Code", nil
	default:
		return "", nil, "", fmt.Errorf("未知终端档 %q", id)
	}
}
