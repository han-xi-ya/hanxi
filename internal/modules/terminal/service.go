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
//	0x00 = JSON 控制帧（open/ready/resize/exit）
//	0x01 = 原始终端字节（双向都是，混帧不错序）
//
// 标签制而不是"首帧 JSON 之后裸流"的隐式状态机，是为了两端任何时刻都能
// 单看一帧自证类型——调试时猜状态是最贵的。
//
// v0 语义两条，如实写在这、也如实进 UI 文案：
//   - 断线不杀进程（窗口刷新后重连即续），但**应用重启进程即清账**——
//     v0 不做会话账本持久化，终端进程与 hanxi 同生命周期是刻意决定；
//   - 一个会话同一时刻只有一路在管输出（后 attach 的接管，先的安静退场），
//     多窗同屏观流是后续批次。

import (
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

// session 一个伪控制台会话的运行态。
type session struct {
	row    TermSessionRow
	pty    ptyLike
	exited atomic.Bool
	owner  atomic.Pointer[application.StreamConn] // 当前在管输出的连接（可被抢）
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
		ID:        fmt.Sprintf("term-%d-%s", time.Now().UnixMilli(), title),
		Title:     fmt.Sprintf("%s #%d", title, s.seq),
		ProfileID: profileID,
		Pid:       pty.Pid(),
		CreatedAt: time.Now().UnixMilli(),
		Alive:     true,
	}
	sess := &session{row: row, pty: pty}
	s.sessions[row.ID] = sess
	s.mu.Unlock()

	go func() {
		<-pty.Done()
		sess.exited.Store(true)
		s.touchAlive(row.ID)
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

// KillTerminal 杀会话（进程树收口在 conpty.Close 内）并除账。
func (s *TerminalService) KillTerminal(id string) error {
	release, err := s.holder.Enter()
	if err != nil {
		return err
	}
	release()
	s.mu.Lock()
	sess, ok := s.sessions[id]
	if ok {
		delete(s.sessions, id)
	}
	s.mu.Unlock()
	if !ok {
		return ErrSessionNotFound
	}
	killErr := sess.pty.Close()
	sess.exited.Store(true)
	return killErr
}

// killAll 模块停用/应用退出收口。
func (s *TerminalService) killAll() {
	s.mu.Lock()
	ids := make([]string, 0, len(s.sessions))
	for id := range s.sessions {
		ids = append(ids, id)
	}
	s.mu.Unlock()
	for _, id := range ids {
		_ = s.KillTerminal(id)
	}
}

func (s *TerminalService) touchAlive(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess, ok := s.sessions[id]; ok {
		sess.row.Alive = false
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

// encodeControl 组一帧控制帧（纯函数，HandleStream 与测试共用——编解码只有一份）。
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

func (s *TerminalService) sendControl(c *application.StreamConn, ctl controlFrame) error {
	frame, err := encodeControl(ctl)
	if err != nil {
		return err
	}
	return c.Send(frame)
}

// handleStream GoStream 入口(包内方法——曾同名导出被 bindings 生成成前端 API,污染契约面,降为不可导出)（module.go OnInit 注册）。每个连接一条 goroutine，
// 生命周期由 c.Context() 兜住（页面刷新/窗口关闭都会到这里取消）。
func (s *TerminalService) handleStream(c *application.StreamConn) {
	defer c.Close()
	ctx := c.Context()

	var sess *session
	var myConn *application.StreamConn

	defer func() {
		if sess != nil && sess.owner.Load() == myConn {
			sess.owner.Store(nil)
		}
	}()

	for {
		select {
		case <-ctx.Done():
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
				s.mu.Lock()
				sess = s.sessions[ctl.SessionID]
				s.mu.Unlock()
				if sess == nil {
					_ = s.sendControl(c, controlFrame{Op: "ready", Error: "会话不存在（已杀死或从未存在）"})
					sess = nil
					continue
				}
				myConn = c
				// 抢主：前一任的泵循环会自行发现 owner != 它而安静退场。
				sess.owner.Store(myConn)
				_ = s.sendControl(c, controlFrame{Op: "ready", SessionID: sess.row.ID})
				go s.pumpOutput(sess, myConn)
			case "resize":
				if sess != nil && ctl.Cols >= 2 && ctl.Rows >= 2 {
					if err := sess.pty.Resize(uint16(ctl.Cols), uint16(ctl.Rows)); err != nil {
						_ = s.sendControl(c, controlFrame{Op: "exit", Error: err.Error()}) // 尽力而为：resize 失败多半进程已亡
					}
				}
			default:
				_ = s.sendControl(c, controlFrame{Op: "ready", Error: "未知控制 op: " + ctl.Op})
			}
		case frameRaw:
			if sess == nil || sess.exited.Load() {
				continue // 未 open 或已死的输入：丢弃不猜（协议错误已由前端断言测试兜住）
			}
			if _, err := sess.pty.Write(frame[1:]); err != nil {
				return
			}
		default:
			// 未知标签：协议破坏，明说不静默。
			_ = s.sendControl(c, controlFrame{Op: "ready", Error: fmt.Sprintf("未知帧标签 0x%02x", frame[0])})
		}
	}
}

// pumpOutput pty 输出 → 当前连接。owner 被抢或连接上下文结束即退；
// 读 pty 是阻塞调用，靠 Close/Kill 时 pty 收口产生 EOF 兜底退出。
func (s *TerminalService) pumpOutput(sess *session, my *application.StreamConn) {
	buf := make([]byte, 16*1024)
	for {
		n, err := sess.pty.Read(buf)
		if n > 0 {
			frame := make([]byte, 0, n+1)
			frame = append(frame, frameRaw)
			frame = append(frame, buf[:n]...)
			// 只发给现任；前任的帧丢弃（它的 onmessage 已随 Stream 关闭失效）。
			if cur := sess.owner.Load(); cur == my {
				if serr := my.Send(frame); serr != nil {
					return
				}
			}
		}
		if err != nil {
			if cur := sess.owner.Load(); cur == my {
				_ = s.sendControl(my, controlFrame{Op: "exit"})
			}
			return
		}
		select {
		case <-my.Context().Done():
			return
		default:
		}
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
