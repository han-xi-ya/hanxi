package msgboard

import (
	"strings"
	"sync"
	"testing"
	"time"

	"hanxi/internal/platform"
	"hanxi/internal/platform/apppackage"
)

// 无头环境可测的服务层路径：热键改判失败回滚、挂牌入口的实例守卫、
// 正文回落与状态回显。真机 GUI 行为（全屏观感/防休眠实测）不在此列。

func newTestService(t *testing.T) *MsgBoardService {
	t.Helper()
	return newMsgBoardService(nil, newMsgBoardStore(t.TempDir()))
}

func TestServiceToggleWithoutApp(t *testing.T) {
	// 无头：application.Get()==nil，Show 必须报"需应用运行"而非 panic。
	s := newTestService(t)
	if err := s.start(); err != nil {
		t.Fatal(err)
	}
	if err := s.Show(); err == nil || !strings.Contains(err.Error(), "应用运行") {
		t.Fatalf("无应用实例 Show 应中文报错，实得 %v", err)
	}
	if err := s.Toggle(); err == nil || !strings.Contains(err.Error(), "应用运行") {
		t.Fatalf("Toggle 应透传 Show 错误，实得 %v", err)
	}
	// 未挂牌时 Dismiss 幂等无操作。
	s.Dismiss()
	if st := s.GetStatus(); st.Shown {
		t.Fatal("Show 失败后不应留在已挂出态")
	}
}

// 热键改判：注册通路不可用（无头）→ 配置热键字段回滚旧值并报错，
// 磁盘/内存/回显三者不得留下"看起来已生效"的假状态。
func TestServiceSetConfigHotkeyRollback(t *testing.T) {
	s := newTestService(t)
	old := s.store.Get()

	err := s.SetConfig(Config{Text: "会议中", FontSize: 64, Screen: "", Hotkey: "Ctrl+Alt+J"})
	if err == nil || !strings.Contains(err.Error(), "注册失败") {
		t.Fatalf("无应用实例时改热键应报错回滚，实得 %v", err)
	}
	if got := s.store.Get().Hotkey; got != old.Hotkey {
		t.Fatalf("热键应回滚为 %q，实为 %q", old.Hotkey, got)
	}
	if got := s.store.Get().Text; got != "会议中" {
		t.Fatalf("非热键字段应照常落盘，实为 %q", got)
	}
}

// 热键未变时 SetConfig 不触碰注册通路（无头也能保存成功）。
func TestServiceSetConfigKeepsHotkeyUnbound(t *testing.T) {
	s := newTestService(t)
	cfg := s.store.Get()
	if err := s.SetConfig(cfg); err != nil {
		t.Fatalf("同值保存不应报错：%v", err)
	}
	if st := s.GetStatus(); st.HotkeyActive {
		t.Fatal("无头环境热键不可能在位，HotkeyActive 谎报")
	}
}

func TestServiceBoardContentAndPresets(t *testing.T) {
	s := newTestService(t)
	if c := s.GetBoardContent(); c.Text != Presets[0] || c.FontSize != 64 {
		t.Fatalf("默认挂牌内容 %+v，期望回落首条预设 64 号", c)
	}
	if err := s.SetConfig(Config{Text: "请勿动我电脑", FontSize: 120, Screen: "", Hotkey: defaultHotkey}); err != nil {
		t.Fatal(err)
	}
	if c := s.GetBoardContent(); c.Text != "请勿动我电脑" || c.FontSize != 120 {
		t.Fatalf("挂牌内容未随配置更新：%+v", c)
	}
	if got := s.ListPresets(); len(got) != len(Presets) {
		t.Fatalf("预设清单长度 %d", len(got))
	}
	got := s.ListPresets()
	got[0] = "污染"
	if Presets[0] == "污染" {
		t.Fatal("ListPresets 必须返回副本，不得暴露内部切片")
	}
}

// 停用/退场收口：stop 撤热键+撤牌幂等，可重复调用。
func TestServiceStopIdempotent(t *testing.T) {
	s := newTestService(t)
	if err := s.stop(); err != nil {
		t.Fatalf("未 start 的 stop 应幂等：%v", err)
	}
	if err := s.start(); err != nil {
		t.Fatalf("start（无头降级热键）应成功：%v", err)
	}
	if err := s.stop(); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if err := s.stop(); err != nil {
		t.Fatalf("重复 stop: %v", err)
	}
}

type blockingKeepAwake struct {
	mu             sync.Mutex
	acquireStarted chan struct{}
	acquireRelease chan struct{}
	holders        map[string]platform.KeepAwakeScope
}

func newBlockingKeepAwake() *blockingKeepAwake {
	return &blockingKeepAwake{
		acquireStarted: make(chan struct{}),
		acquireRelease: make(chan struct{}),
		holders:        make(map[string]platform.KeepAwakeScope),
	}
}

func (k *blockingKeepAwake) Acquire(holder string, scope platform.KeepAwakeScope) error {
	close(k.acquireStarted)
	<-k.acquireRelease
	k.mu.Lock()
	k.holders[holder] = scope
	k.mu.Unlock()
	return nil
}

func (k *blockingKeepAwake) Release(holder string) error {
	k.mu.Lock()
	delete(k.holders, holder)
	k.mu.Unlock()
	return nil
}

func (k *blockingKeepAwake) Holders() []string {
	k.mu.Lock()
	defer k.mu.Unlock()
	out := make([]string, 0, len(k.holders))
	for holder := range k.holders {
		out = append(out, holder)
	}
	return out
}

type msgboardTestPlatform struct{ keepAwake platform.KeepAwakeAPI }

func (p msgboardTestPlatform) Network() platform.NetworkAPI                       { return nil }
func (p msgboardTestPlatform) Port() platform.PortAPI                             { return nil }
func (p msgboardTestPlatform) Process() platform.ProcessAPI                       { return nil }
func (p msgboardTestPlatform) Job() platform.JobAPI                               { return nil }
func (p msgboardTestPlatform) AppPackage() apppackage.API                         { return nil }
func (p msgboardTestPlatform) KeepAwake() platform.KeepAwakeAPI                   { return p.keepAwake }
func (p msgboardTestPlatform) DesktopDir() (string, error)                        { return "", nil }
func (p msgboardTestPlatform) CreateDesktopShortcut(string, string, string) error { return nil }
func (p msgboardTestPlatform) OpenURL(string) error                               { return nil }

// stop 必须等待在途挂牌收口，并在返回前清除最终状态；这里用阻塞 KeepAwake
// 精确模拟 Show 已建窗提交、但 Acquire 尚未返回的竞态窗口。
func TestServiceStopWaitsForInflightShowCleanup(t *testing.T) {
	awake := newBlockingKeepAwake()
	s := newMsgBoardService(msgboardTestPlatform{keepAwake: awake}, newMsgBoardStore(t.TempDir()))

	s.mu.Lock()
	s.started = true
	s.generation = 1
	s.opInFlight = true
	s.shown = true
	s.mu.Unlock()

	showDone := make(chan struct{})
	go func() {
		s.acquireKeepAwake()
		s.endOperation()
		close(showDone)
	}()
	<-awake.acquireStarted

	stopDone := make(chan error, 1)
	go func() { stopDone <- s.stop() }()
	select {
	case err := <-stopDone:
		t.Fatalf("stop 不应越过在途 Show 返回：%v", err)
	case <-time.After(30 * time.Millisecond):
	}

	close(awake.acquireRelease)
	select {
	case <-showDone:
	case <-time.After(time.Second):
		t.Fatal("在途 Show 未收口")
	}
	select {
	case err := <-stopDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("stop 未在 Show 收口后完成")
	}

	if st := s.GetStatus(); st.Shown || st.KeepAwake || st.HotkeyActive {
		t.Fatalf("stop 返回后状态未清空：%+v", st)
	}
	if holders := awake.Holders(); len(holders) != 0 {
		t.Fatalf("stop 返回后仍有 KeepAwake 持有人：%v", holders)
	}
	s.mu.Lock()
	board := s.board
	started, stopping, inFlight := s.started, s.stopping, s.opInFlight
	s.mu.Unlock()
	if board != nil || started || stopping || inFlight {
		t.Fatalf("stop 最终状态异常：board=%v started=%v stopping=%v inFlight=%v", board, started, stopping, inFlight)
	}
}
