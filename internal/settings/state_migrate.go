package settings

import (
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// 本文件实现 v0.3.x 起的数据根治理：历史版本把各模块状态 JSON 平铺在数据根
// （configDir=dataDir=根），日积月累根目录被 20+ 个 `<module>.json` 淹没。
// StateDir()（<base>/state/）成为模块状态文件的新家，本迁移在应用启动、任何
// 模块 store 构造（构造即 load，见各 store）之前，把根目录遗留状态文件搬进去。
//
// 边界与不变量：
//   - config.json 必须留在根目录——ConfigFile()（store 唯一读取锚点）指向
//     根下同名文件，且 isHanxiDataRoot 以它为数据根特征（旧 data/ 废弃告警
//     与 %APPDATA% 旧家搬迁闸门的判据）；损坏隔离取证副本 config.json.corrupt-*
//     同理留在原地。
//   - 目标同名文件已存在时**跳过不覆盖**：唯一现实场景是"回滚旧版期间旧版
//     又写回了根目录"，根侧是更新数据，静默覆盖等于销毁用户数据；跳过并告警
//     留痕，人工可按日志中的双全路径找回。
//   - 迁移先于单实例锁执行，可能与另一实例并发：rename 以"源文件存在"为天然
//     闸门，同一文件必有一方 ENOENT 失败，容忍即闭环，无丢更新路径。
//   - 一切失败都不阻断启动（返回聚合作用的 error 由调用方决定是否只告警），
//     单个文件失败仅记录——日志目录损坏都不该拦门，何况整理。

// keepRootFiles 数据根白名单：即便匹配迁移模式也必须留在根部的文件名。
func keepRootFiles(name string) bool { return name == "config.json" }

// isStateFile 模块状态文件（.json 结尾且不在白名单）。
func isStateFile(name string) bool {
	return strings.HasSuffix(name, ".json") && !keepRootFiles(name)
}

// isTempDebris 原子写崩溃残骸：jsonstore/settings 的 `<name>.json.tmp.<pid>`
// 与 wsl 的 `.wsl-portproxy-*.tmp` / `.wsl-install-pref-*.tmp`。正常路径下
// 保存失败会自清理，能留下的只有进程被杀尸块，无活性。
func isTempDebris(name string) bool {
	if strings.Contains(name, ".json.tmp.") {
		return true
	}
	if strings.HasSuffix(name, ".tmp") {
		return strings.HasPrefix(name, ".wsl-portproxy-") || strings.HasPrefix(name, ".wsl-install-pref-")
	}
	return false
}

// migrateRootStateFiles 把 baseDir 根部的模块状态文件与原子写残骸搬进 stateDir，
// 返回迁移与跳过的文件名清单及聚合错误。纯参数化设计（同 detectPortableBaseDir
// 的可测性先例），不触碰全局 Paths，供单测直接喂 t.TempDir()。
func migrateRootStateFiles(baseDir, stateDir string) (moved, skipped []string, errs error) {
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		// ensureDirs 吞错时 state 可能未建，迁移侧兜底；建不出来就整体放弃。
		return nil, nil, err
	}
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		return nil, nil, err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		isState := isStateFile(name)
		isDebris := isTempDebris(name)
		if !isState && !isDebris {
			continue
		}
		src := filepath.Join(baseDir, name)
		dest := filepath.Join(stateDir, name)

		// 目标冲突：残骸一律让位（删根侧——同名主文件的存在说明该状态有活性，
		// 尸块无价值）；状态文件保留根侧并告警（见文件头"跳过不覆盖"）。
		if _, err := os.Stat(dest); err == nil {
			if isDebris {
				if rerr := os.Remove(src); rerr != nil && !errors.Is(rerr, fs.ErrNotExist) {
					errs = errors.Join(errs, rerr)
				}
				continue
			}
			slog.Warn("settings: 数据根状态文件与 state/ 同名，跳过迁移保留双方（可手工找回根目录副本）",
				"root", src, "state", dest)
			skipped = append(skipped, name)
			continue
		}

		if err := os.Rename(src, dest); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				continue // 双实例竞态：另一方已搬走，视为完成
			}
			slog.Warn("settings: 迁移数据根状态文件失败", "file", src, "err", err)
			errs = errors.Join(errs, err)
			continue
		}
		moved = append(moved, name)
	}
	return moved, skipped, errs
}

// MigrateRootStateFiles 按全局路径布局执行数据根状态文件收拢（幂等，二跑 no-op）。
// 应用启动序列中、模块装配前调用；moved>0 时记录汇总日志便于事后追溯。
func MigrateRootStateFiles(p *Paths) {
	moved, skipped, err := migrateRootStateFiles(p.BaseDir(), p.StateDir())
	if len(moved) > 0 {
		slog.Info("settings: 数据根遗留状态文件已收拢进 state/ 目录",
			"count", len(moved), "files", strings.Join(moved, ", "), "skipped", len(skipped))
	}
	if err != nil {
		slog.Warn("settings: 数据根状态文件迁移存在失败项（不影响启动）", "err", err)
	}
}
