package wsl

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hanxi/internal/modules/wsl/usbipd"
)

// fakeUSB usbipd 用户态命令面替身（接口注入，全离线）。
type fakeUSB struct {
	version    string
	versionErr error
	devices    []usbipd.Device
	stateErr   error
	attachLog  []string // "distro|busid"
	attachErr  map[string]error
	detachLog  []string
}

func (f *fakeUSB) Version(context.Context) (string, error) {
	if f.versionErr != nil {
		return "", f.versionErr
	}
	return f.version, nil
}

func (f *fakeUSB) State(context.Context) ([]usbipd.Device, error) {
	if f.stateErr != nil {
		return nil, f.stateErr
	}
	return f.devices, nil
}

func (f *fakeUSB) Attach(_ context.Context, distro, busID string) error {
	key := distro + "|" + busID
	f.attachLog = append(f.attachLog, key)
	return f.attachErr[key]
}

func (f *fakeUSB) Detach(_ context.Context, busID string) error {
	f.detachLog = append(f.detachLog, busID)
	return nil
}

// wslNames 构造 runWsl 替身：`-l -q[ --running]` 出名单，其余命令默认成功。
func wslNames(all, running []string) func(context.Context, ...string) (string, error) {
	return func(_ context.Context, args ...string) (string, error) {
		joined := strings.Join(args, " ")
		switch {
		case joined == "-l -q":
			return strings.Join(all, "\n"), nil
		case joined == "-l -q --running":
			return strings.Join(running, "\n"), nil
		}
		return "", nil
	}
}

func newUSBFakeService(t *testing.T, cli usbCLI, runWsl func(context.Context, ...string) (string, error)) (*WslService, *fakeElevated) {
	t.Helper()
	ev := &fakeElevated{out: OperationOutcome{Success: true, Message: "ok"}}
	svc := &WslService{
		opener:        &fakeOpener{},
		elevProc:      ev.run,
		runWsl:        runWsl,
		usbRun:        cli,
		usbPath:       filepath.Join(t.TempDir(), "wsl-usbipd.json"),
		emit:          func(string, any) {},
		distroOps:     map[string]string{},
		exportRecords: map[string]ExportRecord{},
		cloneOps:      map[string]longOpHandle{},
	}
	return svc, ev
}

func dev(busID, vid, pid, state string) usbipd.Device {
	return usbipd.Device{BusID: busID, Vid: vid, Pid: pid, Description: "dev " + busID, State: state}
}

func identifiedDev(busID, vid, pid, instanceID, serial, guid, state string) usbipd.Device {
	return usbipd.Device{
		BusID:       busID,
		Vid:         vid,
		Pid:         pid,
		InstanceID:  instanceID,
		Serial:      serial,
		Guid:        guid,
		Description: "dev " + busID,
		State:       state,
	}
}

func entry(id, busID, vid, pid, distro string) USBShareEntry {
	return USBShareEntry{ID: id, BusID: busID, Vid: vid, Pid: pid, Description: "dev " + busID, Distro: distro, Enabled: true}
}

func runningSet(names ...string) map[string]bool {
	m := map[string]bool{}
	for _, n := range names {
		m[strings.ToLower(n)] = true
	}
	return m
}

// ---- 重放计划纯函数 ----

func TestPlanUsbReplayRejectsDifferentDeviceReusingBusID(t *testing.T) {
	// 账本记录的是 VID=aaaa 的 A；同一端口如今插着 VID=bbbb 的 B。
	// BusID 只是拓扑位置，不能绕过物理身份核验触发 bind/attach。
	entries := []USBShareEntry{entry("e1", "2-3", "aaaa", "0001", "Ubuntu")}
	devices := []usbipd.Device{dev("2-3", "bbbb", "0002", usbipd.StateNotShared)}
	steps := planUsbReplay(entries, devices, runningSet("Ubuntu"), true)
	if len(steps) != 1 || steps[0].Kind != usbStepSkip {
		t.Fatalf("同端口换成其他设备必须 skip，绝不能 bind/attach: %+v", steps)
	}
	if !strings.Contains(steps[0].Reason, "身份") {
		t.Fatalf("skip 必须明确归因物理身份不匹配: %+v", steps[0])
	}
}

func TestPlanUsbReplayMatrix(t *testing.T) {
	entries := []USBShareEntry{
		entry("e1", "1-1", "aaaa", "0001", "Ubuntu"), // 已共享未附加 → attach
		entry("e2", "2-2", "bbbb", "0002", "Ubuntu"), // 未共享 → bind+attach
		entry("e3", "3-3", "cccc", "0003", "Ubuntu"), // 已附加 → skip
		entry("e4", "4-4", "dddd", "0004", "Fedora"), // 发行版未运行 → skip
		entry("e5", "5-5", "eeee", "0005", "Ubuntu"), // 不在场且无 vid/pid 线索 → skip
		entry("e6", "9-9", "ffff", "0006", "Ubuntu"), // 换插口：VID:PID 唯一命中 6-1 → attach(6-1)
		entry("e7", "7-7", "ffff", "0007", "Ubuntu"), // 不在场但已共享沉底 → 仍算不在场 skip
		entry("e8", "8-8", "ffff", "0008", "Ubuntu"), // 停用 → 无步骤
	}
	entries[7].Enabled = false
	devices := []usbipd.Device{
		dev("1-1", "aaaa", "0001", usbipd.StateShared),
		dev("2-2", "bbbb", "0002", usbipd.StateNotShared),
		dev("3-3", "cccc", "0003", usbipd.StateAttached),
		dev("4-4", "dddd", "0004", usbipd.StateShared),
		dev("6-1", "ffff", "0006", usbipd.StateShared),
		{BusID: "", Vid: "ffff", Pid: "0007", Guid: "g", State: usbipd.StateShared}, // 已共享未在场
		dev("8-8", "ffff", "0008", usbipd.StateShared),
	}
	steps := planUsbReplay(entries, devices, runningSet("Ubuntu"), true)
	if len(steps) != len(entries)-1 { // e8 停用无步骤
		t.Fatalf("步骤数 = %d, want %d: %+v", len(steps), len(entries)-1, steps)
	}
	want := map[string]string{ // 条目 ID → kind
		"e1": usbStepAttach, "e2": usbStepBind, "e3": usbStepSkip,
		"e4": usbStepSkip, "e5": usbStepSkip, "e6": usbStepAttach, "e7": usbStepSkip,
	}
	byID := map[string]usbPlanStep{}
	for _, st := range steps {
		byID[st.Entry.ID] = st
	}
	for id, kind := range want {
		st, ok := byID[id]
		if !ok {
			t.Fatalf("%s 应有步骤", id)
		}
		if st.Kind != kind {
			t.Errorf("%s kind = %s, want %s（reason=%s）", id, st.Kind, kind, st.Reason)
		}
	}
	if byID["e6"].BusID != "6-1" || !strings.Contains(byID["e6"].Reason, "换插口") {
		t.Errorf("e6 换插口回落失败: %+v", byID["e6"])
	}
	if !strings.Contains(byID["e7"].Reason, "不在场") {
		t.Errorf("e7 应判不在场: %+v", byID["e7"])
	}
	if !strings.Contains(byID["e4"].Reason, "未运行") {
		t.Errorf("e4 应判发行版未运行: %+v", byID["e4"])
	}
}

func TestPlanUsbReplayAmbiguousVidPidSkips(t *testing.T) {
	entries := []USBShareEntry{entry("e1", "9-9", "ffff", "0006", "Ubuntu")}
	devices := []usbipd.Device{dev("6-1", "ffff", "0006", usbipd.StateShared), dev("6-2", "ffff", "0006", usbipd.StateShared)}
	steps := planUsbReplay(entries, devices, runningSet("Ubuntu"), true)
	if len(steps) != 1 || steps[0].Kind != usbStepSkip {
		t.Fatalf("同 VID:PID 多义必须 skip 而非挑一个: %+v", steps)
	}
}

func TestPlanUsbReplayRunningUnknown(t *testing.T) {
	// 运行名单通道故障（runningOK=false）：不做未运行拦截，交给 usbipd 自己报错。
	entries := []USBShareEntry{entry("e1", "1-1", "a", "b", "Any")}
	devices := []usbipd.Device{dev("1-1", "a", "b", usbipd.StateShared)}
	steps := planUsbReplay(entries, devices, nil, false)
	if len(steps) != 1 || steps[0].Kind != usbStepAttach {
		t.Fatalf("名单不可得时应放行 attach: %+v", steps)
	}
}

// ---- 账本落盘 ----

func TestUsbLedgerRoundTripAndUpsert(t *testing.T) {
	cli := &fakeUSB{version: "5.3.0", devices: []usbipd.Device{dev("2-3", "aaaa", "0001", usbipd.StateNotShared)}}
	svc, _ := newUSBFakeService(t, cli, wslNames([]string{"Ubuntu", "Debian"}, []string{"Ubuntu"}))

	out, err := svc.SetUsbShare("2-3", "Ubuntu")
	if err != nil || !out.Success {
		t.Fatalf("登记失败: %v %+v", err, out)
	}
	led, err := svc.usbLoad()
	if err != nil || len(led.Entries) != 1 || led.Entries[0].Distro != "Ubuntu" {
		t.Fatalf("账本落盘异常: %+v %v", led, err)
	}
	if led.AutoEnabled {
		t.Error("总开关默认必须关（卡片裁定）")
	}
	// 同 busid 再登记 = 改目标发行版不换身份。
	firstID := led.Entries[0].ID
	if _, err := svc.SetUsbShare("2-3 ", "Debian"); err != nil {
		t.Fatal(err)
	}
	led, _ = svc.usbLoad()
	if len(led.Entries) != 1 || led.Entries[0].Distro != "Debian" || led.Entries[0].ID != firstID {
		t.Fatalf("改绑语义破裂: %+v", led.Entries)
	}
	// 不在场设备拒绝登记。
	if _, err := svc.SetUsbShare("9-9", "Ubuntu"); err == nil {
		t.Error("不在场设备必须拒绝登记")
	}
	// 停用/删除。
	if _, err := svc.SetUsbShareEnabled(firstID, false); err != nil {
		t.Fatal(err)
	}
	led, _ = svc.usbLoad()
	if led.Entries[0].Enabled {
		t.Error("停用未落账")
	}
	if _, err := svc.RemoveUsbShare(firstID); err != nil {
		t.Fatal(err)
	}
	led, _ = svc.usbLoad()
	if len(led.Entries) != 0 {
		t.Fatalf("删除未落账: %+v", led.Entries)
	}
	// 发行版白名单：不在 `wsl -l -q` 名单里的名字必须被拒（不触达任何写盘）。
	if _, err := svc.SetUsbShare("2-3", "Evil;calc"); err == nil {
		t.Error("非白名单发行版必须拒绝")
	}
}

func TestUsbLedgerCorruptRefusesOverwrite(t *testing.T) {
	cli := &fakeUSB{version: "5.3.0", devices: []usbipd.Device{dev("2-3", "a", "b", usbipd.StateShared)}}
	svc, _ := newUSBFakeService(t, cli, wslNames([]string{"Ubuntu"}, nil))
	if err := os.WriteFile(svc.usbPath, []byte("{ broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	view, err := svc.GetUsbOverview()
	if err == nil || !strings.Contains(err.Error(), "损坏") {
		t.Fatalf("损坏账本必须报错拦写: %v", err)
	}
	if view.ReleasesPage != usbipdReleasesURL {
		t.Error("引导卡固定地址缺失")
	}
	if _, err := svc.SetUsbShare("2-3", "Ubuntu"); err == nil {
		t.Error("损坏账本上禁止追加写")
	}
}

// ---- 重放执行 ----

func TestReplaySkippedWhenAutoOff(t *testing.T) {
	cli := &fakeUSB{version: "5.3.0", devices: []usbipd.Device{dev("1-1", "a", "b", usbipd.StateShared)}}
	svc, ev := newUSBFakeService(t, cli, wslNames([]string{"Ubuntu"}, nil))
	if _, err := svc.usbPatchAppend(entry("e1", "1-1", "a", "b", "Ubuntu")); err != nil {
		t.Fatal(err)
	}
	// 非 manual 触发 + 开关关：秒退不取数。
	if _, err := svc.runUsbReplay(context.Background(), "", "startup"); err != nil {
		t.Fatal(err)
	}
	if len(cli.attachLog) > 0 || len(ev.calls) > 0 {
		t.Fatalf("开关关不得有任何动作: %v %v", cli.attachLog, ev.calls)
	}
}

func TestReplayManualAttachesAndRecords(t *testing.T) {
	cli := &fakeUSB{version: "5.3.0", devices: []usbipd.Device{
		dev("1-1", "aaaa", "0001", usbipd.StateShared),
		dev("2-2", "bbbb", "0002", usbipd.StateShared),
	}}
	cli.attachErr = map[string]error{"Ubuntu|2-2": errors.New("attach failed: no wsl client")}
	svc, _ := newUSBFakeService(t, cli, wslNames([]string{"Ubuntu"}, []string{"Ubuntu"}))
	svc.usbSave(usbLedger{AutoEnabled: true, Entries: []USBShareEntry{
		entry("e1", "1-1", "aaaa", "0001", "Ubuntu"),
		entry("e2", "2-2", "bbbb", "0002", "Ubuntu"),
	}})
	out, err := svc.ReplayUsbNow()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.Message, "附加 1 台") || !strings.Contains(out.Message, "no wsl client") {
		t.Fatalf("回执必须同时有成功数与失败归因: %+v", out)
	}
	if strings.Join(cli.attachLog, ",") != "Ubuntu|1-1,Ubuntu|2-2" {
		t.Errorf("attach 次序/参数异常: %v", cli.attachLog)
	}
	led, _ := svc.usbLoad()
	if !strings.Contains(led.Entries[0].LastStatus, "已附加到 Ubuntu") {
		t.Errorf("e1 状态未记成功: %q", led.Entries[0].LastStatus)
	}
	if !strings.Contains(led.Entries[1].LastStatus, "附加失败") || led.Entries[1].LastAt == "" {
		t.Errorf("e2 失败状态未落账: %+v", led.Entries[1])
	}
}

func TestReplayBindsViaSingleUACThenAttaches(t *testing.T) {
	cli := &fakeUSB{version: "5.3.0", devices: []usbipd.Device{
		dev("1-1", "aaaa", "0001", usbipd.StateNotShared),
		dev("2-2", "bbbb", "0002", usbipd.StateNotShared),
	}}
	svc, ev := newUSBFakeService(t, cli, wslNames([]string{"Ubuntu"}, []string{"Ubuntu"}))
	svc.usbSave(usbLedger{AutoEnabled: true, Entries: []USBShareEntry{
		entry("e1", "1-1", "aaaa", "0001", "Ubuntu"),
		entry("e2", "2-2", "bbbb", "0002", "Ubuntu"),
	}})
	if _, err := svc.ReplayUsbNow(); err != nil {
		t.Fatal(err)
	}
	if len(ev.calls) != 1 {
		t.Fatalf("两台待绑设备必须合批一次 UAC, got %d calls", len(ev.calls))
	}
	script := fmt.Sprint(ev.calls[0].args)
	if !strings.Contains(script, "usbipd bind --busid 1-1") || !strings.Contains(script, "usbipd bind --busid 2-2") {
		t.Errorf("提权脚本缺绑定命令: %s", script)
	}
	if !strings.Contains(script, "$LASTEXITCODE") {
		t.Errorf("提权脚本必须逐条传播退出码（#37 红线）: %s", script)
	}
	if len(cli.attachLog) != 2 {
		t.Errorf("绑定成功后应继续两台 attach: %v", cli.attachLog)
	}
}

func TestReplayUacCancelKeepsLedgerHonest(t *testing.T) {
	cli := &fakeUSB{version: "5.3.0", devices: []usbipd.Device{dev("1-1", "aaaa", "0001", usbipd.StateNotShared)}}
	svc, ev := newUSBFakeService(t, cli, wslNames([]string{"Ubuntu"}, []string{"Ubuntu"}))
	ev.out = OperationOutcome{Success: false, Message: "已取消 UAC 授权，操作未执行"}
	svc.usbSave(usbLedger{AutoEnabled: true, Entries: []USBShareEntry{entry("e1", "1-1", "aaaa", "0001", "Ubuntu")}})
	out, err := svc.ReplayUsbNow()
	if err != nil {
		t.Fatalf("UAC 取消不能炸调用链: %v", err)
	}
	if !strings.Contains(out.Message, "绑定失败") {
		t.Errorf("回执须点名绑定失败: %+v", out)
	}
	if len(cli.attachLog) != 0 {
		t.Error("绑定未成不得抢跑 attach")
	}
	led, _ := svc.usbLoad()
	if !strings.Contains(led.Entries[0].LastStatus, "绑定未完成") {
		t.Errorf("取消 UAC 要落在账本状态上: %q", led.Entries[0].LastStatus)
	}
}

func TestReplayStateFailureNotesNotPanics(t *testing.T) {
	cli := &fakeUSB{version: "5.3.0", stateErr: errors.New("usbipd 状态输出中没有可识别的 JSON")}
	svc, _ := newUSBFakeService(t, cli, wslNames([]string{"Ubuntu"}, nil))
	svc.usbSave(usbLedger{AutoEnabled: true, Entries: []USBShareEntry{entry("e1", "1-1", "a", "b", "Ubuntu")}})
	if _, err := svc.runUsbReplay(context.Background(), "", "startup"); err != nil {
		t.Fatalf("取数失败必须收成回执而非错误: %v", err)
	}
	svc.mu.Lock()
	msg := svc.usbReplayMsg
	svc.mu.Unlock()
	if !strings.Contains(msg, "重放取消") {
		t.Errorf("失败摘要未记录: %q", msg)
	}
}

func TestReplaySingleFlight(t *testing.T) {
	svc, _ := newUSBFakeService(t, &fakeUSB{version: "5.3.0"}, wslNames(nil, nil))
	svc.usbReplayBusy = true
	if _, err := svc.ReplayUsbNow(); err == nil || !strings.Contains(err.Error(), "正在进行中") {
		t.Fatalf("重放撞车必须快速失败: %v", err)
	}
}

// ---- 绑定方法（服务面） ----

func TestGetUsbOverviewNotInstalled(t *testing.T) {
	cli := &fakeUSB{versionErr: fmt.Errorf("%w: exec not found", usbipd.ErrNotInstalled)}
	svc, _ := newUSBFakeService(t, cli, wslNames(nil, nil))
	view, err := svc.GetUsbOverview()
	if err != nil {
		t.Fatalf("未安装不是错误: %v", err)
	}
	if view.Installed || view.Error != "" {
		t.Errorf("未安装态异常: %+v", view)
	}
	if view.ReleasesPage == "" {
		t.Error("引导地址缺失")
	}
}

func TestBindUsbDeviceRejectsBadBusidBeforeUAC(t *testing.T) {
	svc, ev := newUSBFakeService(t, &fakeUSB{version: "5.3.0"}, wslNames(nil, nil))
	if _, err := svc.BindUsbDevice("2-3; calc", false); err == nil {
		t.Fatal("坏 busid 必须拒绝")
	}
	if len(ev.calls) != 0 {
		t.Fatal("白名单失败不得触达提权通道")
	}
	if _, err := svc.BindUsbDevice("2-3", false); err != nil {
		t.Fatal(err)
	}
	if len(ev.calls) != 1 || ev.calls[0].file != "usbipd.exe" ||
		strings.Join(ev.calls[0].args, " ") != "bind --busid 2-3" {
		t.Fatalf("提权命令拼装异常: %+v", ev.calls)
	}
}

func TestAttachUsbDeviceWhitelistAndLift(t *testing.T) {
	var wslCalls [][]string
	cli := &fakeUSB{version: "5.3.0", devices: []usbipd.Device{dev("1-1", "a", "b", usbipd.StateShared)}}
	base := wslNames([]string{"Ubuntu"}, nil) // Ubuntu 在册、未运行
	svc, _ := newUSBFakeService(t, cli, func(ctx context.Context, args ...string) (string, error) {
		cp := append([]string{}, args...)
		wslCalls = append(wslCalls, cp)
		return base(ctx, args...)
	})
	if _, err := svc.AttachUsbDevice("1-1", "Evil"); err == nil {
		t.Fatal("非白名单发行版必须拒绝")
	}
	if len(cli.attachLog) != 0 {
		t.Fatal("白名单失败不得触达 usbipd")
	}
	out, err := svc.AttachUsbDevice("1-1", "Ubuntu")
	if err != nil || !out.Success {
		t.Fatalf("附加失败: %v %+v", err, out)
	}
	if !strings.Contains(out.Message, "已顺手拉起") {
		t.Errorf("停止实例被拉起必须如实标注: %+v", out)
	}
	lifted := false
	for _, c := range wslCalls {
		if strings.Join(c, " ") == "-d Ubuntu --exec /bin/echo hanxi-usb-lift" {
			lifted = true
		}
	}
	if !lifted {
		t.Error("未见 echo 探针拉起调用")
	}
}

func TestSetUsbAutoAttachTurnsOnAndReplays(t *testing.T) {
	cli := &fakeUSB{version: "5.3.0", devices: []usbipd.Device{dev("1-1", "aaaa", "0001", usbipd.StateShared)}}
	svc, _ := newUSBFakeService(t, cli, wslNames([]string{"Ubuntu"}, []string{"Ubuntu"}))
	// 先登记（开关未开），验证登记不依赖开关。
	if _, err := svc.SetUsbShare("1-1", "Ubuntu"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetUsbAutoAttach(true); err != nil {
		t.Fatal(err)
	}
	led, _ := svc.usbLoad()
	if !led.AutoEnabled {
		t.Fatal("总开关未落账")
	}
	// "打开即补打一发"是后台调度：轮询等在飞重放落账（2s 预算，正常毫秒级）。
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(cli.attachLog) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(cli.attachLog) != 1 || cli.attachLog[0] != "Ubuntu|1-1" {
		t.Fatalf("打开总开关未触发补打一发: %v", cli.attachLog)
	}
	led, _ = svc.usbLoad()
	if !strings.Contains(led.Entries[0].LastStatus, "已附加到 Ubuntu") {
		t.Errorf("补打一发的状态未落账: %+v", led.Entries[0])
	}
	// 关闭总开关。
	if _, err := svc.SetUsbAutoAttach(false); err != nil {
		t.Fatal(err)
	}
	led, _ = svc.usbLoad()
	if led.AutoEnabled {
		t.Fatal("总开关关闭未落账")
	}
}

func TestOpenUsbipdReleasesFixedURL(t *testing.T) {
	svc, _ := newUSBFakeService(t, &fakeUSB{}, wslNames(nil, nil))
	if err := svc.OpenUsbipdReleases(); err != nil {
		t.Fatal(err)
	}
	op := svc.opener.(*fakeOpener)
	if len(op.urls) != 1 || op.urls[0] != usbipdReleasesURL {
		t.Fatalf("引导地址必须是固定官方 releases: %v", op.urls)
	}
}

// usbPatchAppend 测试助手：直写账本（绕开设备在场校验）。
func (s *WslService) usbPatchAppend(e ...USBShareEntry) (OperationOutcome, error) {
	led, err := s.usbLoad()
	if err != nil {
		return OperationOutcome{}, err
	}
	led.Entries = append(led.Entries, e...)
	return OperationOutcome{}, s.usbSave(led)
}
