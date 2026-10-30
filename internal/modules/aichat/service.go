package aichat

// AiChatService：本机 Claude Code 聊天会话的调用面。
//
// 与 jsonfmt 那发"一次性问答"的口径分野（aiagent 包里的实测数字在这里同样成立）：
// 这里是**常驻进程**——一个会话一个 claude 进程，进程活着期间占一个背景租约，
// 停用/退出会经 lease ctx 一路传到杀树（实测 taskkill /T /F 收 .cmd→cmd.exe→node
// 三 pid；一次性 -p 那套"前台租约压满 60s drain"的教训 #132 在这里同构适用）。
//
// 能力边界只报实测过的（内核契约见 aiagent/stream.go 头注，逐条钉着探测原始帧）：
//   - 文本粒度 = content 块快照，**不是**逐 token——UI 打字机效果别按 token 许诺
//     （include-partial-messages 未实测，不进主链）；
//   - "停止" = 结束本轮并回收进程，下一条消息自动 --resume 复进（实测跨进程记忆
//     成立）；秒级 interrupt 是控制帧功能，未验证，不做；
//   - 对话正文归 claude 自己的 transcript（落 WorkDir 派生的项目目录），hanxi
//     账本只存"有哪些会话"；因此 WorkDir 必须固定——换目录 resume 随缘（实测坑）。

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hanxi/internal/extapi"
	"hanxi/packages/go/aiagent"
)

const (
	// maxMessageBytes 单条消息上限。聊天输入不该撞 jsonfmt 那 2MiB 的量级，
	// 给 256KiB 拦"整篇日志误粘"的误操作，也给 stdin 帧一个明确上界。
	maxMessageBytes = 256 << 10
	// defaultIdleReap 空闲回收窗口（Paseo 用 1h；这里保守 10min——进程被杀不丢
	// 内容，resume 实测可复进，回收便宜、不回收白烧内存，取严的一侧）。
	defaultIdleReap = 10 * time.Minute
)

var ErrSessionNotFound = errors.New("会话不存在")

// agentStream 是内核能力的窄接口——单测注入假会话即可覆盖"回合状态机"全家
// （与 jsonfmt 的 Asker 同一条理由：不注入就只能真装 CLI 真联网才能测）。
type agentStream interface {
	Send(text string) (string, error)
	Events() <-chan aiagent.Event
	Info() aiagent.SessionInfo
	StderrNote() string
	DroppedFrames() int64
	Close() error
}

// runtime 一个活着的会话进程。busy 是**会话级**单飞：EventText 不带回合标记
// （内核实测口径），回合归属靠到达序推，所以同会话一轮在途期间拒绝第二发——
// 这是协议事实逼出来的闸，不是照抄 jsonfmt 的样子。
type runtime struct {
	session agentStream
	lease   *extapi.BackgroundLease
	busy    atomic.Bool
	curTurn atomic.Pointer[string] // 在途回合 uuid（推给事件的归属）
	reap    *time.Timer
}

// Service 暴露给前端的 AI 聊天服务。
type Service struct {
	holder    *extapi.LeaseHolder
	store     *store
	workDir   string
	idleReap  time.Duration
	newStream func(ctx context.Context, opts aiagent.SessionOptions) (agentStream, error)
	// emitEvent 事件出口注入位。生产值查 application.Get()（无头环境 nil 则
	// 静默——账本与租约才是权威，事件是投影）；单测换捕获器，状态机的输出
	// 面才"可断言"而不是"只可祈祷"。
	emitEvent func(name string, payload any)

	mu       sync.Mutex
	runtimes map[string]*runtime
	// started 记录哪些会话**已成功起过进程**：首聊走 --session-id（实测 E2），
	// 之后复进走 --resume（实测 E4）。反过来对从未存在的 id 发 --resume、或对
	// 已有 transcript 的 id 再发 --session-id，都属 [未验证] 形态——不赌。
	started map[string]bool
}

// NewService 装配：账本落 stateDir，WorkDir 固定在 dataDir 下派生（transcript
// 归档依赖它，见头注）。签名收目录字符串而不是 *settings.Paths：单测不需要
// 伪造整个装配根——这也是"状态机全家可测"的前提之一。
func NewService(holder *extapi.LeaseHolder, stateDir, dataDir string) (*Service, error) {
	st, err := newStore(filepath.Join(stateDir, "aichat"))
	if err != nil {
		return nil, err
	}
	wd := filepath.Join(dataDir, "aichat", "ws")
	if err := os.MkdirAll(wd, 0o755); err != nil {
		return nil, fmt.Errorf("创建聊天工作目录失败: %w", err)
	}
	return &Service{
		holder:   holder,
		store:    st,
		workDir:  wd,
		idleReap: defaultIdleReap,
		newStream: func(ctx context.Context, opts aiagent.SessionOptions) (agentStream, error) {
			return aiagent.Dial(ctx, opts)
		},
		emitEvent: func(name string, payload any) {
			if app := application.Get(); app != nil && app.Event != nil {
				app.Event.Emit(name, payload)
			}
		},
		runtimes: map[string]*runtime{},
		started:  map[string]bool{},
	}, nil
}

// CreateSession 只落账本，**不起进程**：起进程推迟到第一条消息（按下才外发，
// 建会话不产生任何网络行为——jsonfmt 三道闸里"显式触发"的同一条纪律）。
func (s *Service) CreateSession(title string) (*SessionRow, error) {
	if err := s.admit(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(title) == "" {
		title = "新会话"
	}
	row := &SessionRow{
		ID:        aiagent.NewSessionID(),
		Title:     title,
		CreatedAt: time.Now().UnixMilli(),
	}
	if err := s.store.put(row); err != nil {
		return nil, err
	}
	return row, nil
}

func (s *Service) ListSessions() ([]SessionRow, error) {
	if err := s.admit(); err != nil {
		return nil, err
	}
	rows := s.store.list()
	out := make([]SessionRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, *r)
	}
	return out, nil
}

// RenameSession 改名只动账本。
func (s *Service) RenameSession(id, title string) error {
	if err := s.admit(); err != nil {
		return err
	}
	if strings.TrimSpace(title) == "" {
		return errors.New("会话名不能为空")
	}
	row, ok := s.store.get(id)
	if !ok {
		return ErrSessionNotFound
	}
	row.Title = strings.TrimSpace(title)
	row.Archived = false // 改名是"还要用它"的信号，封存档位顺手解除
	return s.store.put(row)
}

// CloseSession 结束会话进程并标记封存（transcript 保留，可复进）。
func (s *Service) CloseSession(id string) error {
	if err := s.admit(); err != nil {
		return err
	}
	s.teardown(id, "会话已封存（下一条消息会自动复进）")
	row, ok := s.store.get(id)
	if !ok {
		return ErrSessionNotFound
	}
	row.Archived = true
	return s.store.put(row)
}

// DeleteSession 只从账本除名并杀进程。**不删 CLI 的 transcript**——那是
// claude 自己数据根里的文件，hanxi 越界删除等于制造第二套归档语义；
// 需要彻底灭迹请在 claude 侧处置（此边界写在这，不让用户误会账本=全部数据）。
func (s *Service) DeleteSession(id string) error {
	if err := s.admit(); err != nil {
		return err
	}
	s.teardown(id, "")
	ok, err := s.store.remove(id)
	if err != nil {
		return err
	}
	if !ok {
		return ErrSessionNotFound
	}
	return nil
}

// SendMessage 投递一条消息，**RPC 立即返回回合 uuid**，正文/终态全部走事件
// （一次往返实测 2.5~8s，同步 RPC 会把调用门占成"看着卡死"——keyviz 长任务
// 模板同款分法：判定即归还 + 后台租约）。用户气泡由前端按 TurnID 乐观渲染，
// 后端不回推 user 回声（实测协议里 user 类型是 tool_result 回填位，v1 无工具，
// 造第二种"用户消息"事件等于给对账添第二个裁判）。
func (s *Service) SendMessage(id, text string) (*SendResult, error) {
	if err := s.admit(); err != nil {
		return nil, err
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, errors.New("没有要发送的内容")
	}
	if len(text) > maxMessageBytes {
		return nil, fmt.Errorf("消息 %d 字节，超过单条上限 %d——聊天窗口不是日志投喂口",
			len(text), maxMessageBytes)
	}
	row, ok := s.store.get(id)
	if !ok {
		return nil, ErrSessionNotFound
	}

	rt, err := s.ensureRuntime(id)
	if err != nil {
		return nil, err
	}
	if !rt.busy.CompareAndSwap(false, true) {
		return nil, errors.New("上一轮回答还在途中（常驻会话单轮实测 2.5~8 秒），等它收口再发")
	}
	uuid, err := rt.session.Send(text)
	if err != nil {
		rt.busy.Store(false)
		return nil, err
	}
	rt.curTurn.Store(&uuid)
	s.resetReap(id, rt)

	// 首条消息把"新会话"升格为标题：本地截取，不再外发问"给这段起个名"
	// （Paseo 用 Haiku 生成标题，那是模型二次消费，零成本档不学它）。
	if row.Title == "新会话" {
		row.Title = abbrevTitle(text)
		_ = s.store.put(row)
	}
	return &SendResult{TurnID: uuid}, nil
}

// SendResult RPC 回执。
type SendResult struct {
	TurnID string `json:"turnId"`
}

// admit 统一调用门：只做起落判定，不抱着（jsonfmt 两步门同款；
// rpc_gate_matrix 要求导出方法全接门）。
func (s *Service) admit() error {
	release, err := s.holder.Enter()
	if err != nil {
		return err
	}
	release()
	return nil
}

// ensureRuntime 拿到活的会话进程；懒起，含崩溃后的自动复进。
// resume 与否以 started 账为准（见字段注释——两向都是实测过的形态，未验证的
// 组合一次都不发）。租约 ctx 直接透传：停用/退出 → ctx 取消 → watcher 杀树。
func (s *Service) ensureRuntime(id string) (*runtime, error) {
	s.mu.Lock()
	if rt, ok := s.runtimes[id]; ok {
		s.mu.Unlock()
		return rt, nil
	}
	resume := s.started[id]
	s.mu.Unlock()

	bg, err := s.holder.EnterBackground(context.Background())
	if err != nil {
		return nil, err
	}
	sess, err := s.newStream(bg.Context(), aiagent.SessionOptions{
		SessionID: id,
		Resume:    resume,
		WorkDir:   s.workDir,
	})
	if err != nil {
		bg.Release()
		return nil, err
	}
	rt := &runtime{session: sess, lease: bg}
	s.mu.Lock()
	// 双检：Lock 间隙另一发可能已建（并发同会话两条消息——第二发会被 busy 拒，
	// 但两个 ensureRuntime 可能同时越过第一道读锁）。
	if exist, ok := s.runtimes[id]; ok {
		s.mu.Unlock()
		go func() { _ = sess.Close() }()
		bg.Release()
		return exist, nil
	}
	s.runtimes[id] = rt
	s.started[id] = true
	s.mu.Unlock()

	go s.pumpEvents(id, rt)
	go func() {
		<-bg.Context().Done() // 停用/退出：进程必须让路，租约由拆路径统一 Release
		s.teardown(id, "会话进程被取消（下次发送自动复进）")
	}()
	return rt, nil
}

// pumpEvents 内核事件 → 前端事件的翻译泵。单消费者模型（Events 只许一个
// goroutine 读）——本函数就是那个消费者，会话活着期间不退出。
func (s *Service) pumpEvents(id string, rt *runtime) {
	for ev := range rt.session.Events() {
		switch ev.Kind {
		case aiagent.EventText:
			turn := ""
			if p := rt.curTurn.Load(); p != nil {
				turn = *p
			}
			s.emitTimeline(TimelineItem{
				SessionID: id, TurnID: turn, Kind: "assistant",
				Text: ev.Text, Ts: time.Now().UnixMilli(),
			})
		case aiagent.EventTurnDone:
			r := ev.Result
			// 文本**不再双发**：实测 result.text 与末块快照一致，两处都上屏
			// 就是重复渲染；回合终态以本事件为权威，块流只是先到的笔迹。
			s.emitStats(TurnStats{
				SessionID: id, TurnID: r.TurnUUID, ElapsedMs: r.DurationMs,
				TTFTMs: r.TTFTMs, OutBytes: int64(len(r.Text)),
				TotalCostUSD: r.TotalCostUSD,
			})
			if r.IsError {
				s.emitState(SessionState{SessionID: id, State: "idle",
					Note: "本轮以错误收口（subtype=" + r.Subtype + "），可直接重发重试"})
			}
			rt.busy.Store(false)
			s.emitState(SessionState{SessionID: id, State: "idle"})
			s.resetReap(id, rt)
		case aiagent.EventDead:
			rt.busy.Store(false)
			if ev.Err == nil {
				// 体面 Close（ev.Err==nil）不该渲染成故障：note 留空、用 closed。
				s.emitState(SessionState{SessionID: id, State: "closed"})
			} else {
				note := rt.session.StderrNote()
				note = strings.TrimSpace(fmt.Sprintf("%v｜%s", ev.Err, note))
				if d := rt.session.DroppedFrames(); d > 0 {
					note = fmt.Sprintf("%s｜未识别帧 %d 条", note, d)
				}
				s.emitState(SessionState{SessionID: id, State: "dead", Note: note})
			}
			s.teardown(id, "") // 幂等；泵到此结束
			return
		}
	}
}

// teardown 拆进程（幂等；不碰 started——复进账要留着）。
func (s *Service) teardown(id, note string) {
	s.mu.Lock()
	rt, ok := s.runtimes[id]
	if ok {
		delete(s.runtimes, id)
	}
	s.mu.Unlock()
	if !ok {
		return
	}
	if rt.reap != nil {
		rt.reap.Stop()
	}
	if note != "" {
		s.emitState(SessionState{SessionID: id, State: "closed", Note: note})
	}
	if err := rt.session.Close(); err != nil {
		slog.Debug("aichat 进程收口报错（进程多半已自行退场）", "id", id, "err", err)
	}
	rt.lease.Release()
}

func (s *Service) resetReap(id string, rt *runtime) {
	if rt.reap != nil {
		rt.reap.Reset(s.idleReap)
		return
	}
	rt.reap = time.AfterFunc(s.idleReap, func() { s.teardown(id, "") })
}

// abbrevTitle 首条消息升格为标题。
func abbrevTitle(text string) string {
	r := []rune(strings.Join(strings.Fields(text), " "))
	const n = 24
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n]) + "…"
}

// 事件出口三件套经 s.emitEvent（注入位定义处有口径说明）。事件名与
// app.go RegisterEvents、composition_contract.json 三处同改，漏一处闸门红。
func (s *Service) emitTimeline(item TimelineItem) { s.emitEvent("aichat:timeline", item) }
func (s *Service) emitState(st SessionState)      { s.emitEvent("aichat:session-state", st) }
func (s *Service) emitStats(ts TurnStats)         { s.emitEvent("aichat:turn-stats", ts) }
