package docgen

import (
	"strings"
	"testing"

	"hubkit/internal/domain"
)

func sampleProject() *domain.Project {
	return &domain.Project{
		Name: "联调环境",
		Server: domain.ServerConfig{
			ServerAddr:     "frp.example.com",
			ServerPort:     7000,
			Token:          "secret-token",
			TLSEnable:      true,
			UseEncryption:  true,
			UseCompression: false,
			LogLevel:       "info",
		},
		Proxies: []domain.ProxyRule{
			{Name: "ssh", Type: "tcp", LocalIP: "127.0.0.1", LocalPort: 22, RemotePort: 6000},
			{Name: "web", Type: "http", LocalIP: "192.168.1.5", LocalPort: 8080, CustomDomains: []string{"dev.example.com"}},
			{Name: "secret-sock", Type: "stcp", LocalIP: "127.0.0.1", LocalPort: 9000, SecretKey: "sk-123"},
		},
	}
}

func TestGenerateRoundTrip(t *testing.T) {
	proj := sampleProject()
	tomlText, err := Generate(proj)
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	// 关键格式断言（v0.53+：顶层字段 + 子段，无 [core] 包裹）
	for _, want := range []string{
		`serverAddr = "frp.example.com"`,
		"serverPort = 7000",
		"method = \"token\"",
		`token = "secret-token"`,
		"[[proxies]]",
		`name = "ssh"`,
		"remotePort = 6000",
	} {
		if !strings.Contains(tomlText, want) {
			t.Errorf("generated toml missing %q\n---\n%s", want, tomlText)
		}
	}
	// v0.53+ 无 [core] 包裹段
	if strings.Contains(tomlText, "[core]") {
		t.Errorf("v0.53+ format must not contain [core] section\n---\n%s", tomlText)
	}
	// 项目级加密传播到代理 transport 子段
	if !strings.Contains(tomlText, "useEncryption = true") {
		t.Errorf("proxy-level useEncryption missing\n---\n%s", tomlText)
	}

	// 回读闭环
	back, err := Parse(tomlText)
	if err != nil {
		t.Fatalf("Parse failed: %v\n%s", err, tomlText)
	}
	if back.Server.ServerAddr != "frp.example.com" {
		t.Errorf("ServerAddr mismatch: %q", back.Server.ServerAddr)
	}
	if back.Server.Token != "secret-token" {
		t.Errorf("Token mismatch: %q", back.Server.Token)
	}
	if !back.Server.UseEncryption {
		t.Error("UseEncryption should survive round trip")
	}
	if len(back.Proxies) != 3 {
		t.Fatalf("expected 3 proxies, got %d", len(back.Proxies))
	}
	if back.Proxies[0].Type != "tcp" || back.Proxies[0].RemotePort != 6000 {
		t.Errorf("proxy[0] mismatch: %+v", back.Proxies[0])
	}
	if back.Proxies[1].Type != "http" || len(back.Proxies[1].CustomDomains) != 1 {
		t.Errorf("proxy[1] mismatch: %+v", back.Proxies[1])
	}
	if back.Proxies[2].SecretKey != "sk-123" {
		t.Errorf("proxy[2] SecretKey mismatch: %v", back.Proxies[2])
	}
}

func TestParseLegacyFormat(t *testing.T) {
	const legacy = `
[common]
server_addr = "old.example.com"
server_port = 7500
token = "old-token"
use_encryption = true
log_level = "debug"

[[proxies]]
name = "rdp"
type = "tcp"
local_ip = "127.0.0.1"
local_port = 3389
remote_port = 63389
`
	proj, err := Parse(legacy)
	if err != nil {
		t.Fatalf("Parse legacy failed: %v", err)
	}
	if proj.Server.ServerAddr != "old.example.com" || proj.Server.ServerPort != 7500 {
		t.Errorf("legacy server parse mismatch: %+v", proj.Server)
	}
	if !proj.Server.UseEncryption {
		t.Error("legacy use_encryption should be true")
	}
	if len(proj.Proxies) != 1 || proj.Proxies[0].RemotePort != 63389 {
		t.Errorf("legacy proxy parse mismatch: %+v", proj.Proxies)
	}
}

func TestGenerateValidation(t *testing.T) {
	// 空 serverAddr
	_, err := Generate(&domain.Project{Server: domain.ServerConfig{ServerPort: 7000}})
	if err == nil {
		t.Error("expected error for empty serverAddr")
	}
	// 重复代理名
	_, err = Generate(&domain.Project{
		Server: domain.ServerConfig{ServerAddr: "x.com", ServerPort: 7000},
		Proxies: []domain.ProxyRule{
			{Name: "dup", Type: "tcp", LocalPort: 1, RemotePort: 2},
			{Name: "dup", Type: "tcp", LocalPort: 3, RemotePort: 4},
		},
	})
	if err == nil {
		t.Error("expected error for duplicate proxy names")
	}
	// 非法类型
	_, err = Generate(&domain.Project{
		Server:  domain.ServerConfig{ServerAddr: "x.com", ServerPort: 7000},
		Proxies: []domain.ProxyRule{{Name: "bad", Type: "socks5", LocalPort: 1, RemotePort: 2}},
	})
	if err == nil {
		t.Error("expected error for unsupported proxy type")
	}
}
