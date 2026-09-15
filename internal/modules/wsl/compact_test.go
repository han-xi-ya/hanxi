package wsl

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"hanxi/internal/modules/wsl/readiness"
)

// compactFixture：Ubuntu(WSL2) + 临时源 VHDX + 导出目录/空间探针注入。
// hyperV 控制 Tier1 前提探测；runningAfterTerm 控制 --running 收敛。
type compactEnv struct {
	stub   *wslStub
	export string // 备份 tar 实落盘路径（--export 桩写文件时记录）
}

func compactFixture(t *testing.T, svc *WslService, hyperV string) (*compactEnv, string) {
	t.Helper()
	vhdx := filepath.Join(t.TempDir(), "data", "ext4.vhdx")
	if err := os.MkdirAll(filepath.Dir(vhdx), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(vhdx, []byte(strings.Repeat("v", 4096)), 0o600); err != nil {
		t.Fatal(err)
	}
	oldExport, oldFree := exportDir, diskFree
	t.Cleanup(func() { exportDir, diskFree = oldExport, oldFree })
	expDir := filepath.Join(t.TempDir(), "exports")
	exportDir = func() string { return expDir }
	diskFree = func(string) (uint64, error) { return 1 << 40, nil }

	svc.wslDistros = func(context.Context) []readiness.Distro {
		return []readiness.Distro{{Name: "Ubuntu", Version: "2"}}
	}
	env := &compactEnv{}
	svc.localPS = func(_ context.Context, script string) (string, error) {
		switch {
		case strings.HasPrefix(script, "$items"):
			return fmt.Sprintf(`[{"name":"Ubuntu","basePath":%q,"vhdx":%q,"size":4096}]`, filepath.Dir(vhdx), vhdx), nil
		case strings.Contains(script, "Get-Module"):
			return hyperV, nil
		}
		return "", nil
	}
	stub := &wslStub{resp: func(args []string) (string, error) {
		j := joined(args)
		switch {
		case j == "-l -q":
			return "Ubuntu\x00", nil
		case j == "-l -q --running":
			return "\x00", nil // 终止即停：waitStopped 首轮收敛
		case strings.HasPrefix(j, "--export Ubuntu "):
			path := args[len(args)-1]
			env.export = path
			return "", os.WriteFile(path, []byte("backup-tar"), 0o600)
		case j == "-d Ubuntu -u root -- fstrim -v /":
			return "/: 2.1 GiB (22548578304 bytes) trimmed", nil
		case j == "--terminate Ubuntu":
			return "", nil
		case j == "--unregister Ubuntu":
			return "", nil
		}
		if len(args) >= 3 && args[0] == "--import" {
			// 命令面：--import <名> <目录> <tar>——目录在 args[2]，桩建错地方会污染仓库
			dest := args[2]
			if err := os.MkdirAll(dest, 0o755); err != nil {
				return "", err
			}
			return "", os.WriteFile(filepath.Join(dest, "ext4.vhdx"), []byte("new"), 0o600)
		}
		return "", fmt.Errorf("意外的 wsl 调用: %v", args)
	}}
	svc.runWsl = stub.run
	env.stub = stub
	return env, vhdx
}

// compactCollector 在受理前挂上事件收集（终态或全量序列）。
func compactCollector(svc *WslService) (*[]CompactProgress, chan struct{}, *sync.Mutex) {
	var mu sync.Mutex
	var seq []CompactProgress
	done := make(chan struct{})
	svc.emit = func(_ string, payload any) {
		p, ok := payload.(CompactProgress)
		if !ok {
			return
		}
		mu.Lock()
		seq = append(seq, p)
		last := p.Stage == "done" || p.Stage == "error"
		mu.Unlock()
		if last {
			select {
			case <-done:
			default:
				close(done)
			}
		}
	}
	return &seq, done, &mu
}

func waitCompact(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("压缩后台终态超时")
	}
}

func TestCompactDistroRejections(t *testing.T) {
	// WSL1 源
	svc, _, _ := newTestService()
	env, _ := compactFixture(t, svc, "n")
	svc.wslDistros = func(context.Context) []readiness.Distro {
		return []readiness.Distro{{Name: "Ubuntu", Version: "1"}}
	}
	if _, err := svc.CompactDistro("Ubuntu", ""); err == nil || !strings.Contains(err.Error(), "WSL2") {
		t.Fatalf("WSL1 应拒绝: %v", err)
	}
	// 空间预检不过（注入小剩余）
	svc2, _, _ := newTestService()
	compactFixture(t, svc2, "n")
	diskFree = func(string) (uint64, error) { return 10 << 20, nil }
	if _, err := svc2.CompactDistro("Ubuntu", ""); err == nil || !strings.Contains(err.Error(), "空间预检") {
		t.Fatalf("空间不足应被预检拦下: %v", err)
	}
	_ = env
}

func TestCompactTier2Flow(t *testing.T) {
	svc, _, _ := newTestService()
	env, vhdx := compactFixture(t, svc, "n") // 无 Hyper-V → 跳过 Tier1
	seq, done, mu := compactCollector(svc)
	if _, err := svc.CompactDistro("Ubuntu", ""); err != nil {
		t.Fatal(err)
	}
	waitCompact(t, done)
	mu.Lock()
	stages := make([]string, len(*seq))
	for i, p := range *seq {
		stages[i] = p.Stage
	}
	last := (*seq)[len(*seq)-1]
	mu.Unlock()
	if last.Stage != "done" || last.Tier != "tier2" {
		t.Fatalf("终态应为 tier2 done: %+v", last)
	}
	for _, want := range []string{"backup", "trim", "optimize", "reimport", "done"} {
		if !strings.Contains(strings.Join(stages, ","), want) {
			t.Fatalf("阶段序列缺 %s: %v", want, stages)
		}
	}
	// 备份 tar 已实落盘且被用于重导入
	if env.export == "" {
		t.Fatal("备份未发生")
	}
	var sawUnreg, sawImport bool
	for _, c := range joinedCalls(env.stub) {
		if c == "--unregister Ubuntu" {
			sawUnreg = true
		}
		if strings.HasPrefix(c, "--import Ubuntu ") && strings.HasSuffix(c, env.export) {
			sawImport = true
		}
	}
	if !sawUnreg || !sawImport {
		t.Fatalf("Tier2 命令面缺失 unregister=%v import=%v: %v", sawUnreg, sawImport, joinedCalls(env.stub))
	}
	svc.mu.Lock()
	heavy := svc.heavyOps
	svc.mu.Unlock()
	if heavy != 0 {
		t.Fatal("终态后重操作闸必须释放")
	}
	_ = vhdx
}

func TestCompactTier1SuccessShortCircuits(t *testing.T) {
	svc, _, _ := newTestService()
	env, vhdx := compactFixture(t, svc, "y") // Hyper-V 在位
	// Tier1 "压缩成功"：提权桩把源盘缩到 50MB；alloc 以 200MB 起算 → 省 150MB ≥ 阈值。
	svc.elevProc = func(context.Context, string, ...string) (OperationOutcome, error) {
		return OperationOutcome{Success: true}, os.Truncate(vhdx, 50<<20)
	}
	seq, done, mu := compactCollector(svc)
	// 直接进主体（绕开受理期实测 alloc——单测控制 alloc=200MB 场景）。
	svc.runCompact(context.Background(), "Ubuntu", vhdx, filepath.Dir(vhdx), 200<<20, false, t.TempDir())
	waitCompact(t, done)
	mu.Lock()
	last := (*seq)[len(*seq)-1]
	mu.Unlock()
	if last.Stage != "done" || last.Tier != "tier1" {
		t.Fatalf("应为 tier1 done: %+v", last)
	}
	for _, c := range joinedCalls(env.stub) {
		if strings.HasPrefix(c, "--unregister") || strings.HasPrefix(c, "--import") {
			t.Fatalf("Tier1 达标后不得触碰 Tier2 销毁步骤: %v", c)
		}
	}
}

func TestCompactBackupFailureAbortsClean(t *testing.T) {
	svc, _, _ := newTestService()
	env, _ := compactFixture(t, svc, "n")
	base := env.stub.resp
	env.stub.resp = func(args []string) (string, error) {
		if args[0] == "--export" {
			return "wsl: 导出失败", errors.New("exit status 0x5")
		}
		return base(args)
	}
	seq, done, mu := compactCollector(svc)
	if _, err := svc.CompactDistro("Ubuntu", ""); err != nil {
		t.Fatal(err)
	}
	waitCompact(t, done)
	mu.Lock()
	last := (*seq)[len(*seq)-1]
	mu.Unlock()
	if last.Stage != "error" || !strings.Contains(last.Error, "未对数据做任何改动") {
		t.Fatalf("备份失败必须干净中止: %+v", last)
	}
	for _, c := range joinedCalls(env.stub) {
		if c == "--terminate Ubuntu" || strings.HasPrefix(c, "--unregister") {
			t.Fatalf("备份失败后不得进入销毁链: %v", joinedCalls(env.stub))
		}
	}
}
