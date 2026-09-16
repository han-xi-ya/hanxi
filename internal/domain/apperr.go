// Package domain 保存纯领域模型与规则，不依赖任何平台或 UI 包。
package domain

import "fmt"

// AppError 是所有绑定方法返回的统一错误结构。
// 前端按 Code 渲染：Message 面向用户，Detail 可复制，Action 为建议操作。
type AppError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Detail    string `json:"detail"`
	Action    string `json:"action,omitempty"`
	Retryable bool   `json:"retryable"`
	Cause     error  `json:"-"`
}

// NewAppError 构造仅含错误码与用户文案的 AppError；Detail/Cause 通常由 WithDetail 等链式补充。
func NewAppError(code, message string) *AppError {
	return &AppError{Code: code, Message: message}
}

// Error 实现 error 接口：有 Cause 时输出 "code: cause"（供服务端日志），无 Cause 时仅输出 code。
// 注意面向用户的文案在 Message 字段，不走此输出。
func (e *AppError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %v", e.Code, e.Cause)
	}
	return e.Code
}

// Unwrap 暴露底层错误，支持 errors.Is/As 穿透判定。
func (e *AppError) Unwrap() error { return e.Cause }

// 通用错误码（各模块的详细错误码随模块实现补充）。
const (
	ErrInternal   = "SYS_INTERNAL"
	ErrNotImpl    = "SYS_NOT_IMPLEMENTED"
	ErrValidation = "SYS_VALIDATION"
)
