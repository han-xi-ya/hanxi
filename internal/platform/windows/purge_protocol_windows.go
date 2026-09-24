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
	PurgeProtocolVersion = 1
	purgeProtocolVersion = PurgeProtocolVersion
	purgeResultPrefix    = "hanxi-purge-"
	purgeResultMaxBytes  = 16 << 10
	purgeResultMaxAge    = 24 * time.Hour
)

// PurgeResultFile 是一次性 helper 与宿主之间的私有 JSON 握手载荷。
// 类型虽然导出以便 sysinfo service 读取，字段只描述结果，不暴露 helper 内部实现。
type PurgeResultFile struct {
	ProtocolVersion      int    `json:"protocolVersion"`
	RequestID            string `json:"requestId"`
	State                string `json:"state"`
	BeforeAvailableBytes uint64 `json:"beforeAvailableBytes"`
	AfterAvailableBytes  uint64 `json:"afterAvailableBytes"`
	Message              string `json:"message"`
	ErrorCode            string `json:"errorCode,omitempty"`
	Elevated             bool   `json:"elevated"`
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

func WritePurgeResult(path, requestID, state, message, errorCode string, before, after uint64, elevated bool) error {
	if !validPurgeRequestID(requestID) {
		return errors.New("非法 purge request ID")
	}
	base := filepath.Base(path)
	if path != filepath.Join(filepath.Dir(path), base) || !strings.HasPrefix(base, purgeResultPrefix+requestID) || !strings.HasSuffix(base, ".json") {
		return errors.New("purge 结果路径必须位于其 request ID 的固定文件名下")
	}
	payload := PurgeResultFile{ProtocolVersion: purgeProtocolVersion, RequestID: requestID, State: state, Message: message, ErrorCode: errorCode, BeforeAvailableBytes: before, AfterAvailableBytes: after, Elevated: elevated}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func ReadPurgeResult(path, requestID string) (PurgeResultFile, error) {
	if filepath.Base(path) == "." || filepath.Base(path) == ".." {
		return PurgeResultFile{}, errors.New("非法 purge 结果路径")
	}
	st, err := os.Stat(path)
	if err != nil {
		return PurgeResultFile{}, err
	}
	if st.Size() <= 0 || st.Size() > purgeResultMaxBytes {
		return PurgeResultFile{}, errors.New("purge 结果文件大小异常")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return PurgeResultFile{}, err
	}
	var result PurgeResultFile
	if err := json.Unmarshal(data, &result); err != nil {
		return PurgeResultFile{}, fmt.Errorf("purge 结果 JSON 损坏: %w", err)
	}
	if result.ProtocolVersion != purgeProtocolVersion || result.RequestID != requestID {
		return PurgeResultFile{}, errors.New("purge 结果协议或 request ID 不匹配")
	}
	switch result.State {
	case "success", "denied", "failed", "cancelled":
	default:
		return PurgeResultFile{}, fmt.Errorf("未知 purge 结果状态 %q", result.State)
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
