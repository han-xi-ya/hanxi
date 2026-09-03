package dotnetversion

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"hanxi/internal/modules/envcheck/remoteversion"
)

const (
	// releasesIndexURL 是 .NET 官方 release-metadata 索引（微软官方 CDN，
	// 与 dotnet/core 仓库 release-notes/releases-index.json 同源；GitHub raw 对国内网络不可靠）。
	releasesIndexURL = "https://builds.dotnet.microsoft.com/dotnet/release-metadata/releases-index.json"
	downloadURL      = "https://dotnet.microsoft.com/download/dotnet"
	maxBody          = 256 << 10
)

// supportPhases 是 releases-index support-phase 的受控取值表。
// 空值表示"不作为受支持正式版线展示"（preview/eol）；未知取值按结构漂移报错。
var supportPhases = map[string]string{
	"active":      "活跃支持",
	"maintenance": "维护支持",
	"preview":     "",
	"eol":         "",
}

// releaseTypes 映射 LTS/STS 策略标签；未知取值报错，避免支持策略改名被静默吞掉。
var releaseTypes = map[string]string{
	"lts": "LTS",
	"sts": "STS",
}

var datePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

type releaseLine struct {
	ChannelVersion    string `json:"channel-version"`
	LatestRelease     string `json:"latest-release"`
	LatestReleaseDate string `json:"latest-release-date"`
	SupportPhase      string `json:"support-phase"`
	ReleaseType       string `json:"release-type"`
	LatestRuntime     string `json:"latest-runtime"`
	LatestSDK         string `json:"latest-sdk"`
	EOLDate           string `json:"eol-date"`
}

type releasesIndex struct {
	Releases []releaseLine `json:"releases-index"`
}

type source struct {
	client   *http.Client
	endpoint string
	now      time.Time
}

func defaultSource() source {
	return source{
		client:   remoteversion.NewHTTPClient("builds.dotnet.microsoft.com"),
		endpoint: releasesIndexURL,
		now:      time.Now(),
	}
}

var cache = remoteversion.NewCache(defaultSource().fetch, cloneChannels)

// DownloadPageURL 返回固定的 .NET 官方下载页地址。
func DownloadPageURL() string { return downloadURL }

// Channels 返回当前仍在官方支持范围内的全部版本线，按版本线降序（最新在前）。
func Channels() ([]remoteversion.Channel, bool, time.Time, error) { return cache.Get() }

func (s source) fetch() ([]remoteversion.Channel, error) {
	body, err := remoteversion.Fetch(s.client, s.endpoint, maxBody, map[string]string{"Accept": "application/json"})
	if err != nil {
		return nil, fmt.Errorf("获取 .NET 发布索引失败: %w", err)
	}
	var index releasesIndex
	if err := json.Unmarshal(body, &index); err != nil {
		return nil, fmt.Errorf("解析 .NET 发布索引失败: %w", err)
	}
	return normalize(index, s.now)
}

// normalize 严格校验 releases-index 结构：字段格式漂移或版本线重复一律报错；
// preview/eol 版本线整条过滤（预览线的 latest-release/latest-runtime 带 -preview 后缀，
// 且可能没有 eol-date，不适用正式版校验），受支持线中 eol-date 已过期的同样过滤。
func normalize(index releasesIndex, now time.Time) ([]remoteversion.Channel, error) {
	if len(index.Releases) == 0 {
		return nil, fmt.Errorf(".NET 发布索引为空")
	}
	today := now.UTC().Format("2006-01-02")
	seen := make(map[string]struct{}, len(index.Releases))
	channels := make([]remoteversion.Channel, 0, len(index.Releases))
	for _, line := range index.Releases {
		if !linePattern.MatchString(line.ChannelVersion) {
			return nil, fmt.Errorf(".NET channel-version %q 不符合 major.minor 结构", line.ChannelVersion)
		}
		if _, duplicate := seen[line.ChannelVersion]; duplicate {
			return nil, fmt.Errorf(".NET channel-version %q 重复出现", line.ChannelVersion)
		}
		seen[line.ChannelVersion] = struct{}{}
		phase := strings.ToLower(strings.TrimSpace(line.SupportPhase))
		phaseLabel, known := supportPhases[phase]
		if !known {
			return nil, fmt.Errorf(".NET %s support-phase %q 无法识别", line.ChannelVersion, line.SupportPhase)
		}
		if phaseLabel == "" {
			continue
		}
		typeLabel, knownType := releaseTypes[strings.ToLower(strings.TrimSpace(line.ReleaseType))]
		if !knownType {
			return nil, fmt.Errorf(".NET %s release-type %q 无法识别", line.ChannelVersion, line.ReleaseType)
		}
		// latest-runtime 是版本关系的权威口径；个别历史线缺失时回退 latest-release。
		runtime := strings.TrimSpace(line.LatestRuntime)
		if runtime == "" {
			runtime = strings.TrimSpace(line.LatestRelease)
		}
		if _, ok := parseVersion(runtime); !ok {
			return nil, fmt.Errorf(".NET %s latest-runtime %q 不是正式版本", line.ChannelVersion, runtime)
		}
		eol := ""
		if line.EOLDate != "" {
			if !datePattern.MatchString(line.EOLDate) {
				return nil, fmt.Errorf(".NET %s eol-date %q 不是 ISO 日期", line.ChannelVersion, line.EOLDate)
			}
			eol = line.EOLDate
			if eol < today {
				continue
			}
		}
		published, err := normalizeDate(line.LatestReleaseDate)
		if err != nil {
			return nil, fmt.Errorf(".NET %s latest-release-date: %w", line.ChannelVersion, err)
		}
		channels = append(channels, remoteversion.Channel{
			Key:      "dotnet-" + line.ChannelVersion,
			Label:    ".NET " + line.ChannelVersion,
			Detail:   channelDetail(typeLabel, phaseLabel, line.LatestSDK, eol),
			Releases: []remoteversion.Release{{Version: runtime, Published: published}},
		})
	}
	if len(channels) == 0 {
		return nil, fmt.Errorf(".NET 发布索引中没有受支持的版本线")
	}
	sort.Slice(channels, func(i, j int) bool {
		return compareLines(channels[i].Key, channels[j].Key) > 0
	})
	return channels, nil
}

func channelDetail(typeLabel, phaseLabel, sdk, eol string) string {
	detail := typeLabel + " · " + phaseLabel
	if sdk != "" {
		detail += " · SDK " + sdk
	}
	if eol != "" {
		detail += " · 支持至 " + eol
	}
	return detail
}

// compareLines 比较 dotnet-<major>.<minor> 通道键的版本线数值序。
func compareLines(a, b string) int {
	leftMajor, leftMinor := lineParts(strings.TrimPrefix(a, "dotnet-"))
	rightMajor, rightMinor := lineParts(strings.TrimPrefix(b, "dotnet-"))
	for _, pair := range [][2]uint64{{leftMajor, rightMajor}, {leftMinor, rightMinor}} {
		if pair[0] != pair[1] {
			if pair[0] < pair[1] {
				return -1
			}
			return 1
		}
	}
	return 0
}

func lineParts(line string) (uint64, uint64) {
	matches := linePattern.FindStringSubmatch(line)
	if matches == nil {
		return 0, 0
	}
	major, _ := strconv.ParseUint(matches[1], 10, 64)
	minor, _ := strconv.ParseUint(matches[2], 10, 64)
	return major, minor
}

func normalizeDate(raw string) (string, error) {
	if raw == "" {
		return "", nil
	}
	if datePattern.MatchString(raw) {
		return raw, nil
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return "", fmt.Errorf("%q 不是可识别的发布日期", raw)
	}
	return parsed.UTC().Format("2006-01-02"), nil
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
	for raw, host := range map[string]string{releasesIndexURL: "builds.dotnet.microsoft.com", downloadURL: "dotnet.microsoft.com"} {
		u, err := url.Parse(raw)
		if err != nil || u.Scheme != "https" || u.Hostname() != host {
			panic("envcheck/dotnetversion: invalid official URL")
		}
	}
}
