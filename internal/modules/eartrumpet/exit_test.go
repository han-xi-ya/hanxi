package eartrumpet

import (
	"context"
	"errors"
	"testing"

	"hanxi/internal/platform"
	"hanxi/internal/platform/apppackage"
)

type fakeProcs struct {
	kills  []platform.VerifyToken
	failOn uint32
}

func (f *fakeProcs) Query(pid uint32) (platform.ProcInfo, error) {
	return platform.ProcInfo{PID: pid, Name: exeName}, nil
}

func (f *fakeProcs) KillVerified(_ context.Context, token platform.VerifyToken, _ bool) error {
	if f.failOn != 0 && token.PID == f.failOn {
		return errors.New("Access is denied.")
	}
	f.kills = append(f.kills, token)
	return nil
}

func (f *fakeProcs) IsProtected(uint32, platform.ProcInfo) bool { return false }

func procAt(pid uint32, dir string) platform.ProcInfo {
	return platform.ProcInfo{PID: pid, ExePath: dir + `\EarTrumpet\EarTrumpet.exe`}
}

func TestIsUnderDir(t *testing.T) {
	cases := []struct {
		path, dir string
		want      bool
	}{
		{`C:\PFWA\pkg.2.3.0!\EarTrumpet\EarTrumpet.exe`, `C:\PFWA\pkg.2.3.0!`, true},
		{`c:\pfwa\PKG.2.3.0!\eartrumpet\eartrumpet.exe`, `C:\PFWA\pkg.2.3.0!`, true},
		{`C:\PFWA\pkg.2.3.0!X\EarTrumpet.exe`, `C:\PFWA\pkg.2.3.0!`, false}, // 前缀相同但非子目录
		{`C:\Other\EarTrumpet.exe`, `C:\PFWA\pkg.2.3.0!`, false},
		{``, `C:\PFWA\pkg`, false},
	}
	for _, c := range cases {
		if got := isUnderDir(c.path, c.dir); got != c.want {
			t.Fatalf("isUnderDir(%q, %q) = %v", c.path, c.dir, got)
		}
	}
}

func TestExitTerminatesOnlyManagedChannelProcesses(t *testing.T) {
	pkg := installedPkg("2.3.0.20", managedIdentity.Family, "X86")
	pkgs := &fakePackages{byFamily: map[string]*apppackage.Package{managedIdentity.Family: pkg}}
	procs := &fakeProcs{}
	svc := newTestService(pkgs, &fakeOpener{}, remoteFetch(), &fakeDownload{})
	svc.procs = procs
	var sawLoc string
	svc.findProcs = func(loc string) []platform.ProcInfo {
		sawLoc = loc
		return []platform.ProcInfo{procAt(101, loc), procAt(102, loc)}
	}

	killed, err := svc.Exit()
	if err != nil || killed != 2 {
		t.Fatalf("killed=%d err=%v", killed, err)
	}
	if sawLoc != pkg.InstallLocation {
		t.Fatalf("应按直装包安装目录探测: %s", sawLoc)
	}
	if len(procs.kills) != 2 || procs.kills[0].PID != 101 || procs.kills[1].ExePath == "" {
		t.Fatalf("终止令牌错误: %+v", procs.kills)
	}
}

func TestExitPartialFailureStillReportsKilled(t *testing.T) {
	pkg := installedPkg("2.3.0.20", managedIdentity.Family, "X86")
	svc := newTestService(&fakePackages{byFamily: map[string]*apppackage.Package{managedIdentity.Family: pkg}}, &fakeOpener{}, remoteFetch(), &fakeDownload{})
	svc.procs = &fakeProcs{failOn: 202}
	svc.findProcs = func(loc string) []platform.ProcInfo {
		return []platform.ProcInfo{procAt(201, loc), procAt(202, loc)}
	}
	killed, err := svc.Exit()
	if err != nil || killed != 1 {
		t.Fatalf("killed=%d err=%v", killed, err)
	}
}

func TestExitGuards(t *testing.T) {
	pkg := installedPkg("2.3.0.20", managedIdentity.Family, "X86")
	svc := newTestService(&fakePackages{byFamily: map[string]*apppackage.Package{managedIdentity.Family: pkg}}, &fakeOpener{}, remoteFetch(), &fakeDownload{})
	svc.procs = &fakeProcs{}

	notInstalled := newTestService(&fakePackages{}, &fakeOpener{}, remoteFetch(), &fakeDownload{})
	notInstalled.procs = &fakeProcs{}
	notInstalled.findProcs = func(string) []platform.ProcInfo { return nil }
	if _, err := notInstalled.Exit(); err == nil {
		t.Fatal("未安装时应报错")
	}

	svc.findProcs = func(string) []platform.ProcInfo { return nil }
	if _, err := svc.Exit(); err == nil {
		t.Fatal("未运行时应报错而非静默成功")
	}

	// 探测能力未接线（仅测试场景）：保守报错，不 panic
	bare := newTestService(&fakePackages{byFamily: map[string]*apppackage.Package{managedIdentity.Family: pkg}}, &fakeOpener{}, remoteFetch(), &fakeDownload{})
	if _, err := bare.Exit(); err == nil {
		t.Fatal("缺进程能力时应报错")
	}
}

func TestGetStatusRunningFlag(t *testing.T) {
	pkg := installedPkg("2.3.0.20", managedIdentity.Family, "X86")
	svc := newTestService(&fakePackages{byFamily: map[string]*apppackage.Package{managedIdentity.Family: pkg}}, &fakeOpener{}, remoteFetch(), &fakeDownload{})
	svc.findProcs = func(loc string) []platform.ProcInfo {
		return []platform.ProcInfo{procAt(1, loc)}
	}

	snap, err := svc.GetStatus()
	if err != nil {
		t.Fatal(err)
	}
	if !snap.Running {
		t.Fatalf("应报告运行中: %+v", snap)
	}
}
