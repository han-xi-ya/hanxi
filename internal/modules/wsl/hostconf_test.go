package wsl

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stubHostConf 把 .wslconfig 路径指向临时文件；content="" 表示文件不存在。
func stubHostConf(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), ".wslconfig")
	if content != "" {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	old := hostWslConfigPath
	hostWslConfigPath = func() string { return path }
	t.Cleanup(func() { hostWslConfigPath = old })
	return path
}

func TestExtractNetworkingMode(t *testing.T) {
	cases := []struct {
		text  string
		mode  string
		found bool
	}{
		{"[networking]\nnetworkingMode=mirrored\n", "mirrored", true},
		{"[wsl2]\nmemory=8GB\n[networking]\nNetworkingMode = Mirrored # 镜像\n", "mirrored", true},
		{"[networking]\nnetworkingMode=nat\n", "nat", true},
		{"[wsl2]\nprocessors=4\n", "", false},
		{"[networking]\nlocalhostInteractions=true\n", "", false},
		{"# networkingMode=bridged 在注释里不算\n", "", false},
	}
	for _, c := range cases {
		got, found := extractNetworkingMode(c.text)
		if got != c.mode || found != c.found {
			t.Fatalf("extractNetworkingMode(%q)=(%q,%v)，期望(%q,%v)", c.text, got, found, c.mode, c.found)
		}
	}
}

func TestHostNetworkModeDefaults(t *testing.T) {
	stubHostConf(t, "") // 文件不存在
	if got := hostNetworkMode(); got != "nat" {
		t.Fatalf("缺文件应归一 nat: %q", got)
	}
	stubHostConf(t, "[networking]\nnetworkingMode=weird\n")
	if got := hostNetworkMode(); got != "unknown" {
		t.Fatalf("非法值应标 unknown（现状如实反映，不猜）: %q", got)
	}
	stubHostConf(t, "[networking]\nnetworkingMode=bridged\n")
	if got := hostNetworkMode(); got != "bridged" {
		t.Fatalf("bridged 识别失败: %q", got)
	}
}

func TestGetWslHostConfMissingAndMode(t *testing.T) {
	stubHostConf(t, "[wsl2]\nmemory=6GB\n[networking]\nnetworkingMode=mirrored\n")
	svc, _, _ := newTestService()
	doc, err := svc.GetWslHostConf()
	if err != nil {
		t.Fatal(err)
	}
	if doc.Missing || doc.NetworkMode != "mirrored" {
		t.Fatalf("读取态错误: %+v", doc)
	}
	stubHostConf(t, "")
	doc2, err := svc.GetWslHostConf()
	if err != nil || !doc2.Missing || doc2.NetworkMode != "nat" {
		t.Fatalf("缺文件空态错误: %v %+v", err, doc2)
	}
}

func TestSaveWslHostConfGuardsAndBackup(t *testing.T) {
	path := stubHostConf(t, "[wsl2]\nmemory=6GB\n")
	svc, _, _ := newTestService()
	out, err := svc.SaveWslHostConf("[wsl2]\nmemory=8GB\r\n[networking]\nnetworkingMode=mirrored")
	if err != nil || !out.Success {
		t.Fatalf("合法保存应成功: %v %+v", err, out)
	}
	// CRLF 归一 + 落盘内容
	data, _ := os.ReadFile(path)
	s := string(data)
	if strings.Contains(s, "\r") {
		t.Fatalf("CRLF 未归一: %q", s)
	}
	if hostNetworkMode() != "mirrored" {
		t.Fatal("写后模式未生效")
	}
	// 原文件已备份
	bakData, berr := os.ReadFile(path + ".hanxi.bak")
	if berr != nil || !strings.Contains(string(bakData), "memory=6GB") {
		t.Fatalf("写前备份缺失/内容错: %v %q", berr, bakData)
	}
	// 非法 networkingMode 必须拦（写错全员失联）
	if _, err := svc.SaveWslHostConf("[networking]\nnetworkingMode=mirror"); err == nil || !strings.Contains(err.Error(), "nat / bridged / mirrored") {
		t.Fatalf("非法模式必须白名单拒绝: %v", err)
	}
	// 语法闸门
	if _, err := svc.SaveWslHostConf("[wsl2\nmemory=8GB"); err == nil {
		t.Fatal("坏节头必须拒绝")
	}
	// 拒绝后磁盘内容不变（先验后写）
	after, _ := os.ReadFile(path)
	if !strings.Contains(string(after), "mirrored") {
		t.Fatal("被拒的保存不得触碰现文件")
	}
}

func TestSaveWslHostConfNewFileNoBackup(t *testing.T) {
	path := stubHostConf(t, "")
	svc, _, _ := newTestService()
	if _, err := svc.SaveWslHostConf("[networking]\nnetworkingMode=nat"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".hanxi.bak"); !os.IsNotExist(err) {
		t.Fatal("首建无原件，不该有备份")
	}
}

func TestShutdownWsl(t *testing.T) {
	svc, _, _ := newTestService()
	stub := &wslStub{resp: func(args []string) (string, error) {
		if joined(args) == "--shutdown" {
			return "", nil
		}
		return "", errors.New("意外的调用")
	}}
	svc.runWsl = stub.run
	out, err := svc.ShutdownWsl()
	if err != nil || !out.Success {
		t.Fatalf("全停失败: %v %+v", err, out)
	}
	svc.mu.Lock()
	svc.heavyOps++ // 占闸模拟重操作在飞
	svc.mu.Unlock()
	if _, err := svc.ShutdownWsl(); err == nil {
		t.Fatal("重操作在飞时全停必须让路")
	}
	svc.mu.Lock()
	svc.heavyOps--
	svc.mu.Unlock()
}

// ---- 视图接线：mode 出现在取证与端口转发 ----

func TestNetworkModeSurfacesInViews(t *testing.T) {
	stubHostConf(t, "[networking]\nnetworkingMode=mirrored\n")
	svc, _, _ := newTestService()
	fixture := &wslStub{resp: func(args []string) (string, error) {
		j := joined(args)
		if j == "-l -q" || j == "-l -q --running" {
			return "Ubuntu\x00", nil
		}
		return "", nil
	}}
	svc.runWsl = fixture.run
	svc.localPS = func(context.Context, string) (string, error) { return "", nil }
	f, err := svc.GetDistroForensics("Ubuntu")
	if err != nil {
		t.Fatal(err)
	}
	if f.NetworkMode != "mirrored" {
		t.Fatalf("取证未带网络模式: %+v", f)
	}
	sawNote := false
	for _, n := range f.Notes {
		if strings.Contains(n, "镜像网络") {
			sawNote = true
		}
	}
	if !sawNote {
		t.Fatalf("镜像模式应有说明注记: %v", f.Notes)
	}

	svc.ppPath = filepath.Join(t.TempDir(), "pp.json")
	stubPPShow(t, "0.0.0.0 80 1.2.3.4 80\n")
	view, err := svc.ListPortRules()
	if err != nil || view.NetworkMode != "mirrored" {
		t.Fatalf("端口转发视图未带网络模式: %v %+v", err, view)
	}
}
