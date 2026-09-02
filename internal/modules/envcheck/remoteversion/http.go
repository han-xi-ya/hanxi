package remoteversion

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	UserAgent    = "Hanxi/0.2"
	ProbeTimeout = 12 * time.Second
)

func NewHTTPClient(allowedHosts ...string) *http.Client {
	hosts := make(map[string]struct{}, len(allowedHosts))
	for _, host := range allowedHosts {
		hosts[strings.ToLower(host)] = struct{}{}
	}
	return &http.Client{
		Timeout: ProbeTimeout,
		CheckRedirect: func(req *http.Request, _ []*http.Request) error {
			if err := ValidateURL(req.URL, hosts); err != nil {
				return fmt.Errorf("拒绝官网重定向: %w", err)
			}
			return nil
		},
	}
}

func ValidateURL(rawURL *url.URL, allowedHosts map[string]struct{}) error {
	if rawURL == nil || rawURL.Scheme != "https" {
		return fmt.Errorf("地址必须使用 HTTPS")
	}
	if _, ok := allowedHosts[strings.ToLower(rawURL.Hostname())]; !ok {
		return fmt.Errorf("非官方主机: %s", rawURL.Redacted())
	}
	return nil
}

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
