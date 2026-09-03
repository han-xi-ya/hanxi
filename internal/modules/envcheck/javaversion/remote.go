package javaversion

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"sync"
	"time"

	"hanxi/internal/modules/envcheck/remoteversion"
)

const (
	apiBaseURL  = "https://api.adoptium.net/v3"
	downloadURL = "https://adoptium.net/temurin/releases/"
	maxBody     = 2 << 20
	maxInfoBody = 64 << 10
	pageSize    = 20
)

type availableReleases struct {
	MostRecentFeatureRelease int `json:"most_recent_feature_release"`
	MostRecentLTS            int `json:"most_recent_lts"`
}

type apiRelease struct {
	ReleaseName string `json:"release_name"`
	ReleaseType string `json:"release_type"`
	Timestamp   string `json:"timestamp"`
	Vendor      string `json:"vendor"`
	VersionData struct {
		Major          int    `json:"major"`
		OpenJDKVersion string `json:"openjdk_version"`
		Optional       string `json:"optional"`
	} `json:"version_data"`
	Binaries []struct {
		Architecture string `json:"architecture"`
		ImageType    string `json:"image_type"`
		JVMImpl      string `json:"jvm_impl"`
		OS           string `json:"os"`
		Project      string `json:"project"`
	} `json:"binaries"`
}

type source struct {
	client   *http.Client
	baseURL  string
	features []int
}

func defaultSource() source {
	return source{client: remoteversion.NewHTTPClient("api.adoptium.net"), baseURL: apiBaseURL}
}

var cache = remoteversion.NewCache(defaultSource().fetch, cloneChannels)

func DownloadPageURL() string { return downloadURL }

func Channels() ([]remoteversion.Channel, bool, time.Time, error) { return cache.Get() }

func (s source) fetch() ([]remoteversion.Channel, error) {
	features := s.features
	if len(features) == 0 {
		info, err := s.fetchAvailableReleases()
		if err != nil {
			return nil, err
		}
		features = []int{info.MostRecentLTS}
		if info.MostRecentFeatureRelease != info.MostRecentLTS {
			features = append(features, info.MostRecentFeatureRelease)
		}
	}
	type result struct {
		feature int
		release apiRelease
		ok      bool
		err     error
	}
	results := make([]result, len(features))
	var wg sync.WaitGroup
	for i, feature := range features {
		wg.Add(1)
		go func(i, feature int) {
			defer wg.Done()
			release, ok, err := s.fetchFeature(feature)
			results[i] = result{feature: feature, release: release, ok: ok, err: err}
		}(i, feature)
	}
	wg.Wait()

	channels := make([]remoteversion.Channel, 0, len(features))
	for _, result := range results {
		if result.err != nil {
			return nil, result.err
		}
		if !result.ok {
			continue
		}
		feature, release := result.feature, result.release
		kind := "Feature"
		if release.VersionData.Optional == "LTS" {
			kind = "LTS"
		}
		channels = append(channels, remoteversion.Channel{
			Key: strconv.Itoa(feature), Label: fmt.Sprintf("Java %d", feature), Detail: kind + " · Temurin JDK HotSpot GA",
			Releases: []remoteversion.Release{{Version: normalizeVersion(release), Published: normalizeDate(release.Timestamp)}},
		})
	}
	if len(channels) == 0 {
		return nil, fmt.Errorf("Adoptium 响应中未找到 Temurin JDK HotSpot GA 版本")
	}
	sort.Slice(channels, func(i, j int) bool {
		left, _ := strconv.Atoi(channels[i].Key)
		right, _ := strconv.Atoi(channels[j].Key)
		return left > right
	})
	return channels, nil
}

func (s source) fetchAvailableReleases() (availableReleases, error) {
	endpoint := s.baseURL + "/info/available_releases"
	body, err := remoteversion.Fetch(s.client, endpoint, maxInfoBody, map[string]string{"Accept": "application/json"})
	if err != nil {
		return availableReleases{}, fmt.Errorf("获取 Adoptium 发布通道失败: %w", err)
	}
	var info availableReleases
	if err := json.Unmarshal(body, &info); err != nil {
		return availableReleases{}, fmt.Errorf("解析 Adoptium 发布通道失败: %w", err)
	}
	if info.MostRecentFeatureRelease <= 0 || info.MostRecentLTS <= 0 {
		return availableReleases{}, fmt.Errorf("Adoptium 发布通道响应不完整")
	}
	return info, nil
}

func (s source) fetchFeature(feature int) (apiRelease, bool, error) {
	endpoint, err := featureURL(s.baseURL, feature)
	if err != nil {
		return apiRelease{}, false, err
	}
	body, err := remoteversion.Fetch(s.client, endpoint, maxBody, map[string]string{"Accept": "application/json"})
	if err != nil {
		return apiRelease{}, false, fmt.Errorf("获取 Java %d Adoptium 版本失败: %w", feature, err)
	}
	var releases []apiRelease
	if err := json.Unmarshal(body, &releases); err != nil {
		return apiRelease{}, false, fmt.Errorf("解析 Java %d Adoptium 版本失败: %w", feature, err)
	}
	for _, release := range releases {
		if validRelease(release, feature) {
			return release, true, nil
		}
	}
	return apiRelease{}, false, nil
}

func featureURL(base string, feature int) (string, error) {
	u, err := url.Parse(fmt.Sprintf("%s/assets/feature_releases/%d/ga", base, feature))
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("architecture", "x64")
	q.Set("heap_size", "normal")
	q.Set("image_type", "jdk")
	q.Set("jvm_impl", "hotspot")
	q.Set("os", "windows")
	q.Set("page", "0")
	q.Set("page_size", strconv.Itoa(pageSize))
	q.Set("project", "jdk")
	q.Set("sort_method", "DEFAULT")
	q.Set("sort_order", "DESC")
	q.Set("vendor", "eclipse")
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func validRelease(release apiRelease, feature int) bool {
	if release.ReleaseType != "ga" || release.Vendor != "eclipse" || release.VersionData.Major != feature || normalizeVersion(release) == "" {
		return false
	}
	for _, binary := range release.Binaries {
		if binary.Architecture == "x64" && binary.ImageType == "jdk" && binary.JVMImpl == "hotspot" && binary.OS == "windows" && binary.Project == "jdk" {
			return true
		}
	}
	return false
}

func normalizeVersion(release apiRelease) string {
	if _, ok := parseVersion(release.VersionData.OpenJDKVersion); ok {
		return release.VersionData.OpenJDKVersion
	}
	name := release.ReleaseName
	if len(name) > 4 && name[:4] == "jdk-" {
		name = name[4:]
	}
	if _, ok := parseVersion(name); ok {
		return name
	}
	return ""
}

func normalizeDate(raw string) string {
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
	for raw, host := range map[string]string{apiBaseURL: "api.adoptium.net", downloadURL: "adoptium.net"} {
		u, err := url.Parse(raw)
		if err != nil || u.Scheme != "https" || u.Hostname() != host {
			panic("envcheck/javaversion: invalid official URL")
		}
	}
}
