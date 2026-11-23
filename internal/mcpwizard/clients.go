package mcpwizard

import (
	"os"
	"path/filepath"
)

// 客户端配置文件格式（决定编辑引擎走 JSON 外科合并还是 TOML 哨兵区块）。
const (
	fmtJSON = "json"
	fmtTOML = "toml"
)

// clientSpec 已知客户端建档：路径解析规则照 PLAN_MCP §2.5 表格——
// Claude Code 支持 CLAUDE_CONFIG_DIR、Codex 支持 CODEX_HOME、omp 支持 PI_CODING_AGENT_DIR，
// Cursor 无环境变量通道。
type clientSpec struct {
	ID     string
	Name   string
	Format string
	dirEnv string // 配置目录环境变量（空 = 无覆盖通道）
	// defaultDir 从用户主目录推导配置目录；claude 特例：默认目录即 home 本身
	//（状态文件形态，配置文件是 home 下的点文件而非目录内文件）。
	defaultDir func(home string) string
	fileName   string
}

var clientSpecs = []clientSpec{
	{
		ID: "claude", Name: "Claude Code", Format: fmtJSON,
		dirEnv: "CLAUDE_CONFIG_DIR",
		defaultDir: func(home string) string {
			return home
		},
		fileName: ".claude.json",
	},
	{
		ID: "codex", Name: "Codex", Format: fmtTOML,
		dirEnv: "CODEX_HOME",
		defaultDir: func(home string) string {
			return filepath.Join(home, ".codex")
		},
		fileName: "config.toml",
	},
	{
		ID: "cursor", Name: "Cursor", Format: fmtJSON,
		dirEnv: "",
		defaultDir: func(home string) string {
			return filepath.Join(home, ".cursor")
		},
		fileName: "mcp.json",
	},
	{
		// omp（Oh My Pi）用户级原生 MCP 档：<配置根>/agent/mcp.json，形态与 Cursor 同款
		// （顶层 mcpServers 映射，stdio 可省 type）——路径与形态均在 omp 18.20.3 真机实测。
		// 只建 PI_CODING_AGENT_DIR 一条通道（指目录本身，绝对路径实测有效）；
		// PI_CONFIG_DIR 刻意不建模：实测它按 home 相对拼接（塞绝对路径直接 ENOENT 崩），
		// 且换的是 omp 全局配置根而非 MCP 专属目录，具名 profile 又自行派生目录
		// ——三者都不是"改一个变量换一处 mcp.json"的静态映射，猜错只会写进读不到的角落。
		// 那种环境下本行回落默认路径：目录在则可写（由用户自行核对路径），
		// 不在则 fail-closed 呈现"未检测到"，手动片段兜底。
		ID: "omp", Name: "omp", Format: fmtJSON,
		dirEnv: "PI_CODING_AGENT_DIR",
		defaultDir: func(home string) string {
			return filepath.Join(home, ".omp", "agent")
		},
		fileName: "mcp.json",
	},
}

func clientSpecByID(id string) (clientSpec, bool) {
	for _, c := range clientSpecs {
		if c.ID == id {
			return c, true
		}
	}
	return clientSpec{}, false
}

// resolve 计算客户端配置文件绝对路径：环境变量覆盖优先，空值回退默认推导。
// home 为空（主目录不可解析）时返回空串，调用方按不可达处理。
func (c clientSpec) resolve(home string, env func(string) string) (dir, path string) {
	if home == "" {
		return "", ""
	}
	dir = ""
	if c.dirEnv != "" && env != nil {
		dir = env(c.dirEnv)
	}
	if dir == "" {
		dir = c.defaultDir(home)
	}
	return dir, filepath.Join(dir, c.fileName)
}

// dirExists 配置目录是否存在（目录缺失 = 客户端无踪迹，拒绝凭空创建整棵目录）。
func dirExists(dir string) bool {
	fi, err := os.Stat(dir)
	return err == nil && fi.IsDir()
}
