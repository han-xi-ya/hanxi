package portkill

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"hanxi/internal/extapi"
	"hanxi/internal/platform"
)

// ---------- A6 审查修复回归：连接表错误不得谎报"无占用" ----------

// flakyPort 可按族注入表读取错误；未注入错误的族返回空表（零值语义）。
type flakyPort struct {
	tcpRows map[platform.Family][]platform.TCPRow
	tcpErr  map[platform.Family]error
	udpRows map[platform.Family][]platform.UDPRow
	udpErr  map[platform.Family]error
}

func (p flakyPort) TCPTable(f platform.Family) ([]platform.TCPRow, error) {
	if err, ok := p.tcpErr[f]; ok && err != nil {
		return nil, err
	}
	return p.tcpRows[f], nil
}

func (p flakyPort) UDPTable(f platform.Family) ([]platform.UDPRow, error) {
	if err, ok := p.udpErr[f]; ok && err != nil {
		return nil, err
	}
	return p.udpRows[f], nil
}

func failAll() flakyPort {
	e := errors.New("iphelper unavailable")
	return flakyPort{
		tcpErr: map[platform.Family]error{platform.FamilyIPv4: e, platform.FamilyIPv6: e},
		udpErr: map[platform.Family]error{platform.FamilyIPv4: e, platform.FamilyIPv6: e},
	}
}

// TestQueryPortAllTablesFailMustError 锁外部事实：四路连接表全部读取失败时，
// 必须如实报错，而不是返回"空清单=无占用"的假话（修复前 4 个 continue 吞尽
// 错误，查询恒成功返回空表）。文案可读面同时构成**跨包契约回归门**：portkill
// 把 platform.Family 直喂 %s，可读性依赖 A2 在枚举侧落地的 Family.String()
// （internal/platform/family_string.go）——将来谁删了 String() 或改了词表，
// 用户文案会立刻退回 "%!s(int=…)" 乱码，由本测试含 IPv4/IPv6 + 反断言 "%!s("
// 抓住（A6 发现 → A2 根治 → A6 回归门；此处只断消费侧外部渲染事实，不抄
// family_string_test.go 的实现面断言）。
func TestQueryPortAllTablesFailMustError(t *testing.T) {
	s := NewPortKillService(fakePlat{port: failAll(), proc: &fakeProc{}}, extapi.NewLeaseHolder(ID))
	list, err := s.QueryPort(8080)
	if err == nil {
		t.Fatalf("全表失败必须报错，实际返回 %d 项且 err=nil（假话空闲）", len(list))
	}
	if list != nil {
		t.Fatalf("报错时不得附带半成品清单: %+v", list)
	}
	msg := err.Error()
	for _, want := range []string{"TCP/IPv4", "TCP/IPv6", "UDP/IPv4", "UDP/IPv6"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("错误文案应含可读协议族字样 %q，实际: %s", want, msg)
		}
	}
	if strings.Contains(msg, "%!s(") {
		t.Fatalf("错误文案含未渲染格式占位（Family 被直喂 %%s）: %s", msg)
	}
}

// TestQueryPortPartialFailureBestEffort 锁另一半语义：部分族失败（如 IPv6 栈
// 禁用）仍返回已读到的占用，不因局部降级而整体报错。
func TestQueryPortPartialFailureBestEffort(t *testing.T) {
	p := flakyPort{
		tcpRows: map[platform.Family][]platform.TCPRow{
			platform.FamilyIPv4: {{LocalPort: 8080, LocalIP: "0.0.0.0", PID: 4321, State: platform.TCPStateListen}},
		},
		tcpErr: map[platform.Family]error{platform.FamilyIPv6: errors.New("v6 disabled")},
		udpErr: map[platform.Family]error{platform.FamilyIPv6: errors.New("v6 disabled")},
	}
	s := NewPortKillService(fakePlat{port: p, proc: &fakeProc{info: platform.ProcInfo{PID: 4321, Name: "node.exe"}}}, extapi.NewLeaseHolder(ID))
	list, err := s.QueryPort(8080)
	if err != nil {
		t.Fatalf("部分族失败应尽力返回，不得整体报错: %v", err)
	}
	if len(list) != 1 || list[0].PID != 4321 {
		t.Fatalf("已读到的占用必须可见: %+v", list)
	}
}

// TestListListeningPortsAllFailMustError 同口径覆盖 LISTEN 清单入口。
func TestListListeningPortsAllFailMustError(t *testing.T) {
	s := NewPortKillService(fakePlat{port: failAll(), proc: &fakeProc{}}, extapi.NewLeaseHolder(ID))
	if _, err := s.ListListeningPorts(); err == nil {
		t.Fatal("全表失败时 LISTEN 清单必须报错而非空表谎报")
	}
}

// TestKillProcessWrappedAccessDeniedStillAsksElevation 锁外部事实：平台层
// 错误哪怕被包了一层上下文（哨兵经 %w 传递），前端拿到的仍必须是
// needElevate=true 的"该提权"指令；修复前用 == 比较，包装即漏判成普通失败。
func TestKillProcessWrappedAccessDeniedStillAsksElevation(t *testing.T) {
	proc := &fakeProc{killErr: fmt.Errorf("终止 PID 4321 失败: %w", platform.ErrAccessDenied)}
	s := NewPortKillService(fakePlat{proc: proc}, extapi.NewLeaseHolder(ID))
	res, gateErr := s.KillProcess(4321, `C:\node\node.exe`, 0)
	if gateErr != nil {
		t.Fatalf("门内失败不该走门错误通道: %v", gateErr)
	}
	if res.Success || !res.NeedElevate {
		t.Fatalf("包装后的权限不足仍须判为需提权: %+v", res)
	}
}
