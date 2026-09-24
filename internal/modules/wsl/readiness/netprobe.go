package readiness

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"hanxi/internal/product"
	netx "hanxi/packages/go/netx"
)

// GitHub 安装通道探测端点（固定官方 HTTPS 地址）。
const (
	apiLatestURL = "https://api.github.com/repos/microsoft/WSL/releases/latest"
	releasesURL  = "https://github.com/microsoft/WSL/releases"
	probeBudget  = 14 * time.Second
)

// ProbeNetwork 并发 HEAD 探测 GitHub API 与发布页，两路状态互不拖累。
// 返回值语义与旧 PS 探针一致："200"/"403"/…/ "net-fail"（网络层失败）。
// 403 判读是「已禁止(403)」根因证据；api.github.com 对国内出口时有区域性拦截。
func ProbeNetwork(ctx context.Context) (api string, gh string) {
	ctx, cancel := context.WithTimeout(ctx, probeBudget)
	defer cancel()

	// 代理链：环境变量 → WinINET 系统代理 → 直连（与浏览器同出口，
	// 否则"开了代理仍 403"的落差会原样复现到体检结论里）。
	client := netx.NewClient(probeBudget, func(req *http.Request, _ []*http.Request) error {
		host := strings.ToLower(req.URL.Hostname())
		if req.URL.Scheme != "https" || (host != "api.github.com" && host != "github.com") {
			return fmt.Errorf("拒绝通道探测重定向到非官方主机: %s", req.URL.Redacted())
		}
		return nil
	})
	var wg sync.WaitGroup
	wg.Go(func() { api = headStatus(ctx, client, apiLatestURL) })
	wg.Go(func() { gh = headStatus(ctx, client, releasesURL) })
	wg.Wait()
	return api, gh
}

func headStatus(ctx context.Context, client *http.Client, rawURL string) string {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, rawURL, nil)
	if err != nil {
		return "net-fail"
	}
	req.Header.Set("User-Agent", product.UserAgent())
	resp, err := client.Do(req)
	if err != nil {
		return "net-fail"
	}
	defer resp.Body.Close()
	return fmt.Sprintf("%d", resp.StatusCode)
}
