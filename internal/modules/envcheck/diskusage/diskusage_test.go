//go:build windows

package diskusage

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hanxi/internal/modules/envcheck/detect"
)

func installed(name, exe string) detect.ToolInfo {
	return detect.ToolInfo{Name: name, Display: strings.ToUpper(name), Path: exe, Status: detect.StatusInstalled}
}

func envMap(m map[string]string) envLookupFunc {
	return func(k string) string { return m[k] }
}

func fakeRun(outputs map[string]string) runOutputFunc {
	return func(_ context.Context, exe string, args ...string) (string, error) {
		key := filepath.Base(exe) + " " + strings.Join(args, " ")
		if out, ok := outputs[key]; ok {
			return out, nil
		}
		return "", os.ErrNotExist
	}
}

func mkDirWithFile(t *testing.T, rel string, bytes int) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), rel)
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p, "payload.bin"), make([]byte, bytes), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func findDir(t *testing.T, tu ToolUsage, label string) DirUsage {
	t.Helper()
	for _, d := range tu.Dirs {
		if d.Label == label {
			return d
		}
	}
	t.Fatalf("%s 缺少目录 %q: %+v", tu.Tool, label, tu.Dirs)
	return DirUsage{}
}

func TestGoResolutionPrecedence(t *testing.T) {
	goRoot := mkDirWithFile(t, "goroot", 100)
	modCache := mkDirWithFile(t, "modcache", 200)
	out := collectWith(context.Background(),
		[]detect.ToolInfo{installed("go", `C:\go\bin\go.exe`)},
		envMap(map[string]string{"GOROOT": goRoot, "GOCACHE": modCache}),
		fakeRun(map[string]string{"go.exe env GOMODCACHE": mkDirWithFile(t, "gomod", 50) + "\n"}),
	)
	if len(out) != 1 {
		t.Fatalf("go 应产出一组家底: %+v", out)
	}
	tu := out[0]
	if d := findDir(t, tu, "Go SDK (GOROOT)"); d.Path != goRoot || !d.Exists || d.Bytes != 100 {
		t.Errorf("GOROOT env 优先失败: %+v", d)
	}
	if d := findDir(t, tu, "模块缓存 (GOMODCACHE)"); d.Bytes != 50 {
		t.Errorf("GOMODCACHE 应走命令档: %+v", d)
	}
	if d := findDir(t, tu, "构建缓存 (GOCACHE)"); d.Bytes != 200 || d.Partial {
		t.Errorf("GOCACHE env 档失败: %+v", d)
	}
}

func TestSkipUnknownAndMissing(t *testing.T) {
	out := collectWith(context.Background(), []detect.ToolInfo{
		{Name: "rustc", Status: detect.StatusInstalled, Path: "C:\\x\\rustc.exe"},
		installed("node", ""),
		{Name: "python", Status: detect.StatusMissing, Path: "C:\\py\\python.exe"},
	}, envMap(nil), fakeRun(nil))
	if len(out) != 0 {
		t.Fatalf("未注册/未安装/exe 缺失工具不得产出: %+v", out)
	}
}

func TestJavaEcosystemAndExistenceOnly(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".m2", "repository"), 0o755); err != nil {
		t.Fatal(err)
	}
	out := collectWith(context.Background(),
		[]detect.ToolInfo{installed("java", filepath.Join(home, "jdk", "bin", "java.exe"))},
		envMap(map[string]string{"HOME": home, "USERPROFILE": home}), fakeRun(nil))
	tu := out[0]
	if d := findDir(t, tu, "JDK/JRE 安装目录"); d.Path != filepath.Join(home, "jdk") {
		t.Errorf("jdk 应取 bin 上两级: %+v", d)
	}
	if d := findDir(t, tu, "Maven 仓库"); !d.Exists {
		t.Errorf("磁盘在场的 Maven 仓库应标 Exists: %+v", d)
	}
	if d := findDir(t, tu, "Gradle 缓存"); d.Exists {
		t.Errorf("磁盘不在场的 Gradle 不得谎称 Exists: %+v", d)
	}
}

func TestGitRootMarkers(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "mingw64"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "cmd"), 0o755); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(root, "cmd", "git.exe")
	out := collectWith(context.Background(), []detect.ToolInfo{installed("git", exe)}, envMap(nil), fakeRun(nil))
	if d := findDir(t, out[0], "Git 安装目录"); d.Path != root {
		t.Errorf("命中标记应上跳根目录: got %q want %q", d.Path, root)
	}
}

func TestDotnetNugetExtraction(t *testing.T) {
	pkgs := mkDirWithFile(t, "nuget", 7)
	out := collectWith(context.Background(),
		[]detect.ToolInfo{installed("dotnet", `C:\Program Files\dotnet\dotnet.exe`)},
		envMap(nil),
		fakeRun(map[string]string{"dotnet.exe nuget locals global-packages --list": "global-packages: " + pkgs + "\n"}))
	if d := findDir(t, out[0], "NuGet 全局包"); d.Path != pkgs || d.Bytes != 7 {
		t.Errorf("nuget 输出提取失败: %+v", d)
	}
}

func TestFailureOutputGuard(t *testing.T) {
	// npm config get cache 报 "undefined" 或非路径话术时必须落默认档而非当路径用。
	out := collectWith(context.Background(),
		[]detect.ToolInfo{installed("npm", `C:\node\npm.cmd`)},
		envMap(map[string]string{"LOCALAPPDATA": t.TempDir()}),
		fakeRun(map[string]string{"npm.cmd config get cache": "not found\n"}))
	d := findDir(t, out[0], "npm 缓存")
	if strings.Contains(d.Path, "not found") {
		t.Fatalf("失败话术被误当路径: %q", d.Path)
	}
}
