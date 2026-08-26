//go:build !windows

package version

import "errors"

// exeFileVersion 非 Windows 平台无版本资源 API（HubKit 实际仅在 Windows 运行）。
func exeFileVersion(string) (string, error) {
	return "", errors.New("PE 版本探测仅支持 Windows")
}