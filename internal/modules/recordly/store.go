package recordly

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// recordlyStore 持久化 Recordly 模块少量偏好：
// 位置 <dataDir>/recordly.json，存 releaseChannel（stable/beta）与 followOnExit。
// 无 activeVersion——NSIS oneClick 安装器语义决定托管目录恒为单一
// versions/recordly（多版本共存形同虚设，详见 version.Manager 包注释），
// "当前版本"由安装目录实测得出。
// 与 ccswitchStore/markeronStore 同款原子写（tmp+rename），损坏容忍
// （解析失败按空配置继续，不阻断模块）。
type recordlyStore struct {
	filePath       string
	mu             sync.RWMutex
	releaseChannel string // "stable"（默认）| "beta"
	followOnExit   bool   // 默认 true：随 Hanxi 退出一起关闭；false：独立运行
}

type recordlyConfig struct {
	ReleaseChannel string `json:"releaseChannel"`
	FollowOnExit   *bool  `json:"followOnExit"`
}

func newRecordlyStore(dir string) *recordlyStore {
	s := &recordlyStore{
		filePath:       filepath.Join(dir, "recordly.json"),
		releaseChannel: ChannelStable,
		followOnExit:   true,
	}
	_ = s.load()
	return s
}

func (s *recordlyStore) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	bytes, err := os.ReadFile(s.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var cfg recordlyConfig
	if json.Unmarshal(bytes, &cfg) != nil {
		// 损坏容忍：内容视为空，通道/开关注入默认值
		return nil
	}
	switch cfg.ReleaseChannel {
	case ChannelStable, ChannelBeta:
		s.releaseChannel = cfg.ReleaseChannel
	}
	s.followOnExit = cfg.FollowOnExit == nil || *cfg.FollowOnExit
	return nil
}

func (s *recordlyStore) saveLocked() error {
	dir := filepath.Dir(s.filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	bytes, err := json.MarshalIndent(recordlyConfig{ReleaseChannel: s.releaseChannel, FollowOnExit: &s.followOnExit}, "", "  ")
	if err != nil {
		return err
	}
	tmp := fmt.Sprintf("%s.tmp.%d", s.filePath, os.Getpid())
	if err := os.WriteFile(tmp, bytes, 0644); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.filePath); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// GetReleaseChannel 返回当前更新通道（"stable" | "beta"）。
func (s *recordlyStore) GetReleaseChannel() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.releaseChannel
}

// SetReleaseChannel 设定更新通道并立即落盘（非法值拒绝）。
func (s *recordlyStore) SetReleaseChannel(channel string) error {
	if channel != ChannelStable && channel != ChannelBeta {
		return fmt.Errorf("非法更新通道: %q（仅支持 %s/%s）", channel, ChannelStable, ChannelBeta)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.releaseChannel = channel
	return s.saveLocked()
}

// GetFollowOnExit 返回"随 Hanxi 退出一起关闭"开关值（默认 true）。
func (s *recordlyStore) GetFollowOnExit() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.followOnExit
}

// SetFollowOnExit 设定开关并立即落盘。
func (s *recordlyStore) SetFollowOnExit(b bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.followOnExit = b
	return s.saveLocked()
}
