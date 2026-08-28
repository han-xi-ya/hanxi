//go:build !windows

package versioninfo

import "errors"

// StringValue 非 Windows 平台无版本资源 API（HubKit 实际仅在 Windows 运行）。
func StringValue(string, string) (string, error) {
	return "", errors.New("PE 版本探测仅支持 Windows")
}

func FileVersion(path string) (string, error) {
	return StringValue(path, "FileVersion")
}

func ProductName(path string) (string, error) {
	return StringValue(path, "ProductName")
}
