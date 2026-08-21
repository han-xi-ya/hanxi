// Package docgen 负责 frp 配置 TOML 的生成与解析。
//
// 生成目标：frp v0.53+ 标准 TOML 格式（顶层 serverAddr/serverPort +
// [auth] / [log] / [transport] 子段 + [[proxies]] 数组，camelCase 字段）。
// 注意：v0.52 时代的 [core] 包裹段在 v0.53 已被移除，本项目不生成也不接受该格式。
//
// 解析兼容：v0.53+ 新格式与 v0.x 旧格式（[common] + snake_case）均可读回。
package docgen

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/BurntSushi/toml"

	"hubkit/internal/domain"
)

// ---------------------------
// 生成/解析共用结构（v0.53+ 格式）
// ---------------------------

type tomlAuth struct {
	Method string `toml:"method"` // token | oidc（默认 token）
	Token  string `toml:"token"`
}

type tomlLog struct {
	Level string `toml:"level"`
}

type tomlTLS struct {
	Enable *bool `toml:"enable"`
}

type tomlServerTransport struct {
	Protocol string  `toml:"protocol"` // tcp/kcp/quic/websocket/wss
	TLS      tomlTLS `toml:"tls"`
}

type tomlProxy struct {
	Name          string             `toml:"name"`
	Type          string             `toml:"type"`
	LocalIP       string             `toml:"localIP"`
	LocalPort     int                `toml:"localPort"`
	RemotePort    *int               `toml:"remotePort,omitempty"` // 仅 tcp/udp 输出；其他类型省略（frp 拒绝 stcp/xtcp 携带 remotePort）
	CustomDomains []string           `toml:"customDomains,omitempty"`
	Subdomain     string             `toml:"subdomain,omitempty"`
	SecretKey     string             `toml:"secretKey,omitempty"`
	Transport     tomlProxyTransport `toml:"transport"`
}

type tomlProxyTransport struct {
	UseEncryption  bool `toml:"useEncryption"`
	UseCompression bool `toml:"useCompression"`
}

// tomlFile v0.53+ 文件骨架（顶层字段 + 各子段）
type tomlFile struct {
	ServerAddr string          `toml:"serverAddr"`
	ServerPort int             `toml:"serverPort"`
	Auth       tomlAuth        `toml:"auth"`
	Log        tomlLog         `toml:"log"`
	Transport  tomlServerTransport `toml:"transport"`
	Proxies    []tomlProxy     `toml:"proxies"`
}

// ---------------------------
// 生成
// ---------------------------

// Generate 将项目领域模型序列化为 frp v0.53+ TOML 配置内容。
//
// v0.53+ 移除了客户端级加密/压缩开关（仅存在于 proxy 级 transport 子段），
// 因此项目级 Server.UseEncryption/UseCompression 将传播到每一条代理规则，
// 保持"项目级开关"的用户语义。
func Generate(p *domain.Project) (string, error) {
	server := p.Server
	if strings.TrimSpace(server.ServerAddr) == "" {
		return "", fmt.Errorf("服务端地址 serverAddr 不能为空")
	}
	if server.ServerPort <= 0 || server.ServerPort > 65535 {
		return "", fmt.Errorf("服务端端口 serverPort 无效: %d", server.ServerPort)
	}

	logLevel := server.LogLevel
	if logLevel == "" {
		logLevel = "info"
	}
	tlsEnable := server.TLSEnable

	seen := make(map[string]bool, len(p.Proxies))
	root := tomlFile{
		ServerAddr: strings.TrimSpace(server.ServerAddr),
		ServerPort: server.ServerPort,
		Auth: tomlAuth{
			Method: "token",
			Token:  server.Token,
		},
		Log: tomlLog{Level: logLevel},
		Transport: tomlServerTransport{
			Protocol: "tcp",
			TLS:      tomlTLS{Enable: &tlsEnable},
		},
		Proxies: make([]tomlProxy, 0, len(p.Proxies)),
	}

	for i, pr := range p.Proxies {
		name := strings.TrimSpace(pr.Name)
		if name == "" {
			return "", fmt.Errorf("第 %d 条代理规则名称不能为空", i+1)
		}
		if seen[name] {
			return "", fmt.Errorf("代理规则名称重复: %s", name)
		}
		seen[name] = true

		switch pr.Type {
		case "tcp", "udp", "http", "https", "stcp", "xtcp":
		default:
			return "", fmt.Errorf("代理规则 %s 类型不支持: %s", name, pr.Type)
		}
		if pr.LocalPort <= 0 || pr.LocalPort > 65535 {
			return "", fmt.Errorf("规则 %s 的本地端口无效", name)
		}
		if (pr.Type == "tcp" || pr.Type == "udp") && (pr.RemotePort <= 0 || pr.RemotePort > 65535) {
			return "", fmt.Errorf("规则 %s 的远程端口无效", name)
		}

		proxy := tomlProxy{
			Name:          name,
			Type:          pr.Type,
			LocalIP:       pruneLocalIP(pr.LocalIP),
			LocalPort:     pr.LocalPort,
			CustomDomains: pr.CustomDomains,
			Subdomain:     pr.Subdomain,
			SecretKey:     pr.SecretKey,
			Transport: tomlProxyTransport{
				UseEncryption:  pr.EncryptTransport || server.UseEncryption,
				UseCompression: server.UseCompression,
			},
		}
		if pr.Type == "tcp" || pr.Type == "udp" {
			port := pr.RemotePort
			proxy.RemotePort = &port
		}
		root.Proxies = append(root.Proxies, proxy)
	}

	var buf bytes.Buffer
	enc := toml.NewEncoder(&buf)
	enc.Indent = "  "
	if err := enc.Encode(root); err != nil {
		return "", fmt.Errorf("toml encode: %w", err)
	}
	return buf.String(), nil
}

// derefInt 解引用可选 int（TOML omitempty 载体）；nil 返回 0。
func derefInt(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

func pruneLocalIP(ip string) string {
	if strings.TrimSpace(ip) == "" {
		return "127.0.0.1"
	}
	return ip
}

// ---------------------------
// 解析
// ---------------------------

// tomlFileLegacy frp v0.x 的 [common] + snake_case 骨架
type tomlFileLegacy struct {
	Common struct {
		ServerAddr     string `toml:"server_addr"`
		ServerPort     int    `toml:"server_port"`
		Token          string `toml:"token"`
		TLSEnable      bool   `toml:"tls_enable"`
		UseEncryption  bool   `toml:"use_encryption"`
		UseCompression bool   `toml:"use_compression"`
		LogLevel       string `toml:"log_level"`
	} `toml:"common"`
	Proxies []struct {
		Name          string   `toml:"name"`
		Type          string   `toml:"type"`
		LocalIP       string   `toml:"local_ip"`
		LocalPort     int      `toml:"local_port"`
		RemotePort    int      `toml:"remote_port"`
		Subdomain     string   `toml:"subdomain"`
		SecretKey     string   `toml:"secret_key"`
		CustomDomains []string `toml:"custom_domains"`
	} `toml:"proxies"`
}

// Parse 读取 TOML 配置内容回领域模型。
// 支持 v0.53+ 新格式（顶层 + [auth]/[log]/[transport] + [[proxies]]）与
// v0.x 旧格式（[common] + snake_case）。
func Parse(content string) (domain.Project, error) {
	// 1. 新格式 v0.53+
	var tf tomlFile
	if err := toml.Unmarshal([]byte(content), &tf); err != nil {
		return domain.Project{}, fmt.Errorf("toml 解析失败: %w", err)
	}
	if tf.ServerAddr != "" {
		return projectFromNew(tf), nil
	}

	// 2. 旧格式 v0.x
	var tfl tomlFileLegacy
	if err := toml.Unmarshal([]byte(content), &tfl); err != nil {
		return domain.Project{}, fmt.Errorf("toml 解析失败: %w", err)
	}
	c := tfl.Common
	if c.ServerAddr != "" {
		proj := domain.Project{
			Server: domain.ServerConfig{
				ServerAddr:     c.ServerAddr,
				ServerPort:     c.ServerPort,
				Token:          c.Token,
				TLSEnable:      c.TLSEnable,
				UseEncryption:  c.UseEncryption,
				UseCompression: c.UseCompression,
				LogLevel:       c.LogLevel,
			},
			Proxies: make([]domain.ProxyRule, 0, len(tfl.Proxies)),
		}
		for _, p := range tfl.Proxies {
			proj.Proxies = append(proj.Proxies, domain.ProxyRule{
				Name:          p.Name,
				Type:          p.Type,
				LocalIP:       pruneLocalIP(p.LocalIP),
				LocalPort:     p.LocalPort,
				RemotePort:    p.RemotePort,
				CustomDomains: p.CustomDomains,
				Subdomain:     p.Subdomain,
				SecretKey:     p.SecretKey,
			})
		}
		return proj, nil
	}

	return domain.Project{}, fmt.Errorf("未识别到有效的 frp 配置（缺少 serverAddr / server_addr）")
}

// projectFromNew 从 v0.53+ 结构提取领域模型。
// 项目级加密/压缩开关为聚合值：仅当全部代理一致开启时才回填为 true。
func projectFromNew(tf tomlFile) domain.Project {
	tlsOn := true
	if tf.Transport.TLS.Enable != nil {
		tlsOn = *tf.Transport.TLS.Enable
	}

	proj := domain.Project{
		Server: domain.ServerConfig{
			ServerAddr: tf.ServerAddr,
			ServerPort: tf.ServerPort,
			Token:      tf.Auth.Token,
			TLSEnable:  tlsOn,
			LogLevel:   tf.Log.Level,
		},
		Proxies: make([]domain.ProxyRule, 0, len(tf.Proxies)),
	}
	allEnc, anyEnc := true, false
	allCmp, anyCmp := true, false
	for _, p := range tf.Proxies {
		allEnc = allEnc && p.Transport.UseEncryption
		anyEnc = anyEnc || p.Transport.UseEncryption
		allCmp = allCmp && p.Transport.UseCompression
		anyCmp = anyCmp || p.Transport.UseCompression
		proj.Proxies = append(proj.Proxies, domain.ProxyRule{
			Name:             p.Name,
			Type:             p.Type,
			LocalIP:          pruneLocalIP(p.LocalIP),
			LocalPort:        p.LocalPort,
			RemotePort:       derefInt(p.RemotePort),
			CustomDomains:    p.CustomDomains,
			Subdomain:        p.Subdomain,
			SecretKey:        p.SecretKey,
			EncryptTransport: p.Transport.UseEncryption,
		})
	}
	if len(tf.Proxies) > 0 && allEnc && anyEnc {
		proj.Server.UseEncryption = true
	}
	if len(tf.Proxies) > 0 && allCmp && anyCmp {
		proj.Server.UseCompression = true
	}
	return proj
}