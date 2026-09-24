package remoteversion

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"hanxi/internal/product"
	"hanxi/packages/go/netx"
)

// UserAgent 探测请求统一 UA：派生自产品身份，构建注入 Version 后自动跟随，
// 不再手写版本串（导出供调用方复用；无 const 依赖方，故用 var）。
var UserAgent = product.UserAgent()

const (
	ProbeTimeout = 12 * time.Second
)

// NewHTTPClient 创建仅信任指定官网主机的客户端：每次重定向都复验目标仍在白名单且为 HTTPS，
// 防止官网被劫持/重定向后把请求（及潜在凭据）发给任意主机。
//
// N25 收口：Transport 走 netx 代理链（env→WinINET→直连）——此前裸默认
// Transport 只认环境变量代理，"开着 Clash 系统代理仍探测失败"的断层在
// 本厂一次修复，envcheck 七路在线探针（git/go/node/python/dotnet/java/npm）
// 与所有经 Fetch 的官网页探测同批受益。
func NewHTTPClient(allowedHosts ...string) *http.Client {
	hosts := make(map[string]struct{}, len(allowedHosts))
	for _, host := range allowedHosts {
		hosts[strings.ToLower(host)] = struct{}{}
	}
	return &http.Client{
		Timeout:   ProbeTimeout,
		Transport: netx.Transport(),
		CheckRedirect: func(req *http.Request, _ []*http.Request) error {
			if err := ValidateURL(req.URL, hosts); err != nil {
				return fmt.Errorf("拒绝官网重定向: %w", err)
			}
			return nil
		},
	}
}

// ValidateURL 校验 URL 为 https 且主机在表白名单内（大小写不敏感）；报错信息用 Redacted 隐藏 query 凭据。
func ValidateURL(rawURL *url.URL, allowedHosts map[string]struct{}) error {
	if rawURL == nil || rawURL.Scheme != "https" {
		return fmt.Errorf("地址必须使用 HTTPS")
	}
	if _, ok := allowedHosts[strings.ToLower(rawURL.Hostname())]; !ok {
		return fmt.Errorf("非官方主机: %s", rawURL.Redacted())
	}
	return nil
}

// Fetch GET 单个 URL 并限幅读取：Content-Length 预检 + LimitReader 双保险，
// 防无长度声明的恶意/异常响应撑爆内存。非 200 一律视为错误。
func Fetch(client *http.Client, rawURL string, limit int64, headers map[string]string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", UserAgent)
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if resp.ContentLength > limit {
		return nil, fmt.Errorf("响应超过 %d 字节限制", limit)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("响应超过 %d 字节限制", limit)
	}
	return body, nil
}
