package ompversion

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"time"

	"hanxi/internal/modules/envcheck/remoteversion"
)

const (
	// releasesURL per_page=5 实测响应约 241KB（Release body 是大头，见
	// TROUBLESHOOTING #15：按真实仓库测量、per_page 贴近展示数）；
	// 展示层折叠为仅最新一条，近期列表给 3 条足够关系判断。
	releasesURL     = "https://api.github.com/repos/can1357/oh-my-pi/releases?per_page=5"
	downloadURL     = "https://omp.sh"
	maxBody         = 4 << 20
	maxReleaseCount = 3
)

// githubRelease 仅取所需四字段的 GitHub Releases API 响应 DTO。
type githubRelease struct {
	TagName    string `json:"tag_name"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
	Published  string `json:"published_at"`
}

type source struct {
	client   *http.Client
	endpoint string
}

func defaultSource() source {
	return source{client: remoteversion.NewHTTPClient("api.github.com"), endpoint: releasesURL}
}

var cache = remoteversion.NewCache(defaultSource().fetch, cloneChannels)

// DownloadPageURL 官网地址（前端"去官网"跳转按钮用）。
func DownloadPageURL() string { return downloadURL }

// Channels 返回 omp 官方稳定版通道（remoteversion.Cache 包装：TTL 内免网络，
// 拉取失败回吐旧数据并置 stale=true）。
func Channels() ([]remoteversion.Channel, bool, time.Time, error) {
	return cache.Get()
}

func (s source) fetch() ([]remoteversion.Channel, error) {
	body, err := remoteversion.Fetch(s.client, s.endpoint, maxBody, map[string]string{
		"Accept": "application/vnd.github+json", "X-GitHub-Api-Version": "2022-11-28",
	})
	if err != nil {
		return nil, fmt.Errorf("获取 Oh My Pi 官网版本失败: %w", err)
	}
	var releases []githubRelease
	if err := json.Unmarshal(body, &releases); err != nil {
		return nil, fmt.Errorf("解析 Oh My Pi 官网版本失败: %w", err)
	}
	channels := normalize(releases)
	if len(channels) == 0 {
		return nil, fmt.Errorf("Oh My Pi 官网响应中未找到可识别的稳定版本")
	}
	return channels, nil
}

// normalize 过滤 draft/prerelease 与非法 tag，去重降序截断为单一 stable 通道。
func normalize(src []githubRelease) []remoteversion.Channel {
	type item struct {
		version   string
		published string
	}
	seen := make(map[string]struct{}, len(src))
	items := make([]item, 0, len(src))
	for _, release := range src {
		if release.Draft || release.Prerelease {
			continue
		}
		parsed, ok := parseVersion(release.TagName)
		if !ok {
			continue
		}
		version := fmt.Sprintf("%d.%d.%d", parsed.major, parsed.minor, parsed.patch)
		if _, exists := seen[version]; exists {
			continue
		}
		seen[version] = struct{}{}
		items = append(items, item{version: version, published: normalizePublished(release.Published)})
	}
	sort.Slice(items, func(i, j int) bool {
		result, _ := Compare(items[i].version, items[j].version)
		return result > 0
	})
	if len(items) == 0 {
		return nil
	}
	if len(items) > maxReleaseCount {
		items = items[:maxReleaseCount]
	}
	releases := make([]remoteversion.Release, 0, len(items))
	for _, item := range items {
		releases = append(releases, remoteversion.Release{Version: item.version, Published: item.published})
	}
	return []remoteversion.Channel{{
		Key:      "stable",
		Label:    "Stable",
		Detail:   "更新指路：官方脚本装用 omp update（页内「更新到最新版」即此通道）；npm/bun 装用各自包管理器更新",
		Releases: releases,
	}}
}

// normalizePublished RFC3339 → UTC 日期（与 gitversion.formatPublished 同口径），
// 解析失败留空由前端显"发布日期未知"。
func normalizePublished(raw string) string {
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return ""
	}
	return parsed.UTC().Format("2006-01-02")
}

func cloneChannels(src []remoteversion.Channel) []remoteversion.Channel {
	out := make([]remoteversion.Channel, len(src))
	for i := range src {
		out[i] = src[i]
		out[i].Releases = append([]remoteversion.Release(nil), src[i].Releases...)
	}
	return out
}

func init() {
	for _, raw := range []string{releasesURL, downloadURL} {
		u, err := url.Parse(raw)
		if err != nil || u.Scheme != "https" || (u.Hostname() != "api.github.com" && u.Hostname() != "omp.sh") {
			panic("envcheck/ompversion: invalid official URL")
		}
	}
}
