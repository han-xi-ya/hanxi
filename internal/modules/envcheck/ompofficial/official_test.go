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

// fakeOps 假官方路线：内存 PATH + 可控脚本执行，绝不碰真注册表/真跑脚本/真目录。
type fakeOps struct {
	path       string
	writes     int
	broadcasts int
	scriptOut  string
	scriptErr  error
	present    bool
}

func (f *fakeOps) hooks(dir string) *Manager {
	return &Manager{
		dir:       dir,
		readPath:  func() (string, error) { return f.path, nil },
		writePath: func(v string) error { f.writes++; f.path = v; return nil },
		broadcast: func() { f.broadcasts++ },
		runScript: func(ctx context.Context) (string, error) {
			return f.scriptOut, f.scriptErr
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
