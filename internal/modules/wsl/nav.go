package wsl

// 固定官方地址导航：所有外呼地址来自本包常量与白名单校验后的版本号，
// 前端传进来的字符串永远拼不进 URL（assetDownloadURL 双重白名单）。

import (
	"fmt"
	"regexp"
	"strings"

	"hanxi/internal/modules/wsl/releases"
)

// ---- 固定官方地址导航 ----

var wslTagRe = regexp.MustCompile(`^\d+\.\d+(\.\d+){1,2}$`)

// wslAssetNameRe 形如 wsl.2.9.10.0.x64.msi / wsl.2.7.13.0.arm64.msi。
var wslAssetNameRe = regexp.MustCompile(`^wsl\.\d+\.\d+\.\d+\.\d+\.(x64|arm64)\.msi$`)

// OpenReleasesPage 打开 microsoft/WSL 官方发布页。
func (s *WslService) OpenReleasesPage() error {
	return s.openURL("WSL 官方发布页", releases.ReleasesPageURL())
}

// OpenReleaseTag 打开指定版本 tag 的发行说明页（tag 必须为纯数字点分版本）。
func (s *WslService) OpenReleaseTag(tag string) error {
	tag = strings.TrimSpace(strings.TrimPrefix(tag, "v"))
	if !wslTagRe.MatchString(tag) {
		return fmt.Errorf("版本号 %q 格式不合法", tag)
	}
	return s.openURL("版本 "+tag, "https://github.com/microsoft/WSL/releases/tag/"+tag)
}

// assetDownloadURL 拼装并校验官方 MSI 直链（tag 与文件名双重白名单）。
func assetDownloadURL(tag, name string) (string, error) {
	tag = strings.TrimSpace(strings.TrimPrefix(tag, "v"))
	name = strings.TrimSpace(name)
	if !wslTagRe.MatchString(tag) || !wslAssetNameRe.MatchString(name) {
		return "", fmt.Errorf("下载参数不合法: %s / %s", tag, name)
	}
	return "https://github.com/microsoft/WSL/releases/download/" + tag + "/" + name, nil
}

// OpenOfficialDocs 打开微软官方 WSL 安装文档。
func (s *WslService) OpenOfficialDocs() error {
	return s.openURL("WSL 官方文档", "https://learn.microsoft.com/windows/wsl/install")
}

// OpenPowerSettings 打开系统「电源」设置页：功能启用/关闭后引导用户自行重启
// （不代点重启——重启是打断用户工作的高危动作）。
func (s *WslService) OpenPowerSettings() error {
	return s.openURL("系统电源设置", "ms-settings:power")
}

func (s *WslService) openURL(name, rawURL string) error {
	if s.opener == nil {
		return fmt.Errorf("打开 %s 失败: 平台能力不可用", name)
	}
	if err := s.opener.OpenURL(rawURL); err != nil {
		return fmt.Errorf("打开 %s 失败: %w", name, err)
	}
	return nil
}
