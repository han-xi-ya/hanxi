package wifi

// Profile 表示一个已保存的 WiFi 连接及其密码
type Profile struct {
	SSID     string `json:"ssid"`     // WiFi 名称
	Password string `json:"password"` // 密码明文
}
