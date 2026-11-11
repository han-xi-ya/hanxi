package ompofficial

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeOps 假官方路线：内存 PATH + 可控脚本/自更新执行，绝不碰真注册表/真跑脚本/真目录。
type fakeOps struct {
	path        string
	writes      int
	broadcasts  int
	scriptOut   string
	scriptErr   error
	scriptCalls int
	updateOut   string
	updateErr   error
	updateCalls int
	lastExe     string
	lastForce   bool
	present     bool
}

func (f *fakeOps) hooks(dir string) *Manager {
	return &Manager{
		dir:       dir,
		readPath:  func() (string, error) { return f.path, nil },
		writePath: func(v string) error { f.writes++; f.path = v; return nil },
		broadcast: func() { f.broadcasts++ },
		runScript: func(ctx context.Context) (string, error) {
			f.scriptCalls++
			return f.scriptOut, f.scriptErr
		},
		runUpdate: func(ctx context.Context, exe string, force bool) (string, error) {
			f.updateCalls++
			f.lastExe, f.lastForce = exe, force
			return f.updateOut, f.updateErr
		},
		probeVer: func(exe string) string { return "omp/18.16.0" },
	}
}

// withPresent 让 dir/omp.exe 真实在场（present() 走文件系统，无从伪造接缝）。
func withPresent(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "omp.exe"), []byte("MZ fake"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRunOfficialScriptSuccessAndExistenceGuard(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "omp")
	f := &fakeOps{path: `C:\Windows`}
	o := f.hooks(dir)

	// 脚本"成功"但文件没落位——兜底核对必须拦下（防信错脚本自报）
	if _, err := o.RunScript(context.Background()); err == nil {
		t.Fatalf("缺文件兜底核对必须报错")
	}
	withPresent(t, dir)
	out, err := o.RunScript(context.Background())
	if err != nil || out != "" {
		t.Errorf("在场后应通过：%q/%v", out, err)
	}
}

func TestOfficialUninstallReclaimsAndPreservesData(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "omp")
	withPresent(t, dir)
	elsewhere := `C:\Other\tools`
	f := &fakeOps{path: `C:\Windows;` + elsewhere + `;` + dir}
	o := f.hooks(dir)

	msg, err := o.Uninstall()
	if err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("官方目录应被删除")
	}
	if strings.Contains(f.path, dir) {
		t.Errorf("PATH 条目未剔除：%q", f.path)
	}
	if !strings.Contains(f.path, elsewhere) {
		t.Errorf("无关条目被误删：%q", f.path)
	}
	if f.writes != 1 || f.broadcasts != 1 {
		t.Errorf("写/广播各一次：%d/%d", f.writes, f.broadcasts)
	}
	if !strings.Contains(msg, "~/.omp") {
		t.Errorf("回执须指路数据根保留： %q", msg)
	}
}

func TestOfficialProbeFourFacts(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "omp")
	f := &fakeOps{path: `C:\Windows;` + dir}
	o := f.hooks(dir)
	st := o.Probe()
	if st.OfficialPresent || st.OfficialVersion != "" || !st.OnPath {
		t.Errorf("无文件应 present=false/ver 空、PATH 命中：%+v", st)
	}
	withPresent(t, dir)
	st = o.Probe()
	if !st.OfficialPresent || st.OfficialVersion != "omp/18.16.0" {
		t.Errorf("在场应探出实版：%+v", st)
	}
	if st.DataDir == "" || st.OfficialDir != dir {
		t.Errorf("数据根/目录字段缺失：%+v", st)
	}
}

func TestEnvpathPureFunctions(t *testing.T) {
	parts, added := pathAdd([]string{`C:\Windows`, `c:\users\x\tools\`}, `C:\Users\X\Tools`)
	if added || len(parts) != 2 {
		t.Errorf("判同失败（大小写/尾斜杠）：%v %v", parts, added)
	}
	parts, added = pathAdd([]string{`C:\Windows`, ``}, `D:\omp-bin`)
	if !added || len(parts) != 2 {
		t.Errorf("空段应被压缩、新目录应追加：%v", parts)
	}
	kept, removed := pathRemove([]string{`C:\Windows`, `D:\omp-bin`}, `d:\omp-bin\`)
	if !removed || len(kept) != 1 || kept[0] != `C:\Windows` {
		t.Errorf("剔除失败：%v %v", kept, removed)
	}
	if !pathContains([]string{`C:\Windows`}, `c:\windows`) || pathContains([]string{`C:\Windows`}, `D:\x`) {
		t.Errorf("pathContains 判同失准")
	}
}

func TestRunOfficialScriptRetriesTransientFailure(t *testing.T) {
	retryBackoff = func(int) time.Duration { return time.Millisecond }
	dir := filepath.Join(t.TempDir(), "omp")
	withPresent(t, dir)
	calls := 0
	o := &Manager{dir: dir,
		readPath: func() (string, error) { return "", nil }, writePath: func(string) error { return nil },
		broadcast: func() {},
		runScript: func(ctx context.Context) (string, error) {
			calls++
			if calls < 3 {
				return "irm : 基础连接已经关闭", errors.New("connection closed")
			}
			return "installed", nil
		},
		probeVer: func(string) string { return "omp/18.16.0" }}

	out, err := o.RunScript(context.Background())
	if err != nil || calls != 3 {
		t.Fatalf("应自动重试至第 3 次成功：%v calls=%d", err, calls)
	}
	if !strings.Contains(out, "第 3 次尝试成功") {
		t.Errorf("回执须如实注记重试次数：%q", out)
	}
}

func TestRunOfficialScriptCancelBeatsRetry(t *testing.T) {
	o := &Manager{dir: filepath.Join(t.TempDir(), "omp"),
		readPath: func() (string, error) { return "", nil }, writePath: func(string) error { return nil },
		broadcast: func() {}, probeVer: func(string) string { return "" },
		runScript: func(ctx context.Context) (string, error) { return "", errors.New("transient") }}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := o.RunScript(ctx); err == nil {
		t.Fatalf("已取消的 ctx 必须立即失败，不进重试")
	}
}

// TestUpdateLatestRoutesByMachineState 更新/修复的通道归属（2026-11-11 重切）：
// 装件在场只走 omp 自带自更新（增量补丁、脚本整包重下是错位），不在场才回退
// 官方脚本首装；force 逐字透传给 `omp update --force`（页内「修复/重装」语义）。
func TestUpdateLatestRoutesByMachineState(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "omp")

	t.Run("在场=只叫自更新不叫脚本", func(t *testing.T) {
		withPresent(t, dir)
		f := &fakeOps{path: `C:\Windows`, updateOut: "Updated to 18.18.8"}
		if _, err := f.hooks(dir).UpdateLatest(context.Background(), false); err != nil {
			t.Fatalf("UpdateLatest: %v", err)
		}
		if f.updateCalls != 1 || f.scriptCalls != 0 {
			t.Errorf("update=%d script=%d, want 1/0（在用装件不许走整包脚本）", f.updateCalls, f.scriptCalls)
		}
		if f.lastExe != filepath.Join(dir, "omp.exe") || f.lastForce {
			t.Errorf("自更新入参失真：exe=%q force=%v", f.lastExe, f.lastForce)
		}
	})

	t.Run("修复重装=force 透传", func(t *testing.T) {
		withPresent(t, dir)
		f := &fakeOps{path: `C:\Windows`, updateOut: "reinstalled"}
		if _, err := f.hooks(dir).UpdateLatest(context.Background(), true); err != nil {
			t.Fatalf("UpdateLatest(force): %v", err)
		}
		if f.updateCalls != 1 || f.scriptCalls != 0 || !f.lastForce {
			t.Errorf("update=%d script=%d force=%v, want 1/0/true", f.updateCalls, f.scriptCalls, f.lastForce)
		}
	})

	t.Run("不在场=回退脚本首装", func(t *testing.T) {
		empty := filepath.Join(t.TempDir(), "omp")
		f := &fakeOps{path: `C:\Windows`, scriptOut: "installed"}
		o := f.hooks(empty)
		// 脚本真会落盘，假件照此把文件种上（否则 RunScript 的"跑完没落盘"兜底核对
		// 会先把这条路由判成故障，测不到分流本身）。
		o.runScript = func(context.Context) (string, error) {
			f.scriptCalls++
			withPresent(t, empty)
			return "installed", nil
		}
		if _, err := o.UpdateLatest(context.Background(), false); err != nil {
			t.Fatalf("UpdateLatest(缺装件): %v", err)
		}
		if f.scriptCalls != 1 || f.updateCalls != 0 {
			t.Errorf("script=%d update=%d, want 1/0（没装件时无从自更新）", f.scriptCalls, f.updateCalls)
		}
	})
}

// TestSelfUpdateGuardsAndRetries 自更新的三道关：缺装件直接拒（不许对着空气更新）、
// 瞬时失败整命令级重试并在回执注记、"跑完文件不见了"兜底核对拦下。
func TestSelfUpdateGuardsAndRetries(t *testing.T) {
	retryBackoff = func(int) time.Duration { return time.Millisecond }
	t.Cleanup(func() {
		retryBackoff = func(attempt int) time.Duration { return time.Duration(attempt*4) * time.Second }
	})

	dir := filepath.Join(t.TempDir(), "omp")
	if _, err := (&fakeOps{}).hooks(dir).SelfUpdate(context.Background(), false); err == nil {
		t.Fatal("目录里没有 omp.exe 必须拒掉自更新")
	}

	withPresent(t, dir)
	f := &fakeOps{path: `C:\Windows`}
	attempts := 0
	o := f.hooks(dir)
	o.runUpdate = func(context.Context, string, bool) (string, error) {
		attempts++
		if attempts < 2 {
			return "fetch failed", errors.New("connection reset")
		}
		return "Updated to 18.18.8", nil
	}
	out, err := o.SelfUpdate(context.Background(), false)
	if err != nil || attempts != 2 {
		t.Fatalf("应重试至第 2 次成功：%v attempts=%d", err, attempts)
	}
	if !strings.Contains(out, "第 2 次尝试成功") {
		t.Errorf("回执须如实注记重试次数：%q", out)
	}

	// 成功判据不只看退出码：换件半途失败（文件消失）必须现形
	o.runUpdate = func(context.Context, string, bool) (string, error) {
		if rmErr := os.Remove(filepath.Join(dir, "omp.exe")); rmErr != nil {
			return "", rmErr
		}
		return "Installing update...", nil
	}
	if _, err := o.SelfUpdate(context.Background(), false); err == nil {
		t.Fatal("命令自报成功但 omp.exe 不见了，兜底核对必须报错")
	}
}
