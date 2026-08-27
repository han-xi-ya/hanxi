package instance

import (
	"os"
	"strings"
)

// ensureHiddenTray 托管实例启动前确保其托盘图标隐藏（show_tray_icon=0）：
// Everything 内嵌进 HubKit 后，启停/唤窗全由 HubKit 按钮负责，托盘图标是多余的视觉噪音，
// 隐藏后关闭搜索窗也不会误触托盘退出路径（实例仍驻留后台）。
//
// Everything.ini 是 key=value 行式文件（单 [Everything] 段）：
//   - 已存在 show_tray_icon 行 → 原位替换值（保留该行缩进与 CRLF）；
//   - 不存在 → 文末追加。
//
// 失败不阻断启动（托盘可见不构成功能故障），调用方自行决定是否记录。
// 注意：只对托管实例的 ini 生效（版本隔离目录内）；外部实例的配置从不触碰。
func ensureHiddenTray(iniPath string) error {
	const key = "show_tray_icon"

	var present []byte
	if b, err := os.ReadFile(iniPath); err == nil {
		present = b
	} else if !os.IsNotExist(err) {
		return err
	}

	lines := strings.Split(string(present), "\n")
	replaced := false
	for i := range lines {
		body := strings.TrimSuffix(lines[i], "\r")
		hasCR := len(body) != len(lines[i])
		eq := strings.Index(body, "=")
		if eq < 0 || !strings.EqualFold(strings.TrimSpace(body[:eq]), key) {
			continue
		}
		// 仅替换"值"本身：值起点为 = 后首个非空白，替换后其前的空白与整行格式原样保留
		v := eq + 1
		for v < len(body) && (body[v] == ' ' || body[v] == '\t') {
			v++
		}
		if v == len(body) {
			body += "0"
		} else {
			body = body[:v] + "0"
		}
		if hasCR {
			lines[i] = body + "\r"
		} else {
			lines[i] = body
		}
		replaced = true
		break
	}

	out := strings.Join(lines, "\n")
	if !replaced {
		if len(out) > 0 && !strings.HasSuffix(out, "\n") {
			out += "\r\n"
		}
		out += key + "=0\r\n"
	}
	return os.WriteFile(iniPath, []byte(out), 0644)
}
