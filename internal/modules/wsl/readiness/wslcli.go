package readiness

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"

	"hanxi/internal/platform/windows"
)

// wsl.exe 的 stdout/stderr 均为 UTF-16LE（常带 BOM），与主流 CLI 的 UTF-8 惯例不同，
// 直接按字节处理会得到隔空格的乱码（docs/TROUBLESHOOTING 已录此坑）。
// RunWsl 统一解码后返回合并输出；exit error 一并返回供上层判断语义。
func RunWsl(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "wsl.exe", args...)
	windows.HideConsole(cmd)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	out := decodeWslOutput(stdout.Bytes())
	if s := decodeWslOutput(stderr.Bytes()); s != "" {
		if out != "" {
			out += "\n"
		}
		out += s
	}
	return out, err
}

// RunWslWithStdin 执行 wsl.exe 子命令并把 stdin 内容喂给 guest 进程
// （wsl.conf 写回等"经管道改文件"的通道）。解码语义与 RunWsl 一致。
func RunWslWithStdin(ctx context.Context, stdin string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "wsl.exe", args...)
	windows.HideConsole(cmd)
	cmd.Stdin = strings.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	out := decodeWslOutput(stdout.Bytes())
	if s := decodeWslOutput(stderr.Bytes()); s != "" {
		if out != "" {
			out += "\n"
		}
		out += s
	}
	return out, err
}

// decodeWslOutput 判别 wsl.exe 输出编码。判据（用户机器字节级实证修正）：
// UTF-16LE 换行 \r\n 编码为 0D 00 0A 00 必含 NUL；而 UTF-8/GBK 文本永远不含 0x00。
// 因此"含 NUL 且偶数长度"即为 UTF-16——不能按 NUL 占比设阈值：
// 中文 CJK 码位高字节非零（如"未安装任何发行版"提示行 NUL 占比实测仅 29%），
// 密度启发式会把纯中文输出误判成单字节，导致守卫串（wsl.exe）被 NUL 打散漏检。
func decodeWslOutput(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	if len(b)%2 == 0 && bytes.IndexByte(b, 0) >= 0 {
		return strings.TrimSpace(decodeUTF16LE(b))
	}
	return strings.TrimSpace(string(b)) // 单字节码页（GBK/UTF-8）原样透传
}

// decodeUTF16LE 解码 UTF-16LE 字节流（容忍 BOM 与落单的代理项半区）。
func decodeUTF16LE(b []byte) string {
	if len(b) >= 2 && b[0] == 0xFF && b[1] == 0xFE {
		b = b[2:]
	}
	var sb strings.Builder
	for i := 0; i+1 < len(b); i += 2 {
		u := uint32(b[i]) | uint32(b[i+1])<<8
		switch {
		case u >= 0xD800 && u <= 0xDBFF: // 高位代理：凑齐一对再写，凑不齐按替换符
			if i+3 < len(b) {
				lo := uint32(b[i+2]) | uint32(b[i+3])<<8
				if lo >= 0xDC00 && lo <= 0xDFFF {
					sb.WriteRune(rune(0x10000 + (u-0xD800)<<10 + (lo - 0xDC00)))
					i += 2
					continue
				}
			}
			sb.WriteRune(0xFFFD)
			i += 2
		case u >= 0xDC00 && u <= 0xDFFF:
			sb.WriteRune(0xFFFD)
		default:
			sb.WriteRune(rune(u))
		}
	}
	return sb.String()
}

var wslVersionRe = regexp.MustCompile(`(?i)WSL[^\r\n]*?(\d+\.\d+\.\d+)(?:\.\d+)?`)

// ParseVersion 从 `wsl --version` 输出提取 WSL 本体版本（兼容中英文标签行）；
// 未安装时输出为用法帮助，提不到版本即返回空串。
func ParseVersion(out string) string {
	if m := wslVersionRe.FindStringSubmatch(out); m != nil {
		return m[1]
	}
	return ""
}

// ParseDistros 解析 `wsl -l -v` 的三列表格（名称/状态/版本）。
// 表头依系统语言变化，按"字段间以 2+ 空格分隔 + 跳过含表头关键词的行"处理；
// 未安装时命令打印帮助文本（无表格），返回空列表。
func ParseDistros(out string) []Distro {
	var list []Distro
	for _, raw := range strings.Split(out, "\n") {
		line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		if line == "" || containsAnyFold(line, "NAME", "STATE", "VERSION", "名称", "状态", "版本") {
			continue
		}
		// 帮助/错误行的特征是出现命令行样式片段，一律不当表格行解析。
		if strings.Contains(line, "--install") || strings.Contains(line, "wsl.exe") || strings.Contains(line, "版权所有") || strings.Contains(line, "Copyright") {
			continue
		}
		isDefault := strings.HasPrefix(line, "*")
		line = strings.TrimSpace(strings.TrimPrefix(line, "*"))
		fields := regexp.MustCompile(`\s{2,}`).Split(line, -1)
		if len(fields) < 2 {
			continue
		}
		// 结构约束：表尾列必为 WSL 版本 1/2——帮助/提示行绝无此形态，
		// 与关键词守卫互为双保险（编码误判历史教训：守卫串可能被乱码打散漏检）。
		last := strings.TrimSpace(fields[len(fields)-1])
		if last != "1" && last != "2" {
			continue
		}
		d := Distro{Name: strings.TrimSpace(fields[0]), State: strings.TrimSpace(fields[1]), Default: isDefault, Version: last}
		if d.Name == "" {
			continue
		}
		list = append(list, d)
	}
	return list
}

// 清单行格式跨版本漂移实录：旧版行首两格缩进，WSL 2.9.10 起改为顶格
// （实机抓样校准）。ID 列限 ASCII 发行版命名形态（字母数字起头，含 . _ + -），
// 说明行（如 使用“wsl.exe --install <Distro>”安装。）因无"列间 2+ 空格"或
// 首列含中文/引号而天然落空，表头再显式剔除一道。
var onlineLineRe = regexp.MustCompile(`^\s*([A-Za-z0-9][A-Za-z0-9._+-]*)\s{2,}(\S.*?)\s*$`)

// ParseOnline 解析 `wsl --list --online` 清单：ID 列与友好名列以多空格对齐。
func ParseOnline(out string) []DistroOption {
	var list []DistroOption
	seen := make(map[string]struct{})
	for _, raw := range strings.Split(out, "\n") {
		m := onlineLineRe.FindStringSubmatch(strings.TrimSuffix(raw, "\r"))
		if m == nil {
			continue
		}
		id := m[1]
		if id == "NAME" { // 表头同为"多空格对齐"版式，须显式剔除
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		list = append(list, DistroOption{ID: id, Label: strings.TrimSpace(m[2])})
	}
	return list
}

// Version 返回本机 WSL 本体版本；未安装或不可用时返回空串（不是错误——
// "未安装"是本模块的正常业务态而非异常）。
func Version(ctx context.Context) string {
	out, _ := RunWsl(ctx, "--version")
	return ParseVersion(out)
}

// Distros 返回已安装发行版列表；未安装时列表为空。
func Distros(ctx context.Context) []Distro {
	out, _ := RunWsl(ctx, "-l", "-v")
	return ParseDistros(out)
}

// OnlineDistros 返回官方在线可安装清单；命令失败时报错（该清单纯离线不可得，
// 与 ParseDistros 的"空即正常"语义不同）。
func OnlineDistros(ctx context.Context) ([]DistroOption, error) {
	out, err := RunWsl(ctx, "--list", "--online")
	list := ParseOnline(out)
	if len(list) == 0 {
		if err != nil {
			return nil, fmt.Errorf("查询在线发行版清单失败: %w", err)
		}
		return nil, fmt.Errorf("在线发行版清单解析为空（wsl.exe 输出格式可能已变化）")
	}
	return list, nil
}

func containsAnyFold(s string, subs ...string) bool {
	lower := strings.ToLower(s)
	for _, sub := range subs {
		if strings.Contains(lower, strings.ToLower(sub)) {
			return true
		}
	}
	return false
}
