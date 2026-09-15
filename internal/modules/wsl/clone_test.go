package wsl

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hanxi/internal/modules/wsl/readiness"
)

func TestVersionAtLeast(t *testing.T) {
	min := []int{2, 7, 3}
	cases := []struct {
		v    string
		want bool
	}{
		{"2.7.3", true}, {"2.7.2", false}, {"2.8.0", true}, {"3.0.0", true},
		{"2.7.13", true}, {"2.6.9", false}, {"", false}, {"2.7", false}, {"wsl2", false},
	}
	for _, c := range cases {
		if got := versionAtLeast(c.v, min); got != c.want {
			t.Fatalf("versionAtLeast(%q)=%v，期望 %v", c.v, got, c.want)
		}
	}
}

// cloneFixture：Ubuntu（WSL2）源盘为临时文件，--vhd 能力版本可注入。
// 返回调用桩与源 VHDX 路径；lxss 巡查、版本、名单通道全部离线桩化。
func cloneFixture(t *testing.T, svc *WslService, wslVer string) (*wslStub, string) {
	t.Helper()
	oldFree := diskFree
	diskFree = func(string) (uint64, error) { return 1 << 40, nil }
	t.Cleanup(func() { diskFree = oldFree })
	vhdx := filepath.Join(t.TempDir(), "src", "ext4.vhdx")
	if err := os.MkdirAll(filepath.Dir(vhdx), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(vhdx, []byte(strings.Repeat("x", 4096)), 0o600); err != nil {
		t.Fatal(err)
	}
	svc.wslVersion = func(context.Context) string { return wslVer }
	svc.wslDistros = func(context.Context) []readiness.Distro {
		return []readiness.Distro{{Name: "Ubuntu", State: "Running", Version: "2"}}
	}
	svc.localPS = func(_ context.Context, script string) (string, error) {
		if strings.HasPrefix(script, "$items") {
			return fmt.Sprintf(`[{"name":"Ubuntu","basePath":%q,"vhdx":%q,"size":4096}]`, filepath.Dir(vhdx), vhdx), nil
		}
		return "", nil
	}
	stub := &wslStub{resp: func(args []string) (string, error) {
		switch joined(args) {
		case "-l -q", "-l -q --running":
			return "Ubuntu\x00", nil
		case "--terminate Ubuntu":
			return "", nil
		}
		if len(args) >= 1 && (args[0] == "--import" || args[0] == "--import-in-place" || args[0] == "--unregister") {
			return "", nil
		}
		return "", fmt.Errorf("意外的 wsl 调用: %v", args)
	}}
	svc.runWsl = stub.run
	return stub, vhdx
}

func TestCloneDistroRejections(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "clone")
	cases := []struct {
		desc    string
		ver     string
		newName string
		want    string
	}{
		{"源与目标同名", "2.9.0", "ubuntu", "同名"},
		{"新名以选项形态开头", "2.9.0", "--evil", "不合法"},
		{"新名撞本机名单", "2.9.0", "Docker", "已存在"},
		{"WSL 版本过旧不满足 --import-in-place", "2.6.0", "Ubuntu-Copy", "--import-in-place"},
	}
	for _, c := range cases {
		svc, _, _ := newTestService()
		cloneFixture(t, svc, c.ver)
		if c.desc == "新名撞本机名单" {
			svc.runWsl = quietList("Ubuntu", "Docker")
		}
		if _, err := svc.CloneDistro("Ubuntu", c.newName, dest); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Fatalf("%s: 应报含 %q 的错误，实得 %v", c.desc, c.want, err)
		}
	}
	// WSL1 源拒绝并指路
	svc, _, _ := newTestService()
	cloneFixture(t, svc, "2.9.0")
	svc.wslDistros = func(context.Context) []readiness.Distro {
		return []readiness.Distro{{Name: "Ubuntu", Version: "1"}}
	}
	if _, err := svc.CloneDistro("Ubuntu", "Ubuntu-Copy", dest); err == nil || !strings.Contains(err.Error(), "WSL2") {
		t.Fatalf("WSL1 源应被拒并指路: %v", err)
	}
}

// cloneDoneCh 在受理克隆前挂上终态事件通道（done 或 error），避免与后台协程竞写 emit。
func cloneDoneCh(svc *WslService) <-chan CloneProgress {
	ch := make(chan CloneProgress, 8)
	svc.emit = func(_ string, payload any) {
		if p, ok := payload.(CloneProgress); ok && (p.Stage == "done" || p.Stage == "error") {
			select {
			case ch <- p:
			default:
			}
		}
	}
	return ch
}

func waitCloneDone(t *testing.T, ch <-chan CloneProgress) CloneProgress {
	t.Helper()
	select {
	case p := <-ch:
		return p
	case <-time.After(10 * time.Second):
		t.Fatal("克隆后台终态事件超时未到")
		return CloneProgress{}
	}
}

func TestCloneDistroHappyPath(t *testing.T) {
	svc := newSvc(t)
	stub, _ := cloneFixture(t, svc, "2.7.13")
	dest := filepath.Join(t.TempDir(), "clone")
	// 复验名单：源白名单、新名防撞两次查询都只见 Ubuntu；导入后第三次复验才见 Ubuntu-Copy。
	qcalls := 0
	base := stub.resp
	stub.resp = func(args []string) (string, error) {
		if joined(args) == "-l -q" {
			qcalls++
			if qcalls <= 2 {
				return "Ubuntu\x00", nil
			}
			return "Ubuntu\x00Ubuntu-Copy\x00", nil
		}
		return base(args)
	}
	ch := cloneDoneCh(svc)
	out, err := svc.CloneDistro("Ubuntu", "Ubuntu-Copy", dest)
	if err != nil {
		t.Fatal(err)
	}
	if !out.Success {
		t.Fatalf("受理回执应为成功: %+v", out)
	}
	p := waitCloneDone(t, ch)
	if p.Stage != "done" {
		t.Fatalf("终态应为 done: %+v", p)
	}
	// 数据面对：副本落目标目录且与源同尺寸
	cp := filepath.Join(dest, "ext4.vhdx")
	st, err := os.Stat(cp)
	if err != nil || st.Size() != 4096 {
		t.Fatalf("克隆盘未正确落位: %v %+v", err, st)
	}
	// 命令面对：--import-in-place 带克隆盘路径；先 terminate 源
	var sawImport, sawTerm bool
	for _, c := range stub.calls {
		j := joined(c)
		if j == "--terminate Ubuntu" {
			sawTerm = true
		}
		if j == "--import-in-place Ubuntu-Copy "+cp {
			sawImport = true
		}
	}
	if !sawImport || !sawTerm {
		t.Fatalf("命令面错误: import=%v term=%v calls=%v", sawImport, sawTerm, joinedCalls(stub))
	}
	svc.mu.Lock()
	idle := len(svc.distroOps) == 0
	svc.mu.Unlock()
	if !idle {
		t.Fatal("终态后两把单飞闸必须释放")
	}
}

func TestCloneDistroImportFailureKeepsCopyAndUnregisters(t *testing.T) {
	svc := newSvc(t)
	stub, _ := cloneFixture(t, svc, "2.7.13")
	dest := filepath.Join(t.TempDir(), "clone2")
	q := quietList("Ubuntu")
	stub.resp = func(args []string) (string, error) {
		if args[0] == "--import-in-place" {
			return "挂载失败", errors.New("exit status 42")
		}
		return q(context.Background(), args...)
	}
	ch := cloneDoneCh(svc)
	if _, err := svc.CloneDistro("Ubuntu", "Ubuntu-Copy", dest); err != nil {
		t.Fatal(err)
	}
	p := waitCloneDone(t, ch)
	if p.Stage != "error" || !strings.Contains(p.Error, "保留在") {
		t.Fatalf("导入失败应报 error 并点名保留盘: %+v", p)
	}
	if _, err := os.Stat(filepath.Join(dest, "ext4.vhdx")); err != nil {
		t.Fatalf("已拷贝数据盘必须保留（绝不暗删）: %v", err)
	}
	sawUnreg := false
	for _, c := range stub.calls {
		if joined(c) == "--unregister Ubuntu-Copy" {
			sawUnreg = true
		}
	}
	if !sawUnreg {
		t.Fatal("半成品新实例应尽力注销回收")
	}
}

func TestImportDistro(t *testing.T) {
	tar := filepath.Join(t.TempDir(), "rootfs.tar")
	if err := os.WriteFile(tar, []byte("tar-bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "fresh")

	// 校验矩阵：坏名 / 坏扩展名 / 文件不存在
	svc, _, _ := newTestService()
	svc.runWsl = quietList("Ubuntu")
	for _, bad := range []struct{ name, p, want string }{
		{"--calc", tar, "不合法"},
		{"Fresh", strings.TrimSuffix(tar, ".tar") + ".exe", ".tar"},
		{"Fresh", filepath.Join(t.TempDir(), "nope.tar"), "不存在"},
	} {
		if _, err := svc.ImportDistro(bad.name, dest+bad.name, bad.p); err == nil || !strings.Contains(err.Error(), bad.want) {
			t.Fatalf("%s: 期望报 %q，实得 %v", bad.name, bad.want, err)
		}
	}

	// 成功路径：命令面为 --import Fresh <dest> <tar>，复验名单含新名
	svc2 := newSvc(t)
	stub, _ := cloneFixture(t, svc2, "2.7.13")
	base := stub.resp
	qcalls := 0
	stub.resp = func(args []string) (string, error) {
		if joined(args) == "-l -q" {
			qcalls++
			if qcalls <= 1 {
				return "Ubuntu\x00", nil
			}
			return "Ubuntu\x00Fresh\x00", nil
		}
		return base(args)
	}
	res, err := svc2.ImportDistro("Fresh", dest, tar)
	if err != nil || !res.Success {
		t.Fatalf("导入应成功: %v %+v", err, res)
	}
	saw := false
	for _, c := range stub.calls {
		if joined(c) == fmt.Sprintf("--import Fresh %s %s", dest, tar) {
			saw = true
		}
	}
	if !saw {
		t.Fatalf("导入命令面错误: %v", joinedCalls(stub))
	}
}

// newSvc 便捷构造（克隆测试要先把 svc 交给 fixture 再拿桩）。
func newSvc(t *testing.T) *WslService {
	t.Helper()
	svc, _, _ := newTestService()
	return svc
}

// quietList 恒答 `-l -q`/`-l -q --running` 名单的 wsl 桩。
func quietList(names ...string) func(context.Context, ...string) (string, error) {
	list := strings.Join(names, "\x00") + "\x00"
	return func(_ context.Context, args ...string) (string, error) {
		switch joined(args) {
		case "-l -q", "-l -q --running":
			return list, nil
		}
		return "", fmt.Errorf("意外的 wsl 调用: %v", args)
	}
}
