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
// Claude Code 支持 CLAUDE_CONFIG_DIR、Codex 支持 CODEX_HOME，Cursor 无环境变量通道。
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
