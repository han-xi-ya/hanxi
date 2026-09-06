package readiness

import (
	"encoding/binary"
	"strings"
	"testing"
	"unicode/utf16"
)

// utf16le 模拟 wsl.exe 的真实输出编码（UTF-16LE，常带 BOM）。
func utf16le(s string) []byte {
	units := utf16.Encode([]rune(s))
	b := make([]byte, 0, 2+2*len(units))
	b = append(b, 0xFF, 0xFE)
	for _, u := range units {
		b = binary.LittleEndian.AppendUint16(b, u)
	}
	return b
}

func TestDecodeWslOutput(t *testing.T) {
	cases := []struct {
		name string
		in   []byte
		want string
	}{
		{"BOM UTF-16 中文", utf16le("WSL 版本: 2.7.13.0"), "WSL 版本: 2.7.13.0"},
		{"无 BOM ASCII UTF-16", func() []byte {
			b := utf16le("WSL version: 2.9.10.0")
			return b[2:]
		}(), "WSL version: 2.9.10.0"},
		{"单字节原样透传", []byte("plain text"), "plain text"},
		// 回归锁：中文提示行 NUL 占比实测仅约 29%（用户机器 wsllv.bin 字节画像），
		// 旧版按占比阈值会误判成单字节 → 守卫串被 NUL 打散漏检 → 帮助文本进表。
		{"CJK 高占比 UTF-16（无 BOM）", func() []byte {
			b := utf16le("未安装任何发行版。可通过运行 “wsl.exe --install <Distro>” 进行安装。\r\n")
			return b[2:]
		}(), "未安装任何发行版。可通过运行 “wsl.exe --install <Distro>” 进行安装。"},
		{"空输入", nil, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := decodeWslOutput(c.in); got != c.want {
				t.Fatalf("decode = %q, want %q", got, c.want)
			}
		})
	}
}

func TestParseVersion(t *testing.T) {
	if got := ParseVersion("WSL 版本: 2.7.13.0\n内核版本: 6.18.33.2-2"); got != "2.7.13" {
		t.Fatalf("中文标签解析失败: %q", got)
	}
	if got := ParseVersion("WSL version: 2.9.10.0\nKernel version: 6.6"); got != "2.9.10" {
		t.Fatalf("英文标签解析失败: %q", got)
	}
	// 未安装时的帮助文本：不得从 aka.ms/wsl2 之类的 URL 里脑补出版本号。
	help := "版权所有 (C) Microsoft Corporation。保留所有权利。\n\n用法: wsl.exe [Argument]\n\n参数:\n\n    --install <Options>\n        安装可选组件。 https://aka.ms/wslstore\n"
	if got := ParseVersion(help); got != "" {
		t.Fatalf("帮助文本不应解析出版本: %q", got)
	}
}

func TestParseDistrosChineseTable(t *testing.T) {
	out := "  名称                   状态           版本\r\n" +
		"* Ubuntu                 正在运行       2\r\n" +
		"  docker-desktop         已停止         2\r\n"
	list := ParseDistros(out)
	if len(list) != 2 {
		t.Fatalf("应解析出 2 个发行版，得 %d: %+v", len(list), list)
	}
	if list[0].Name != "Ubuntu" || !list[0].Default || list[0].State != "正在运行" || list[0].Version != "2" {
		t.Fatalf("首行解析错误: %+v", list[0])
	}
	if list[1].Name != "docker-desktop" || list[1].Default {
		t.Fatalf("次行解析错误: %+v", list[1])
	}
}

func TestParseDistrosHelpTextYieldsNothing(t *testing.T) {
	help := "未安装适合 Linux 的 Windows 子系统。\n\n可通过运行 “wsl.exe --install” 进行安装。\n有关详细信息，请访问 https://aka.ms/wslinstall\n"
	if list := ParseDistros(help); len(list) != 0 {
		t.Fatalf("未安装输出不应产生发行版行: %+v", list)
	}
	// 用户实机事故样本：WSL 已装但零发行版的中文提示行（曾因解码失判进表）。
	garbledProne := "未安装任何发行版。可通过运行 “wsl.exe --install <Distro>” 进行安装。"
	if list := ParseDistros(garbledProne); len(list) != 0 {
		t.Fatalf("零发行版提示行不应进表: %+v", list)
	}
	// 表尾列非 1/2 的行一律拒收（结构约束双保险，不依赖关键词守卫）。
	if list := ParseDistros("  Ubuntu                 正在运行       预览版"); len(list) != 0 {
		t.Fatalf("版本列非法应拒收: %+v", list)
	}
}

func TestParseOnlineList(t *testing.T) {
	// 旧版：行首两格缩进
	out := "以下是可安装的有效发行版的列表。\n" +
		"  NAME                 FRIENDLY NAME\n" +
		"  Ubuntu               Ubuntu\n" +
		"  Ubuntu-24.04         Ubuntu 24.04 LTS\n" +
		"  Debian               Debian GNU/Linux\n"
	list := ParseOnline(out)
	if len(list) != 3 {
		t.Fatalf("旧版缩进式应解析出 3 项，得 %d: %+v", len(list), list)
	}
	if list[1].ID != "Ubuntu-24.04" || list[1].Label != "Ubuntu 24.04 LTS" {
		t.Fatalf("清单项解析错误: %+v", list[1])
	}
	for _, o := range list {
		if strings.EqualFold(o.ID, "NAME") {
			t.Fatal("表头不应进清单")
		}
	}
}

func TestParseOnlineWSL29FlushLeft(t *testing.T) {
	// WSL 2.9.10 实机抓样：顶格无缩进 + 中文说明行不得混入
	out := "以下是可安装的有效分发的列表。\n" +
		"使用“wsl.exe --install <Distro>”安装。\n" +
		"\n" +
		"NAME                            FRIENDLY NAME\n" +
		"Ubuntu                          Ubuntu\n" +
		"Ubuntu-26.04                    Ubuntu 26.04 LTS\n" +
		"SUSE-Linux-Enterprise-15-SP7    SUSE Linux Enterprise 15 SP7\n"
	list := ParseOnline(out)
	if len(list) != 3 {
		t.Fatalf("2.9.10 顶格式应解析出 3 项，得 %d: %+v", len(list), list)
	}
	if list[2].ID != "SUSE-Linux-Enterprise-15-SP7" || list[2].Label != "SUSE Linux Enterprise 15 SP7" {
		t.Fatalf("长 ID 解析错误: %+v", list[2])
	}
}
