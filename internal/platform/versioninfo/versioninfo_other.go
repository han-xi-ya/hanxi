//go:build !windows

package versioninfo

import "errors"

// StringValue 非 Windows 平台无版本资源 API（Hanxi 实际仅在 Windows 运行）。
func StringValue(string, string) (string, error) {
	return "", errors.New("PE 版本探测仅支持 Windows")
}

// FileVersion 非 Windows 平台恒返回错误（无 PE 版本资源可查）。
func FileVersion(path string) (string, error) {
	return StringValue(path, "FileVersion")
}

// ProductName 非 Windows 平台恒返回错误（无 PE 版本资源可查）。
func ProductName(path string) (string, error) {
	return StringValue(path, "ProductName")
}
