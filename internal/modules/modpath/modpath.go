// Package modpath 解析托管工具的用户数据目录（Windows 环境变量派生路径）。
//
// 只负责"环境变量取根 + 子目录拼接 + 缺失时中文报错"这一步样板；
// 上游标识符语义（该用哪个子目录名、依据哪份上游实证）留在各模块调用点，
// 以模块侧常量 + 注释承载。
package modpath

import (
	"fmt"
	"os"
	"path/filepath"
)

// UserConfigDir 返回 %APPDATA% 下指定子目录（当前用户 Roaming 数据目录）。
// subdir 允许以 "/" 书写多级（如 "com.follow/clash"），内部按本地分隔符归一。
func UserConfigDir(subdir string) (string, error) {
	appData := os.Getenv("APPDATA")
	if appData == "" {
		return "", fmt.Errorf("无法定位 APPDATA 目录")
	}
	return filepath.Join(appData, filepath.FromSlash(subdir)), nil
}
