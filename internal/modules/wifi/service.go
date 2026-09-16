package wifi

import "log/slog"

// WifiService Wails 绑定服务：查看本机已保存的 Wi-Fi 密码
type WifiService struct{}

// NewWifiService 创建无状态查询服务（每次调用即时执行 netsh，不缓存明文密码）。
func NewWifiService() *WifiService {
	return &WifiService{}
}

// ListProfiles 直接获取全部 WiFi 名称与明文密码。
// 绑定签名只回传列表（无 error 通道），故枚举失败与"本机确无已保存配置"在返回值上
// 都表现为空表——前端无从分辨；这里把失败原因落到日志（含 netsh 报错原文），
// 排障时看 slog 即可判定是 WLAN 服务/权限问题还是真的一个配置都没有。
func (s *WifiService) ListProfiles() []Profile {
	profiles, err := GetAllWiFiPasswords()
	if err != nil {
		slog.Warn("枚举已保存的 Wi-Fi 配置失败，前端将看到空列表（并非无配置）", "err", err)
		return []Profile{}
	}
	if len(profiles) == 0 {
		slog.Debug("本机无已保存的 Wi-Fi 配置")
	}
	return profiles
}
