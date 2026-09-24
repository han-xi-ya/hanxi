package netx

import (
	"golang.org/x/sys/windows/registry"
)

// systemProxyURL 读 WinINET 系统代理（浏览器同款设置）：
// ProxyEnable=1 且 ProxyServer 非空时返回带 scheme 的代理地址；
// 纯 PAC（AutoConfigURL）场景不做 JS 解析、如实返回空（回落直连）。
func systemProxyURL() string {
	k, err := registry.OpenKey(registry.CURRENT_USER,
		`Software\Microsoft\Windows\CurrentVersion\Internet Settings`, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer k.Close()
	if enabled, _, err := k.GetIntegerValue("ProxyEnable"); err != nil || enabled != 1 {
		return ""
	}
	raw, _, err := k.GetStringValue("ProxyServer")
	if err != nil {
		return ""
	}
	return parseProxyServer(raw)
}
