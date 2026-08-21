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

func NewAppError(code, message string) *AppError {
	return &AppError{Code: code, Message: message}
}

func (e *AppError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %v", e.Code, e.Cause)
	}
	return e.Code
}

func (e *AppError) Unwrap() error { return e.Cause }

// 通用错误码（各模块的详细错误码随模块实现补充）。
const (
	ErrInternal   = "SYS_INTERNAL"
	ErrNotImpl    = "SYS_NOT_IMPLEMENTED"
	ErrValidation = "SYS_VALIDATION"
)