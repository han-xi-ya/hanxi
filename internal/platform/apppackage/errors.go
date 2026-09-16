package apppackage

import "fmt"

// 稳定的机器可读错误码：不随 PowerShell 输出语言漂移，前端/模块层据此分支处理。
const (
	CodeNotSupported     = "APP_PACKAGE_NOT_SUPPORTED"
	CodePowerShellAbsent = "APP_PACKAGE_POWERSHELL_NOT_FOUND"
	CodeProtocol         = "APP_PACKAGE_PROTOCOL_ERROR"
	CodeIdentityMismatch = "APP_PACKAGE_IDENTITY_MISMATCH"
	CodeNotInstalled     = "APP_PACKAGE_NOT_INSTALLED"
	CodeInUse            = "APP_PACKAGE_IN_USE"
	CodeDependency       = "APP_PACKAGE_DEPENDENCY_MISSING"
	CodeSignature        = "APP_PACKAGE_SIGNATURE_INVALID"
	CodeAccessDenied     = "APP_PACKAGE_ACCESS_DENIED"
	CodeDeployment       = "APP_PACKAGE_DEPLOYMENT_FAILED"
	CodeActivation       = "APP_PACKAGE_ACTIVATION_FAILED"
	CodeCancelled        = "APP_PACKAGE_CANCELLED"
)

// Error is a stable package-management error independent of PowerShell locale.
type Error struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Detail    string `json:"detail,omitempty"`
	HResult   string `json:"hresult,omitempty"`
	Retryable bool   `json:"retryable"`
	Cause     error  `json:"-"`
}

// Error 实现 error 接口：优先输出 "message: detail"（脚本侧结构化明细），
// 无 detail 时退化为 "message: cause"（本地包装错误），两者皆无则仅输出 message。
func (e *Error) Error() string {
	if e.Detail != "" {
		return fmt.Sprintf("%s: %s", e.Message, e.Detail)
	}
	if e.Cause != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Cause)
	}
	return e.Message
}

// Unwrap 暴露底层 Go 错误，支持 errors.Is/As 穿透（如判定 context.Canceled）。
func (e *Error) Unwrap() error { return e.Cause }
