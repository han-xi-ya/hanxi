package version

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"hubkit/internal/platform/versioncmp"
)

const (
	repoOwner    = "M2Team"
	repoName     = "NanaZip"
	userAgent    = "HubKit/0.2"
	cacheTTL     = 10 * time.Minute
	digestPrefix = "sha256:"
)

var (
	stableVersionRe = regexp.MustCompile(`^\d+\.\d+\.\d+\.\d+$`)
	sha256Re        = regexp.MustCompile(`^[0-9a-f]{64}$`)
	apiBaseURLs     = []string{
		"https://api.github.com",
		"https://ghfast.top/https://api.github.com",
		"https://gh-proxy.com/https://api.github.com",
		"https://ghproxy.net/https://api.github.com",
	}
)

func RepoURL() string { return "https://github.com/" + repoOwner + "/" + repoName }

type release struct {
	TagName     string  `json:"tag_name"`
	PublishedAt string  `json:"published_at"`
	Prerelease  bool    `json:"prerelease"`
	Draft       bool    `json:"draft"`
	Assets      []asset `json:"assets"`
}

type asset struct {
	Name   string `json:"name"`
	URL    string `json:"browser_download_url"`
	Size   int64  `json:"size"`
	Digest string `json:"digest"`
}

type releaseCache struct {
	mu        sync.Mutex
	data      []Release
	fetchedAt time.Time
}

func (c *releaseCache) get() ([]Release, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.fetchedAt.IsZero() && time.Since(c.fetchedAt) < cacheTTL {
		return append([]Release(nil), c.data...), nil
	}
	body, err := fetchReleases()
	if err != nil {
		if !c.fetchedAt.IsZero() {
			stale := append([]Release(nil), c.data...)
			for i := range stale {
				stale[i].Stale = true
			}
			return stale, nil
		}
		return nil, err
	}
	list, err := parseReleasesBody(body)
	if err != nil {
		return nil, err
	}
	c.data, c.fetchedAt = list, time.Now()
	return append([]Release(nil), list...), nil
}

func fetchReleases() ([]byte, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	var lastErr error
	for _, base := range apiBaseURLs {
		url := base + "/repos/" + repoOwner + "/" + repoName + "/releases?per_page=60"
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			lastErr = err
			continue
		}
		req.Header.Set("User-Agent", userAgent)
		req.Header.Set("Accept", "application/vnd.github+json")
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			continue
		}
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("GitHub API status %d", resp.StatusCode)
			continue
		}
		return body, nil
	}
	return nil, fmt.Errorf("all GitHub API mirrors failed: %w", lastErr)
}

func parseReleasesBody(body []byte) ([]Release, error) {
	var raw []release
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse releases: %w", err)
	}
	list := make([]Release, 0, len(raw))
	for _, item := range raw {
		if item.Draft || item.Prerelease || !stableVersionRe.MatchString(item.TagName) {
			continue
		}
		wanted := "NanaZip_" + item.TagName + ".msixbundle"
		var found *asset
		for i := range item.Assets {
			if item.Assets[i].Name == wanted {
				found = &item.Assets[i]
				break
			}
		}
		if found == nil || found.Size <= 0 {
			continue
		}
		sha := strings.TrimPrefix(strings.ToLower(found.Digest), digestPrefix)
		if !sha256Re.MatchString(sha) {
			continue
		}
		list = append(list, Release{Version: item.TagName, Published: item.PublishedAt, AssetName: found.Name, AssetURL: found.URL, Size: found.Size, SHA256: sha})
	}
	sort.Slice(list, func(i, j int) bool { return versioncmp.Compare(list[i].Version, list[j].Version) > 0 })
	return list, nil
}

var remoteCache = &releaseCache{}
