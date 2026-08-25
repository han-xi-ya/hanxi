package wifi

import (
	"fmt"
	"os/exec"
	"strings"
	"syscall"
)

const createNoWindow = 0x08000000 // CREATE_NO_WINDOW: 避免 Windows 弹出 cmd 黑框

func runCommandSilent(name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNoWindow,
	}
	return cmd.Output()
}

// GetAllWiFiPasswords 获取本机所有已保存 WiFi 的 SSID 和明文密码
func GetAllWiFiPasswords() ([]Profile, error) {
	ssids, err := getSavedSSIDs()
	if err != nil {
		return nil, err
	}

	var results []Profile
	for _, ssid := range ssids {
		pwd, err := getPasswordForSSID(ssid)
		if err != nil {
			pwd = "未设置密码或无法读取"
		}
		results = append(results, Profile{SSID: ssid, Password: pwd})
	}
	return results, nil
}

// getSavedSSIDs 获取本机已保存的所有 WiFi 名称
func getSavedSSIDs() ([]string, error) {
	out, err := runCommandSilent("netsh", "wlan", "show", "profiles")
	if err != nil {
		return nil, err
	}

	var ssids []string
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, "所有用户配置文件") || strings.Contains(line, "User Profile") || strings.Contains(line, "All User Profile") {
			parts := strings.Split(line, ":")
			if len(parts) >= 2 {
				ssid := strings.TrimSpace(strings.Join(parts[1:], ":"))
				if ssid != "" {
					ssids = append(ssids, ssid)
				}
			}
		}
	}
	return ssids, nil
}

// getPasswordForSSID 查询指定 WiFi 的明文密码
func getPasswordForSSID(ssid string) (string, error) {
	out, err := runCommandSilent("netsh", "wlan", "show", "profile",
		fmt.Sprintf("name=%s", ssid), "key=clear")
	if err != nil {
		return "", err
	}

	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, "关键内容") || strings.Contains(line, "Key Content") {
			parts := strings.Split(line, ":")
			if len(parts) >= 2 {
				return strings.TrimSpace(strings.Join(parts[1:], ":")), nil
			}
		}
	}
	return "", fmt.Errorf("未找到密码")
}
