//go:build windows

package notify

import (
	"fmt"
	"os/exec"
	"syscall"

	"github.com/wailsapp/wails/v3/pkg/application"
)

const createNoWindow = 0x08000000 // CREATE_NO_WINDOW

// showNativeToast 在 Windows 最小化或后台时发送系统通知
func showNativeToast(n *Notification, win *application.WebviewWindow) {
	go sendWindowsNotification(n.Title, fmt.Sprintf("[%s] %s", n.ModuleID, n.Message))
}

// sendWindowsNotification 使用 PowerShell 脚本极速触发 Windows 10/11 原生 Toast 通知
func sendWindowsNotification(title, message string) {
	psScript := fmt.Sprintf(`[Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime] > $null
$template = [Windows.UI.Notifications.ToastTemplateType]::ToastText02
$xml = [Windows.UI.Notifications.ToastNotificationManager]::GetTemplateContent($template)
$textNodes = $xml.GetElementsByTagName("text")
$textNodes.Item(0).AppendChild($xml.CreateTextNode("%s")) > $null
$textNodes.Item(1).AppendChild($xml.CreateTextNode("%s")) > $null
$toast = [Windows.UI.Notifications.ToastNotification]::new($xml)
[Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier("HubKit").Show($toast)
`, escapePS(title), escapePS(message))

	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-Command", psScript)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNoWindow,
	}
	_ = cmd.Run()
}

func escapePS(s string) string {
	res := ""
	for _, r := range s {
		if r == '"' || r == '`' || r == '$' {
			res += "`" + string(r)
		} else if r == '\n' || r == '\r' {
			res += " "
		} else {
			res += string(r)
		}
	}
	return res
}
