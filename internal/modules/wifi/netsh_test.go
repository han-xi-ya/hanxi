package wifi_test

import (
	"strings"
	"testing"
)

func TestParseSSIDLine(t *testing.T) {
	lines := []string{
		"    所有用户配置文件 : MyHomeWiFi",
		"    All User Profile     : Office-5G",
		"    All User Profile     : Guest",
		"    所有用户配置文件 : Special:Colon:SSID",
		"    其他无关行",
	}
	expected := []string{"MyHomeWiFi", "Office-5G", "Guest", "Special:Colon:SSID"}

	var got []string
	for _, line := range lines {
		if strings.Contains(line, "所有用户配置文件") || strings.Contains(line, "User Profile") || strings.Contains(line, "All User Profile") {
			parts := strings.Split(line, ":")
			if len(parts) >= 2 {
				name := strings.TrimSpace(strings.Join(parts[1:], ":"))
				if name != "" {
					got = append(got, name)
				}
			}
		}
	}
	if len(got) != len(expected) {
		t.Fatalf("期望 %d 个 SSID，实际 %d 个", len(expected), len(got))
	}
	for i, s := range expected {
		if got[i] != s {
			t.Errorf("第 %d 个 SSID 期望 %q，实际 %q", i, s, got[i])
		}
	}
}

func TestParsePasswordLine(t *testing.T) {
	sample := `
    安全设置
        关键内容            : 12345678:abc
        成本设置            : 无限制
    `
	var pwd string
	for _, line := range strings.Split(sample, "\n") {
		if strings.Contains(line, "关键内容") || strings.Contains(line, "Key Content") {
			parts := strings.Split(line, ":")
			if len(parts) >= 2 {
				pwd = strings.TrimSpace(strings.Join(parts[1:], ":"))
				break
			}
		}
	}
	if pwd != "12345678:abc" {
		t.Errorf("期望密码 12345678:abc，实际 %q", pwd)
	}
}
