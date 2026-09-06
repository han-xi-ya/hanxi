//go:build windows

package version

import (
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"

	"hanxi/internal/platform/versioninfo"
)

// userUninstallKeys VS Code User Installer（x64/ARM64）的 Inno 卸载注册表键。
// AppId GUID 取自发行版 product.json（win32x64UserAppId/win32arm64UserAppId，
// 剥离 iss 预处理的 "{{" 转义后为本值），实测 HKCU 命中（本机 1.136.1）。
var userUninstallKeys = []string{
	`Software\Microsoft\Windows\CurrentVersion\Uninstall\{771FD6B0-FA20-440A-A002-3B3BAC16DC50}_is1`, // x64 User
	`Software\Microsoft\Windows\CurrentVersion\Uninstall\{D9E514E7-1A56-452D-9337-2990C0DC4310}_is1`, // ARM64 User
}

// DetectInstalled 经注册表探测本机安装版 VS Code。
// 安装目录一律以 InstallLocation 为准并校验 Code.exe 存在——实测用户可在
// 安装向导自选目录（本机即在 E:\Program Files），硬编码 %LOCALAPPDATA% 必漏。
func DetectInstalled() InstalledInfo {
	for _, keyPath := range userUninstallKeys {
		key, err := registry.OpenKey(registry.CURRENT_USER, keyPath, registry.READ)
		if err != nil {
			continue
		}
		loc, _, locErr := key.GetStringValue("InstallLocation")
		dispVer, _, _ := key.GetStringValue("DisplayVersion")
		key.Close()
		if locErr != nil {
			continue
		}
		dir := filepath.Clean(strings.Trim(strings.TrimSpace(loc), `"`))
		exe := filepath.Join(dir, exeName)
		if fi, statErr := os.Stat(exe); statErr != nil || fi.IsDir() || fi.Size() == 0 {
			continue // 注册表残留指向已消失目录：视为未安装
		}
		version := strings.TrimSpace(dispVer)
		if version == "" {
			if fv, err := versioninfo.FileVersion(exe); err == nil {
				version = fv
			}
		}
		return InstalledInfo{Installed: true, Version: version, Dir: dir, ExePath: exe}
	}
	return InstalledInfo{}
}
