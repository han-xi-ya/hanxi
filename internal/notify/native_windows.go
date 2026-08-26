//go:build windows

package notify

import (
	"encoding/base64"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"unicode/utf16"
)

const createNoWindow = 0x08000000 // CREATE_NO_WINDOW

// showNativeToast 在 Windows 最小化或后台时发送系统通知
func showNativeToast(n *Notification) {
	go sendWindowsNotification(n.Title, fmt.Sprintf("[%s] %s", n.ModuleID, n.Message))
}

// sendWindowsNotification 使用 PowerShell WinRT API 触发 Windows 10/11 原生 Toast 通知
func sendWindowsNotification(title, message string) {
	psScript := fmt.Sprintf(`[Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime] | Out-Null
$template = [Windows.UI.Notifications.ToastTemplateType]::ToastText02
$xml = [Windows.UI.Notifications.ToastNotificationManager]::GetTemplateContent($template)
$textNodes = $xml.GetElementsByTagName("text")
$textNodes.Item(0).AppendChild($xml.CreateTextNode("%s")) | Out-Null
$textNodes.Item(1).AppendChild($xml.CreateTextNode("%s")) | Out-Null
$toast = [Windows.UI.Notifications.ToastNotification]::new($xml)
[Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier("{1AC14E77-02E7-4E5D-B744-2EB1AE5198B7}\WindowsPowerShell\v1.0\powershell.exe").Show($toast)
`, escapePS(title), escapePS(message))

	utf16Units := utf16.Encode([]rune(psScript))
	bytes := make([]byte, len(utf16Units)*2)
	for i, u := range utf16Units {
		bytes[i*2] = byte(u)
		bytes[i*2+1] = byte(u >> 8)
	}
	encoded := base64.StdEncoding.EncodeToString(bytes)

	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-EncodedCommand", encoded)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: createNoWindow,
	}
	_ = cmd.Run()
}

func escapePS(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '"', '`', '$':
			b.WriteRune('`')
			b.WriteRune(r)
		case '\n', '\r':
			b.WriteRune(' ')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

