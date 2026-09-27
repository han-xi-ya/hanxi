package snapshot

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// whitelistFile 是安全枚举确认过的普通文件。枚举不跟随符号链接、junction 或
// 其他 reparse point；严格模式发现此类边界项直接报错，避免把数据根外内容入保。
type whitelistFile struct {
	Rel  string
	Path string
	Info fs.FileInfo
}

func enumerateWhitelist(dataDir string, strict bool) ([]whitelistFile, error) {
	var files []whitelistFile
	add := func(path string, info fs.FileInfo) {
		rel, err := filepath.Rel(dataDir, path)
		if err != nil {
			return
		}
		rel = filepath.ToSlash(rel)
		if Whitelisted(rel) {
			files = append(files, whitelistFile{Rel: rel, Path: path, Info: info})
		}
	}
	reject := func(path string, info fs.FileInfo) error {
		unsafe, err := unsafeLinkLike(path, info)
		if err != nil {
			if strict {
				return fmt.Errorf("检查快照路径 %s 失败: %w", path, err)
			}
			return nil
		}
		if unsafe && strict {
			return fmt.Errorf("快照白名单拒绝符号链接或 reparse point: %s", path)
		}
		return nil
	}

	configPath := filepath.Join(dataDir, rootConfig)
	if info, err := os.Lstat(configPath); err == nil {
		unsafe, uerr := unsafeLinkLike(configPath, info)
		if uerr != nil {
			if strict {
				return nil, fmt.Errorf("检查快照路径 %s 失败: %w", configPath, uerr)
			}
		} else if unsafe {
			if err := reject(configPath, info); err != nil {
				return nil, err
			}
		} else if info.Mode().IsRegular() {
			add(configPath, info)
		}
	} else if strict && !os.IsNotExist(err) {
		return nil, err
	}

	for _, root := range []string{rootState, rootMemo} {
		base := filepath.Join(dataDir, root)
		info, err := os.Lstat(base)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			if strict {
				return nil, err
			}
			continue
		}
		unsafe, uerr := unsafeLinkLike(base, info)
		if uerr != nil || unsafe || !info.IsDir() {
			if uerr != nil && strict {
				return nil, fmt.Errorf("检查快照路径 %s 失败: %w", base, uerr)
			}
			if unsafe {
				if err := reject(base, info); err != nil {
					return nil, err
				}
			}
			continue
		}
		err = filepath.WalkDir(base, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				if strict {
					return walkErr
				}
				return nil
			}
			if path == base {
				return nil
			}
			entryInfo, err := entry.Info()
			if err != nil {
				if strict {
					return err
				}
				return nil
			}
			// 波 1 修复 1(b)（2026-09-27）：Windows 三种 reparse 形态（文件符号链接 /
			// 目录符号链接 / junction）经 Go 1.26.4 本机实测在枚举数据
			// （FindFirstFile 属性位）中一律直报 fs.ModeSymlink，DirEntry.Type() 与
			// entry.Info().Mode() 同源——普通条目不再补发 GetFileAttributes（修复前
			// 每拍每条目一次）。可疑条目走 unsafeLinkLike 全语义：其内部先查 mode 位
			// （已命中即为 true），与 Type() 短路判定等价，保留该函数口径以便
			// 非 Windows 平台与错误路径语义不变。残余差异仅在枚举后条目被替换为
			// 链接的 TOCTOU 竞态——原逐条目探测同样不免疫（探测后、add 前替换等价）。
			if entry.Type()&(fs.ModeSymlink|fs.ModeIrregular) != 0 {
				unsafe, uerr := unsafeLinkLike(path, entryInfo)
				if uerr != nil {
					if strict {
						return uerr
					}
					if entry.IsDir() {
						return filepath.SkipDir
					}
					return nil
				}
				if unsafe {
					if strict {
						return fmt.Errorf("快照白名单拒绝符号链接或 reparse point: %s", path)
					}
					if entry.IsDir() || entryInfo.IsDir() {
						return filepath.SkipDir
					}
					return nil
				}
				// 位命中却判安全（理论不可达）：落入下方常规处理，与原行为逐项等价。
			}
			if entryInfo.IsDir() {
				return nil
			}
			if entryInfo.Mode().IsRegular() {
				add(path, entryInfo)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Rel < files[j].Rel })
	return files, nil
}

// linkGuard "逐段 Lstat + 拒链接/reparse" 盘边门卫的参数组（波 1 修复 4，
// 2026-09-27）：恢复链（原 pending.go validateRestoreTarget）与快照观察链
// （原 validateWhitelistPathOnDisk）曾各持一份同构实现，构成双漂移面；合并为
// validatePathLinks 单函数后，两份实现的真实差异全集如下，全部入表：
//   - requireWhitelist：是否先行强制白名单前缀判定（观察链要求；恢复链在
//     readPendingRestore→normalizeWhitelistPath 上游已判，不重复设闸）；
//   - allowMissing：叶子及缺失祖先之下的段允许盘上不存在（git D 记录 / 恢复
//     目标尚未落写）；
//   - rejectSubject：拒绝错误措辞主体，保留两链既有文案口径不漂移。
//
// 等价性论证（合并前逐条核对）：恢复链"中间段缺失→continue 续查后续段"与
// 观察链"缺失段→立即放行"在真实文件系统上枚举等价——缺失段之下任何路径必
// 不存在，后续段只可能同样 IsNotExist，两式终态同为放行；统一按"缺失段放行
// 全链剩余"实现。
type linkGuard struct {
	allowMissing     bool
	requireWhitelist bool
	rejectSubject    string
}

var (
	// whitelistDiskGuard 快照观察面（PreviewRevision 盘上直读 / gitEngine.changes）：
	// 强制白名单；叶子默认必须存在于盘，删除记录由调用方显式开 allowMissing。
	whitelistDiskGuard = linkGuard{requireWhitelist: true, rejectSubject: "快照白名单"}
	// pendingTargetGuard pending 恢复链（writePendingTarget）：恢复目标允许尚未
	// 落写（叶子/中间段可缺），白名单已由上游归一闸门判过。
	pendingTargetGuard = linkGuard{allowMissing: true, rejectSubject: "恢复目标"}
)

// validatePathLinks 验证目标及每级现存祖先均不是链接/reparse point：不跟随
// 链接本身，但链上任一存在段是链接时，后续读写即可能越出数据根边界。
func validatePathLinks(dataDir, rel string, g linkGuard) error {
	if g.requireWhitelist && !Whitelisted(rel) {
		return fmt.Errorf("路径不在历史版本白名单内: %s", rel)
	}
	current := dataDir
	for _, segment := range strings.Split(filepath.ToSlash(rel), "/") {
		current = filepath.Join(current, segment)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			if !g.allowMissing {
				return err
			}
			return nil // 缺失段之下不可能存在任何条目，链上已无祖先可校验
		}
		if err != nil {
			return err
		}
		unsafe, err := unsafeLinkLike(current, info)
		if err != nil {
			return err
		}
		if unsafe {
			return fmt.Errorf("%s拒绝符号链接或 reparse point: %s", g.rejectSubject, current)
		}
	}
	return nil
}
