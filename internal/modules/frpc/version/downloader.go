package version

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"
	"sync"
	"time"

	"hanxi/internal/product"
)

const (
	repoOwner  = "fatedier"
	repoName   = "frp"
	userAgent  = product.Name + "/" + product.Version
	releaseURL = "https://api.github.com/repos/" + repoOwner + "/" + repoName + "/releases?per_page=60"

	// cacheTTL 远程 Release 列表内存缓存时长（规避 GitHub 未认证 60 次/小时限流）
	cacheTTL = 10 * time.Minute
)

// 拉取 GitHub API 的候选前缀：直连优先，镜像逐个回退
var apiBaseURLs = []string{
	"https://api.github.com",
	"https://ghfast.top/https://api.github.com",
	"https://gh-proxy.com/https://api.github.com",
	"https://ghproxy.net/https://api.github.com",
}

type release struct {
	TagName     string  `json:"tag_name"`
	PublishedAt string  `json:"published_at"`
	Prerelease  bool    `json:"prerelease"`
	Assets      []asset `json:"assets"`
	Body        string  `json:"body"`
}

type asset struct {
	Name string `json:"name"`
	URL  string `json:"url"` // 资产直链（302 到 objects.githubusercontent.com）
	Size int64  `json:"size"`
}

// apiClient GitHub API 请求客户端（10s 超时）
func apiClient() *http.Client {
	return &http.Client{Timeout: 12 * time.Second}
}

// fetchJSON 按候选地址逐个尝试，成功返回响应体
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

// findArchAsset 从 release 资产中筛选 windows amd64 zip
func findArchAsset(assets []asset, version string) (asset, bool) {
	// 资产名形如 frp_0.61.1_windows_amd64.zip
	needPrefix := "frp_" + strings.TrimPrefix(version, "v") + "_windows_amd64.zip"
	for _, a := range assets {
		if a.Name == needPrefix || strings.Contains(a.Name, "windows_amd64.zip") {
			return a, true
		}
	}
	return asset{}, false
}

// getChecksumsAsset 找到 release 中附带的官方 SHA256 校验清单资产
func getChecksumsAsset(assets []asset, version string) (asset, bool) {
	suffix := strings.TrimPrefix(version, "v") + "_sha256-checksums.txt"
	for _, a := range assets {
		if strings.HasSuffix(a.Name, "sha256-checksums.txt") && strings.Contains(a.Name, strings.TrimPrefix(version, "v")) {
			return a, true
		}
	}
	// 回退：只要文件名是 sha256-checksums.txt
	for _, a := range assets {
		if strings.HasSuffix(a.Name, suffix) || a.Name == "sha256-checksums.txt" {
			return a, true
		}
	}
	return asset{}, false
}

// fetchChecksums 拉取官方校验清单，返回 map[assetName]sha256
func fetchChecksums(url string) (map[string]string, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := apiClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("checksums fetch status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}

	result := make(map[string]string)
	for _, line := range strings.Split(string(body), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 {
			result[path.Base(fields[1])] = strings.ToLower(fields[0])
		}
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("no valid checksum entries found")
	}
	return result, nil
}

// releaseCache 远程列表缓存（防 GitHub 限流）
type releaseCache struct {
	mu        sync.Mutex
	data      []FrpRelease
	checksums map[string]string // version -> zip sha256
	fetchedAt time.Time
}

func (c *releaseCache) get() ([]FrpRelease, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.fetchedAt.IsZero() && time.Since(c.fetchedAt) < cacheTTL {
		return c.data, nil
	}

	body, err := fetchJSON(apiBaseURLs)
	if err != nil {
		if !c.fetchedAt.IsZero() {
			// 网络异常时降级返回旧缓存
			return c.data, nil
		}
		return nil, err
	}

	var releases []release
	if err := json.Unmarshal(body, &releases); err != nil {
		return nil, fmt.Errorf("parse releases: %w", err)
	}

	var list []FrpRelease
	checksums := make(map[string]string)
	for _, r := range releases {
		arch, ok := findArchAsset(r.Assets, r.TagName)
		if !ok {
			continue
		}
		rel := FrpRelease{
			Version:   r.TagName,
			Published: r.PublishedAt,
			IsPre:     r.Prerelease,
			AssetName: arch.Name,
			AssetURL:  arch.URL,
			Size:      arch.Size,
		}
		// 尝试获取该版本官方校验哈希（失败不阻塞整个列表）
		if chk, ok := getChecksumsAsset(r.Assets, r.TagName); ok {
			if m, err := fetchChecksums(chk.URL); err == nil {
				if sum, ok := m[arch.Name]; ok {
					rel.SHA256 = sum
					checksums[r.TagName] = sum
				}
			}
		}
		list = append(list, rel)
	}

	c.data = list
	c.checksums = checksums
	c.fetchedAt = time.Now()
	return list, nil
}

var remoteCache = &releaseCache{}
