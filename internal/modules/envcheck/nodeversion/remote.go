package nodeversion

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"hanxi/internal/modules/envcheck/remoteversion"
)

const (
	indexURL        = "https://nodejs.org/dist/index.json"
	scheduleURL     = "https://raw.githubusercontent.com/nodejs/Release/main/schedule.json"
	downloadURL     = "https://nodejs.org/en/download"
	maxIndexBody    = 1 << 20
	maxScheduleBody = 512 << 10
)

type ltsValue struct {
	Name string
}

func (v *ltsValue) UnmarshalJSON(data []byte) error {
	if bytes.Equal(data, []byte("false")) || bytes.Equal(data, []byte("null")) {
		v.Name = ""
		return nil
	}
	return json.Unmarshal(data, &v.Name)
}

type nodeRelease struct {
	Version string   `json:"version"`
	Date    string   `json:"date"`
	LTS     ltsValue `json:"lts"`
}

type scheduleEntry struct {
	Start       string `json:"start"`
	LTS         string `json:"lts"`
	Maintenance string `json:"maintenance"`
	End         string `json:"end"`
	Codename    string `json:"codename"`
}

type source struct {
	indexClient      *http.Client
	scheduleClient   *http.Client
	indexEndpoint    string
	scheduleEndpoint string
	now              func() time.Time
}

func defaultSource() source {
	return source{
		indexClient:      remoteversion.NewHTTPClient("nodejs.org"),
		scheduleClient:   remoteversion.NewHTTPClient("raw.githubusercontent.com"),
		indexEndpoint:    indexURL,
		scheduleEndpoint: scheduleURL,
		now:              time.Now,
	}
}

var cache = remoteversion.NewCache(defaultSource().fetch, cloneChannels)

func DownloadPageURL() string { return downloadURL }

func Channels() ([]remoteversion.Channel, bool, time.Time, error) {
	return cache.Get()
}

func (s source) fetch() ([]remoteversion.Channel, error) {
	indexBody, err := remoteversion.Fetch(s.indexClient, s.indexEndpoint, maxIndexBody, map[string]string{"Accept": "application/json"})
	if err != nil {
		return nil, fmt.Errorf("获取 Node.js 官网版本失败: %w", err)
	}
	scheduleBody, err := remoteversion.Fetch(s.scheduleClient, s.scheduleEndpoint, maxScheduleBody, map[string]string{"Accept": "application/json"})
	if err != nil {
		return nil, fmt.Errorf("获取 Node.js 生命周期失败: %w", err)
	}
	var releases []nodeRelease
	if err := json.Unmarshal(indexBody, &releases); err != nil {
		return nil, fmt.Errorf("解析 Node.js 官网版本失败: %w", err)
	}
	var schedule map[string]scheduleEntry
	if err := json.Unmarshal(scheduleBody, &schedule); err != nil {
		return nil, fmt.Errorf("解析 Node.js 生命周期失败: %w", err)
	}
	channels := normalize(releases, schedule, s.now())
	if len(channels) == 0 {
		return nil, fmt.Errorf("Node.js 官网响应中未找到 LTS 或 Current 版本")
	}
	return channels, nil
}

func normalize(releases []nodeRelease, schedule map[string]scheduleEntry, now time.Time) []remoteversion.Channel {
	today := now.UTC().Format("2006-01-02")
	byMajor := make(map[uint64][]nodeRelease)
	for _, release := range releases {
		parsed, ok := parseVersion(release.Version)
		if !ok || !validDate(release.Date) {
			continue
		}
		byMajor[parsed.major] = append(byMajor[parsed.major], release)
	}
	for major := range byMajor {
		sort.Slice(byMajor[major], func(i, j int) bool {
			result, _ := Compare(byMajor[major][i].Version, byMajor[major][j].Version)
			return result > 0
		})
	}

	var ltsMajor, currentMajor uint64
	var ltsEntry scheduleEntry
	for key, entry := range schedule {
		major, ok := scheduleMajor(key)
		if !ok || entry.Start == "" || today < entry.Start || (entry.End != "" && today >= entry.End) {
			continue
		}
		if entry.LTS != "" && today >= entry.LTS {
			if major > ltsMajor {
				ltsMajor, ltsEntry = major, entry
			}
			continue
		}
		currentEnd := entry.LTS
		if currentEnd == "" {
			currentEnd = entry.Maintenance
		}
		if currentEnd == "" {
			currentEnd = entry.End
		}
		if (currentEnd == "" || today < currentEnd) && major > currentMajor {
			currentMajor = major
		}
	}

	channels := make([]remoteversion.Channel, 0, 2)
	if candidates := byMajor[ltsMajor]; ltsMajor > 0 {
		for _, release := range candidates {
			if release.LTS.Name == "" {
				continue
			}
			detail := release.LTS.Name
			if detail == "" {
				detail = ltsEntry.Codename
			}
			if ltsEntry.Maintenance != "" && today >= ltsEntry.Maintenance {
				detail = strings.TrimSpace(detail + " · Maintenance LTS")
			} else {
				detail = strings.TrimSpace(detail + " · Active LTS")
			}
			channels = append(channels, remoteversion.Channel{
				Key: "lts", Label: "LTS", Detail: detail,
				Releases: []remoteversion.Release{{Version: strings.TrimPrefix(release.Version, "v"), Published: release.Date}},
			})
			break
		}
	}
	if candidates := byMajor[currentMajor]; currentMajor > 0 && len(candidates) > 0 {
		release := candidates[0]
		channels = append(channels, remoteversion.Channel{
			Key: "current", Label: "Current", Detail: "最新功能通道",
			Releases: []remoteversion.Release{{Version: strings.TrimPrefix(release.Version, "v"), Published: release.Date}},
		})
	}
	return channels
}

func validDate(raw string) bool {
	_, err := time.Parse("2006-01-02", raw)
	return err == nil
}

func scheduleMajor(key string) (uint64, bool) {
	value, err := strconv.ParseUint(strings.TrimPrefix(key, "v"), 10, 64)
	return value, err == nil && strings.HasPrefix(key, "v")
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
	urls := map[string]string{indexURL: "nodejs.org", scheduleURL: "raw.githubusercontent.com", downloadURL: "nodejs.org"}
	for raw, host := range urls {
		u, err := url.Parse(raw)
		if err != nil || u.Scheme != "https" || u.Hostname() != host {
			panic("envcheck/nodeversion: invalid official URL")
		}
	}
}
