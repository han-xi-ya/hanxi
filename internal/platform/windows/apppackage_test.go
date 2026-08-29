//go:build windows

package windows

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
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
