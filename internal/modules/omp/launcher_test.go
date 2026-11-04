package omp

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeLauncher 假注册表 launcher：全部内存态，绝不触碰机主真实 HKCU\Environment。
type fakeLauncher struct {
	path       string // 模拟用户 PATH 原文
	writes     int
	broadcasts int
	linkErr    error
	readErr    error
	writeErr   error
	lastLinked [2]string
}

func (f *fakeLauncher) hooks() *launcher {
	return &launcher{
		readPath: func() (string, error) { return f.path, f.readErr },
		writePath: func(v string) error {
			f.writes++
			if f.writeErr != nil {
				return f.writeErr
			}
			f.path = v
			return nil
		},
		broadcast: func() { f.broadcasts++ },
		link: func(src, dst string) error {
			if f.linkErr != nil {
				return f.linkErr
			}
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				return err
			}
			f.lastLinked = [2]string{src, dst}
			return os.WriteFile(dst, []byte("linked:"+src), 0o755)
		},
	}
}

func TestLauncherInstallFirstTimeWritesPath(t *testing.T) {
	dir := t.TempDir()
	active := filepath.Join(dir, "omp-windows-x64.exe")
	os.WriteFile(active, []byte("MZ..."), 0o755)
	bin := filepath.Join(dir, "bin", "omp")
	f := &fakeLauncher{path: `C:\Windows;C:\Users\x\tools`}
	l := f.hooks()

	out, err := l.install(bin, active)
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if !out.PathAdded || !out.Broadcasted {
		t.Errorf("首装应写 PATH 并广播：%+v", out)
	}
	if !strings.Contains(f.path, bin) || f.writes != 1 || f.broadcasts != 1 {
		t.Errorf("PATH=%q writes=%d broadcasts=%d", f.path, f.writes, f.broadcasts)
	}
	if installed, onPath := l.status(bin); !installed || !onPath {
		t.Errorf("status 应为双真：%v/%v", installed, onPath)
	}
}

func TestLauncherInstallIdempotentAndRepoints(t *testing.T) {
	dir := t.TempDir()
	v1 := filepath.Join(dir, "v1.exe")
	v2 := filepath.Join(dir, "v2.exe")
	os.WriteFile(v1, []byte("MZ-v1"), 0o755)
	os.WriteFile(v2, []byte("MZ-v2"), 0o755)
	bin := filepath.Join(dir, "bin", "omp")
	f := &fakeLauncher{path: `C:\Windows`}
	l := f.hooks()

	if _, err := l.install(bin, v1); err != nil {
		t.Fatal(err)
	}
	writesAfterFirst := f.writes
	// 二次安装（同版）：PATH 不重复写；换版：链接重指、注册表仍不动
	if _, err := l.install(bin, v2); err != nil {
		t.Fatal(err)
	}
	if f.writes != writesAfterFirst {
		t.Errorf("二次安装不应再写 PATH（幂等），writes=%d", f.writes)
	}
	if f.lastLinked[0] != v2 {
		t.Errorf("入口应重指新激活版：%v", f.lastLinked)
	}
	if got := strings.Count(f.path, ";"+bin) + strings.Count(f.path, bin+";"); got > 1 {
		t.Errorf("PATH 条目重复：%q", f.path)
	}
}

func TestLauncherUninstallReclaimsOnlyOwnEntries(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin", "omp")
	elsewhere := `C:\Users\Administrator\AppData\Local\omp` // 官方脚本装的那种：绝不替用户删
	f := &fakeLauncher{path: `C:\Windows;` + elsewhere + `;` + bin}
	l := f.hooks()
	os.MkdirAll(bin, 0o755)
	os.WriteFile(filepath.Join(bin, launcherExeName), []byte("x"), 0o755)

	out, err := l.uninstall(bin)
	if err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if !out.PathRemoved {
		t.Errorf("应剔除本模块条目：%+v", out)
	}
	if !strings.Contains(f.path, strings.ToLower(elsewhere)) && !strings.Contains(f.path, elsewhere) {
		t.Errorf("官方装件条目不得被误删：%q", f.path)
	}
	if strings.Contains(f.path, bin) {
		t.Errorf("本模块条目未剔除：%q", f.path)
	}
	if _, err := os.Stat(bin); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("入口目录删空后应顺手回收，实际仍在")
	}
}

func TestEnvpathPureFunctions(t *testing.T) {
	// pathAdd 大小写折叠 + 尾分隔符宽容；pathRemove 同口径；空段压缩只在写入时发生
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
	if _, removed := pathRemove([]string{`C:\Windows`}, `D:\other`); removed {
		t.Errorf("不含目标不应报已剔除")
	}
	if !pathContains([]string{`C:\Windows`}, `c:\windows`) || pathContains([]string{`C:\Windows`}, `D:\x`) {
		t.Errorf("pathContains 判同失准")
	}
}
