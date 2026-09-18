package snapshot

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	pendingRestoreDirName = "pending-restore"
	pendingPayloadName    = "payload"
	pendingManifestName   = "manifest.json"
)

type pendingRestoreManifest struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// StagePendingRestore 把 config/state 恢复内容发布为启动期一次性恢复包。
// memo 不使用此 API，仍由 MemoService 热恢复。
func StagePendingRestore(dataDir, rel string, data []byte) error {
	rel, err := normalizeWhitelistPath(rel)
	if err != nil {
		return err
	}
	if strings.HasPrefix(rel, memoPrefix) {
		return fmt.Errorf("便签必须走热恢复，不得进入 pending restore: %s", rel)
	}
	if rel != rootConfig && !strings.HasPrefix(rel, statePrefix) {
		return fmt.Errorf("pending restore 仅支持 config/state: %s", rel)
	}
	root := filepath.Join(dataDir, snapshotsDirName, pendingRestoreDirName)
	if err := os.MkdirAll(root, 0755); err != nil {
		return err
	}
	key := sha256.Sum256([]byte(rel))
	id := hex.EncodeToString(key[:16])
	stage, err := os.MkdirTemp(root, ".pending-"+id+"-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	payload := filepath.Join(stage, pendingPayloadName)
	if err := os.WriteFile(payload, data, 0600); err != nil {
		return err
	}
	if err := syncFile(payload); err != nil {
		return err
	}
	sum := sha256.Sum256(data)
	manifest := pendingRestoreManifest{Path: rel, SHA256: hex.EncodeToString(sum[:]), Size: int64(len(data))}
	raw, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	manifestPath := filepath.Join(stage, pendingManifestName)
	if err := os.WriteFile(manifestPath, raw, 0600); err != nil {
		return err
	}
	if err := syncFile(manifestPath); err != nil {
		return err
	}
	if err := syncDirectory(stage); err != nil {
		return err
	}
	final := filepath.Join(root, id+"-"+time.Now().Format(backupTimeLayout))
	for suffix := 2; ; suffix++ {
		if _, err := os.Lstat(final); os.IsNotExist(err) {
			break
		} else if err != nil {
			return err
		}
		final = filepath.Join(root, fmt.Sprintf("%s-%s-%d", id, time.Now().Format(backupTimeLayout), suffix))
	}
	if err := os.Rename(stage, final); err != nil {
		return err
	}
	return syncDirectory(root)
}

// ApplyPendingRestores 在 settings.Store 等内存态构造前调用，把已发布恢复包按发布时间
// 顺序原子写回数据根。成功应用后删除对应包；坏包返回错误并原地保留供诊断。
func ApplyPendingRestores(dataDir string) ([]string, error) {
	root := filepath.Join(dataDir, snapshotsDirName, pendingRestoreDirName)
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() && !strings.HasPrefix(entry.Name(), ".pending-") {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	applied := make([]string, 0, len(names))
	for _, name := range names {
		dir := filepath.Join(root, name)
		manifest, data, err := readPendingRestore(dir)
		if err != nil {
			return applied, fmt.Errorf("pending restore %s 无效: %w", name, err)
		}
		if err := writePendingTarget(dataDir, manifest.Path, data); err != nil {
			return applied, err
		}
		if err := os.RemoveAll(dir); err != nil {
			return applied, fmt.Errorf("清理已应用恢复包失败: %w", err)
		}
		applied = append(applied, manifest.Path)
	}
	return applied, nil
}

func readPendingRestore(dir string) (pendingRestoreManifest, []byte, error) {
	var manifest pendingRestoreManifest
	raw, err := os.ReadFile(filepath.Join(dir, pendingManifestName))
	if err != nil {
		return manifest, nil, err
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return manifest, nil, err
	}
	rel, err := normalizeWhitelistPath(manifest.Path)
	if err != nil || rel != manifest.Path || strings.HasPrefix(rel, memoPrefix) {
		return manifest, nil, fmt.Errorf("非法恢复路径 %q", manifest.Path)
	}
	data, err := os.ReadFile(filepath.Join(dir, pendingPayloadName))
	if err != nil {
		return manifest, nil, err
	}
	if int64(len(data)) != manifest.Size {
		return manifest, nil, fmt.Errorf("payload 大小不匹配")
	}
	sum := sha256.Sum256(data)
	if !strings.EqualFold(hex.EncodeToString(sum[:]), manifest.SHA256) {
		return manifest, nil, fmt.Errorf("payload 校验失败")
	}
	return manifest, data, nil
}

func writePendingTarget(dataDir, rel string, data []byte) error {
	if err := validateRestoreTarget(dataDir, rel); err != nil {
		return err
	}
	target := filepath.Join(dataDir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(target), ".snapshot-restore-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0644); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, target); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(target))
}

func validateRestoreTarget(dataDir, rel string) error {
	current := dataDir
	segments := strings.Split(filepath.ToSlash(rel), "/")
	for i, segment := range segments {
		current = filepath.Join(current, segment)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			if i == len(segments)-1 {
				return nil
			}
			continue
		}
		if err != nil {
			return err
		}
		unsafe, err := unsafeLinkLike(current, info)
		if err != nil {
			return err
		}
		if unsafe {
			return fmt.Errorf("恢复目标拒绝符号链接或 reparse point: %s", current)
		}
	}
	return nil
}
