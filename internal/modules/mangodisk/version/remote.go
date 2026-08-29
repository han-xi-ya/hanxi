package version

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	repoOwner  = "harry0703"
	repoName   = "MangoDisk"
	userAgent  = "Hanxi/0.2"
	releaseURL = "https://api.github.com/repos/" + repoOwner + "/" + repoName + "/releases?per_page=60"
	cacheTTL   = 10 * time.Minute
)

var (
	plainSemverTag = regexp.MustCompile(`^v\d+\.\d+\.\d+$`)
	sha256Re       = regexp.MustCompile(`^[0-9a-f]{64}$`)
	apiBaseURLs    = []string{
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
	URL    string `json:"url"`
	Size   int64  `json:"size"`
	Digest string `json:"digest"`
}

func apiClient() *http.Client { return &http.Client{Timeout: 12 * time.Second} }

func fetchJSON(urls []string) ([]byte, error) {
	var lastErr error
	for _, base := range urls {
		url := releaseURL
		if !strings.HasPrefix(base, "https://api.github.com") {
			url = base + "/repos/" + repoOwner + "/" + repoName + "/releases?per_page=60"
		}
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			lastErr = err
			continue
		}
		req.Header.Set("User-Agent", userAgent)
		req.Header.Set("Accept", "application/vnd.github+json")
		resp, err := apiClient().Do(req)
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
			lastErr = fmt.Errorf("GitHub API %s: status %d", url, resp.StatusCode)
			continue
		}
		return body, nil
	}
	return nil, fmt.Errorf("all GitHub API mirrors failed: %w", lastErr)
}

func findPortableAsset(assets []asset, version string) (asset, bool) {
	ver := strings.TrimPrefix(version, "v")
	want := strings.ToLower("MangoDisk-" + ver + "-windows-portable.exe")
	for _, a := range assets {
		if strings.ToLower(a.Name) == want {
			return a, true
		}
	}
	return asset{}, false
}

type releaseCache struct {
	mu        sync.Mutex
	data      []MangoDiskRelease
	fetchedAt time.Time
}

func (c *releaseCache) get() ([]MangoDiskRelease, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.fetchedAt.IsZero() && time.Since(c.fetchedAt) < cacheTTL {
		return c.data, nil
	}
	body, err := fetchJSON(apiBaseURLs)
	if err != nil {
		if !c.fetchedAt.IsZero() {
			return c.data, nil
		}
		return nil, err
	}
	list, err := parseReleasesBody(body)
	if err != nil {
		return nil, err
	}
	c.data = list
	c.fetchedAt = time.Now()
	return list, nil
}

func parseReleasesBody(body []byte) ([]MangoDiskRelease, error) {
	var releases []release
	if err := json.Unmarshal(body, &releases); err != nil {
		return nil, fmt.Errorf("parse releases: %w", err)
	}
	var list []MangoDiskRelease
	for _, r := range releases {
		if r.Draft || !plainSemverTag.MatchString(r.TagName) {
			continue
		}
		a, ok := findPortableAsset(r.Assets, r.TagName)
		if !ok || a.Size <= 0 {
			continue
		}
		sha := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(a.Digest)), "sha256:")
		if !sha256Re.MatchString(sha) {
			continue
		}
		list = append(list, MangoDiskRelease{
			Version: r.TagName, Published: r.PublishedAt, IsPre: r.Prerelease,
			AssetName: a.Name, AssetURL: a.URL, Size: a.Size, SHA256: sha,
		})
	}
	return list, nil
}

var remoteCache = &releaseCache{}
