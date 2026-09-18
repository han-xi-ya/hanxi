package settings

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"hanxi/internal/product"
)

// 本文件实现 F6 存量迁移（硬问题②的裁定）：首启若发现 %APPDATA%\Hanxi 旧家
// 携带有效数据且新家尚未安身，做一次性静默搬迁（move 语义），避免会话用户
// 升级即"数据蒸发"。自 F6 起旧家不再作为"家"使用，此处只充当搬迁来源；
// 旧便携包 data/ 按裁定③彻底不认不搬（仅 paths.go 首启告警指路）。

// legacyAppDataHome 定位旧标准模式家 %APPDATA%\Hanxi；%APPDATA% 缺失返回空串。
func legacyAppDataHome(appData string) string {
	appData = strings.TrimSpace(appData)
	if appData == "" {
		return ""
	}
	return filepath.Join(appData, product.DataDirName)
}

// shouldMigrateLegacyHome 搬迁闸门（纯判定，四道全过才搬）：
//   - 旧家可定位且旧家 ≠ 新家（大小写不敏感，Windows 路径口径）；
//   - 新家不在旧家之内——exe 恰好住在 %APPDATA%\Hanxi 下时搬家等于把母亲搬进
//     子宫，直接放弃并交由人工；
//   - 旧家携带数据根特征（config.json 或 versions/）——空壳旧家不值得动手；
//   - 新家尚未安身（无数据根特征）——防止降级回滚期间反向污染新家。
func shouldMigrateLegacyHome(oldHome, newHome string) bool {
	if oldHome == "" || strings.EqualFold(filepath.Clean(oldHome), filepath.Clean(newHome)) {
		return false
	}
	if rel, err := filepath.Rel(oldHome, newHome); err == nil && !strings.HasPrefix(rel, "..") {
		return false
	}
	return isHanxiDataRoot(oldHome) && !isHanxiDataRoot(newHome)
}

// migrateLegacyHome 搬迁执行入口（InitPaths 在 ensureDirs 之前调用，
// 时机即"新家还没被派生目录污染"）。一切失败不阻断启动：聚合告警留痕，
// 旧家数据永远保持可找回。
func migrateLegacyHome(newHome string) {
	oldHome := legacyAppDataHome(os.Getenv("APPDATA"))
	journalExists := false
	if _, err := os.Stat(filepath.Join(newHome, migrationJournalName)); err == nil {
		journalExists = true
	}
	if !journalExists && !shouldMigrateLegacyHome(oldHome, newHome) {
		return
	}
	slog.Info("detected legacy %APPDATA% home, migrating to new data root once",
		"from", oldHome, "to", newHome)
	if err := migrateFromLegacyHome(oldHome, newHome); err != nil {
		slog.Warn("legacy home migration incomplete, remaining entries kept in place for manual recovery",
			"from", oldHome, "err", err)
	}
}

const migrationJournalName = ".hanxi-home-migration.json"

type migrationJournal struct {
	Pending []string `json:"pending"`
}

var (
	moveLegacyPath    = movePath
	replaceAtomicFile = replaceFileOS
)

// migrateFromLegacyHome 把旧家顶层条目逐一搬进新家。pending journal 在每项
// 成功后立即落盘，因此后项失败或进程中断时，下次只续跑未完成项。目标冲突会
// 保留在 pending，绝不把迁移宣布完成；journal 仅在 pending 清空后删除。
func migrateFromLegacyHome(oldHome, newHome string) error {
	if err := os.MkdirAll(newHome, 0755); err != nil {
		return fmt.Errorf("创建新家 %s 失败: %w", newHome, err)
	}
	journalPath := filepath.Join(newHome, migrationJournalName)
	pending, err := loadOrCreateMigrationJournal(oldHome, journalPath)
	if err != nil {
		return err
	}

	var moved int
	var errs []error
	for len(pending) > 0 {
		name := pending[0]
		src := filepath.Join(oldHome, name)
		dst := filepath.Join(newHome, name)
		if _, err := os.Stat(dst); err == nil {
			errs = append(errs, fmt.Errorf("%s: 新家存在同名条目，保留待处理", name))
			break
		} else if !os.IsNotExist(err) {
			errs = append(errs, fmt.Errorf("%s: 检查新家目标失败: %w", name, err))
			break
		}
		if _, err := os.Stat(src); os.IsNotExist(err) {
			// 上次已完成移动、但在 journal 更新前中断：按已提交恢复。
			pending = pending[1:]
			if err := saveMigrationJournal(journalPath, pending); err != nil {
				return fmt.Errorf("更新搬迁 journal 失败: %w", err)
			}
			continue
		} else if err != nil {
			errs = append(errs, fmt.Errorf("%s: 检查旧家源失败: %w", name, err))
			break
		}
		if err := moveLegacyPath(src, dst); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", name, err))
			break
		}
		moved++
		pending = pending[1:]
		if err := saveMigrationJournal(journalPath, pending); err != nil {
			return fmt.Errorf("%s 已搬迁但更新 journal 失败: %w", name, err)
		}
	}

	if len(pending) == 0 {
		if err := os.Remove(journalPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("清理搬迁 journal 失败: %w", err)
		}
		if err := os.Remove(oldHome); err != nil && !os.IsNotExist(err) {
			slog.Debug("legacy home dir not empty or removable, left as-is", "path", oldHome, "err", err)
		}
	}
	slog.Info("legacy home migration pass finished", "moved", moved, "pending", len(pending), "failed", len(errs))
	return errors.Join(errs...)
}

func loadOrCreateMigrationJournal(oldHome, journalPath string) ([]string, error) {
	raw, err := os.ReadFile(journalPath)
	if err == nil {
		var journal migrationJournal
		if err := json.Unmarshal(raw, &journal); err != nil {
			return nil, fmt.Errorf("解析搬迁 journal 失败: %w", err)
		}
		return journal.Pending, nil
	}
	if !os.IsNotExist(err) {
		return nil, fmt.Errorf("读取搬迁 journal 失败: %w", err)
	}
	entries, err := os.ReadDir(oldHome)
	if err != nil {
		return nil, fmt.Errorf("读取旧家 %s 失败: %w", oldHome, err)
	}
	pending := make([]string, 0, len(entries))
	for _, entry := range entries {
		pending = append(pending, entry.Name())
	}
	sort.Strings(pending)
	if err := saveMigrationJournal(journalPath, pending); err != nil {
		return nil, fmt.Errorf("创建搬迁 journal 失败: %w", err)
	}
	return pending, nil
}

func saveMigrationJournal(path string, pending []string) error {
	raw, err := json.Marshal(migrationJournal{Pending: pending})
	if err != nil {
		return err
	}
	return writeAtomicFile(path, append(raw, '\n'))
}

// movePath 搬迁单个顶层条目：rename 直通，跨卷（%APPDATA% 在 C 盘、新家
// 在外置盘等）落到 copy+delete，复制失败源不动。
func movePath(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	if err := copyPath(src, dst); err != nil {
		// 复制半途失败：清掉半成品，旧家原件仍在。
		os.RemoveAll(dst)
		return err
	}
	// 复制全成才删源（目录子树整体删）；此点之后新家已持有完整副本。
	return os.RemoveAll(src)
}

// copyPath 递归复制文件树（仅目录与常规文件；异常条目判错，宁可不搬）。
func copyPath(src, dst string) error {
	fi, err := os.Lstat(src)
	if err != nil {
		return err
	}
	switch {
	case fi.IsDir():
		if err := os.MkdirAll(dst, 0755); err != nil {
			return err
		}
		entries, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if err := copyPath(filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())); err != nil {
				return err
			}
		}
		return nil
	case fi.Mode().IsRegular():
		return copyFile(src, dst, fi.Mode().Perm())
	default:
		return fmt.Errorf("不支持搬迁的条目类型: %s", src)
	}
}

func copyFile(src, dst string, perm fs.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func writeAtomicFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}
	if _, err := tmp.Write(data); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Sync(); err != nil {
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := replaceAtomicFile(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}
