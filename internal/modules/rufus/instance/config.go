package instance

import (
	"os"
	"path/filepath"
)

// iniFileName 上游便携模式开关文件（src/rufus.c 实证：exe 同目录存在
// rufus.ini 哪怕空文件，即全部设置读写走 ini 而非注册表，app_data_dir 锚定 exe 目录）。
const iniFileName = "rufus.ini"

// iniSeed 托管种子内容。两个作用：
//  1. 文件存在本身即激活便携模式——设置零注册表污染，随版本目录整体卸载；
//  2. UpdateCheckInterval = -1 永久关闭上游内置更新检查（src/net.c 实证：
//     ReadSetting32(SETTING_UPDATE_INTERVAL) == -1 → "Check for updates disabled,
//     as per settings"，托管版本升级由 Hanxi 版本管理接管，双更渠道必然打架）。
//
// 注释用 ASCII 文案：上游 ini 解析器为自研 C 实现，种子不引入编码不确定性；
// 键名与官方 res/rufus.ini 样例文档一致。
const iniSeed = ";; Hanxi managed-mode seed for Rufus.\n" +
	";; 1. The presence of this file enables portable mode: all settings stay\n" +
	";;    in this directory instead of the registry.\n" +
	";; 2. UpdateCheckInterval = -1 permanently disables the built-in update\n" +
	";;    check (see src/net.c); version management is taken over by Hanxi.\n" +
	";; Feel free to edit or remove this file; Hanxi never rewrites it once present.\n" +
	"UpdateCheckInterval = -1\n"

// seedPortableSettings 托管实例启动前播种 rufus.ini：仅当文件**不存在**时写入。
// 文件已存在时绝不改写——用户（或「导入本地」搬来的源配置）之后的任何设置
// 都是明确意图，Hanxi 不越权覆盖。失败不阻断启动（最坏回到上游默认行为：
// 设置落注册表 + 弹更新检查），调用方记录即可。
// 只对托管实例的版本隔离目录生效；外部实例的配置从不触碰。
func seedPortableSettings(installDir string) error {
	path := filepath.Join(installDir, iniFileName)
	if _, err := os.Stat(path); err == nil {
		return nil // 已有配置（含导入携带 / 用户自建），尊重现状
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.WriteFile(path, []byte(iniSeed), 0644); err != nil {
		// 竞态：上游几乎同时自建了文件——视为已存在，非错误
		if _, statErr := os.Stat(path); statErr == nil {
			return nil
		}
		return err
	}
	return nil
}
