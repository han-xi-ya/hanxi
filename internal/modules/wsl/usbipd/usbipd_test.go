package usbipd

import (
	"encoding/json"
	"strings"
	"testing"
)

// 上游 usbipd-win master 的 state JSON 实测形态（InstanceId 等 PascalCase，
// BusId/PersistedGuid/StubInstanceId/ClientIPAddress 可空）。
const sampleState = `{
  "Devices": [
    {
      "BusId": "2-3",
      "InstanceId": "USB\\VID_2717&PID_507B\\0123456789ABCDEF",
      "Description": "USB Serial Propagator",
      "PersistedGuid": null,
      "StubInstanceId": null,
      "ClientIPAddress": null,
      "IsForced": false
    },
    {
      "BusId": "1-4",
      "InstanceId": "USB\\VID_1A86&PID_7523\\6&1F2E3D4C&0&1",
      "Description": "USB-SERIAL CH340",
      "PersistedGuid": "9F94F897-8B2D-47A4-9A85-2C4B9D5D2E01",
      "StubInstanceId": "USB\\VID_80EE&PID_0001\\7&ABCD&0&2",
      "ClientIPAddress": "127.0.1.1",
      "IsForced": true
    },
    {
      "BusId": "10-2",
      "InstanceId": "USB\\VID_045E&PID_07A9\\A1B2C3D4E5",
      "Description": "Microsoft Wireless Display Adapter",
      "PersistedGuid": null,
      "StubInstanceId": null,
      "ClientIPAddress": null,
      "IsForced": false
    },
    {
      "BusId": null,
      "InstanceId": "USB\\VID_0403&PID_6001\\FT123456",
      "Description": "USB Serial Port (COM3)",
      "PersistedGuid": "11111111-2222-3333-4444-555555555555",
      "StubInstanceId": null,
      "ClientIPAddress": null,
      "IsForced": false
    }
  ]
}`

func TestParseStateNormal(t *testing.T) {
	devs, err := ParseState(sampleState)
	if err != nil {
		t.Fatalf("ParseState: %v", err)
	}
	if len(devs) != 4 {
		t.Fatalf("设备行数 = %d, want 4", len(devs))
	}
	// 在场设备按 BusID 自然序（2-1 先于 2-10），不在场沉底。
	wantOrder := []string{"1-4", "2-3", "10-2", ""}
	for i, want := range wantOrder {
		if devs[i].BusID != want {
			t.Errorf("devs[%d].BusID = %q, want %q（自然序/沉底错误）", i, devs[i].BusID, want)
		}
	}
	attached := devs[0]
	if attached.State != StateAttached || !attached.Forced {
		t.Errorf("1-4 应为已附加+forced: %s forced=%v", attached.State, attached.Forced)
	}
	if attached.Vid != "1a86" || attached.Pid != "7523" {
		t.Errorf("VID/PID 应从 InstanceId 抠出且小写: %s:%s", attached.Vid, attached.Pid)
	}
	if attached.Serial != "" {
		t.Errorf("含 & 的实例尾段不是真序列号，应留空: %q", attached.Serial)
	}
	if attached.ClientIP != "127.0.1.1" {
		t.Errorf("ClientIP = %q", attached.ClientIP)
	}
	plain := devs[1]
	if plain.State != StateNotShared || plain.Guid != "" {
		t.Errorf("2-3 应未共享: %s guid=%q", plain.State, plain.Guid)
	}
	if plain.Serial != "0123456789ABCDEF" {
		t.Errorf("Serial = %q", plain.Serial)
	}
	gone := devs[3]
	if gone.State != StateShared || gone.Connected() || gone.Guid == "" {
		t.Errorf("不在场设备应为已共享沉底: %+v", gone)
	}
}

func TestParseStateEmptyArray(t *testing.T) {
	devs, err := ParseState(`{"version":"5.3.0","Devices":[]}`)
	if err != nil {
		t.Fatalf("空设备表不该报错: %v", err)
	}
	if len(devs) != 0 {
		t.Fatalf("want 0 devices, got %d", len(devs))
	}
}

func TestParseStateBareArrayAndWarnings(t *testing.T) {
	// 前置告警行 + 裸数组形态（蓝本实测兼容面）。
	out := "usbipd service not running, showing cached state\n" +
		`[{"BusId":"1-1","InstanceId":"USB\\VID_1234&PID_5678\\X","Description":"d","IsForced":false}]`
	devs, err := ParseState(out)
	if err != nil {
		t.Fatalf("裸数组+告警前缀应能解析: %v", err)
	}
	if len(devs) != 1 || devs[0].BusID != "1-1" {
		t.Fatalf("解析结果异常: %+v", devs)
	}
	if devs[0].Vid != "1234" || devs[0].Pid != "5678" {
		t.Errorf("vid/pid = %q/%q", devs[0].Vid, devs[0].Pid)
	}
}

func TestParseStateBOM(t *testing.T) {
	devs, err := ParseState("\xEF\xBB\xBF" + `{"Devices":[]}`)
	if err != nil {
		t.Fatalf("BOM 头应被容忍: %v", err)
	}
	if len(devs) != 0 {
		t.Fatalf("want 0")
	}
}

func TestParseStateBadJSON(t *testing.T) {
	for _, in := range []string{"", "usbipd: command not found", "not json at all { oops"} {
		if _, err := ParseState(in); err == nil {
			t.Errorf("坏输出 %q 必须报错而非谎报空表", in)
		}
	}
}

func TestParseStateMissingFieldsTolerated(t *testing.T) {
	// 缺 BusId/Description/可空字段全没有：只给 InstanceId 也要出可渲染行。
	devs, err := ParseState(`{"Devices":[{"InstanceId":"USB\\VID_ABCD&PID_0001\\SN1"}]}`)
	if err != nil {
		t.Fatalf("缺字段不该报错: %v", err)
	}
	d := devs[0]
	if d.Description == "" || d.Vid != "abcd" || d.Pid != "0001" || d.Serial != "SN1" {
		t.Errorf("归一兜底不完整: %+v", d)
	}
	if d.State != StateNotShared {
		t.Errorf("无绑定线索应判未共享, got %s", d.State)
	}
}

func TestParseStateLegacyVidPidAndState(t *testing.T) {
	// 旧版兼容字段：Vid/Pid/State 存在时优先于推导。
	devs, err := ParseState(`{"Devices":[{
		"BusId":"3-1","Vid":"0x046D","Pid":"C52B","State":"Shared (forced)",
		"InstanceId":"USB\\UNKNOWN\\X","Description":"mouse"}]}`)
	if err != nil {
		t.Fatal(err)
	}
	d := devs[0]
	if d.Vid != "046d" || d.Pid != "c52b" {
		t.Errorf("旧版 Vid/Pid 字段归一失败: %q/%q", d.Vid, d.Pid)
	}
	if d.State != StateShared || !d.Forced {
		t.Errorf("State 文案解析失败: %+v", d)
	}
}

func TestBusIDNaturalOrder(t *testing.T) {
	if compareBusID("2-1", "2-10") >= 0 {
		t.Error("2-1 应排在 2-10 前")
	}
	if compareBusID("1-4.1", "1-4") <= 0 {
		t.Error("带接口号的应排后")
	}
}

func TestCheckBusID(t *testing.T) {
	for _, ok := range []string{"2-3", "1-4.1", "10-2", " 2-3 "} {
		if _, err := CheckBusID(ok); err != nil {
			t.Errorf("%q 应合法: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "2", "2-3; rm", "--busid", "a-b", "2-3 ", "2-3..4", `2-3"`, "2-3 4"} {
		if _, err := CheckBusID(strings.TrimSpace(bad)); err == nil && bad != "2-3 " {
			t.Errorf("%q 必须被拒绝", bad)
		}
	}
}

func TestCommandArgs(t *testing.T) {
	args, err := BindArgs("2-3", true)
	if err != nil || strings.Join(args, " ") != "bind --busid 2-3 --force" {
		t.Errorf("BindArgs = %v, %v", args, err)
	}
	args, _ = AttachArgs("Ubuntu-24.04", "1-4")
	if strings.Join(args, " ") != "attach --wsl Ubuntu-24.04 --busid 1-4" {
		t.Errorf("AttachArgs 带发行版 = %v", args)
	}
	args, _ = AttachArgs("", "1-4")
	if strings.Join(args, " ") != "attach --wsl --busid 1-4" {
		t.Errorf("AttachArgs 默认发行版 = %v", args)
	}
	if _, err := AttachArgs("Ubuntu; calc", "1-4"); err != nil {
		t.Errorf("发行版名放行范围意外收紧: %v", err) // 白名单在 wsl 层做，这里只管控制字符
	}
	if _, err := AttachArgs("Ubuntu\n--force", "1-4"); err == nil {
		t.Error("含换行的发行版名必须拒绝")
	}
	args, _ = DetachArgs("2-3")
	if strings.Join(args, " ") != "detach --busid 2-3" {
		t.Errorf("DetachArgs = %v", args)
	}
	args, _ = UnbindGUIDArgs("9F94F897-8B2D-47A4-9A85-2C4B9D5D2E01")
	if strings.Join(args, " ") != "unbind --guid 9f94f897-8b2d-47a4-9a85-2c4b9d5d2e01" {
		t.Errorf("UnbindGUIDArgs = %v", args)
	}
	if _, err := UnbindGUIDArgs("not-a-guid"); err == nil {
		t.Error("坏 GUID 必须拒绝")
	}
}

func TestZhError(t *testing.T) {
	cases := map[string]string{
		"Device is not shared; run 'usbipd bind --busid 2-3' as administrator first.": "设备尚未共享",
		"There is no device with busid '9-9'.":                                        "该总线号上此刻没有设备",
		"Device with busid '1-4' is already attached.":                                "已附加",
		"some unknown usbipd failure\nsecond line":                                    "second line",
		"No WSL distributions are running.":                                           "没有",
		"Attach failed; WSL is not running on this machine":                           "WSL 没有在运行",
	}
	for in, want := range cases {
		got := ZhError(in, 1)
		if !strings.Contains(got, want) {
			t.Errorf("ZhError(%q) = %q, 应含 %q", in, got, want)
		}
	}
	if got := ZhError("", 3); !strings.Contains(got, "管理员") {
		t.Errorf("AccessDenied 码应给管理员提示: %q", got)
	}
}

func TestDeviceJSONRoundTrip(t *testing.T) {
	// 绑定层依赖 json 标签直出前端：锁定 camelCase 键名不漂移。
	b, err := json.Marshal(Device{BusID: "2-3", Description: "d", InstanceID: "i", State: StateShared})
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"busId"`, `"description"`, `"instanceId"`, `"state"`} {
		if !strings.Contains(string(b), key) {
			t.Errorf("设备 JSON 应含 %s: %s", key, b)
		}
	}
}
