package terminal

// 终端服务状态机单测：窄接口假 pty 注入，不起真伪控制台。
// 断言取外部行为（账目变化、Close 计数、帧字节形状），不断言内部字段——
// "断言抄实现等于没测"在同族用例上已经红过太多次。

import (
	"encoding/json"
	"errors"
	"testing"

	"strings"

	"hanxi/internal/extapi"
	"hanxi/packages/go/aiagent"
	"hanxi/packages/go/conpty"
)

type fakePty struct {
	closed   int
	written  [][]byte
	resizes  [][2]uint16
	resizeEr error
	done     chan struct{}
}

func newFakePty() *fakePty { return &fakePty{done: make(chan struct{})} }

func (f *fakePty) Read(b []byte) (int, error) { return 0, errors.New("unused in unit path") }
func (f *fakePty) Write(b []byte) (int, error) {
	f.written = append(f.written, append([]byte(nil), b...))
	return len(b), nil
}
func (f *fakePty) Resize(c, r uint16) error {
	f.resizes = append(f.resizes, [2]uint16{c, r})
	return f.resizeEr
}
func (f *fakePty) Close() error {
	f.closed++
	select {
	case <-f.done:
	default:
		close(f.done)
	}
	return nil
}
func (f *fakePty) Pid() uint32           { return 4242 }
func (f *fakePty) Done() <-chan struct{} { return f.done }
func (f *fakePty) Err() error            { return nil }

func newTestService(t *testing.T) (*TerminalService, *fakePty) {
	t.Helper()
	f := newFakePty()
	svc := NewService(extapi.NewLeaseHolder(ID))
	svc.spawnFn = func(string, []string, conpty.Options) (ptyLike, error) { return f, nil }
	return svc, f
}

func TestOpenKillLifecycle(t *testing.T) {
	svc, f := newTestService(t)
	row, err := svc.OpenTerminal("cmd", 100, 30)
	if err != nil {
		t.Fatal(err)
	}
	if row.Pid != 4242 || !row.Alive || row.ProfileID != "cmd" {
		t.Fatalf("行形状不对: %+v", row)
	}
	list, _ := svc.ListTerminals()
	if len(list) != 1 {
		t.Fatalf("应恰有一条: %+v", list)
	}
	if err := svc.RenameTerminal(row.ID, "编译专用"); err != nil {
		t.Fatal(err)
	}
	list, _ = svc.ListTerminals()
	if list[0].Title != "编译专用" {
		t.Fatalf("改名未生效: %+v", list[0])
	}
	if err := svc.KillTerminal(row.ID); err != nil {
		t.Fatal(err)
	}
	if f.closed != 1 {
		t.Fatalf("Kill 必须 Close 伪控制台恰好一次，得 %d", f.closed)
	}
	list, _ = svc.ListTerminals()
	if len(list) != 0 {
		t.Fatal("Kill 后应除账")
	}
	if err := svc.KillTerminal(row.ID); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("重复 Kill 应如实报: %v", err)
	}
}

func TestOpenGuards(t *testing.T) {
	svc, _ := newTestService(t)
	if _, err := svc.OpenTerminal("cmd", 0, 30); err == nil {
		t.Fatal("非法尺寸应拒")
	}
	if _, err := svc.OpenTerminal("nope", 80, 24); err == nil {
		t.Fatal("未知档应拒")
	}
}

func TestKillAllOnDisable(t *testing.T) {
	svc, f := newTestService(t)
	r1, _ := svc.OpenTerminal("cmd", 80, 24)
	r2, _ := svc.OpenTerminal("powershell", 80, 24)
	_ = r1
	_ = r2
	svc.killAll()
	if f.closed != 2 {
		t.Fatalf("停用应收掉全部会话，close=%d", f.closed)
	}
	if list, _ := svc.ListTerminals(); len(list) != 0 {
		t.Fatal("killAll 后账目应清空")
	}
}

func TestResolveProfileStatic(t *testing.T) {
	if c, _, _, err := resolveProfile("cmd"); err != nil || c != "cmd.exe" {
		t.Fatalf("cmd 档: %v %s", err, c)
	}
	if _, a, _, err := resolveProfile("powershell"); err != nil || len(a) != 1 || a[0] != "-NoProfile" {
		t.Fatalf("powershell 档: %v %v", err, a)
	}
	if _, _, _, err := resolveProfile("banana"); err == nil {
		t.Fatal("未知档必须拒")
	}
	// claude 档两态都合法（装了/没装），断言只锁"不会 panic、错误必是哨兵"：
	// 真路径由探针 run3 与机主验收覆盖。
	c, a, _, err := resolveProfile("claude")
	if err != nil {
		if !errors.Is(err, aiagent.ErrNotInstalled) {
			t.Fatalf("未装 claude 必须报 ErrNotInstalled 哨兵，得: %v", err)
		}
	} else if c != "cmd.exe" || len(a) != 2 || a[0] != "/c" {
		t.Fatalf("claude 档必须是 cmd.exe /c <绝对路径>（探针 run3 姿势），得: %s %v", c, a)
	}
}

func TestControlFrameEncoding(t *testing.T) {
	frame, err := encodeControl(controlFrame{Op: "ready", SessionID: "x1"})
	if err != nil {
		t.Fatal(err)
	}
	if frame[0] != frameControl {
		t.Fatalf("控制帧标签必须 0x00，得 0x%02x", frame[0])
	}
	var back controlFrame
	if err := json.Unmarshal(frame[1:], &back); err != nil {
		t.Fatal(err)
	}
	if back.Op != "ready" || back.SessionID != "x1" {
		t.Fatalf("回读不一致: %+v", back)
	}
	// omitempty 是契约面（前端按键存在判形），裸零值字段不许上屏
	if s := string(frame[1:]); strings.Contains(s, `"cols"`) || strings.Contains(s, `"code"`) {
		t.Fatalf("空字段泄漏进帧: %s", s)
	}
}
