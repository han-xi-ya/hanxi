//go:build windows

package windows

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	purgeProtocolVersion = 1
	purgeResultPrefix    = "hanxi-purge-"
	purgeResultMaxBytes  = 16 << 10
	purgeResultMaxAge    = 24 * time.Hour
)

type purgeResultFile struct {
	ProtocolVersion      int    `json:"protocolVersion"`
	RequestID            string `json:"requestId"`
	State                string `json:"state"`
	BeforeAvailableBytes uint64 `json:"beforeAvailableBytes"`
	AfterAvailableBytes  uint64 `json:"afterAvailableBytes"`
	Message              string `json:"message"`
	ErrorCode            string `json:"errorCode,omitempty"`
}

// NewPurgeRequestFile 在 runtime 目录创建唯一结果文件。文件先占位，helper 只允许
// 原子替换同名目标；路径不从前端传入，避免任意路径写入。
func NewPurgeRequestFile(dir, requestID string) (string, error) {
	if !validPurgeRequestID(requestID) {
		return "", errors.New("无效的 purge request ID")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("创建 purge 运行目录失败: %w", err)
	}
	f, err := os.CreateTemp(dir, purgeResultPrefix+requestID+"-*.json")
	if err != nil {
		return "", fmt.Errorf("创建 purge 结果文件失败: %w", err)
	}
	name := f.Name()
	if err := f.Close(); err != nil {
		_ = os.Remove(name)
		return "", err
	}
	return name, nil
}

func validPurgeRequestID(id string) bool {
	if len(id) < 16 || len(id) > 80 {
		return false
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') && r != '-' {
			return false
		}
	}
	return true
}

// ReadPurgeResult 严格读取 helper 结果：大小、协议版本、request ID、state 均校验。
func ReadPurgeResult(path, requestID string) (purgeResultFile, error) {
	if filepath.Base(path) == "." || filepath.Base(path) == ".." {
		return purgeResultFile{}, errors.New("非法 purge 结果路径")
	}
	st, err := os.Stat(path)
	if err != nil {
		return purgeResultFile{}, err
	}
	if st.Size() <= 0 || st.Size() > purgeResultMaxBytes {
		return purgeResultFile{}, errors.New("purge 结果文件大小异常")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return purgeResultFile{}, err
	}
	var result purgeResultFile
	if err := json.Unmarshal(data, &result); err != nil {
		return purgeResultFile{}, fmt.Errorf("purge 结果 JSON 损坏: %w", err)
	}
	if result.ProtocolVersion != purgeProtocolVersion || result.RequestID != requestID {
		return purgeResultFile{}, errors.New("purge 结果协议或 request ID 不匹配")
	}
	switch result.State {
	case "success", "denied", "failed", "cancelled":
	default:
		return purgeResultFile{}, fmt.Errorf("未知 purge 结果状态 %q", result.State)
	}
	return result, nil
}

// CleanupStalePurgeResults 删除 runtime 中固定前缀且超龄的普通文件；目录/链接与
// 非固定命名一律不碰。调用方可在主进程启动时显式调用，helper 结果不会混入下载收尸器。
func CleanupStalePurgeResults(dir string, now time.Time) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var removed []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), purgeResultPrefix) || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		info, err := entry.Info()
		if err != nil || now.Sub(info.ModTime()) < purgeResultMaxAge {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		if os.Remove(path) == nil {
			removed = append(removed, path)
		}
	}
	return removed
}
