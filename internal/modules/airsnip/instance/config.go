package instance

import (
	"os"
	"path/filepath"
	"strings"
)

// AirSnip 内置更新检查接管（机主 2026-11-03 拍板：版本升级单源归 Hanxi）。
//
// 上游自 v0.7.0 起自带自动更新（ADR 0018 / src/update/*）：启动 3 秒后静默拉
// latest.json（ed25519 验签）→ 下载裸 exe → 另起 cmd 脚本等父进程退出后
// **原地覆盖当前 exe 并重启**。这条链与 Hanxi 的版本树必然打架——它替换的是
// versions/airsnip_X.Y.Z/ 里那份被账本摘要锁定、被目录令牌标定的文件，
// 一旦跑成，Hanxi 记录的 AssetSHA256 与实际字节失配、版本"永远装不完"，
// 用户在两套更新入口之间会看到互相矛盾的现状。
//
// 与 litemonitor 的 seed 语义不同，这里不能"文件不存在才播种"：AirSnip 的配置
// 落在 %APPDATA%\airsnip\config.toml（**不随 exe 目录走**，源码实证
// Config::config_dir），首次运行即由上游自己写满全量默认值，其中
// auto_check_update 默认 true——只播种等于永远播不上。故现行做法是就地改写
// 单个键，每次托管启动前执行。
//
// 两条如实的边界，请勿在后续维护中当成 bug 修掉：
//   - 配置文件是上游的**全局单文件**，改写同样作用于用户自己独立安装/外部
//     启动的 AirSnip（它们读同一份 config.toml）——这不是 Hanxi 越界，是上游
//     把配置做成了全局态；决策已确认接受该连带效果；
//   - 用户若在 AirSnip「关于 → 版本更新」里主动重开，下次 Hanxi 托管启动会
//     再压回 false。要彻底尊重上游自更新，只能停用本模块的托管启动。
//
// 写失败不阻断启动（最坏回到上游默认行为=会自更新），调用方记录即可。

const (
	// configDirName 上游配置目录名（%APPDATA%\airsnip）。
	configDirName = "airsnip"
	// configFileName 上游配置文件名。
	configFileName = "config.toml"
	// updateKey 被接管的唯一 TOML 键（src/settings/mod.rs Config::auto_check_update，
	// 顶层标量键，serde 结构体级 #[serde(default)] 已实证：只写该键的极简文件
	// 上游能正常解析、其余字段回落默认值）。
	updateKey = "auto_check_update"
)

// configDir 上游配置目录（%APPDATA%\airsnip），不可用时返回空串。
func configDir() string {
	p := configPath()
	if p == "" {
		return ""
	}
	return filepath.Dir(p)
}

// ConfigDir 导出给 service 层的「打开配置目录」RPC：空串 = 环境无 APPDATA，
// 调用方据此给出人话错误，不得拿空串去 explorer 撞运气。
func ConfigDir() string { return configDir() }

// configPath 返回上游配置文件绝对路径；APPDATA 不可用（非 Windows/异常环境）
// 返回空串，调用方按"无法接管"跳过。
func configPath() string {
	appData := os.Getenv("APPDATA")
	if appData == "" {
		return ""
	}
	return filepath.Join(appData, configDirName, configFileName)
}

// takeoverUpdateCheck 确保上游内置更新检查处于关闭态：键已存在且为 false 时
// 一字节不动；存在但为 true（或其他值）时只替换等号右侧；键缺席时在文件顶部
// 补一行（顶格是 TOML 顶层键的唯一安全位置——表头之后写键会落进那个表）。
// 原子写：同目录 tmp + rename，避免半截文件让上游 load() 回落全默认值。
func takeoverUpdateCheck(path string) error {
	if path == "" {
		return nil // 无 APPDATA 环境：不猜路径，不阻断
	}
	raw, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	next, changed := disableAutoUpdate(string(raw))
	if !changed {
		return nil // 已是关闭态：不重排、不动时间戳（文件不存在时 changed 恒 true）
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	tmp := path + ".hanxi.tmp"
	if err := os.WriteFile(tmp, []byte(next), 0644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		// 竞态：上游几乎同时自建了文件——目标已在场即视为接管失败，
		// 留下 tmp 反而是垃圾，删掉后上报错误由调用方记录。
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// disableAutoUpdate 纯函数改写（不触盘，供单测穷举）：返回新内容与是否变更。
// 逐行只认第一个非注释、非表头的 updateKey 赋值行——命中即只改等号右侧，
// 行首缩进与 CRLF/LF 形态原样保留；遇到表头 [xxx] 立即停止扫描（表内不可能
// 有顶层键，继续找会把同名表键误当目标）；全程不重排任何其他字节。
func disableAutoUpdate(text string) (string, bool) {
	nl := "\n"
	if strings.Contains(text, "\r\n") {
		nl = "\r\n"
	}
	lines := strings.SplitAfter(text, "\n")
	for i, ln := range lines {
		core := strings.TrimRight(ln, "\r\n")
		body := strings.TrimLeft(core, " \t")
		switch {
		case strings.HasPrefix(body, "#"):
			continue
		case strings.HasPrefix(body, "["):
			// 顶层键区结束：键不在场，补在文件最前面
			return updateKey + " = false" + nl + text, true
		}
		eq := strings.Index(body, "=")
		if eq < 0 {
			continue
		}
		if strings.TrimSpace(body[:eq]) != updateKey {
			continue
		}
		if strings.TrimSpace(body[eq+1:]) == "false" {
			return text, false // 已关闭，一字节不动
		}
		head := core[:len(core)-len(body)+eq+1] // 含原缩进与等号
		lines[i] = head + " false" + nl
		return strings.Join(lines, ""), true
	}
	// 整文件无表头也无该键（含空文件/纯注释文件）：追加一行即成合法顶层键
	return updateKey + " = false" + nl + text, true
}
