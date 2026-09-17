package paseo

import (
	"fmt"
	"path/filepath"
	"sync"

	"hanxi/internal/jsonstore"
)

// paseoStore 持久化 Paseo 模块少量偏好：
// 位置 <stateDir>/paseo.json，存 activeVersion、releaseChannel（stable/beta）与
// followOnExit。多版本目录（versions/paseo_X.Y.Z）并存，activeVersion 空串 =
// 未指定，冷启动自动回退最新已装（vscode 便携同款语义）。
// 原子写（tmp+rename）与损坏容忍
// （解析失败按空配置继续，不阻断模块）收口至 internal/jsonstore。
type paseoStore struct {
	filePath       string
	mu             sync.RWMutex
	activeVersion  string
	releaseChannel string // "stable"（默认）| "beta"
	followOnExit   bool   // 默认 false：独立运行，不随 Hanxi 退出；true：随 Hanxi 退出一起关闭
}

type paseoConfig struct {
	ActiveVersion  string `json:"activeVersion"`
	ReleaseChannel string `json:"releaseChannel"`
	FollowOnExit   *bool  `json:"followOnExit"`
}

func newPaseoStore(dir string) *paseoStore {
	s := &paseoStore{
		filePath:       filepath.Join(dir, "paseo.json"),
		releaseChannel: ChannelStable,
		followOnExit:   false,
	}
	_ = s.load()
	return s
}

func (s *paseoStore) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var cfg paseoConfig
	if ok, err := jsonstore.Load(s.filePath, &cfg); err != nil || !ok {
		// 未初始化或损坏容忍：内容视为空，activeVersion 自然兜底到"自动最新已装"
		return err
	}
	switch cfg.ReleaseChannel {
	case ChannelStable, ChannelBeta:
		s.releaseChannel = cfg.ReleaseChannel
	}
	s.activeVersion = cfg.ActiveVersion
	s.followOnExit = cfg.FollowOnExit != nil && *cfg.FollowOnExit
	return nil
}

func (s *paseoStore) saveLocked() error {
	return jsonstore.Save(s.filePath, paseoConfig{
		ActiveVersion:  s.activeVersion,
		ReleaseChannel: s.releaseChannel,
		FollowOnExit:   &s.followOnExit,
	})
}

// GetActive 返回当前设定的使用版本（空字符串 = 未指定，回退最新已装）。
func (s *paseoStore) GetActive() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.activeVersion
}

// SetActive 设定使用版本并立即落盘（空串 = 恢复自动最新）。
func (s *paseoStore) SetActive(version string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.activeVersion = version
	return s.saveLocked()
}

// GetReleaseChannel 返回当前更新通道（"stable" | "beta"）。
func (s *paseoStore) GetReleaseChannel() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.releaseChannel
}

// SetReleaseChannel 设定更新通道并立即落盘（非法值拒绝）。
func (s *paseoStore) SetReleaseChannel(channel string) error {
	if channel != ChannelStable && channel != ChannelBeta {
		return fmt.Errorf("非法更新通道: %q（仅支持 %s/%s）", channel, ChannelStable, ChannelBeta)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.releaseChannel = channel
	return s.saveLocked()
}

// GetFollowOnExit 返回"随 Hanxi 退出一起关闭"开关值（默认 false）。
func (s *paseoStore) GetFollowOnExit() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.followOnExit
}

// SetFollowOnExit 设定开关并立即落盘。
func (s *paseoStore) SetFollowOnExit(b bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.followOnExit = b
	return s.saveLocked()
}
