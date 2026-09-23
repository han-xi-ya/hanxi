//go:build windows

package sysinfo

import (
	"strings"
	"testing"
)

func TestNormalizeProductName(t *testing.T) {
	cases := []struct {
		name, registry, build, want string
	}{
		{"Win11 注册表仍写 10（已知事实）", "Windows 10 Pro", "22631.4317", "Windows 11 Pro"},
		{"Win10 世代原样", "Windows 10 Home", "19045", "Windows 10 Home"},
		{"Server 名不碰", "Windows Server 2022 Datacenter", "20348", "Windows Server 2022 Datacenter"},
		{"构建号异常不猜", "Windows 10 Pro", "not-a-build", "Windows 10 Pro"},
	}
	for _, c := range cases {
		if got := normalizeProductName(c.registry, c.build); got != c.want {
			t.Errorf("%s: normalizeProductName(%q,%q)=%q want %q", c.name, c.registry, c.build, got, c.want)
		}
	}
}

func TestMajorBuild(t *testing.T) {
	for in, want := range map[string]int{"22631.4317": 22631, "19041": 19041, " x": 0, "": 0, "26100.1": 26100} {
		if got := majorBuild(in); got != want {
			t.Errorf("majorBuild(%q)=%d want %d", in, got, want)
		}
	}
}

func TestFormatUptime(t *testing.T) {
	for sec, want := range map[int64]string{
		30:    "30秒",
		125:   "2分钟",
		3725:  "1小时2分钟",
		93780: "1天2小时3分钟",
		-1:    "",
	} {
		if got := formatUptime(sec); got != want {
			t.Errorf("formatUptime(%d)=%q want %q", sec, got, want)
		}
	}
}

func TestPopcountU64(t *testing.T) {
	if popcountU64(0) != 0 || popcountU64(0xFF) != 8 || popcountU64(1<<63) != 1 {
		t.Fatal("popcount 口径失真")
	}
}

// ---------- 真机采集冒烟（本测试即跑在 Windows 宿主上） ----------

func TestCollectorsSmokeOnHost(t *testing.T) {
	cpu, err := collectCPU()
	if err != nil {
		t.Fatalf("collectCPU: %v", err)
	}
	if cpu.Logical <= 0 || cpu.Name == "" {
		t.Fatalf("CPU 档案缺项: %+v", cpu)
	}
	if cpu.Cores > 0 && cpu.Cores > cpu.Logical {
		t.Fatalf("核数不可能大于逻辑线程: %+v", cpu)
	}

	mem, err := collectMemory()
	if err != nil {
		t.Fatalf("collectMemory: %v", err)
	}
	if mem.TotalBytes == 0 || mem.AvailableBytes == 0 || mem.LoadPercent > 100 {
		t.Fatalf("内存档案异常: %+v", mem)
	}

	ost, err := collectOS()
	if err != nil {
		t.Fatalf("collectOS: %v", err)
	}
	if !strings.HasPrefix(ost.ProductName, "Windows") || ost.Build == "" || !ost.Is64Bit {
		t.Fatalf("OS 档案异常: %+v", ost)
	}

	vols, err := collectVolumes()
	if err != nil {
		t.Fatalf("collectVolumes: %v", err)
	}
	var sawC bool
	for _, v := range vols {
		if v.Letter == "C:" {
			sawC = true
			if v.TotalBytes == 0 || v.FileSystem == "" {
				t.Fatalf("C 卷档案缺项: %+v", v)
			}
		}
	}
	if !sawC {
		t.Fatal("必须看到 C: 卷")
	}

	disps, err := collectDisplays()
	if err != nil {
		// 无活动显示器的会话（服务会话/RDP 断开瞬态）属事实边界而非缺陷：如实 skip
		t.Skipf("本会话无活动显示器，显示采集断言不适用: %v", err)
	}
	for _, d := range disps {
		if d.Primary && (d.Width == 0 || d.Height == 0) {
			t.Fatalf("主显示器分辨率缺项: %+v", d)
		}
	}

	nets, err := collectNetwork()
	if err != nil || len(nets) == 0 {
		t.Fatalf("collectNetwork: %v len=%d", err, len(nets))
	}

	gpus, _ := collectGPUs() // 虚拟图形驱动环境可空，不断言内容
	_ = gpus
	machine, _ := collectMachine() // OEM 信息在虚拟机可缺省，仅确保不炸
	_ = machine
}
