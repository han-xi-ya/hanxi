package snapshot

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"hanxi/internal/jsonstore"
)

// backupEngine：git 不可用（未安装 / Microsoft Store 假存根 / 仓库连炸）时的
// 降级影子拷贝（PLAN §3.3）。同一白名单逐个 copy 进
// `.snapshots/backup/<YYYYmmdd-HHMMSS>/`，白名单式拷贝天然规避
// "目标为源子集" 的自嵌套坑（.snapshots 不在白名单）；滚动保留最近 30 份。
// 与 gitEngine 实现同一 engine 接口，服务层两模式无感。

// backupKeepCount 降级链滚动份数（Q3 拍板）。
const backupKeepCount = 30

// manifestName 每份备份的内容指纹清单文件名（不进 revisions/revisionFiles 视野）。
const manifestName = "manifest.json"

type backupEngine struct {
	dataDir    string
	backupRoot string // <snapshotDir>/backup

	mu sync.Mutex // 与 gitEngine 同款仓库级串行闸（三源共一目录）
}

func newBackupEngine(dataDir, snapshotDir string) *backupEngine {
	return &backupEngine{
		dataDir:    dataDir,
		backupRoot: filepath.Join(snapshotDir, backupDirName),
	}
}

func (b *backupEngine) mode() string { return ModeBackup }

// changes 当前白名单文件指纹与最近一份 manifest 的差集（增/改/删都算变更）。
func (b *backupEngine) changes(ctx context.Context) ([]string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	cur, err := fingerprint(b.dataDir)
	if err != nil {
		return nil, err
	}
	prev, ok, err := b.latestManifest()
	if err != nil {
		return nil, err
	}
	if !ok {
		// 从无备份：有任何白名单文件即待拍
		var files []string
		for p := range cur {
			files = append(files, p)
		}
		sort.Strings(files)
		return files, nil
	}
	var files []string
	for p, sum := range cur {
		if prev[p] != sum {
			files = append(files, p)
		}
	}
	for p := range prev {
		if _, alive := cur[p]; !alive {
			files = append(files, p)
		}
	}
	sort.Strings(files)
	return files, nil
}

// commit 全量白名单影子拷贝进新时间戳目录 + manifest，再滚动修剪。
func (b *backupEngine) commit(ctx context.Context, _ []string) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	cur, err := fingerprint(b.dataDir)
	if err != nil {
		return err
	}
	if len(cur) == 0 {
		return nil // 白名单为空：拍一份空目录没有意义
	}
	dir, err := b.newBackupDir()
	if err != nil {
		return err
	}
	for rel := range cur {
		if err := copyFile(
			filepath.Join(b.dataDir, filepath.FromSlash(rel)),
			filepath.Join(dir, filepath.FromSlash(rel)),
		); err != nil {
			return fmt.Errorf("影子拷贝 %s 失败: %w", rel, err)
		}
	}
	if err := jsonstore.Save(filepath.Join(dir, manifestName), cur); err != nil {
		return fmt.Errorf("写备份清单失败: %w", err)
	}
	return b.prune()
}

// revisions 时间戳目录列表（字典序即时间序，倒序=新→旧）。
func (b *backupEngine) revisions(ctx context.Context, limit int) ([]Revision, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	dirs, err := b.backupDirs()
	if err != nil {
		return nil, err
	}
	sort.Sort(sort.Reverse(sort.StringSlice(dirs)))
	if len(dirs) > limit {
		dirs = dirs[:limit]
	}
	out := make([]Revision, 0, len(dirs))
	for _, d := range dirs {
		manifest, _, merr := b.readManifest(d)
		summary := "备份目录"
		if merr == nil {
			summary = fmt.Sprintf("%d 个文件", len(manifest))
		}
		out = append(out, Revision{
			ID:      d,
			Time:    parseBackupDirTime(d).Format(time.RFC3339),
			Summary: summary,
		})
	}
	return out, nil
}

// revisionFiles 该份备份包含的文件清单（备份模式无逐文件变化语义，恒 M）。
func (b *backupEngine) revisionFiles(ctx context.Context, id string) ([]RevisionFile, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	manifest, ok, err := b.readManifest(id)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("备份版本 %s 不存在", id)
	}
	paths := make([]string, 0, len(manifest))
	for p := range manifest {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	out := make([]RevisionFile, 0, len(paths))
	for _, p := range paths {
		out = append(out, RevisionFile{Path: p, Status: "M"})
	}
	return out, nil
}

// file 读某份备份中的文件字节。rel 先过白名单+穿越双闸（引擎侧防御，
// 不依赖服务层先行校验的时序）。
func (b *backupEngine) file(ctx context.Context, id, relPath string) ([]byte, error) {
	if !Whitelisted(relPath) || strings.Contains(relPath, "..") {
		return nil, fmt.Errorf("非法的恢复路径: %s", relPath)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if !backupIDRe.MatchString(id) {
		return nil, fmt.Errorf("备份模式版本标识应为时间戳目录: %s", id)
	}
	target := filepath.Join(b.backupRoot, id, filepath.FromSlash(relPath))
	// Join 后仍须落在该份备份目录内（双保险）
	absDir, _ := filepath.Abs(filepath.Join(b.backupRoot, id))
	absTarget, _ := filepath.Abs(target)
	if !strings.HasPrefix(absTarget, absDir+string(os.PathSeparator)) {
		return nil, fmt.Errorf("非法的恢复路径: %s", relPath)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		return nil, fmt.Errorf("该版本中不存在 %s: %w", relPath, err)
	}
	return data, nil
}

// heal 备份目录无"仓库损坏"概念（每份独立时间戳目录），no-op。
func (b *backupEngine) heal(ctx context.Context) {}

// ---------- 内部工具 ----------

// fingerprint 当前白名单文件的 sha256 指纹表（rel → hex）。
// 白名单 JSON 均 KB 级，全量哈希成本可忽略，换来"内容相同即无变更"的精确语义
// （mtime 跳变但字节不变——如 wechat 原样回写——不会灌碎历史）。
func fingerprint(dataDir string) (map[string]string, error) {
	out := make(map[string]string)
	consider := func(rel string) {
		sum, err := hashFile(filepath.Join(dataDir, filepath.FromSlash(rel)))
		if err != nil {
			return // 原子写 rename 竞态：下一拍再看
		}
		out[rel] = sum
	}
	if _, err := os.Stat(filepath.Join(dataDir, rootConfig)); err == nil {
		consider(rootConfig)
	}
	for _, dir := range []string{rootState, rootMemo} {
		base := filepath.Join(dataDir, dir)
		_ = filepath.WalkDir(base, func(p string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			rel, rerr := filepath.Rel(dataDir, p)
			if rerr == nil {
				rel = filepath.ToSlash(rel)
				if Whitelisted(rel) {
					consider(rel)
				}
			}
			return nil
		})
	}
	return out, nil
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0644)
}

// backupDirs 列出现存合法时间戳备份目录名。
func (b *backupEngine) backupDirs() ([]string, error) {
	entries, err := os.ReadDir(b.backupRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() && backupIDRe.MatchString(e.Name()) {
			out = append(out, e.Name())
		}
	}
	return out, nil
}

// newBackupDir 创建下一份时间戳目录（同秒撞名追加 -n 序号）。
func (b *backupEngine) newBackupDir() (string, error) {
	if err := os.MkdirAll(b.backupRoot, 0755); err != nil {
		return "", err
	}
	base := time.Now().Format(backupTimeLayout)
	name := base
	for i := 2; ; i++ {
		if _, err := os.Stat(filepath.Join(b.backupRoot, name)); os.IsNotExist(err) {
			break
		} else if err != nil {
			return "", err
		}
		name = fmt.Sprintf("%s-%d", base, i)
	}
	dir := filepath.Join(b.backupRoot, name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	return dir, nil
}

// latestManifest 最近一份备份的指纹清单（无备份返回 ok=false）。
func (b *backupEngine) latestManifest() (map[string]string, bool, error) {
	dirs, err := b.backupDirs()
	if err != nil || len(dirs) == 0 {
		return nil, false, err
	}
	sort.Strings(dirs)
	return b.readManifest(dirs[len(dirs)-1])
}

func (b *backupEngine) readManifest(dirName string) (map[string]string, bool, error) {
	data, err := os.ReadFile(filepath.Join(b.backupRoot, dirName, manifestName))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	m := map[string]string{}
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, false, nil // 清单损坏：视作无基线（下拍重建覆盖）
	}
	return m, true, nil
}

// prune 滚动修剪：字典序倒排（新在前），保留最近 backupKeepCount 份。
func (b *backupEngine) prune() error {
	dirs, err := b.backupDirs()
	if err != nil {
		return err
	}
	sort.Strings(dirs)
	if len(dirs) <= backupKeepCount {
		return nil
	}
	var errs []error
	for _, d := range dirs[:len(dirs)-backupKeepCount] {
		if err := os.RemoveAll(filepath.Join(b.backupRoot, d)); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

// parseBackupDirTime 时间戳目录名 → 本地时刻（非法形态回落零值由调用方容错）。
func parseBackupDirTime(name string) time.Time {
	core := name
	if i := strings.Index(name, "-"); i > 0 {
		if j := strings.Index(name[i+1:], "-"); j >= 0 {
			core = name[:i+1+j] // 剥离 -n 序号后缀
		}
	}
	t, err := time.ParseInLocation(backupTimeLayout, core, time.Local)
	if err != nil {
		return time.Unix(0, 0)
	}
	return t
}
