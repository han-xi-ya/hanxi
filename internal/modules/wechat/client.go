package wechat

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	defaultBaseURL   = "https://ilinkai.weixin.qq.com"
	defaultCDNBase   = "https://novac2c.cdn.weixin.qq.com/c2c"
	ilinkAppID       = "bot"
	ilinkClientVer   = "132102" // (2<<16)|(4<<8)|6 => 2.4.6
	defaultBotAgent  = "OpenClaw"
	defaultChanVer   = "2.4.6"
)

// BaseInfo 微信 iLink 基础载荷
type BaseInfo struct {
	ChannelVersion string `json:"channel_version"`
	BotAgent       string `json:"bot_agent"`
}

func defaultBaseInfo() BaseInfo {
	return BaseInfo{
		ChannelVersion: defaultChanVer,
		BotAgent:       defaultBotAgent,
	}
}

// Client 微信 iLink Bot 底层 HTTP 客户端
type Client struct {
	baseURL    string
	cdnBase    string
	httpClient *http.Client
}

func NewClient(baseURL string) *Client {
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	baseURL = strings.TrimRight(baseURL, "/")

	transport := &http.Transport{
		DisableKeepAlives:   false,
		DisableCompression:  true, // 腾讯 CDN 密文下载禁止 gzip 压缩导致字节数变异
		MaxIdleConns:        10,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
	}

	return &Client{
		baseURL: baseURL,
		cdnBase: defaultCDNBase,
		httpClient: &http.Client{
			Transport: transport,
			Timeout:   45 * time.Second,
		},
	}
}

// buildHeaders 组装微信 iLink 协议所必须的 Header
func (c *Client) buildHeaders(req *http.Request, botToken string) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("iLink-App-Id", ilinkAppID)
	req.Header.Set("iLink-App-ClientVersion", ilinkClientVer)

	// X-WECHAT-UIN: base64(itoa(rand_uint32))
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	uinStr := strconv.FormatUint(uint64(r.Uint32()), 10)
	req.Header.Set("X-WECHAT-UIN", base64.StdEncoding.EncodeToString([]byte(uinStr)))

	if botToken != "" {
		req.Header.Set("AuthorizationType", "ilink_bot_token")
		req.Header.Set("Authorization", "Bearer "+botToken)
	}
}

// post 执行标准 JSON POST 请求
func (c *Client) post(ctx context.Context, endpoint string, botToken string, body any, respOut any) error {
	url := endpoint
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		url = c.baseURL + endpoint
	}

	var jsonBytes []byte
	var err error
	if body != nil {
		jsonBytes, err = json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request body failed: %w", err)
		}
	} else {
		jsonBytes = []byte("{}")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(jsonBytes))
	if err != nil {
		return fmt.Errorf("create request failed: %w", err)
	}

	c.buildHeaders(req, botToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("http post %s failed: %w", endpoint, err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response from %s failed: %w", endpoint, err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("http post %s returned status %d: %s", endpoint, resp.StatusCode, string(respBytes))
	}

	if respOut != nil && len(respBytes) > 0 {
		if err := json.Unmarshal(respBytes, respOut); err != nil {
			return fmt.Errorf("unmarshal response from %s failed: %w, raw: %s", endpoint, err, string(respBytes))
		}
	}

	return nil
}

// get 执行 GET 请求
func (c *Client) get(ctx context.Context, endpoint string, botToken string, respOut any) error {
	url := endpoint
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		url = c.baseURL + endpoint
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("create request failed: %w", err)
	}

	c.buildHeaders(req, botToken)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("http get %s failed: %w", endpoint, err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response from %s failed: %w", endpoint, err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("http get %s returned status %d: %s", endpoint, resp.StatusCode, string(respBytes))
	}

	if respOut != nil && len(respBytes) > 0 {
		if err := json.Unmarshal(respBytes, respOut); err != nil {
			return fmt.Errorf("unmarshal response from %s failed: %w, raw: %s", endpoint, err, string(respBytes))
		}
	}

	return nil
}
