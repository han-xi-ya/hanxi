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
	defaultBaseURL  = "https://ilinkai.weixin.qq.com"
	defaultCDNBase  = "https://novac2c.cdn.weixin.qq.com/c2c"
	ilinkAppID      = "bot"
	ilinkClientVer  = "132102" // (2<<16)|(4<<8)|6 => 2.4.6
	defaultBotAgent = "OpenClaw"
	defaultChanVer  = "2.4.6"
)

// API JSON 响应体的读取与报错口径：
//   - maxAPIResponseBytes（8MB）：baseURL 是用户可自定义的网关，本模块按信任处理该
//     地址，但其响应体量不受信——异常/恶意网关回超大数据即可经无界 io.ReadAll 打爆
//     内存。正常 iLink 响应（含长轮询整包消息）远小于此，超限一律视为协议异常拒绝，
//     与 CDN 下载侧 maxInboundAttachmentSize 同口径设硬上限。
//   - maxErrorBodyBytes（512B）：状态码异常与反序列化失败两处错误文本都引用响应原文
//     做诊断，响应会整份进 error 与 toast，必须截断到可读长度。
const (
	maxAPIResponseBytes = 8 << 20
	maxErrorBodyBytes   = 512
)

// readAPIBody 封顶读取 API 响应体：多读 1 字节用于判别"恰好达到上限"与"确已超限"。
func readAPIBody(body io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(body, maxAPIResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxAPIResponseBytes {
		return nil, fmt.Errorf("api response exceeds %d MB cap", maxAPIResponseBytes>>20)
	}
	return data, nil
}

// truncateForError 把响应体压进可进错误文本/提示的长度，超限部分以如实标记替代。
func truncateForError(data []byte) string {
	if len(data) <= maxErrorBodyBytes {
		return string(data)
	}
	return fmt.Sprintf("%s... [truncated, %d bytes total]", data[:maxErrorBodyBytes], len(data))
}

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

// NewClient 创建 iLink HTTP 客户端；baseURL 传空回退官方端点（自定义网关场景由账号配置传入）。
// DisableCompression 必须保持 true：CDN 密文下载若被 gzip 变换字节数，AES 解密必然失败。
func NewClient(baseURL string) *Client {
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	baseURL = strings.TrimRight(baseURL, "/")

	transport := &http.Transport{
		Proxy: nil, // N25 显式定调：腾讯国内端点恒定直连（此前为零值 Transport 的隐性直连）。
		// 微信协议服务器按地区就近调度，走用户 Clash 式代理反而改出口触发风控/掉线，
		// 与"浏览器同进退"的下载族诉求相反——收口时点名排除，防止后续公共件改造误伤。
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

	respBytes, err := readAPIBody(resp.Body)
	if err != nil {
		return fmt.Errorf("read response from %s failed: %w", endpoint, err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("http post %s returned status %d: %s", endpoint, resp.StatusCode, truncateForError(respBytes))
	}

	if respOut != nil && len(respBytes) > 0 {
		if err := json.Unmarshal(respBytes, respOut); err != nil {
			return fmt.Errorf("unmarshal response from %s failed: %w, raw: %s", endpoint, err, truncateForError(respBytes))
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

	respBytes, err := readAPIBody(resp.Body)
	if err != nil {
		return fmt.Errorf("read response from %s failed: %w", endpoint, err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("http get %s returned status %d: %s", endpoint, resp.StatusCode, truncateForError(respBytes))
	}

	if respOut != nil && len(respBytes) > 0 {
		if err := json.Unmarshal(respBytes, respOut); err != nil {
			return fmt.Errorf("unmarshal response from %s failed: %w, raw: %s", endpoint, err, truncateForError(respBytes))
		}
	}

	return nil
}
