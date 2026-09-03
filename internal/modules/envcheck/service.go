package envcheck

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"hanxi/internal/modules/envcheck/detect"
	"hanxi/internal/modules/envcheck/dotnetversion"
	"hanxi/internal/modules/envcheck/gitversion"
	"hanxi/internal/modules/envcheck/goversion"
	"hanxi/internal/modules/envcheck/javaversion"
	"hanxi/internal/modules/envcheck/nodeversion"
	"hanxi/internal/modules/envcheck/pythonversion"
	"hanxi/internal/modules/envcheck/remoteversion"
)

// urlOpener 是 EnvCheckService 打开固定官网地址所需的最小平台能力。
type urlOpener interface {
	OpenURL(url string) error
}

// EnvCheckService Wails 绑定服务：本机开发工具链探测与 Git、Go、Node.js、Java、Python、.NET 官网版本查询。
type EnvCheckService struct {
	opener         urlOpener
	detectOne      func(context.Context, string) (detect.ToolInfo, error)
	recentReleases func() ([]gitversion.Release, error)
	goChannels     func() ([]remoteversion.Channel, bool, time.Time, error)
	nodeChannels   func() ([]remoteversion.Channel, bool, time.Time, error)
	javaChannels   func() ([]remoteversion.Channel, bool, time.Time, error)
	pythonChannels pythonversion.MinorChannel
	dotnetChannels func() ([]remoteversion.Channel, bool, time.Time, error)
}

func NewEnvCheckService(opener urlOpener) *EnvCheckService {
	return &EnvCheckService{
		opener:         opener,
		detectOne:      detect.RunOne,
		recentReleases: gitversion.RecentReleases,
		goChannels:     goversion.Channels,
		nodeChannels:   nodeversion.Channels,
		javaChannels:   javaversion.Channels,
		pythonChannels: pythonversion.ChannelsForLocal,
		dotnetChannels: dotnetversion.Channels,
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
	local, channels, stale, fetchedAt, err := s.getChannelOverview("go", s.goChannels, goversion.Compare, goversion.VersionLine)
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
	local, channels, stale, fetchedAt, err := s.getChannelOverview("node", s.nodeChannels, nodeversion.Compare, nodeversion.VersionLine)
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

// GetJavaOverview 并发查询本机 Java 与 Eclipse Temurin GA 版本。
func (s *EnvCheckService) GetJavaOverview() (javaversion.Overview, error) {
	var (
		local     detect.ToolInfo
		localErr  error
		channels  []remoteversion.Channel
		stale     bool
		fetchedAt time.Time
		remoteErr error
		wg        sync.WaitGroup
	)
	wg.Go(func() { local, localErr = s.detectOne(context.Background(), "java") })
	wg.Go(func() { channels, stale, fetchedAt, remoteErr = s.javaChannels() })
	wg.Wait()
	if localErr != nil {
		return javaversion.Overview{}, fmt.Errorf("检测本机 Java 失败: %w", localErr)
	}
	if remoteErr != nil {
		return javaversion.Overview{Local: local}, remoteErr
	}
	installed := local.Status == detect.StatusInstalled
	for i := range channels {
		latest := ""
		if len(channels[i].Releases) > 0 {
			latest = channels[i].Releases[0].Version
		}
		channels[i].Relation = javaversion.RelationFor(local, latest)
		if installed && channels[i].Relation == remoteversion.RelationUnknown {
			channels[i].RelationDetail = "当前 Java 发行版与 Eclipse Temurin 不同，补丁版本不能直接比较"
		}
	}
	if installed {
		remoteversion.PrioritizeLocalLine(channels, local.Version, javaversion.VersionLine)
	}
	return javaversion.Overview{Local: local, Channels: channels, IsStale: stale, FetchedAt: formatFetchedAt(fetchedAt)}, nil
}

// OpenJavaDownloadPage 使用系统默认浏览器打开固定 Temurin 下载页。
func (s *EnvCheckService) OpenJavaDownloadPage() error {
	return s.openURL("Eclipse Temurin", javaversion.DownloadPageURL())
}

// GetPythonOverview 查询本机 Python、Python.org 最新稳定版及本机受支持版本线。
func (s *EnvCheckService) GetPythonOverview() (pythonversion.Overview, error) {
	local, err := s.detectOne(context.Background(), "python")
	if err != nil {
		return pythonversion.Overview{}, fmt.Errorf("检测本机 Python 失败: %w", err)
	}
	channels, stale, fetchedAt, remoteErr := s.pythonChannels(local.Version)
	if remoteErr != nil {
		return pythonversion.Overview{Local: local}, remoteErr
	}
	installed := local.Status == detect.StatusInstalled
	for i := range channels {
		latest := ""
		if len(channels[i].Releases) > 0 {
			latest = channels[i].Releases[0].Version
		}
		if channels[i].Key == "stable" && installed {
			channels[i].Relation = remoteversion.RelationUnknown
			channels[i].RelationDetail = "跨 Python minor 版本线不视为普通补丁升级"
			if result, ok := pythonversion.Compare(local.Version, latest); ok && result == 0 {
				channels[i].Relation = remoteversion.RelationLatest
				channels[i].RelationDetail = "本机已是 Python.org 最新稳定版"
			}
		} else {
			channels[i].Relation = remoteversion.RelationFor(installed, local.Version, latest, pythonversion.Compare)
		}
	}
	if installed {
		remoteversion.PrioritizeLocalLine(channels, local.Version, pythonversion.VersionLine)
	}
	return pythonversion.Overview{Local: local, Channels: channels, IsStale: stale, FetchedAt: formatFetchedAt(fetchedAt)}, nil
}

// OpenPythonDownloadPage 使用系统默认浏览器打开固定 Python 官方下载页。
func (s *EnvCheckService) OpenPythonDownloadPage() error {
	return s.openURL("Python", pythonversion.DownloadPageURL())
}

// GetDotNetOverview 并发查询本机 .NET 与官方 release-metadata 支持线。
// 卡片展示版本为 SDK 优先，但通道关系统一使用运行时（Microsoft.NETCore.App）版本比较：
// 官方 latest-runtime 是运行时编号体系（9.0.19），与 SDK 编号（9.0.100）不可直接比较。
func (s *EnvCheckService) GetDotNetOverview() (dotnetversion.Overview, error) {
	var (
		local     detect.ToolInfo
		localErr  error
		lines     []remoteversion.Channel
		stale     bool
		fetchedAt time.Time
		remoteErr error
		wg        sync.WaitGroup
	)
	wg.Go(func() { local, localErr = s.detectOne(context.Background(), "dotnet") })
	wg.Go(func() { lines, stale, fetchedAt, remoteErr = s.dotnetChannels() })
	wg.Wait()
	if localErr != nil {
		return dotnetversion.Overview{}, fmt.Errorf("检测本机 .NET 失败: %w", localErr)
	}
	if remoteErr != nil {
		return dotnetversion.Overview{Local: local}, remoteErr
	}
	// 版本关系按本机最高运行时版本线口径比较（Runtimes 由探测器升序归并，末位最高）。
	runtimeVersion := ""
	if local.Details != nil && local.Details.DotNet != nil {
		if runtimes := local.Details.DotNet.Runtimes; len(runtimes) > 0 {
			runtimeVersion = runtimes[len(runtimes)-1]
		}
	}
	installed := local.Status == detect.StatusInstalled
	channels, localLineSupported := dotnetversion.SelectChannels(lines, runtimeVersion)
	for i := range channels {
		latest := ""
		if len(channels[i].Releases) > 0 {
			latest = channels[i].Releases[0].Version
		}
		channels[i].Relation = remoteversion.RelationFor(installed, runtimeVersion, latest, dotnetversion.Compare)
		switch {
		case installed && runtimeVersion == "":
			channels[i].RelationDetail = "未能解析本机 .NET 运行时（Microsoft.NETCore.App）版本，无法与官方版本线比较"
		case installed && !localLineSupported:
			channels[i].RelationDetail = fmt.Sprintf("本机 .NET %s 版本线已超出官方支持范围，仅展示最新支持线", dotnetversion.VersionLine(runtimeVersion))
		}
	}
	return dotnetversion.Overview{Local: local, Channels: channels, IsStale: stale, FetchedAt: formatFetchedAt(fetchedAt)}, nil
}

// OpenDotNetDownloadPage 使用系统默认浏览器打开固定 .NET 官方下载页。
func (s *EnvCheckService) OpenDotNetDownloadPage() error {
	return s.openURL(".NET", dotnetversion.DownloadPageURL())
}

func (s *EnvCheckService) getChannelOverview(
	tool string,
	fetch func() ([]remoteversion.Channel, bool, time.Time, error),
	compare func(string, string) (int, bool),
	lineOf func(string) string,
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
	if installed {
		remoteversion.PrioritizeLocalLine(channels, local.Version, lineOf)
	}
	return local, channels, stale, fetchedAt, nil
}

// revealInExplorer 为包级函数变量，单测可替换，避免测试中真实唤起 explorer.exe。
var revealInExplorer = func(path string) error {
	// /select, 与路径必须是同一个参数，中间逗号是语法的一部分；
	// 不可直接 explorer.exe <exe>——该语义是"执行"而非"定位"（markeron 事故教训）。
	return exec.Command("explorer.exe", "/select,"+path).Start()
}

// RevealToolPath 在资源管理器中定位该工具探测到的可执行文件。
// 前端只允许传注册名，路径由后端探测器基于实机 PATH 解析重新获得，
// 不接受前端传入任意路径字符串，避免本模块被用作任意本地路径探测面。
func (s *EnvCheckService) RevealToolPath(name string) error {
	local, err := s.detectOne(context.Background(), strings.TrimSpace(name))
	if err != nil {
		return fmt.Errorf("探测 %s 失败: %w", name, err)
	}
	if local.Status != detect.StatusInstalled || local.Path == "" {
		return fmt.Errorf("%s 未安装或路径不可用", local.Display)
	}
	return revealInExplorer(local.Path)
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
