package wsl

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"hanxi/internal/extapi"
	"hanxi/internal/modules/wsl/usbipd"
)

// fakeUSB usbipd 用户态命令面替身（接口注入，全离线）。
type fakeUSB struct {
	mu          sync.Mutex
	version     string
	versionErr  error
	devices     []usbipd.Device
	stateErr    error
	stateCalls  int
	attachLog   []string // "distro|busid"
	attachErr   map[string]error
	attachBlock <-chan struct{}
	detachLog   []string
}

func (f *fakeUSB) Version(context.Context) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.versionErr != nil {
		return "", f.versionErr
	}
	return f.version, nil
}

func (f *fakeUSB) State(context.Context) ([]usbipd.Device, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stateCalls++
	if f.stateErr != nil {
		return nil, f.stateErr
	}
	return slices.Clone(f.devices), nil
}

func (f *fakeUSB) Attach(ctx context.Context, distro, busID string) error {
	if f.attachBlock != nil {
		select {
		case <-f.attachBlock:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	key := distro + "|" + busID
	f.mu.Lock()
	defer f.mu.Unlock()
	f.attachLog = append(f.attachLog, key)
	return f.attachErr[key]
}

func (f *fakeUSB) Detach(_ context.Context, busID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.detachLog = append(f.detachLog, busID)
	return nil
}

func (f *fakeUSB) setDevices(devices []usbipd.Device) {
	f.mu.Lock()
	f.devices = slices.Clone(devices)
	f.mu.Unlock()
}

func (f *fakeUSB) attaches() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.attachLog)
}

func (f *fakeUSB) stateCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.stateCalls
}

func (f *fakeUSB) detachCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.detachLog)
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
		holder: extapi.NewLeaseHolder(ID),
		opener: &fakeOpener{},
		elevProc: func(ctx context.Context, file string, args ...string) (OperationOutcome, error) {
			out, err := ev.run(ctx, file, args...)
			if file == "powershell.exe" && err == nil && out.Success && len(args) > 0 {
				script := args[len(args)-1]
				path := extractPSLiteralAfter(script, "Remove-Item -LiteralPath ")
				if path != "" {
					matches := regexp.MustCompile(`HANXI_USB_BIND\|([0-9]+-[0-9]+(?:\.[0-9]+)?)`).FindAllStringSubmatch(script, -1)
					if len(matches) == 0 {
						matches = regexp.MustCompile(`--busid ([0-9]+-[0-9]+(?:\.[0-9]+)?)`).FindAllStringSubmatch(script, -1)
					}
					var lines []string
					for _, m := range matches {
						lines = append(lines, usbBindReceiptPrefix+m[1]+"|0")
					}
					_ = os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o600)
				}
			}
			return out, err
		},
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
	if !strings.Contains(steps[0].Reason, "多个") || !strings.Contains(steps[0].Reason, "身份") {
		t.Fatalf("多义跳过必须给出明确身份归因: %+v", steps[0])
	}
}

func TestPlanUsbReplayStableSerialSurvivesPortChange(t *testing.T) {
	e := entry("e1", "2-3", "aaaa", "0001", "Ubuntu")
	e.InstanceID = `USB\\VID_AAAA&PID_0001\\SERIAL-A`
	e.Serial = "SERIAL-A"
	devices := []usbipd.Device{
		identifiedDev("4-1", "aaaa", "0001", `USB\\VID_AAAA&PID_0001\\SERIAL-A`, "SERIAL-A", "", usbipd.StateShared),
		identifiedDev("4-2", "aaaa", "0001", `USB\\VID_AAAA&PID_0001\\SERIAL-B`, "SERIAL-B", "", usbipd.StateShared),
	}
	steps := planUsbReplay([]USBShareEntry{e}, devices, runningSet("Ubuntu"), true)
	if len(steps) != 1 || steps[0].Kind != usbStepAttach || steps[0].BusID != "4-1" {
		t.Fatalf("换口应按唯一稳定序列号命中原设备: %+v", steps)
	}
}

func TestPlanUsbReplayStableIdentityDoesNotFallbackToVidPid(t *testing.T) {
	e := entry("e1", "2-3", "aaaa", "0001", "Ubuntu")
	e.InstanceID = `USB\\VID_AAAA&PID_0001\\SERIAL-A`
	e.Serial = "SERIAL-A"
	devices := []usbipd.Device{
		identifiedDev("4-1", "aaaa", "0001", `USB\\VID_AAAA&PID_0001\\SERIAL-B`, "SERIAL-B", "", usbipd.StateShared),
	}
	steps := planUsbReplay([]USBShareEntry{e}, devices, runningSet("Ubuntu"), true)
	if len(steps) != 1 || steps[0].Kind != usbStepSkip {
		t.Fatalf("已有稳定身份时禁止降级按 VID/PID 串挂同型号设备: %+v", steps)
	}
}

func TestPlanUsbReplayLegacyLedgerConservativeCompatibility(t *testing.T) {
	legacy := entry("e1", "9-9", "aaaa", "0001", "Ubuntu") // 无新增身份字段
	one := planUsbReplay([]USBShareEntry{legacy}, []usbipd.Device{
		dev("4-1", "aaaa", "0001", usbipd.StateShared),
	}, runningSet("Ubuntu"), true)
	if len(one) != 1 || one[0].Kind != usbStepAttach || one[0].BusID != "4-1" {
		t.Fatalf("旧账本唯一 VID/PID 候选应继续兼容: %+v", one)
	}
	many := planUsbReplay([]USBShareEntry{legacy}, []usbipd.Device{
		dev("4-1", "aaaa", "0001", usbipd.StateShared),
		dev("4-2", "aaaa", "0001", usbipd.StateShared),
	}, runningSet("Ubuntu"), true)
	if len(many) != 1 || many[0].Kind != usbStepSkip {
		t.Fatalf("旧账本多候选必须 fail closed: %+v", many)
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
	device := identifiedDev("2-3", "aaaa", "0001", `USB\\VID_AAAA&PID_0001\\SERIAL-A`, "SERIAL-A", "12345678-1234-1234-1234-123456789abc", usbipd.StateNotShared)
	cli := &fakeUSB{version: "5.3.0", devices: []usbipd.Device{device}}
	svc, _ := newUSBFakeService(t, cli, wslNames([]string{"Ubuntu", "Debian"}, []string{"Ubuntu"}))

	out, err := svc.SetUsbShare("2-3", "Ubuntu")
	if err != nil || !out.Success {
		t.Fatalf("登记失败: %v %+v", err, out)
	}
	led, err := svc.usbLoad()
	if err != nil || len(led.Entries) != 1 || led.Entries[0].Distro != "Ubuntu" {
		t.Fatalf("账本落盘异常: %+v %v", led, err)
	}
	got := led.Entries[0]
	if got.InstanceID != device.InstanceID || got.Vid != device.Vid || got.Pid != device.Pid || got.Serial != device.Serial || got.Guid != device.Guid {
		t.Fatalf("账本未完整保存物理身份: got=%+v device=%+v", got, device)
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

func TestUsbLedgerLegacyJSONCompatibility(t *testing.T) {
	cli := &fakeUSB{version: "5.3.0"}
	svc, _ := newUSBFakeService(t, cli, wslNames(nil, nil))
	legacy := `{"autoEnabled":true,"entries":[{"id":"old","busId":"2-3","vid":"aaaa","pid":"0001","description":"legacy","distro":"Ubuntu","enabled":true,"addedAt":"2026-01-01 00:00:00"}]}`
	if err := os.WriteFile(svc.usbPath, []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	led, err := svc.usbLoad()
	if err != nil {
		t.Fatalf("旧账本必须可读: %v", err)
	}
	if len(led.Entries) != 1 || led.Entries[0].InstanceID != "" || led.Entries[0].Serial != "" || led.Entries[0].Guid != "" {
		t.Fatalf("旧账本缺失新增字段时应按零值兼容: %+v", led)
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

func TestReplayDifferentDeviceReusingBusIDDoesNothing(t *testing.T) {
	cli := &fakeUSB{version: "5.3.0", devices: []usbipd.Device{dev("2-3", "bbbb", "0002", usbipd.StateNotShared)}}
	svc, ev := newUSBFakeService(t, cli, wslNames([]string{"Ubuntu"}, []string{"Ubuntu"}))
	svc.usbSave(usbLedger{AutoEnabled: true, Entries: []USBShareEntry{
		entry("e1", "2-3", "aaaa", "0001", "Ubuntu"),
	}})
	out, err := svc.ReplayUsbNow()
	if err != nil {
		t.Fatal(err)
	}
	if len(ev.calls) != 0 || len(cli.attachLog) != 0 {
		t.Fatalf("同端口其他设备绝不能产生 bind/attach: elev=%v attach=%v", ev.calls, cli.attachLog)
	}
	if !strings.Contains(out.Message, "跳过 1 台") {
		t.Fatalf("回执应计入跳过: %+v", out)
	}
	led, _ := svc.usbLoad()
	if !strings.Contains(led.Entries[0].LastStatus, "身份不匹配") {
		t.Fatalf("账本须明确记录身份不匹配: %+v", led.Entries[0])
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

func TestReplayBatchBindReceiptsArePerDevice(t *testing.T) {
	cli := &fakeUSB{version: "5.3.0", devices: []usbipd.Device{
		dev("1-1", "aaaa", "0001", usbipd.StateNotShared),
		dev("2-2", "bbbb", "0002", usbipd.StateNotShared),
	}}
	svc, ev := newUSBFakeService(t, cli, wslNames([]string{"Ubuntu"}, []string{"Ubuntu"}))
	svc.usbSave(usbLedger{AutoEnabled: true, Entries: []USBShareEntry{
		entry("e1", "1-1", "aaaa", "0001", "Ubuntu"),
		entry("e2", "2-2", "bbbb", "0002", "Ubuntu"),
	}})
	svc.elevProc = func(_ context.Context, file string, args ...string) (OperationOutcome, error) {
		ev.calls = append(ev.calls, elevatedCall{file: file, args: args})
		script := args[len(args)-1]
		path := extractPSLiteralAfter(script, "Remove-Item -LiteralPath ")
		if path == "" {
			t.Fatalf("脚本未含后端生成的回执文件: %s", script)
		}
		data := usbBindReceiptPrefix + "1-1|0\n" + usbBindReceiptPrefix + "2-2|17\n"
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
		return OperationOutcome{Success: true, Message: "操作已执行完毕"}, nil
	}

	out, err := svc.ReplayUsbNow()
	if err != nil {
		t.Fatal(err)
	}
	if got := cli.attaches(); strings.Join(got, ",") != "Ubuntu|1-1" {
		t.Fatalf("仅 bind 成功项可继续 attach: %v", got)
	}
	if !strings.Contains(out.Message, "2-2 绑定失败") || !strings.Contains(out.Message, "退出码 17") {
		t.Fatalf("失败项须逐条结构化归因: %+v", out)
	}
	led, _ := svc.usbLoad()
	if !strings.Contains(led.Entries[0].LastStatus, "已附加") || !strings.Contains(led.Entries[1].LastStatus, "绑定失败") {
		t.Fatalf("逐条账本状态不诚实: %+v", led.Entries)
	}
}

func TestReplayMissingBindReceiptReconcilesState(t *testing.T) {
	cli := &fakeUSB{version: "5.3.0", devices: []usbipd.Device{
		dev("1-1", "aaaa", "0001", usbipd.StateNotShared),
		dev("2-2", "bbbb", "0002", usbipd.StateNotShared),
	}}
	svc, _ := newUSBFakeService(t, cli, wslNames([]string{"Ubuntu"}, []string{"Ubuntu"}))
	svc.usbSave(usbLedger{AutoEnabled: true, Entries: []USBShareEntry{
		entry("e1", "1-1", "aaaa", "0001", "Ubuntu"),
		entry("e2", "2-2", "bbbb", "0002", "Ubuntu"),
	}})
	svc.elevProc = func(_ context.Context, _ string, args ...string) (OperationOutcome, error) {
		path := extractPSLiteralAfter(args[len(args)-1], "Remove-Item -LiteralPath ")
		if err := os.WriteFile(path, []byte(usbBindReceiptPrefix+"1-1|0\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		cli.setDevices([]usbipd.Device{
			dev("1-1", "aaaa", "0001", usbipd.StateShared),
			dev("2-2", "bbbb", "0002", usbipd.StateShared),
		})
		return OperationOutcome{Success: true}, nil
	}

	out, err := svc.ReplayUsbNow()
	if err != nil {
		t.Fatal(err)
	}
	if got := cli.attaches(); len(got) != 2 {
		t.Fatalf("缺回执但 state 确认已共享的项应继续 attach: %v", got)
	}
	if !strings.Contains(out.Message, "回执缺失，重读现态确认已共享") {
		t.Fatalf("对账路径须进入摘要: %+v", out)
	}
	if cli.stateCount() < 2 {
		t.Fatalf("缺回执必须重读 state 对账, state calls=%d", cli.stateCount())
	}
}

func extractPSLiteralAfter(script, marker string) string {
	start := strings.Index(script, marker)
	if start < 0 {
		return ""
	}
	rest := script[start+len(marker):]
	if len(rest) == 0 || rest[0] != '\'' {
		return ""
	}
	rest = rest[1:]
	end := strings.Index(rest, "'")
	if end < 0 {
		return ""
	}
	return strings.ReplaceAll(rest[:end], "''", "'")
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
	if !strings.Contains(out.Message, "绑定未确认") {
		t.Errorf("回执须点名绑定未确认: %+v", out)
	}
	if len(cli.attachLog) != 0 {
		t.Error("绑定未成不得抢跑 attach")
	}
	led, _ := svc.usbLoad()
	if !strings.Contains(led.Entries[0].LastStatus, "绑定未确认") {
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

func TestWatcherReplaysDebouncedAbsentToPresentEdge(t *testing.T) {
	oldInterval, oldDebounce := usbWatcherInterval, usbWatcherDebounce
	usbWatcherInterval, usbWatcherDebounce = 10*time.Millisecond, 15*time.Millisecond
	defer func() { usbWatcherInterval, usbWatcherDebounce = oldInterval, oldDebounce }()

	cli := &fakeUSB{version: "5.3.0"}
	svc, _ := newUSBFakeService(t, cli, wslNames([]string{"Ubuntu"}, []string{"Ubuntu"}))
	svc.usbSave(usbLedger{AutoEnabled: true, Entries: []USBShareEntry{entry("e1", "1-1", "aaaa", "0001", "Ubuntu")}})
	svc.startUsbWatcherIfNeeded()
	defer svc.cancelUsbAutomation()
	time.Sleep(15 * time.Millisecond)
	cli.setDevices([]usbipd.Device{dev("1-1", "aaaa", "0001", usbipd.StateShared)})

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && len(cli.attaches()) == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if got := cli.attaches(); len(got) != 1 || got[0] != "Ubuntu|1-1" {
		t.Fatalf("absent→present 稳定边沿应只重放一次: %v", got)
	}
	time.Sleep(50 * time.Millisecond)
	if got := cli.attaches(); len(got) != 1 {
		t.Fatalf("持续 present 不得反复重放: %v", got)
	}
}

func TestDisableCancelsWaitingAutomaticReplayButManualStillWorks(t *testing.T) {
	cli := &fakeUSB{version: "5.3.0", devices: []usbipd.Device{dev("1-1", "aaaa", "0001", usbipd.StateShared)}}
	svc, _ := newUSBFakeService(t, cli, wslNames([]string{"Ubuntu"}, []string{"Ubuntu"}))
	svc.usbSave(usbLedger{AutoEnabled: true, Entries: []USBShareEntry{entry("e1", "1-1", "aaaa", "0001", "Ubuntu")}})
	svc.scheduleUsbReplay("test", 80*time.Millisecond, "", false)
	if _, err := svc.SetUsbAutoAttach(false); err != nil {
		t.Fatal(err)
	}
	time.Sleep(120 * time.Millisecond)
	if got := cli.attaches(); len(got) != 0 {
		t.Fatalf("关闭后旧自动任务不得执行: %v", got)
	}
	if _, err := svc.ReplayUsbNow(); err != nil {
		t.Fatal(err)
	}
	if got := cli.attaches(); len(got) != 1 {
		t.Fatalf("显式立即重放应忽略总开关: %v", got)
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
	if got := cli.attaches(); len(got) != 1 || got[0] != "Ubuntu|1-1" {
		led, _ := svc.usbLoad()
		t.Fatalf("打开总开关未触发补打一发: %v ledger=%+v msg=%q", got, led, svc.usbReplayMsg)
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

// ---- N31 方案 B：winget 代装 InstallUsbipdViaWinget ----

// become 模拟"安装落地后可执行文件已可达"：清探测失败态并给出版本号。
func (f *fakeUSB) become(version string) {
	f.mu.Lock()
	f.versionErr = nil
	f.version = version
	f.mu.Unlock()
}

// notInstalledErr fakeUSB 探测替身的"未安装"态（服务层按哨兵放行代装）。
func notInstalledErr() error {
	return fmt.Errorf("%w: fake", usbipd.ErrNotInstalled)
}

func TestInstallUsbipdAlreadyInstalledShortCircuits(t *testing.T) {
	svc, _ := newUSBFakeService(t, &fakeUSB{version: "5.3.0"}, wslNames(nil, nil))
	invoked := false
	svc.usbInstall = func(context.Context) (usbipd.InstallResult, error) {
		invoked = true
		return usbipd.InstallResult{State: usbipd.InstallDone}, nil
	}
	out, err := svc.InstallUsbipdViaWinget()
	if err != nil || !out.Success || !strings.Contains(out.Message, "已经安装") {
		t.Fatalf("已装必须直回不触碰 winget: %+v %v", out, err)
	}
	if invoked {
		t.Error("前置探测命中后不得发起安装")
	}
}

func TestInstallUsbipdProbeFaultBlocksInstall(t *testing.T) {
	svc, _ := newUSBFakeService(t, &fakeUSB{versionErr: errors.New("usbipd 服务未运行")}, wslNames(nil, nil))
	invoked := false
	svc.usbInstall = func(context.Context) (usbipd.InstallResult, error) {
		invoked = true
		return usbipd.InstallResult{}, nil
	}
	_, err := svc.InstallUsbipdViaWinget()
	if err == nil || !strings.Contains(err.Error(), "探测异常") {
		t.Fatalf("非未安装类探测故障必须拦下安装: %v", err)
	}
	if invoked {
		t.Error("探测被拦时不得发起安装")
	}
}

func TestInstallUsbipdMissingWingetGuides(t *testing.T) {
	svc, _ := newUSBFakeService(t, &fakeUSB{versionErr: notInstalledErr()}, wslNames(nil, nil))
	svc.usbInstall = func(context.Context) (usbipd.InstallResult, error) {
		return usbipd.InstallResult{}, fmt.Errorf("%w: fake", usbipd.ErrWingetMissing)
	}
	_, err := svc.InstallUsbipdViaWinget()
	if err == nil || !strings.Contains(err.Error(), "App Installer") {
		t.Fatalf("winget 缺席要明确指路而非笼统失败: %v", err)
	}
}

func TestInstallUsbipdCancelledIsNotSuccess(t *testing.T) {
	svc, _ := newUSBFakeService(t, &fakeUSB{versionErr: notInstalledErr()}, wslNames(nil, nil))
	svc.usbInstall = func(context.Context) (usbipd.InstallResult, error) {
		return usbipd.InstallResult{State: usbipd.InstallCancelled}, nil
	}
	out, err := svc.InstallUsbipdViaWinget()
	if err != nil {
		t.Fatal(err)
	}
	if out.Success || !strings.Contains(out.Message, "已取消 UAC") {
		t.Fatalf("UAC 取消绝不谎报成功: %+v", out)
	}
}

func TestInstallUsbipdDoneReprobesVersion(t *testing.T) {
	cli := &fakeUSB{versionErr: notInstalledErr()}
	svc, _ := newUSBFakeService(t, cli, wslNames(nil, nil))
	svc.usbInstall = func(context.Context) (usbipd.InstallResult, error) {
		cli.become("5.3.0") // 模拟安装完成、Runner 自救后命令可达
		return usbipd.InstallResult{State: usbipd.InstallDone}, nil
	}
	out, err := svc.InstallUsbipdViaWinget()
	if err != nil || !out.Success || !strings.Contains(out.Message, "5.3.0") {
		t.Fatalf("安装成功须复验版本并带入回执: %+v %v", out, err)
	}
}

func TestInstallUsbipdAlreadyButUnreachableHonest(t *testing.T) {
	svc, _ := newUSBFakeService(t, &fakeUSB{versionErr: notInstalledErr()}, wslNames(nil, nil))
	svc.usbInstall = func(context.Context) (usbipd.InstallResult, error) {
		return usbipd.InstallResult{State: usbipd.InstallAlready}, nil
	}
	out, err := svc.InstallUsbipdViaWinget()
	if err != nil {
		t.Fatal(err)
	}
	if out.Success || !strings.Contains(out.Message, "暂未探通") {
		t.Fatalf("winget 报已装但命令未通须如实降级回执: %+v", out)
	}
}

func TestInstallUsbipdFailedKeepsAttribution(t *testing.T) {
	svc, _ := newUSBFakeService(t, &fakeUSB{versionErr: notInstalledErr()}, wslNames(nil, nil))
	svc.usbInstall = func(context.Context) (usbipd.InstallResult, error) {
		return usbipd.InstallResult{State: usbipd.InstallFailed, Detail: "winget 连不上软件源——需要联网"}, nil
	}
	out, err := svc.InstallUsbipdViaWinget()
	if err != nil {
		t.Fatal(err)
	}
	if out.Success || !strings.Contains(out.Message, "连不上软件源") {
		t.Fatalf("失败归因必须原样送达: %+v", out)
	}
}

func TestInstallUsbipdSingleFlight(t *testing.T) {
	svc, _ := newUSBFakeService(t, &fakeUSB{versionErr: notInstalledErr()}, wslNames(nil, nil))
	svc.usbInstallBusy = true
	if _, err := svc.InstallUsbipdViaWinget(); err == nil || !strings.Contains(err.Error(), "单飞") {
		t.Fatalf("在飞安装必须被单飞闸快拒: %v", err)
	}
}
