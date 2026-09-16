package wifi

// WifiService Wails 绑定服务：查看本机已保存的 Wi-Fi 密码
type WifiService struct{}

// NewWifiService 创建无状态查询服务（每次调用即时执行 netsh，不缓存明文密码）。
func NewWifiService() *WifiService {
	return &WifiService{}
}

// ListProfiles 直接获取全部 WiFi 名称与明文密码
func (s *WifiService) ListProfiles() []Profile {
	profiles, err := GetAllWiFiPasswords()
	if err != nil {
		return []Profile{}
	}
	return profiles
}
