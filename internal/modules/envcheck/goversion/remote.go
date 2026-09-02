package goversion

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
	releasesURL     = "https://go.dev/dl/?mode=json"
	downloadURL     = "https://go.dev/dl/"
	maxBody         = 1 << 20
	maxChannelCount = 2
)

type goRelease struct {
	Version string `json:"version"`
	Stable  bool   `json:"stable"`
}

type source struct {
	client   *http.Client
	endpoint string
}

func defaultSource() source {
	return source{client: remoteversion.NewHTTPClient("go.dev"), endpoint: releasesURL}
}

var cache = remoteversion.NewCache(defaultSource().fetch, cloneChannels)

func DownloadPageURL() string { return downloadURL }

func Channels() ([]remoteversion.Channel, bool, time.Time, error) {
	return cache.Get()
}

func (s source) fetch() ([]remoteversion.Channel, error) {
	body, err := remoteversion.Fetch(s.client, s.endpoint, maxBody, map[string]string{"Accept": "application/json"})
	if err != nil {
		return nil, fmt.Errorf("获取 Go 官网版本失败: %w", err)
	}
	var releases []goRelease
	if err := json.Unmarshal(body, &releases); err != nil {
		return nil, fmt.Errorf("解析 Go 官网版本失败: %w", err)
	}
	channels := normalize(releases)
	if len(channels) == 0 {
		return nil, fmt.Errorf("Go 官网响应中未找到稳定版本")
	}
	return channels, nil
}

func normalize(src []goRelease) []remoteversion.Channel {
	type item struct {
		version string
		parsed  parsedVersion
	}
	seen := make(map[string]struct{}, len(src))
	items := make([]item, 0, len(src))
	for _, release := range src {
		if !release.Stable {
			continue
		}
		parsed, ok := parseVersion(release.Version)
		if !ok {
			continue
		}
		version := fmt.Sprintf("%d.%d.%d", parsed.major, parsed.minor, parsed.patch)
		if _, exists := seen[version]; exists {
			continue
		}
		seen[version] = struct{}{}
		items = append(items, item{version: version, parsed: parsed})
	}
	sort.Slice(items, func(i, j int) bool {
		result, _ := Compare(items[i].version, items[j].version)
		return result > 0
	})

	channels := make([]remoteversion.Channel, 0, maxChannelCount)
	seenLine := make(map[[2]uint64]struct{}, maxChannelCount)
	for _, item := range items {
		line := [2]uint64{item.parsed.major, item.parsed.minor}
		if _, exists := seenLine[line]; exists {
			continue
		}
		seenLine[line] = struct{}{}
		key, label := "stable", "Stable"
		if len(channels) == 1 {
			key, label = "oldstable", "Oldstable"
		}
		channels = append(channels, remoteversion.Channel{
			Key: key, Label: label,
			Releases: []remoteversion.Release{{Version: item.version}},
		})
		if len(channels) == maxChannelCount {
			break
		}
	}
	return channels
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
		if err != nil || u.Scheme != "https" || u.Hostname() != "go.dev" {
			panic("envcheck/goversion: invalid official URL")
		}
	}
}
