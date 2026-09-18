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
			unsafe, err := unsafeLinkLike(path, entryInfo)
			if err != nil {
				if strict {
					return err
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

// validateWhitelistPathOnDisk 验证目标及每级现存祖先均不是链接/reparse point。
// allowMissing 用于已跟踪文件删除：叶子可不存在，但祖先边界仍须安全。
func validateWhitelistPathOnDisk(dataDir, rel string, allowMissing bool) error {
	if !Whitelisted(rel) {
		return fmt.Errorf("路径不在历史版本白名单内: %s", rel)
	}
	current := dataDir
	for _, segment := range strings.Split(filepath.ToSlash(rel), "/") {
		current = filepath.Join(current, segment)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) && allowMissing {
			return nil
		}
		if err != nil {
			return err
		}
		unsafe, err := unsafeLinkLike(current, info)
		if err != nil {
			return err
		}
		if unsafe {
			return fmt.Errorf("快照白名单拒绝符号链接或 reparse point: %s", current)
		}
	}
	return nil
}
