package wsl

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---- 解析小件 ----

func TestParseDfRoot(t *testing.T) {
	// 实机常见形态（英文表头 + 9p/overlayfs 长文件系统名折行）
	out := "Filesystem     1M-blocks  Used Available Use% Mounted on\n" +
		"/dev/sdd         50579M 13634M    37135M  27% /"
	v, ok := parseDfRoot(out)
	if !ok || v != [4]int64{50579, 13634, 37135, 27} {
		t.Fatalf("df 解析错误: %v %v", v, ok)
	}
	// 文件系统名过长折行的输出：搜索式解析不受影响
	wrapped := "Filesystem     1M-blocks  Used Available Use% Mounted on\n" +
		"overlayfs-with-a-very-long-source-name-that-wraps\n/dev/sdd" +
		"         20000M 1000M   19000M   5% /"
	if v, ok := parseDfRoot(wrapped); !ok || v[0] != 20000 || v[3] != 5 {
		t.Fatalf("折行输出解析错误: %v %v", v, ok)
	}
	if _, ok := parseDfRoot("df: 未找到命令"); ok {
		t.Fatal("垃圾输出不得判成功")
	}
	if _, ok := parseDfRoot("0M 0M 0M 0%"); ok {
		t.Fatal("total=0 视为不可信")
	}
}

func TestFirstIPv4(t *testing.T) {
	if ip := firstIPv4("172.25.4.136 \r\n"); ip != "172.25.4.136" {
		t.Fatalf("hostname -I 解析错误: %q", ip)
	}
	out := "3: eth0    inet 172.25.9.42/24 brd 172.25.9.255 scope global eth0\\       valid_lft forever"
	if ip := firstIPv4(out); ip != "172.25.9.42" {
		t.Fatalf("ip -o 解析错误: %q", ip)
	}
	if ip := firstIPv4("fe80::1 not-an-ip 9.9.9.9"); ip != "9.9.9.9" {
		t.Fatalf("应跳过 IPv6/非 IP 取第一个 IPv4: %q", ip)
	}
	if ip := firstIPv4("lo 127.0.0.1"); ip != "127.0.0.1" {
		t.Fatalf("127 回环仍为合法 IPv4（是否回环交由前端文案提示）: %q", ip)
	}
	if ip := firstIPv4(""); ip != "" {
		t.Fatalf("空输出应得空: %q", ip)
	}
}

// ---- GetDistroForensics ----

// forensicsFixture：lxss 巡查返回一份真实形态 JSON，ext4.vhdx 指向临时文件，
// df/IP 走 -d 桩。runningList 控制 `-l -q --running` 通道行为。
func forensicsFixture(t *testing.T, svc *WslService, runningList string, runningErr error) *wslStub {
	t.Helper()
	vhdx := filepath.Join(t.TempDir(), "ext4.vhdx")
	if err := os.WriteFile(vhdx, make([]byte, 4096), 0o600); err != nil {
		t.Fatal(err)
	}
	svc.localPS = func(_ context.Context, script string) (string, error) {
		if strings.HasPrefix(script, "$items") {
			return fmt.Sprintf(`[{"name":"Ubuntu","basePath":"C:\\lxss\\u","vhdx":%q,"size":4096,"pfn":"CanonicalGroupLimited.Ubuntu_79rhkp1fndgsc"}]`, vhdx), nil
		}
		return "", nil
	}
	stub := &wslStub{resp: func(args []string) (string, error) {
		switch joined(args) {
		case "-l -q":
			return "Ubuntu\x00", nil
		case "-l -q --running":
			if runningErr != nil {
				return "", runningErr
			}
			if runningList == "" {
				return "\x00", nil
			}
			return runningList + "\x00", nil
		case "-d Ubuntu -- df -B1M /":
			return "Filesystem     1M-blocks  Used Available Use% Mounted on\n/dev/sdd 50579M 13634M 37135M 27% /", nil
		case "-d Ubuntu -- hostname -I":
			return "172.25.4.136", nil
		}
		return "", fmt.Errorf("意外的 wsl 调用: %v", args)
	}}
	svc.runWsl = stub.run
	return stub
}

func TestGetDistroForensicsRunningCollects(t *testing.T) {
	svc, _, _ := newTestService()
	stub := forensicsFixture(t, svc, "Ubuntu", nil)
	f, err := svc.GetDistroForensics("Ubuntu")
	if err != nil {
		t.Fatal(err)
	}
	if !f.Running || !f.RuntimeKnown {
		t.Fatalf("运行态归一错误: %+v", f)
	}
	if f.Pfn == "" || f.BasePath == "" || f.VhdxPath == "" {
		t.Fatalf("注册表项缺失: %+v", f)
	}
	if f.LogicalBytes != 4096 || f.AllocBytes <= 0 || f.Sparse {
		t.Fatalf("磁盘双口径错误: %+v", f)
	}
	if !f.DfOK || f.DfTotalMB != 50579 || f.DfUsePct != 27 {
		t.Fatalf("df 指标错误: %+v", f)
	}
	if f.IPv4 != "172.25.4.136" {
		t.Fatalf("IP 错误: %+v", f)
	}
	for _, c := range joinedCalls(stub) {
		if strings.HasPrefix(c, "-d Ubuntu -- ip ") {
			t.Fatal("hostname -I 已命中时不应再走 ip 兜底")
		}
	}
}

func TestGetDistroForensicsStoppedNeverWakes(t *testing.T) {
	svc, _, _ := newTestService()
	stub := forensicsFixture(t, svc, "", nil) // running 名单为空 = Ubuntu 停止
	f, err := svc.GetDistroForensics("Ubuntu")
	if err != nil {
		t.Fatal(err)
	}
	if f.Running || !f.RuntimeKnown {
		t.Fatalf("停止态归一错误: %+v", f)
	}
	if f.DfOK || f.IPv4 != "" {
		t.Fatalf("停止态不得有 guest 指标: %+v", f)
	}
	for _, c := range joinedCalls(stub) {
		if strings.HasPrefix(c, "-d ") {
			t.Fatalf("只读取证绝不允许进 guest（会顺手拉起发行版）: %s", c)
		}
	}
}

func TestGetDistroForensicsUnknownRunningIsConservative(t *testing.T) {
	svc, _, _ := newTestService()
	stub := forensicsFixture(t, svc, "", errors.New("wsl.exe 拒绝访问"))
	f, err := svc.GetDistroForensics("Ubuntu")
	if err != nil {
		t.Fatal(err)
	}
	if f.RuntimeKnown || f.Running {
		t.Fatalf("通道不可得必须判未知: %+v", f)
	}
	for _, c := range joinedCalls(stub) {
		if strings.HasPrefix(c, "-d ") {
			t.Fatalf("运行态未知时不得进 guest: %s", c)
		}
	}
}

func TestGetDistroForensicsRejectsUnknownName(t *testing.T) {
	svc, _, _ := newTestService()
	svc.runWsl = func(_ context.Context, args ...string) (string, error) {
		if joined(args) == "-l -q" {
			return "Ubuntu\x00", nil
		}
		return "", errors.New("不该触达")
	}
	if _, err := svc.GetDistroForensics("evil && calc"); err == nil {
		t.Fatal("名单外发行版必须拒绝")
	}
}
