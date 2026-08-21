// Package domain 存放与运行细节无关的纯领域模型。
// frpc 项目与代理规则既用于 TOML 生成（docgen），也用于实例进程管理（instance）。
package domain

// Project 一个 frpc 项目（一份配置 = 一个实例）。
// 存储使用 JSON（config 中的值为用户意图），运行时刻生成 TOML 供 frpc.exe 消费。
type Project struct {
	ID        string       `json:"id"`        // 唯一 ID（文件名/配置名基础）
	Name      string       `json:"name"`      // 显示名称
	Server    ServerConfig `json:"server"`    // frp 服务端连接配置
	Proxies   []ProxyRule  `json:"proxies"`   // 隧道规则列表
	Version   string       `json:"version"`   // 绑定的 frpc 版本（如 v0.61.1；空表示未绑定）
	CreatedAt string       `json:"createdAt"` // 创建时间 yyyy-MM-dd HH:mm:ss
	UpdatedAt string       `json:"updatedAt"` // 最后修改时间 yyyy-MM-dd HH:mm:ss
}

// ServerConfig frp 服务端连接配置
type ServerConfig struct {
	ServerAddr     string `json:"serverAddr"`     // 服务器地址（IP 或域名）
	ServerPort     int    `json:"serverPort"`     // 服务器端口（默认 7000）
	Token          string `json:"token"`          // 鉴权令牌
	TLSEnable      bool   `json:"tlsEnable"`      // 启用 TLS 加密传输
	UseEncryption  bool   `json:"useEncryption"`  // 传输层加密
	UseCompression bool   `json:"useCompression"` // 传输层压缩
	LogLevel       string `json:"logLevel"`       // 日志级别 trace/debug/info/warn/error
}

// ProxyRule 一条隧道代理规则。
// 依据 type 不同，字段使用规则：
//
//	tcp/udp:  LocalIP/localPort + RemotePort
//	http/https: LocalIP/localPort + CustomDomains 或 Subdomain
//	stcp/xtcp:  LocalIP/localPort + SecretKey（对端再配置 serverName 指向本规则名）
type ProxyRule struct {
	Name             string   `json:"name"`             // 规则唯一名称（frp 内唯一标识）
	Type             string   `json:"type"`             // tcp | udp | http | https | stcp | xtcp
	LocalIP          string   `json:"localIp"`          // 本地服务地址（默认 127.0.0.1）
	LocalPort        int      `json:"localPort"`        // 本地服务端口
	RemotePort       int      `json:"remotePort"`       // 服务端公网端口（tcp/udp 必填）
	CustomDomains    []string `json:"customDomains"`    // 自定义域名（http/https）
	Subdomain        string   `json:"subdomain"`        // 二级域名（http/https，依托服务端泛解析）
	SecretKey        string   `json:"secretKey"`        // stcp/xtcp 共享密钥
	EncryptTransport bool     `json:"encryptTransport"` // 规则级传输加密（覆盖全局）
}
