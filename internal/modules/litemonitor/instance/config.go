package instance

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// settingsFileName LiteMonitor 全量配置的落盘文件名（AppContext.BaseDirectory 便携语义，
// 源码实证：SettingsHelper._cachedPath = exe 目录/settings.json）。
const settingsFileName = "settings.json"

// seedManagedSettings 托管实例启动前播种 settings.json：仅当文件**不存在**时
// 写入最小种子 {"AutoCheckUpdate": false}，关闭上游内置更新检查
// （启动静默检查 + 手动弹窗，版本管理已由 Hanxi 接管，双更渠道必然打架）。
//
// 上游 SettingsHelper.Load 反序列化 PropertyNameCaseInsensitive=true 且
// 缺失字段回落 C# 属性默认值——最小种子等价于"全默认首启 + 关掉自动更新"，
// 不会丢失任何默认监控项（MonitorItems 为空仍走其 InitDefaultItems）。
//
// 文件已存在时**绝不改写**：用户之后在 LiteMonitor 界面里的任何设置
// （含主动重开自动更新）都是明确意图，Hanxi 不越权覆盖。
// 失败不阻断启动（最坏回到上游默认行为=会弹更新检查），调用方记录即可。
// 只对托管实例的版本隔离目录生效；外部实例的配置从不触碰。
func seedManagedSettings(installDir string) error {
	path := filepath.Join(installDir, settingsFileName)
	if _, err := os.Stat(path); err == nil {
		return nil // 已有配置（含上游运行期自动生成的），尊重现状
	} else if !os.IsNotExist(err) {
		return err
	}
	seed := map[string]any{"AutoCheckUpdate": false}
	data, err := json.MarshalIndent(seed, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		// 竞态：上游几乎同时自建了文件——视为已存在，非错误
		if _, statErr := os.Stat(path); statErr == nil {
			return nil
		}
		return err
	}
	return nil
}
