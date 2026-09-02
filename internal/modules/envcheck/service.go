package envcheck

import (
	"context"
	"fmt"
	"sync"
	"time"

	"hanxi/internal/modules/envcheck/detect"
	"hanxi/internal/modules/envcheck/gitversion"
	"hanxi/internal/modules/envcheck/goversion"
	"hanxi/internal/modules/envcheck/nodeversion"
	"hanxi/internal/modules/envcheck/remoteversion"
)

// urlOpener 是 EnvCheckService 打开固定官网地址所需的最小平台能力。
type urlOpener interface {
	OpenURL(url string) error
}

// EnvCheckService Wails 绑定服务：本机开发工具链探测与 Git、Go、Node.js 官网版本查询。
type EnvCheckService struct {
	opener         urlOpener
	detectOne      func(context.Context, string) (detect.ToolInfo, error)
	recentReleases func() ([]gitversion.Release, error)
	goChannels     func() ([]remoteversion.Channel, bool, time.Time, error)
	nodeChannels   func() ([]remoteversion.Channel, bool, time.Time, error)
}

func NewEnvCheckService(opener urlOpener) *EnvCheckService {
	return &EnvCheckService{
		opener:         opener,
		detectOne:      detect.RunOne,
		recentReleases: gitversion.RecentReleases,
		goChannels:     goversion.Channels,
		nodeChannels:   nodeversion.Channels,
	}
}

// DetectAll 并发探测全部已注册工具，同步返回完整列表（前端主入口）。
func (s *EnvCheckService) DetectAll() []detect.ToolInfo {
	return detect.RunAll(context.Background())
}

// Detect 按注册名探测单个工具（预留单卡刷新扩展），未知名返回错误。
func (s *EnvCheckService) Detect(name string) (detect.ToolInfo, error) {
	return s.detectOne(context.Background(), name)
}

// GetGitForWindowsOverview 并发查询本机 Git 与官网近期稳定版本。
func (s *EnvCheckService) GetGitForWindowsOverview() (gitversion.Overview, error) {
	var (
		local       detect.ToolInfo
		localErr    error
		releases    []gitversion.Release
		releasesErr error
		wg          sync.WaitGroup
	)
	wg.Go(func() {
		local, localErr = s.detectOne(context.Background(), "git")
	})
	wg.Go(func() {
		releases, releasesErr = s.recentReleases()
	})
	wg.Wait()

	if localErr != nil {
		return gitversion.Overview{}, fmt.Errorf("检测本机 Git 失败: %w", localErr)
	}
	if releasesErr != nil {
		return gitversion.Overview{Local: local, Relation: gitversion.RelationUnknown}, releasesErr
	}
	overview := gitversion.Overview{
		Local:    local,
		Releases: releases,
		Relation: gitversion.RelationUnknown,
	}
	if len(releases) > 0 {
		overview.IsStale = releases[0].Stale
		overview.Relation = gitversion.RelationForLocal(
			local.Version,
			local.Status == detect.StatusInstalled,
			releases[0].Version,
		)
	}
	return overview, nil
}

// OpenGitForWindowsDownloadPage 使用系统默认浏览器打开固定官方下载页。
func (s *EnvCheckService) OpenGitForWindowsDownloadPage() error {
	return s.openURL("Git", gitversion.DownloadPageURL())
}

// GetGoOverview 并发查询本机 Go 与官网 Stable/Oldstable 版本。
func (s *EnvCheckService) GetGoOverview() (goversion.Overview, error) {
	local, channels, stale, fetchedAt, err := s.getChannelOverview("go", s.goChannels, goversion.Compare)
	if err != nil {
		return goversion.Overview{Local: local}, err
	}
	return goversion.Overview{
		Local: local, Channels: channels, IsStale: stale, FetchedAt: formatFetchedAt(fetchedAt),
	}, nil
}

// OpenGoDownloadPage 使用系统默认浏览器打开固定 Go 官方下载页。
func (s *EnvCheckService) OpenGoDownloadPage() error {
	return s.openURL("Go", goversion.DownloadPageURL())
}

// GetNodeOverview 并发查询本机 Node.js 与官网 LTS/Current 版本。
func (s *EnvCheckService) GetNodeOverview() (nodeversion.Overview, error) {
	local, channels, stale, fetchedAt, err := s.getChannelOverview("node", s.nodeChannels, nodeversion.Compare)
	if err != nil {
		return nodeversion.Overview{Local: local}, err
	}
	return nodeversion.Overview{
		Local: local, Channels: channels, IsStale: stale, FetchedAt: formatFetchedAt(fetchedAt),
	}, nil
}

// OpenNodeDownloadPage 使用系统默认浏览器打开固定 Node.js 官方下载页。
func (s *EnvCheckService) OpenNodeDownloadPage() error {
	return s.openURL("Node.js", nodeversion.DownloadPageURL())
}

func (s *EnvCheckService) getChannelOverview(
	tool string,
	fetch func() ([]remoteversion.Channel, bool, time.Time, error),
	compare func(string, string) (int, bool),
) (detect.ToolInfo, []remoteversion.Channel, bool, time.Time, error) {
	var (
		local     detect.ToolInfo
		localErr  error
		channels  []remoteversion.Channel
		stale     bool
		fetchedAt time.Time
		remoteErr error
		wg        sync.WaitGroup
	)
	wg.Go(func() { local, localErr = s.detectOne(context.Background(), tool) })
	wg.Go(func() { channels, stale, fetchedAt, remoteErr = fetch() })
	wg.Wait()
	if remoteErr != nil {
		return local, nil, false, time.Time{}, remoteErr
	}
	installed := localErr == nil && local.Status == detect.StatusInstalled
	for i := range channels {
		latest := ""
		if len(channels[i].Releases) > 0 {
			latest = channels[i].Releases[0].Version
		}
		channels[i].Relation = remoteversion.RelationFor(installed, local.Version, latest, compare)
	}
	return local, channels, stale, fetchedAt, nil
}

func (s *EnvCheckService) openURL(name, rawURL string) error {
	if s.opener == nil {
		return fmt.Errorf("打开 %s 官网失败: 平台能力不可用", name)
	}
	if err := s.opener.OpenURL(rawURL); err != nil {
		return fmt.Errorf("打开 %s 官网失败: %w", name, err)
	}
	return nil
}

func formatFetchedAt(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.Local().Format("2006-01-02 15:04:05")
}
