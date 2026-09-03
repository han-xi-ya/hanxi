package eartrumpet

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"
)

// appInstallerURL 是上游 CI 维护的 AppInstaller 清单（master 分支频道），
// 内容里的 MainBundle 指向官方自托管 appxbundle，可自动更新。
const appInstallerURL = "https://install.eartrumpet.app/master/EarTrumpet.Package.appinstaller"

const (
	remoteCacheTTL   = 10 * time.Minute
	remoteFetchLimit = 1 << 20 // appinstaller XML 很小，1MB 上限防异常响应
)

// RemoteRelease 是解析并核验后的官方直装渠道版本信息。
type RemoteRelease struct {
	Version      string   `json:"version"`
	BundleURL    string   `json:"bundleUrl"`
	Dependencies []string `json:"dependencies"` // 框架包 https URL（如 VCLibs）
	FetchedAt    string   `json:"fetchedAt"`
}

// validateRelease 钉死渠道真实性：包名、发布者（防域名劫持/篡改指向他人包）、
// 下载地址必须落在上游官方主机。
func validateRelease(rel *RemoteRelease) error {
	if rel == nil || rel.Version == "" || rel.BundleURL == "" {
		return errors.New("appinstaller 清单缺少版本或包地址")
	}
	u, err := url.Parse(rel.BundleURL)
	if err != nil || u.Scheme != "https" || strings.EqualFold(u.Host, "install.eartrumpet.app") == false {
		return fmt.Errorf("安装包地址不在官方主机: %q", rel.BundleURL)
	}
	for _, dep := range rel.Dependencies {
		du, err := url.Parse(dep)
		if err != nil || du.Scheme != "https" {
			return fmt.Errorf("依赖包地址必须是 https: %q", dep)
		}
	}
	return nil
}

// remoteManifest 对应 appinstaller XML（schema 2017/2），仅取所需字段。
type remoteManifest struct {
	XMLName    xml.Name `xml:"AppInstaller"`
	MainBundle struct {
		Name      string `xml:"Name,attr"`
		Version   string `xml:"Version,attr"`
		Publisher string `xml:"Publisher,attr"`
		URI       string `xml:"Uri,attr"`
	} `xml:"MainBundle"`
	Dependencies struct {
		Packages []struct {
			Name      string `xml:"Name,attr"`
			Publisher string `xml:"Publisher,attr"`
			URI       string `xml:"Uri,attr"`
		} `xml:"Package"`
	} `xml:"Dependencies"`
}

func parseAppInstaller(data []byte) (*RemoteRelease, error) {
	var doc remoteManifest
	if err := xml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("解析 appinstaller 清单失败: %w", err)
	}
	if doc.MainBundle.Name != PackageName {
		return nil, fmt.Errorf("清单包名不符: %q", doc.MainBundle.Name)
	}
	if doc.MainBundle.Publisher != managedIdentity.Publisher {
		return nil, fmt.Errorf("清单发布者不符: %q", doc.MainBundle.Publisher)
	}
	rel := &RemoteRelease{
		Version:   doc.MainBundle.Version,
		BundleURL: doc.MainBundle.URI,
	}
	for _, dep := range doc.Dependencies.Packages {
		if dep.URI != "" {
			rel.Dependencies = append(rel.Dependencies, dep.URI)
		}
	}
	if err := validateRelease(rel); err != nil {
		return nil, err
	}
	return rel, nil
}

// remoteCache 提供 TTL 缓存；网络失败时回退上次成功结果（快照兜底），
// 与 everything 的官网 manifest 解析策略一致。
type remoteCache struct {
	mu       sync.Mutex
	cached   *RemoteRelease
	cachedAt time.Time
}

// fetch 通过注入的 getter 拉取并解析官方清单。getter 签名 (ctx, url) → body。
func (c *remoteCache) fetch(ctx context.Context, getter func(context.Context, string) ([]byte, error)) (*RemoteRelease, error) {
	c.mu.Lock()
	if c.cached != nil && time.Since(c.cachedAt) < remoteCacheTTL {
		out := *c.cached
		c.mu.Unlock()
		return &out, nil
	}
	c.mu.Unlock()

	data, err := getter(ctx, appInstallerURL)
	if err == nil {
		var rel *RemoteRelease
		rel, err = parseAppInstaller(data)
		if err == nil {
			rel.FetchedAt = time.Now().Format(time.RFC3339)
			c.mu.Lock()
			c.cached, c.cachedAt = rel, time.Now()
			c.mu.Unlock()
			out := *rel
			return &out, nil
		}
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cached != nil {
		out := *c.cached // 网络/解析失败：使用上次核验过的清单
		return &out, nil
	}
	return nil, fmt.Errorf("获取 EarTrumpet 官方版本清单失败: %w", err)
}
