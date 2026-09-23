package mcp

import (
	"encoding/json"
	"strings"
	"testing"

	"hanxi/internal/modules/sysinfo"
)

// ---------- hanxi_sysinfo_report（N32） ----------

type fakeReportSource struct {
	rep   sysinfo.Report
	err   error
	calls int
}

func (f *fakeReportSource) GetReport() (sysinfo.Report, error) {
	f.calls++
	return f.rep, f.err
}

func sysinfoFixture() sysinfo.Report {
	return sysinfo.Report{
		Machine: sysinfo.MachineInfo{
			Hostname: "DESKTOP-HX01", Manufacturer: "LENOVO", Model: "ThinkPad X1",
			BIOSVendor: "American Megatrends", BIOSVersion: "N3NET25W", ProductID: "00331-10000-00001-AA518",
		},
		OS:       sysinfo.OSInfo{ProductName: "Windows 10 Pro", Version: "22H2", Build: "22631.4317", Uptime: "3天2小时", Is64Bit: true},
		CPU:      sysinfo.CPUInfo{Name: "Intel Core i7-12700", Vendor: "GenuineIntel", SpeedMHz: 2100, Cores: 12, Logical: 20},
		Memory:   sysinfo.MemoryInfo{TotalBytes: 34359738368, AvailableBytes: 12884901888, LoadPercent: 63, CommitTotal: 1, CommitLimit: 2},
		GPUs:     []sysinfo.GPUInfo{{Desc: "Intel Iris Xe", Provider: "Intel Corporation", DriverVersion: "31.0.101.2115"}},
		Displays: []sysinfo.DisplayInfo{{Name: `\\.\DISPLAY1`, Width: 2560, Height: 1440, RefreshHz: 165, ColorBits: 32, Primary: true}},
		Volumes:  []sysinfo.VolumeInfo{{Letter: "C:", Label: "系统盘", FileSystem: "NTFS", Type: "fixed", TotalBytes: 512 << 30, FreeBytes: 80 << 30}},
		Network: []sysinfo.NetInfo{
			{Name: "WLAN", Description: "Intel Wi-Fi 6E", MAC: "AA:BB:CC:DD:EE:FF", MTU: 1500, Up: true, Addresses: []string{"192.168.10.7/24"}},
			{Name: "Loopback Pseudo-Interface 1", MAC: "", MTU: 0, Loopback: true, Addresses: []string{"127.0.0.1/8"}},
		},
		Errors: []string{"gpu: 打开设备键失败"},
	}
}

func TestSysInfoOverviewTier(t *testing.T) {
	src := &fakeReportSource{rep: sysinfoFixture()}
	deps, access, _ := newTestServer(t)
	grant(t, access, map[string]bool{"sysinfo": true})
	deps.SysInfo = src
	c := inProcClient(t, deps)

	res, text := callText(t, c, toolSysInfo, nil)
	if res.IsError {
		t.Fatalf("overview 档应成功: %s", text)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(text), &doc); err != nil {
		t.Fatalf("输出应为合法 JSON: %v", err)
	}
	if src.calls != 1 {
		t.Fatalf("应恰好触发一次采集: %d", src.calls)
	}
	machine, _ := doc["machine"].(map[string]any)
	if machine["hostname"] != "DESKTOP-HX01" {
		t.Fatalf("摘要档必须保留计算机名主干: %v", doc["machine"])
	}
	// 摘要档剥掉 full 细节：BIOS/产品 ID/MAC/环回接口不得出现
	for _, banned := range []string{"N3NET25W", "00331-10000-00001-AA518", "AA:BB:CC:DD:EE:FF", "Loopback"} {
		if strings.Contains(text, banned) {
			t.Errorf("overview 档泄露 full 细节 %q", banned)
		}
	}
	// IP 是主干问答字段（"这台机器现在什么地址"），摘要档保留
	if !strings.Contains(text, "192.168.10.7/24") {
		t.Errorf("overview 档应保留活动接口地址: %s", text)
	}
	// 段级降级两档都如实透传
	if !strings.Contains(text, "gpu: 打开设备键失败") {
		t.Errorf("errors 必须透传: %s", text)
	}
}

func TestSysInfoFullTier(t *testing.T) {
	deps, access, _ := newTestServer(t)
	grant(t, access, map[string]bool{"sysinfo": true})
	deps.SysInfo = &fakeReportSource{rep: sysinfoFixture()}
	c := inProcClient(t, deps)

	res, text := callText(t, c, toolSysInfo, map[string]any{"level": "full"})
	if res.IsError {
		t.Fatalf("full 档应成功: %s", text)
	}
	for _, want := range []string{"N3NET25W", "AA:BB:CC:DD:EE:FF", "Loopback", "biosVendor", "productId"} {
		if !strings.Contains(text, want) {
			t.Errorf("full 档缺字段 %q: %s", want, text)
		}
	}
}

func TestSysInfoUnknownTierAndBackend(t *testing.T) {
	deps, access, _ := newTestServer(t)
	grant(t, access, map[string]bool{"sysinfo": true})
	deps.SysInfo = &fakeReportSource{rep: sysinfoFixture()}
	c := inProcClient(t, deps)

	res, text := callText(t, c, toolSysInfo, map[string]any{"level": "ultra"})
	if !res.IsError || !strings.Contains(text, "未知档位") {
		t.Fatalf("未知档位应指引: %s", text)
	}

	// 后端未装配：报"后端未装配"而非 panic（Deps 允许 nil 的契约面）
	deps2, access2, _ := newTestServer(t)
	grant(t, access2, map[string]bool{"sysinfo": true})
	deps2.SysInfo = nil
	c2 := inProcClient(t, deps2)
	res2, text2 := callText(t, c2, toolSysInfo, nil)
	if !res2.IsError || !strings.Contains(text2, "后端未装配") {
		t.Fatalf("nil 后端应报装配缺失: %s", text2)
	}
}

// TestSysInfoGateDisabled sysinfo 模块停用：指引错误、采集零触发。
func TestSysInfoGateDisabled(t *testing.T) {
	deps, access, _ := newTestServer(t)
	grant(t, access, map[string]bool{"sysinfo": true})
	src := &fakeReportSource{}
	deps.SysInfo = src
	deps.Gate = newFakeGate() // 全停用（含 sysinfo）
	c := inProcClient(t, deps)
	res, text := callText(t, c, toolSysInfo, map[string]any{})
	if !res.IsError || !strings.Contains(text, "停用") {
		t.Fatalf("停用应指引，得: %s", text)
	}
	if src.calls != 0 {
		t.Error("门禁拒绝不得触发采集")
	}
}

// TestSysInfoDescriptionHonesty 描述必须讲清两档差异与机器指纹露出面（N32 ③）。
func TestSysInfoDescriptionHonesty(t *testing.T) {
	tool, _ := buildSysInfoTool(Deps{})
	for _, phrase := range []string{"overview", "full", "MAC", "计算机名", "只读"} {
		if !strings.Contains(tool.Description, phrase) {
			t.Errorf("description must contain %q: %s", phrase, tool.Description)
		}
	}
}
