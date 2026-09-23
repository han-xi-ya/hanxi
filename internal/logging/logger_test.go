package logging_test

import (
	"strings"
	"testing"

	"hanxi/internal/logging"
)

func TestRedactSensitive(t *testing.T) {
	cases := []struct {
		input    string
		contains string
		excludes string
	}{
		{
			input:    `frpc starting with token = "my_secret_token_12345" and user = admin`,
			contains: `token="******"`,
			excludes: `my_secret_token_12345`,
		},
		{
			input:    `header: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9`,
			contains: `Bearer ******`,
			excludes: `eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9`,
		},
		{
			input:    `normal log: server listening on port 8080`,
			contains: `server listening on port 8080`,
			excludes: `******`,
		},
	}

	for _, c := range cases {
		out := logging.Redact(c.input)
		if !strings.Contains(out, c.contains) {
			t.Errorf("expected redacted output to contain %q, got %q", c.contains, out)
		}
		if c.excludes != "" && strings.Contains(out, c.excludes) {
			t.Errorf("expected redacted output to NOT contain %q, got %q", c.excludes, out)
		}
	}
}

// TestRedactPII 出机口径（MCP 日志工具 N34）：Redact 全量继承 + IPv4/邮箱/
// 供应商前缀密钥打码；同时锁死"磁盘窄口径不受污染"——Redact 不吞 IP/邮箱。
func TestRedactPII(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		contains string
		excludes string
	}{
		{
			name:     "ipv4",
			input:    `frpc dialing 192.168.10.7:7000 failed`,
			contains: `[ipv4]:7000`,
			excludes: `192.168.10.7`,
		},
		{
			name:     "email",
			input:    `booking notify zhang.san@example.com accepted`,
			contains: `[email]`,
			excludes: `zhang.san@example.com`,
		},
		{
			name:     "provider key",
			input:    `config check sk-abcdef0123456789abcdef0123 ok`,
			contains: `[redacted-key]`,
			excludes: `sk-abcdef0123456789abcdef0123`,
		},
		{
			name:     "redact base inherited",
			input:    `login attempt password=hunter2 from 10.0.0.1`,
			contains: `password="******"`,
			excludes: `hunter2`,
		},
		{
			name:     "clean line untouched",
			input:    `txn opened module=markeron step=download`,
			contains: `txn opened module=markeron step=download`,
			excludes: `[ipv4]`,
		},
	}
	for _, c := range cases {
		out := logging.RedactPII(c.input)
		if !strings.Contains(out, c.contains) {
			t.Errorf("%s: want contain %q, got %q", c.name, c.contains, out)
		}
		if c.excludes != "" && strings.Contains(out, c.excludes) {
			t.Errorf("%s: want exclude %q, got %q", c.name, c.excludes, out)
		}
	}
	// 窄口径回归锚：磁盘日志通道不得被 PII 层顺手改造（排障要看得见 IP）。
	if got := logging.Redact(`dial 192.168.10.7:7000`); !strings.Contains(got, "192.168.10.7") {
		t.Errorf("Redact（磁盘窄口径）不得吞 IPv4, got %q", got)
	}
}
