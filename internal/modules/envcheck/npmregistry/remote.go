// Package npmregistry 实现 npm registry（registry.npmjs.org）包最新版查询。
// 与 gitversion/goversion/nodeversion 等同构：固定官方 host、TTL 缓存、
// 限字节拉取；按包名分桶缓存以支撑目录内多工具并发查询。
package npmregistry

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"hanxi/internal/modules/envcheck/remoteversion"
)

const (
	registryBase  = "https://registry.npmjs.org/"
	maxLatestBody = 512 << 10
)

// packageDocument 仅取 registry 短元数据中的 version 字段（latest dist-tag 已解析到具体版本）。
type packageDocument struct {
	Version string `json:"version"`
}

type source struct {
	client *http.Client
	base   string // 常量 https://registry.npmjs.org/，测试可注入 httptest
	now    func() time.Time
}

func defaultSource() source {
	return source{
		client: remoteversion.NewHTTPClient("registry.npmjs.org"),
		base:   registryBase,
		now:    time.Now,
	}
}

var src = defaultSource()

// caches 按包名分桶的 TTL 缓存：remoteversion.Cache 是单值缓存，目录内多包
// 必须各自独立缓存，故用 sync.Map 懒建（注册表式 init 风格，读多写少）。
var caches sync.Map // pkg → *remoteversion.Cache[remoteversion.Release]

// Latest 返回指定 npm 包在 registry 上的 latest 版本、是否陈旧缓存与获取时间。
func Latest(pkg string) (remoteversion.Release, bool, time.Time, error) {
	return cacheFor(pkg).Get()
}

func cacheFor(pkg string) *remoteversion.Cache[remoteversion.Release] {
	if cached, ok := caches.Load(pkg); ok {
		return cached.(*remoteversion.Cache[remoteversion.Release])
	}
	created := remoteversion.NewCache(
		func() (remoteversion.Release, error) { return src.fetch(pkg) },
		cloneRelease,
	)
	actual, _ := caches.LoadOrStore(pkg, created)
	return actual.(*remoteversion.Cache[remoteversion.Release])
}

// fetch 查询 registry 的 latest dist-tag 端点。scoped 包名用 url.PathEscape
// 转义（@scope/name → @scope%2Fname，即 npm CLI 自身的 encodeURIComponent 风格）；
// registry 对转义与未转义两种路径均接受，此处锁定转义式并在单测校验请求路径。
func (s source) fetch(pkg string) (remoteversion.Release, error) {
	endpoint := s.base + url.PathEscape(pkg) + "/latest"
	body, err := remoteversion.Fetch(s.client, endpoint, maxLatestBody, map[string]string{"Accept": "application/json"})
	if err != nil {
		return remoteversion.Release{}, fmt.Errorf("获取 npm 包 %s 最新版失败: %w", pkg, err)
	}
	var document packageDocument
	if err := json.Unmarshal(body, &document); err != nil {
		return remoteversion.Release{}, fmt.Errorf("解析 npm 包 %s 响应失败: %w", pkg, err)
	}
	version := normalize(document.Version)
	if version == "" {
		return remoteversion.Release{}, fmt.Errorf("npm registry 响应中未找到 %s 的版本号", pkg)
	}
	return remoteversion.Release{Version: version}, nil
}

// normalize 去空白与可选 "v" 前缀，与本机解析出的裸版本号统一口径比较。
func normalize(raw string) string {
	return strings.TrimPrefix(strings.TrimSpace(raw), "v")
}

func cloneRelease(value remoteversion.Release) remoteversion.Release { return value }

func init() {
	u, err := url.Parse(registryBase)
	if err != nil || u.Scheme != "https" || u.Hostname() != "registry.npmjs.org" {
		panic("envcheck/npmregistry: invalid official URL")
	}
}
