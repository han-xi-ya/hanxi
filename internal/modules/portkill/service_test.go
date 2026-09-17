package portkill

import (
	"context"
	"strings"
	"testing"

	"hanxi/internal/history"
	"hanxi/internal/platform"
	"hanxi/internal/platform/apppackage"
)

// ---------- 平台打桩（FEATURES §5.3 点名零测试区：借历史接入补 defer 路径兜底） ----------

// fakePlat 仅实装 Port()/Process() 两路（其余恒 nil——本包路径不会触达）。
type fakePlat struct {
	port platform.PortAPI
	proc platform.ProcessAPI
}

func (f fakePlat) Network() platform.NetworkAPI               { return nil }
func (f fakePlat) Port() platform.PortAPI                     { return f.port }
func (f fakePlat) Process() platform.ProcessAPI               { return f.proc }
func (f fakePlat) Job() platform.JobAPI                       { return nil }
func (f fakePlat) KeepAwake() platform.KeepAwakeAPI           { return nil }
func (f fakePlat) AppPackage() apppackage.API                 { return nil }
func (f fakePlat) DesktopDir() (string, error)                { return "", platform.ErrNotSupported }
func (f fakePlat) CreateDesktopShortcut(_, _, _ string) error { return platform.ErrNotSupported }
func (f fakePlat) OpenURL(string) error                       { return nil }

type fakePort struct {
	tcp map[platform.Family][]platform.TCPRow
}

func (p fakePort) TCPTable(f platform.Family) ([]platform.TCPRow, error) { return p.tcp[f], nil }
func (p fakePort) UDPTable(platform.Family) ([]platform.UDPRow, error)   { return nil, nil }

type fakeProc struct {
	info      platform.ProcInfo
	queryErr  error
	killErr   error
	protected bool
}

func (p *fakeProc) Query(uint32) (platform.ProcInfo, error) {
	if p.queryErr != nil {
		return platform.ProcInfo{}, p.queryErr
	}
	return p.info, nil
}

func (p *fakeProc) KillVerified(context.Context, platform.VerifyToken, bool) error {
	return p.killErr
}

func (p *fakeProc) IsProtected(uint32, platform.ProcInfo) bool { return p.protected }

func newSvcWithHistory(t *testing.T, plat platform.Platform) (*PortKillService, *history.Store) {
	t.Helper()
	s := NewPortKillService(plat)
	h := history.NewStore(t.TempDir())
	s.SetHistory(h)
	return s, h
}

// ---------- QueryPort：Q2 portkill 查询入库（轻量清单） ----------

func TestQueryPortRecordsHistory(t *testing.T) {
	s, h := newSvcWithHistory(t, fakePlat{
		port: fakePort{tcp: map[platform.Family][]platform.TCPRow{
			platform.FamilyIPv4: {{LocalPort: 8080, LocalIP: "0.0.0.0", PID: 4321, State: platform.TCPStateListen}},
		}},
		proc: &fakeProc{info: platform.ProcInfo{PID: 4321, Name: "node.exe", ExePath: `C:\node\node.exe`}},
	})

	list, err := s.QueryPort(8080)
	if err != nil || len(list) == 0 {
		t.Fatalf("查询失真: %+v %v", list, err)
	}
	records, _ := h.List(ID, "")
	if len(records) != 1 {
		t.Fatalf("查询应记 1 条: %+v", records)
	}
	r := records[0]
	if r.Input != "8080" || r.Extra != "query" || !strings.Contains(r.Output, "node.exe") {
		t.Fatalf("查询记录字段失真: %+v", r)
	}
	if !strings.Contains(r.Summary, "1 项占用") {
		t.Fatalf("摘要应含占用数: %q", r.Summary)
	}

	// 非法端口（error 通道）不入库
	if _, err := s.QueryPort(0); err == nil {
		t.Fatal("0 端口应被拒")
	}
	if got, _ := h.List(ID, ""); len(got) != 1 {
		t.Fatal("非法入参不得刷桶")
	}
}

// ---------- KillProcess：成败同记 ----------

func TestKillProcessRecordsSuccessAndFailure(t *testing.T) {
	proc := &fakeProc{}
	s, h := newSvcWithHistory(t, fakePlat{proc: proc})

	if res := s.KillProcess(4321, `C:\node\node.exe`, 0); !res.Success {
		t.Fatalf("桩查杀应成功: %+v", res)
	}
	records, _ := h.List(ID, "")
	if len(records) != 1 || records[0].Extra != "kill" || !strings.Contains(records[0].Summary, "成功") {
		t.Fatalf("成功记录失真: %+v", records)
	}
	if !strings.HasPrefix(records[0].Input, "PID 4321 ") {
		t.Fatalf("Input 须 PID 前缀（非纯数字，防误回填端口框）: %q", records[0].Input)
	}

	proc.killErr = platform.ErrAccessDenied
	if res := s.KillProcess(4321, `C:\node\node.exe`, 0); !res.NeedElevate {
		t.Fatalf("权限不足应给 needElevate: %+v", res)
	}
	records, _ = h.List(ID, "")
	if len(records) != 2 || !strings.HasSuffix(records[0].Extra, "kill|fail") ||
		!strings.Contains(records[0].Output, "权限不足") {
		t.Fatalf("失败记录失真: %+v", records[0])
	}
}

// ---------- KillProcessElevated：Q3 拒绝也记（denied） ----------

func TestKillProcessElevatedDeniesRecorded(t *testing.T) {
	s, h := newSvcWithHistory(t, fakePlat{proc: &fakeProc{}})

	// 系统红线（pid=4）本地直拒——不触 powershell
	if res := s.KillProcessElevated(4); res.Success {
		t.Fatal("pid 4 应被拒")
	}
	records, _ := h.List(ID, "")
	if len(records) != 1 || !strings.HasSuffix(records[0].Extra, "elevated|denied") {
		t.Fatalf("红线拒绝应记 denied: %+v", records)
	}

	// 目标不存在（非拒绝语义）→ fail 不标 denied
	s2, h2 := newSvcWithHistory(t, fakePlat{proc: &fakeProc{queryErr: context.DeadlineExceeded}})
	if res := s2.KillProcessElevated(999999); res.Success {
		t.Fatal("不存在目标应失败")
	}
	records2, _ := h2.List(ID, "")
	if len(records2) != 1 || !strings.HasSuffix(records2[0].Extra, "elevated|fail") {
		t.Fatalf("目标消失应记 fail: %+v", records2[0])
	}

	// 红线进程（Query 成功 + IsProtected）→ denied
	s3, h3 := newSvcWithHistory(t, fakePlat{proc: &fakeProc{
		info: platform.ProcInfo{PID: 777, Name: "csrss.exe"}, protected: true,
	}})
	if res := s3.KillProcessElevated(777); res.Success {
		t.Fatal("保护进程应被拒")
	}
	records3, _ := h3.List(ID, "")
	if len(records3) != 1 || !strings.HasSuffix(records3[0].Extra, "elevated|denied") {
		t.Fatalf("IsProtected 拒绝应记 denied: %+v", records3[0])
	}
}

// ---------- 未接线时全链无害 ----------

func TestWithoutHistoryNoop(t *testing.T) {
	s := NewPortKillService(fakePlat{
		port: fakePort{tcp: map[platform.Family][]platform.TCPRow{}},
		proc: &fakeProc{},
	})
	if _, err := s.QueryPort(1234); err != nil {
		t.Fatal(err)
	}
	if res := s.KillProcess(1, "", 0); !res.Success {
		t.Fatalf("未接历史不得影响查杀: %+v", res)
	}
}
