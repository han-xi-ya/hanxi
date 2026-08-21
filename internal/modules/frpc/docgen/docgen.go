// Package docgen 负责 frp 配置 TOML 的生成与解析。
//
// 生成目标：frp v1.x 标准新格式（[core] + [[proxies]]，camelCase 字段）。
// 解析兼容：v1.x 新格式与 v0.x 旧格式（[common] + snake_case）均可读回。
package docgen

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/BurntSushi/toml"

	"hubkit/internal/domain"
)

// ---------------------------
// 生成用结构（v1.x 格式）
// ---------------------------

type tomlCore struct {
	ServerAddr string        `toml:"serverAddr"`
	ServerPort int           `toml:"serverPort"`
	Token      string        `toml:"token"`
	TLSEnable  bool          `toml:"tlsEnable"`
	Transport  tomlTransport `toml:"transport"`
	Log        tomlLog       `toml:"log"`
}

type tomlTransport struct {
	UseEncryption  bool `toml:"useEncryption"`
	UseCompression bool `toml:"useCompression"`
}

type tomlLog struct {
	Level string `toml:"level"`
}

type tomlProxy struct {
	Name          string             `toml:"name"`
	Type          string             `toml:"type"`
	LocalIP       string             `toml:"localIP"`
	LocalPort     int                `toml:"localPort"`
	RemotePort    int                `toml:"remotePort,omitempty"`
	CustomDomains []string           `toml:"customDomains,omitempty"`
	Subdomain     string             `toml:"subdomain,omitempty"`
	SecretKey     string             `toml:"secretKey,omitempty"`
	Transport     tomlProxyTransport `toml:"transport,omitempty"`
}

type tomlProxyTransport struct {
	UseEncryption bool `toml:"useEncryption"`
}

type tomlRoot struct {
	Core    tomlCore    `toml:"core"`
	Proxies []tomlProxy `toml:"proxies"`
}

// Generate 将项目领域模型序列化为 frp v1.x TOML 配置内容
func Generate(p *domain.Project) (string, error) {
	if strings.TrimSpace(p.Server.ServerAddr) == "" {
		return "", fmt.Errorf("服务端地址 serverAddr 不能为空")
	}
	if p.Server.ServerPort <= 0 || p.Server.ServerPort > 65535 {
		return "", fmt.Errorf("服务端端口 serverPort 无效: %d", p.Server.ServerPort)
	}

	logLevel := p.Server.LogLevel
	if logLevel == "" {
		logLevel = "info"
	}

	root := tomlRoot{
		Core: tomlCore{
			ServerAddr: p.Server.ServerAddr,
			ServerPort: p.Server.ServerPort,
			Token:      p.Server.Token,
			TLSEnable:  p.Server.TLSEnable,
			Transport: tomlTransport{
				UseEncryption:  p.Server.UseEncryption,
				UseCompression: p.Server.UseCompression,
			},
			Log: tomlLog{Level: logLevel},
		},
		Proxies: make([]tomlProxy, 0, len(p.Proxies)),
	}

	seen := make(map[string]bool, len(p.Proxies))
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

		root.Proxies = append(root.Proxies, tomlProxy{
			Name:          name,
			Type:          pr.Type,
			LocalIP:       pruneLocalIP(pr.LocalIP),
			LocalPort:     pr.LocalPort,
			RemotePort:    pr.RemotePort,
			CustomDomains: pr.CustomDomains,
			Subdomain:     pr.Subdomain,
			SecretKey:     pr.SecretKey,
			Transport: tomlProxyTransport{
				UseEncryption: pr.EncryptTransport,
			},
		})
	}

	var buf bytes.Buffer
	enc := toml.NewEncoder(&buf)
	if err := enc.Encode(root); err != nil {
		return "", fmt.Errorf("toml encode: %w", err)
	}
	return buf.String(), nil
}

func pruneLocalIP(ip string) string {
	if strings.TrimSpace(ip) == "" {
		return "127.0.0.1"
	}
	return ip
}

// ---------------------------
// 解析结构（兼容 v1.x 与 v0.x）
// ---------------------------

// tomlCoreLegacy frp v0.x 的 [common] 段
type tomlCoreLegacy struct {
	ServerAddr     string `toml:"server_addr"`
	ServerPort     int    `toml:"server_port"`
	Token          string `toml:"token"`
	TLSEnable      bool   `toml:"tls_enable"`
	UseEncryption  bool   `toml:"use_encryption"`
	UseCompression bool   `toml:"use_compression"`
	LogLevel       string `toml:"log_level"`
}

type tomlProxyLegacy struct {
	Name          string   `toml:"name"`
	Type          string   `toml:"type"`
	LocalIP       string   `toml:"local_ip"`
	LocalPort     int      `toml:"local_port"`
	RemotePort    int      `toml:"remote_port"`
	Subdomain     string   `toml:"subdomain"`
	SecretKey     string   `toml:"secret_key"`
	CustomDomains []string `toml:"custom_domains"`
}

// tomlFile 新格式解析骨架（v1.x：[core] + camelCase）
type tomlFile struct {
	Core    tomlCore    `toml:"core"`
	Proxies []tomlProxy `toml:"proxies"`
}

// tomlFileLegacy 旧格式解析骨架（v0.x：[common] + snake_case）
type tomlFileLegacy struct {
	Common  tomlCoreLegacy    `toml:"common"`
	Proxies []tomlProxyLegacy `toml:"proxies"`
}

// Parse 读取 TOML 配置内容回领域模型（v1.x 新格式与 v0.x 旧格式均可）
func Parse(content string) (domain.Project, error) {
	// 1. 先尝试新格式 v1.x
	var tf tomlFile
	if err := toml.Unmarshal([]byte(content), &tf); err != nil {
		return domain.Project{}, fmt.Errorf("toml 解析失败: %w", err)
	}
	if tf.Core.ServerAddr != "" {
		return projectFromProxies(serverFromNew(tf.Core), tf.Proxies), nil
	}

	// 2. 再尝试旧格式 v0.x
	var tfl tomlFileLegacy
	if err := toml.Unmarshal([]byte(content), &tfl); err != nil {
		return domain.Project{}, fmt.Errorf("toml 解析失败: %w", err)
	}
	if tfl.Common.ServerAddr != "" {
		proj := domain.Project{
			Server:  serverFromLegacy(tfl.Common),
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

	return domain.Project{}, fmt.Errorf("未识别到 [core] 或 [common] 配置段")
}

func projectFromProxies(server domain.ServerConfig, newProxies []tomlProxy) domain.Project {
	proj := domain.Project{
		Server:  server,
		Proxies: make([]domain.ProxyRule, 0, len(newProxies)),
	}
	for _, p := range newProxies {
		proj.Proxies = append(proj.Proxies, domain.ProxyRule{
			Name:             p.Name,
			Type:             p.Type,
			LocalIP:          pruneLocalIP(p.LocalIP),
			LocalPort:        p.LocalPort,
			RemotePort:       p.RemotePort,
			CustomDomains:    p.CustomDomains,
			Subdomain:        p.Subdomain,
			SecretKey:        p.SecretKey,
			EncryptTransport: p.Transport.UseEncryption,
		})
	}
	return proj
}

// serverFromNew 从 v1.x 结构提取 ServerConfig
func serverFromNew(c tomlCore) domain.ServerConfig {
	return domain.ServerConfig{
		ServerAddr:     c.ServerAddr,
		ServerPort:     c.ServerPort,
		Token:          c.Token,
		TLSEnable:      c.TLSEnable,
		UseEncryption:  c.Transport.UseEncryption,
		UseCompression: c.Transport.UseCompression,
		LogLevel:       c.Log.Level,
	}
}

// serverFromLegacy 从 v0.x [common] 结构提取 ServerConfig
func serverFromLegacy(c tomlCoreLegacy) domain.ServerConfig {
	return domain.ServerConfig{
		ServerAddr:     c.ServerAddr,
		ServerPort:     c.ServerPort,
		Token:          c.Token,
		TLSEnable:      c.TLSEnable,
		UseEncryption:  c.UseEncryption,
		UseCompression: c.UseCompression,
		LogLevel:       c.LogLevel,
	}
}
