//go:build windows

package windows

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hanxi/internal/platform/apppackage"
)

type fakeAppPackageExecutor struct {
	response func(appPackageRequest) appPackageResponse
	gotExe   string
	gotArgs  []string
	gotInput []byte
}

func (f *fakeAppPackageExecutor) Execute(_ context.Context, executable string, args []string, stdin []byte) ([]byte, []byte, int, error) {
	f.gotExe = executable
	f.gotArgs = append([]string(nil), args...)
	f.gotInput = append([]byte(nil), stdin...)
	var req appPackageRequest
	if err := json.Unmarshal(stdin, &req); err != nil {
		return nil, nil, -1, err
	}
	resp := f.response(req)
	out, err := json.Marshal(resp)
	return out, nil, 0, err
}

func TestAppPackageQueryUsesJSONProtocol(t *testing.T) {
	exe := filepath.Join(t.TempDir(), "powershell.exe")
	if err := os.WriteFile(exe, []byte("stub"), 0o600); err != nil {
		t.Fatal(err)
	}
	identity := apppackage.Identity{Name: "pkg", Family: "pkg_family", Publisher: "CN=test", AppID: "App"}
	fake := &fakeAppPackageExecutor{response: func(req appPackageRequest) appPackageResponse {
		return appPackageResponse{
			ProtocolVersion: appPackageProtocolVersion,
			RequestID:       req.RequestID,
			OK:              true,
			Result: appPackageResult{Package: &apppackage.Package{
				Name: "pkg", Family: "pkg_family", Publisher: "CN=test", Version: "1.0.0.0",
			}},
		}
	}}
	api := &windowsAppPackageAPI{executable: exe, executor: fake}
	pkg, err := api.Query(context.Background(), identity)
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}
	if pkg == nil || pkg.Version != "1.0.0.0" {
		t.Fatalf("unexpected package: %#v", pkg)
	}
	if fake.gotExe != exe {
		t.Fatalf("unexpected executable: %s", fake.gotExe)
	}
	if len(fake.gotArgs) != 5 || fake.gotArgs[3] != "-EncodedCommand" {
		t.Fatalf("unexpected PowerShell args: %#v", fake.gotArgs)
	}
	var req appPackageRequest
	if err := json.Unmarshal(fake.gotInput, &req); err != nil {
		t.Fatal(err)
	}
	if req.Identity != identity || req.Operation != "query" {
		t.Fatalf("unexpected request: %#v", req)
	}
}

// TestAppPackageQueryAllUsersPresence 全用户探测通道：请求走 queryallusers
// 操作；在册与否完全由脚本回包 package 字段有无决定（存在性口径），查询失败
// 原样回错误——调用方（translucenttb 孤儿闸）必须按"无法证明"保守处理。
func TestAppPackageQueryAllUsersPresence(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "powershell.exe")
	if err := os.WriteFile(exe, []byte("stub"), 0o600); err != nil {
		t.Fatal(err)
	}
	identity := apppackage.Identity{Name: "pkg", Family: "pkg_family", Publisher: "CN=test", AppID: "App"}

	fake := &fakeAppPackageExecutor{response: func(req appPackageRequest) appPackageResponse {
		return appPackageResponse{ProtocolVersion: appPackageProtocolVersion, RequestID: req.RequestID, OK: true,
			Result: appPackageResult{Package: &apppackage.Package{Name: "pkg"}}}
	}}
	api := &windowsAppPackageAPI{executable: exe, executor: fake}
	registered, err := api.IsRegisteredAllUsers(context.Background(), identity)
	if err != nil || !registered {
		t.Fatalf("有包应判在册: %v %v", registered, err)
	}
	var req appPackageRequest
	if err := json.Unmarshal(fake.gotInput, &req); err != nil || req.Operation != "queryallusers" {
		t.Fatalf("操作名未走全用户通道: %+v err=%v", req, err)
	}

	fake.response = func(req appPackageRequest) appPackageResponse {
		return appPackageResponse{ProtocolVersion: appPackageProtocolVersion, RequestID: req.RequestID, OK: true}
	}
	if registered, err = api.IsRegisteredAllUsers(context.Background(), identity); err != nil || registered {
		t.Fatalf("无包应判不在册: %v %v", registered, err)
	}

	fake.response = func(req appPackageRequest) appPackageResponse {
		return appPackageResponse{ProtocolVersion: appPackageProtocolVersion, RequestID: req.RequestID, OK: false,
			Error: &appPackageScriptError{Code: apppackage.CodeDeployment, Message: "全用户枚举被拒"}}
	}
	if _, err = api.IsRegisteredAllUsers(context.Background(), identity); err == nil {
		t.Fatal("探测失败必须回错误，不得静默当无册")
	}
}

func TestAppPackageRejectsInvalidPath(t *testing.T) {
	api := &windowsAppPackageAPI{}
	_, err := api.Install(context.Background(), apppackage.InstallOptions{
		PackagePath: "missing.msixbundle",
		Expected:    apppackage.Identity{Name: "pkg", Family: "pkg_family", Publisher: "CN=test"},
	})
	var packageErr *apppackage.Error
	if !errors.As(err, &packageErr) || packageErr.Code != apppackage.CodeProtocol {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAppPackageAcceptsAppxBundleAndPassesDependencies(t *testing.T) {
	dir := t.TempDir()
	bundle := filepath.Join(dir, "tool.appxbundle")
	if err := os.WriteFile(bundle, []byte("stub"), 0o600); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(dir, "powershell.exe")
	if err := os.WriteFile(exe, []byte("stub"), 0o600); err != nil {
		t.Fatal(err)
	}
	fake := &fakeAppPackageExecutor{response: func(req appPackageRequest) appPackageResponse {
		return appPackageResponse{
			ProtocolVersion: appPackageProtocolVersion, RequestID: req.RequestID, OK: true,
			Result: appPackageResult{Package: &apppackage.Package{Name: "pkg", Version: "2.3.0.0"}},
		}
	}}
	api := &windowsAppPackageAPI{executable: exe, executor: fake}
	deps := []string{"https://aka.ms/framework.appx"}
	pkg, err := api.Install(context.Background(), apppackage.InstallOptions{
		PackagePath:  bundle,
		Expected:     apppackage.Identity{Name: "pkg", Family: "pkg_family", Publisher: "CN=test"},
		Dependencies: deps,
	})
	if err != nil || pkg == nil {
		t.Fatalf("appxbundle 应被接受: %v", err)
	}
	var req appPackageRequest
	if err := json.Unmarshal(fake.gotInput, &req); err != nil {
		t.Fatal(err)
	}
	if len(req.Dependencies) != 1 || req.Dependencies[0] != deps[0] {
		t.Fatalf("依赖未透传: %#v", req.Dependencies)
	}

	if _, err := api.Install(context.Background(), apppackage.InstallOptions{
		PackagePath:  bundle,
		Expected:     apppackage.Identity{Name: "pkg", Family: "pkg_family", Publisher: "CN=test"},
		Dependencies: []string{"relative/framework.appx"},
	}); err == nil {
		t.Fatal("相对路径依赖应被拒绝")
	}
}

func TestAppPackageRejectsNonBundleExtension(t *testing.T) {
	file := filepath.Join(t.TempDir(), "tool.exe")
	if err := os.WriteFile(file, []byte("stub"), 0o600); err != nil {
		t.Fatal(err)
	}
	api := &windowsAppPackageAPI{}
	_, err := api.Install(context.Background(), apppackage.InstallOptions{
		PackagePath: file,
		Expected:    apppackage.Identity{Name: "pkg", Family: "pkg_family", Publisher: "CN=test"},
	})
	var packageErr *apppackage.Error
	if !errors.As(err, &packageErr) || packageErr.Code != apppackage.CodeProtocol {
		t.Fatalf("非 bundle 扩展名应被拒绝: %v", err)
	}
}

// TestAppPackageScriptDispatchesAllRegisteredOps 真机跑内嵌 PowerShell 脚本，
// 断言脚本 switch 对 Go 侧全部操作名都有派发分支（A2 2026-10-26 审查修复的
// 回归锚点：queryallusers 曾只有 Go 侧发送方、脚本侧无接收分支，真机恒被
// default「不支持的包管理操作」拒收，而纯 fake-executor 协议测试全部照绿
// ——mock 掉了唯一会暴露断链的那一层）。
//
// 判据是脚本的**可观察回包**而非源码文本：
//   - 对照组：未知操作必须得到「不支持的包管理操作」（证明 default 分支活着、
//     脚本确实在跑）；
//   - queryallusers：绝不得再出现该拒收文案。本机以管理员运行时返回
//     registered=false（查无此包）；非管理员下 Get-AppxPackage -AllUsers
//     报部署侧错误同样算过——错误来自"分支已执行"，与"分支缺失"性质不同。
func TestAppPackageScriptDispatchesAllRegisteredOps(t *testing.T) {
	psPath := windowsPowerShellPath()
	if _, err := os.Stat(psPath); err != nil {
		t.Skipf("本机无 Windows PowerShell（%s）: %v", psPath, err)
	}
	api := NewAppPackageAPI().(*windowsAppPackageAPI)
	id := apppackage.Identity{
		Name:      "hanxi-a2-probe-not-installed",
		Family:    "hanxi-a2-probe-not-installed_0",
		Publisher: "CN=hanxi-a2-probe",
	}

	// 对照组：脚本必须活着且 default 分支如实拒收未知操作。
	if _, err := api.run(t.Context(), appPackageRequest{Operation: "a2-probe-unknown-op", Identity: id}); err == nil ||
		!strings.Contains(err.Error(), "不支持的包管理操作") {
		t.Fatalf("未知操作对照组失配（脚本未运行或 default 分支失效）: %v", err)
	}

	registered, err := api.IsRegisteredAllUsers(t.Context(), id)
	if err != nil && strings.Contains(err.Error(), "不支持的包管理操作") {
		t.Fatalf("queryallusers 仍被脚本 default 拒收——分支缺失复发: %v", err)
	}
	if err == nil && registered {
		t.Fatalf("幽灵包族不应在册: registered=%v", registered)
	}
	if err != nil {
		t.Logf("非管理员等部署侧报错（分支已派发，接受）: %v", err)
	}
}

func TestAppPackageMapsScriptError(t *testing.T) {
	err := mapAppPackageError(&appPackageScriptError{
		Code: apppackage.CodeInUse, Message: "正在使用", HResult: "0x80073D02", Retryable: true,
	})
	var packageErr *apppackage.Error
	if !errors.As(err, &packageErr) {
		t.Fatalf("unexpected error: %v", err)
	}
	if packageErr.Code != apppackage.CodeInUse || packageErr.HResult != "0x80073D02" || !packageErr.Retryable {
		t.Fatalf("unexpected mapped error: %#v", packageErr)
	}
}
