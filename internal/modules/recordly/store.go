package recordly

import (
	"fmt"
	"path/filepath"
	"sync"

	"hanxi/internal/jsonstore"
)

// recordlyStore 持久化 Recordly 模块少量偏好：
// 位置 <dataDir>/recordly.json，存 releaseChannel（stable/beta）与 followOnExit。
// 无 activeVersion——NSIS oneClick 安装器语义决定托管目录恒为单一
// versions/recordly（多版本共存形同虚设，详见 version.Manager 包注释），
// "当前版本"由安装目录实测得出。
// 原子写（tmp+rename）与损坏容忍
// （解析失败按空配置继续，不阻断模块）收口至 internal/jsonstore。
type recordlyStore struct {
	filePath       string
	mu             sync.RWMutex
	releaseChannel string // "stable"（默认）| "beta"
	followOnExit   bool   // 默认 false：独立运行，不随 Hanxi 退出；true：随 Hanxi 退出一起关闭
}

type recordlyConfig struct {
	ReleaseChannel string `json:"releaseChannel"`
	FollowOnExit   *bool  `json:"followOnExit"`
}

func newRecordlyStore(dir string) *recordlyStore {
	s := &recordlyStore{
		filePath:       filepath.Join(dir, "recordly.json"),
		releaseChannel: ChannelStable,
		followOnExit:   false,
	}
	_ = s.load()
	return s
}

func (s *recordlyStore) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var cfg recordlyConfig
	if ok, err := jsonstore.Load(s.filePath, &cfg); err != nil || !ok {
		// 未初始化或损坏容忍：内容视为空，通道/开关注入默认值
		return err
	}
	switch cfg.ReleaseChannel {
	case ChannelStable, ChannelBeta:
		s.releaseChannel = cfg.ReleaseChannel
	}
	s.followOnExit = cfg.FollowOnExit != nil && *cfg.FollowOnExit
	return nil
}

func (s *recordlyStore) saveLocked() error {
	return jsonstore.Save(s.filePath, recordlyConfig{ReleaseChannel: s.releaseChannel, FollowOnExit: &s.followOnExit})
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

// GetFollowOnExit 返回"随 Hanxi 退出一起关闭"开关值（默认 false）。
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
