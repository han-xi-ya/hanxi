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
	// PurgeProtocolVersion v2：载荷新增 mode/前后账目/工作集计数（读写两端同仓
	// 同版本发布，v1 载荷因版本严格相等校验自然拒收，不做跨版本兼容层）。
	PurgeProtocolVersion     = 2
	purgeProtocolVersion     = PurgeProtocolVersion
	purgeResultPrefix        = "hanxi-purge-"
	purgeResultMaxBytes      = 16 << 10
	purgeResultMaxAge        = 24 * time.Hour
	purgeStateSuccess        = "success"
	purgeStateDenied         = "denied"
	purgeStateFailed         = "failed"
	purgeStateCancelled      = "cancelled"
	purgeModeStandby         = PurgeHelperMode
	purgeModeWorkingSets     = EmptyWorkingSetsHelperMode
	purgeModeLegacyOrStandby = "" // 兼容空位：无 mode 的载荷按待机清理理解
)

// PurgeResultFile 是一次性 helper 与宿主之间的私有 JSON 握手载荷。
// 类型虽然导出以便 sysinfo service 读取，字段只描述结果，不暴露 helper 内部实现。
// Before/AfterAvailableBytes 保持 v1 语义（可用内存前后观测）；BeforeLedger/
// AfterLedger 是逐项链账（总量/可用/可实测的待机与修改页），两种 mode 共用。
type PurgeResultFile struct {
	ProtocolVersion      int          `json:"protocolVersion"`
	Mode                 string       `json:"mode,omitempty"`
	RequestID            string       `json:"requestId"`
	State                string       `json:"state"`
	BeforeAvailableBytes uint64       `json:"beforeAvailableBytes"`
	AfterAvailableBytes  uint64       `json:"afterAvailableBytes"`
	BeforeLedger         MemoryLedger `json:"beforeLedger"`
	AfterLedger          MemoryLedger `json:"afterLedger"`
	EmptiedProcessCount  int          `json:"emptiedProcessCount,omitempty"`
	SkippedProcessCount  int          `json:"skippedProcessCount,omitempty"`
	Message              string       `json:"message"`
	ErrorCode            string       `json:"errorCode,omitempty"`
	Elevated             bool         `json:"elevated"`
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

// validPurgeMode 单飞互斥的协议面守卫：mode 必须落在已知两值（或空位）。
func validPurgeMode(mode string) bool {
	switch mode {
	case purgeModeStandby, purgeModeWorkingSets, purgeModeLegacyOrStandby:
		return true
	}
	return false
}

// WritePurgeResult 原子写回结果载荷。协议版本由本函数统一盖章；path 必须位于
// 该 request ID 的固定文件名下（防串写）。
func WritePurgeResult(path string, result PurgeResultFile) error {
	if !validPurgeRequestID(result.RequestID) {
		return errors.New("非法 purge request ID")
	}
	if !validPurgeMode(result.Mode) {
		return fmt.Errorf("非法 purge mode %q", result.Mode)
	}
	base := filepath.Base(path)
	if path != filepath.Join(filepath.Dir(path), base) || !strings.HasPrefix(base, purgeResultPrefix+result.RequestID) || !strings.HasSuffix(base, ".json") {
		return errors.New("purge 结果路径必须位于其 request ID 的固定文件名下")
	}
	result.ProtocolVersion = purgeProtocolVersion
	data, err := json.Marshal(result)
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
	if !validPurgeMode(result.Mode) {
		return PurgeResultFile{}, fmt.Errorf("未知 purge mode %q", result.Mode)
	}
	switch result.State {
	case purgeStateSuccess, purgeStateDenied, purgeStateFailed, purgeStateCancelled:
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
