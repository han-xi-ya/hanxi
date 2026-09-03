//go:build windows

package windows

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"unicode/utf16"

	"hanxi/internal/platform/apppackage"
)

const (
	appPackageProtocolVersion = 1
	appPackageOutputLimit     = 1 << 20
	appPackageCreateNoWindow  = 0x08000000
)

type appPackageRequest struct {
	ProtocolVersion int                 `json:"protocolVersion"`
	RequestID       string              `json:"requestId"`
	Operation       string              `json:"operation"`
	Identity        apppackage.Identity `json:"identity"`
	PackagePath     string              `json:"packagePath,omitempty"`
	PackageFullName string              `json:"packageFullName,omitempty"`
	ExpectedVersion string              `json:"expectedVersion,omitempty"`
	AllowDowngrade  bool                `json:"allowDowngrade,omitempty"`
	Dependencies    []string            `json:"dependencies,omitempty"`
}

type appPackageResponse struct {
	ProtocolVersion int                    `json:"protocolVersion"`
	RequestID       string                 `json:"requestId"`
	OK              bool                   `json:"ok"`
	Result          appPackageResult       `json:"result"`
	Error           *appPackageScriptError `json:"error"`
}

type appPackageResult struct {
	Package *apppackage.Package `json:"package"`
}

type appPackageScriptError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Detail    string `json:"detail"`
	HResult   string `json:"hresult"`
	Category  string `json:"category"`
	FQID      string `json:"fullyQualifiedErrorId"`
	Retryable bool   `json:"retryable"`
}

type appPackageCommandExecutor interface {
	Execute(ctx context.Context, executable string, args []string, stdin []byte) (stdout, stderr []byte, exitCode int, err error)
}

type windowsAppPackageAPI struct {
	executable string
	executor   appPackageCommandExecutor
	sequence   atomic.Uint64
}

func NewAppPackageAPI() apppackage.API {
	return &windowsAppPackageAPI{
		executable: windowsPowerShellPath(),
		executor:   osAppPackageCommandExecutor{},
	}
}

func (a *windowsAppPackageAPI) Query(ctx context.Context, identity apppackage.Identity) (*apppackage.Package, error) {
	if err := validateIdentity(identity); err != nil {
		return nil, err
	}
	resp, err := a.run(ctx, appPackageRequest{Operation: "query", Identity: identity})
	if err != nil {
		return nil, err
	}
	return resp.Result.Package, nil
}

func (a *windowsAppPackageAPI) Install(ctx context.Context, options apppackage.InstallOptions) (*apppackage.Package, error) {
	if err := validateIdentity(options.Expected); err != nil {
		return nil, err
	}
	path, err := validatePackagePath(options.PackagePath)
	if err != nil {
		return nil, err
	}
	deps, err := validateDependencies(options.Dependencies)
	if err != nil {
		return nil, err
	}
	resp, err := a.run(ctx, appPackageRequest{
		Operation:       "install",
		Identity:        options.Expected,
		PackagePath:     path,
		ExpectedVersion: options.ExpectedVersion,
		AllowDowngrade:  options.AllowDowngrade,
		Dependencies:    deps,
	})
	if err != nil {
		return nil, err
	}
	return resp.Result.Package, nil
}

func (a *windowsAppPackageAPI) Uninstall(ctx context.Context, identity apppackage.Identity, packageFullName string) error {
	if err := validateIdentity(identity); err != nil {
		return err
	}
	if strings.TrimSpace(packageFullName) == "" || strings.ContainsAny(packageFullName, "*?\r\n") {
		return &apppackage.Error{Code: apppackage.CodeProtocol, Message: "无效的包完整名称"}
	}
	_, err := a.run(ctx, appPackageRequest{Operation: "uninstall", Identity: identity, PackageFullName: packageFullName})
	return err
}

func (a *windowsAppPackageAPI) Activate(ctx context.Context, identity apppackage.Identity) error {
	if err := validateIdentity(identity); err != nil {
		return err
	}
	if strings.TrimSpace(identity.AppID) == "" || strings.ContainsAny(identity.AppID, "!\\/\r\n") {
		return &apppackage.Error{Code: apppackage.CodeProtocol, Message: "无效的应用标识"}
	}
	_, err := a.run(ctx, appPackageRequest{Operation: "activate", Identity: identity})
	return err
}

func (a *windowsAppPackageAPI) run(ctx context.Context, req appPackageRequest) (appPackageResponse, error) {
	if _, err := os.Stat(a.executable); err != nil {
		return appPackageResponse{}, &apppackage.Error{Code: apppackage.CodePowerShellAbsent, Message: "系统 Windows PowerShell 不可用", Cause: err}
	}
	req.ProtocolVersion = appPackageProtocolVersion
	req.RequestID = fmt.Sprintf("hanxi-%d", a.sequence.Add(1))
	payload, err := json.Marshal(req)
	if err != nil {
		return appPackageResponse{}, &apppackage.Error{Code: apppackage.CodeProtocol, Message: "生成包管理请求失败", Cause: err}
	}
	args := []string{"-NoLogo", "-NoProfile", "-NonInteractive", "-EncodedCommand", encodedAppPackageScript()}
	stdout, stderr, exitCode, runErr := a.executor.Execute(ctx, a.executable, args, payload)

	stdout = bytes.TrimPrefix(stdout, []byte{0xef, 0xbb, 0xbf})
	var resp appPackageResponse
	decodeErr := json.Unmarshal(stdout, &resp)
	if decodeErr == nil && resp.ProtocolVersion == appPackageProtocolVersion && resp.RequestID == req.RequestID {
		if !resp.OK {
			return appPackageResponse{}, mapAppPackageError(resp.Error)
		}
		return resp, nil
	}
	if ctx.Err() != nil {
		return appPackageResponse{}, &apppackage.Error{Code: apppackage.CodeCancelled, Message: "包操作已取消或超时", Cause: ctx.Err(), Retryable: true}
	}
	detail := strings.TrimSpace(string(stderr))
	if len(detail) > 800 {
		detail = detail[:800]
	}
	if decodeErr != nil {
		detail = fmt.Sprintf("exit=%d, json=%v, stderr=%s", exitCode, decodeErr, detail)
	} else {
		detail = fmt.Sprintf("exit=%d, protocol/request mismatch, stderr=%s", exitCode, detail)
	}
	return appPackageResponse{}, &apppackage.Error{Code: apppackage.CodeProtocol, Message: "Windows 包管理协议响应无效", Detail: detail, Cause: runErr}
}

func validateIdentity(identity apppackage.Identity) error {
	if strings.TrimSpace(identity.Name) == "" || strings.TrimSpace(identity.Family) == "" || strings.TrimSpace(identity.Publisher) == "" {
		return &apppackage.Error{Code: apppackage.CodeProtocol, Message: "应用包身份不完整"}
	}
	for _, value := range []string{identity.Name, identity.Family, identity.Publisher} {
		if strings.ContainsAny(value, "*?\r\n") {
			return &apppackage.Error{Code: apppackage.CodeProtocol, Message: "应用包身份包含非法字符"}
		}
	}
	return nil
}

func validatePackagePath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", &apppackage.Error{Code: apppackage.CodeProtocol, Message: "安装包路径无效", Cause: err}
	}
	info, err := os.Stat(abs)
	if err != nil || !info.Mode().IsRegular() {
		return "", &apppackage.Error{Code: apppackage.CodeProtocol, Message: "安装包文件不存在或不是普通文件", Cause: err}
	}
	switch strings.ToLower(filepath.Ext(abs)) {
	case ".msixbundle", ".appxbundle":
	default:
		return "", &apppackage.Error{Code: apppackage.CodeProtocol, Message: "仅允许安装 MSIXBundle/AppxBundle 包"}
	}
	return filepath.Clean(abs), nil
}

// validateDependencies 过滤空项并拒绝含通配/换行的依赖路径；接受本地绝对路径
// 与 https URL（Add-AppxPackage -DependencyPath 的两种合法输入）。
func validateDependencies(deps []string) ([]string, error) {
	if len(deps) == 0 {
		return nil, nil
	}
	out := make([]string, 0, len(deps))
	for _, dep := range deps {
		dep = strings.TrimSpace(dep)
		if dep == "" {
			continue
		}
		if strings.ContainsAny(dep, "*?\r\n") {
			return nil, &apppackage.Error{Code: apppackage.CodeProtocol, Message: "依赖包路径包含非法字符"}
		}
		if strings.HasPrefix(dep, "https://") || filepath.IsAbs(dep) {
			out = append(out, dep)
			continue
		}
		return nil, &apppackage.Error{Code: apppackage.CodeProtocol, Message: "依赖包必须是绝对路径或 https URL"}
	}
	return out, nil
}

func mapAppPackageError(src *appPackageScriptError) error {
	if src == nil {
		return &apppackage.Error{Code: apppackage.CodeProtocol, Message: "包管理返回了空错误"}
	}
	code := src.Code
	if code == "" {
		code = apppackage.CodeDeployment
	}
	message := src.Message
	if message == "" {
		message = "Windows 包操作失败"
	}
	return &apppackage.Error{Code: code, Message: message, Detail: src.Detail, HResult: src.HResult, Retryable: src.Retryable}
}

func windowsPowerShellPath() string {
	root := os.Getenv("SystemRoot")
	if root == "" {
		root = `C:\Windows`
	}
	return filepath.Join(root, "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
}

func encodedAppPackageScript() string {
	units := utf16.Encode([]rune(appPackagePowerShellScript))
	data := make([]byte, len(units)*2)
	for i, unit := range units {
		data[i*2] = byte(unit)
		data[i*2+1] = byte(unit >> 8)
	}
	return base64.StdEncoding.EncodeToString(data)
}

type limitedBuffer struct {
	buf       bytes.Buffer
	remaining int
	exceeded  bool
}

func newLimitedBuffer(limit int) *limitedBuffer { return &limitedBuffer{remaining: limit} }

func (b *limitedBuffer) Write(p []byte) (int, error) {
	original := len(p)
	if len(p) > b.remaining {
		p = p[:b.remaining]
		b.exceeded = true
	}
	if len(p) > 0 {
		_, _ = b.buf.Write(p)
		b.remaining -= len(p)
	}
	return original, nil
}

func (b *limitedBuffer) Bytes() []byte { return b.buf.Bytes() }

type osAppPackageCommandExecutor struct{}

func (osAppPackageCommandExecutor) Execute(ctx context.Context, executable string, args []string, stdin []byte) ([]byte, []byte, int, error) {
	cmd := exec.CommandContext(ctx, executable, args...)
	cmd.Stdin = bytes.NewReader(stdin)
	stdout := newLimitedBuffer(appPackageOutputLimit)
	stderr := newLimitedBuffer(appPackageOutputLimit)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: appPackageCreateNoWindow}
	err := cmd.Run()
	exitCode := 0
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}
	if stdout.exceeded || stderr.exceeded {
		return stdout.Bytes(), stderr.Bytes(), exitCode, fmt.Errorf("package command output exceeded %d bytes", appPackageOutputLimit)
	}
	return stdout.Bytes(), stderr.Bytes(), exitCode, err
}
