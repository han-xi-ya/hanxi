package wechat

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

type uploadURLReq struct {
	BotToken    string   `json:"bot_token"`
	FileKey     string   `json:"filekey"`
	MediaType   int      `json:"media_type"` // 1: Image, 3: File
	ToUserID    string   `json:"to_user_id"`
	RawSize     int      `json:"rawsize"`
	RawFileMD5  string   `json:"rawfilemd5"`
	FileSize    int      `json:"filesize"` // PKCS#7 密文尺寸
	NoNeedThumb bool     `json:"no_need_thumb"`
	AESKey      string   `json:"aeskey"` // 32 字符 Hex
	BaseInfo    BaseInfo `json:"base_info"`
}

type uploadURLResp struct {
	UploadParam   string `json:"upload_param"`
	UploadFullURL string `json:"upload_full_url"`
	Ret           int    `json:"ret"`
	ErrMsg        string `json:"errmsg"`
}

// UploadMediaResult 上传后的媒体凭据信息
type UploadMediaResult struct {
	EncryptQueryParam string
	AESKeyHex         string
	CipherSize        int
	RawSize           int
}

// UploadMediaFile 读取本地文件，执行 AES-128-ECB 加密并上传到腾讯 Nova CDN
// mediaType: 1=Image, 3=File
func (c *Client) UploadMediaFile(ctx context.Context, botToken, toUserID, filePath string, mediaType int) (*UploadMediaResult, error) {
	rawBytes, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("read file failed: %w", err)
	}
	if len(rawBytes) == 0 {
		return nil, fmt.Errorf("file is empty")
	}

	rawSize := len(rawBytes)
	rawMD5 := md5Hex(rawBytes)
	cipherSize := aesEcbPaddedSize(rawSize)

	// 1. 生成 16 字节 (32 hex) 随机 filekey 与 aeskey
	fileKeyHex, err := randomHex(16)
	if err != nil {
		return nil, fmt.Errorf("generate filekey failed: %w", err)
	}
	aesKeyBytes, err := randomBytes(16)
	if err != nil {
		return nil, fmt.Errorf("generate aeskey failed: %w", err)
	}
	aesKeyHex := fmt.Sprintf("%x", aesKeyBytes)

	// 2. 向微信申请 CDN 上传地址
	uploadReq := uploadURLReq{
		BotToken:    botToken,
		FileKey:     fileKeyHex,
		MediaType:   mediaType, // 1 for Image, 3 for File
		ToUserID:    toUserID,
		RawSize:     rawSize,
		RawFileMD5:  rawMD5,
		FileSize:    cipherSize,
		NoNeedThumb: true,
		AESKey:      aesKeyHex,
		BaseInfo:    defaultBaseInfo(),
	}

	var urlResp uploadURLResp
	err = c.post(ctx, "/ilink/bot/getuploadurl", botToken, uploadReq, &urlResp)
	if err != nil {
		return nil, fmt.Errorf("request upload url failed: %w", err)
	}
	if urlResp.Ret != 0 && urlResp.Ret != 200 {
		return nil, fmt.Errorf("request upload url error: ret=%d, msg=%s", urlResp.Ret, urlResp.ErrMsg)
	}

	uploadURL := urlResp.UploadFullURL
	if uploadURL == "" {
		if urlResp.UploadParam == "" {
			return nil, fmt.Errorf("both upload_full_url and upload_param are empty")
		}
		uploadURL = fmt.Sprintf("%s/upload?encrypted_query_param=%s&filekey=%s", c.cdnBase, url.QueryEscape(urlResp.UploadParam), fileKeyHex)
	}

	// 3. 本地 AES-128-ECB 加密
	cipherBytes, err := aesEcbEncrypt(rawBytes, aesKeyBytes)
	if err != nil {
		return nil, fmt.Errorf("encrypt media failed: %w", err)
	}
	if len(cipherBytes) != cipherSize {
		return nil, fmt.Errorf("ciphertext length %d does not match expected %d", len(cipherBytes), cipherSize)
	}

	// 4. POST 密文流到 CDN
	cdnReq, err := http.NewRequestWithContext(ctx, http.MethodPost, uploadURL, bytes.NewReader(cipherBytes))
	if err != nil {
		return nil, fmt.Errorf("create cdn upload request failed: %w", err)
	}
	cdnReq.Header.Set("Content-Type", "application/octet-stream")
	cdnReq.Header.Set("Content-Length", fmt.Sprintf("%d", len(cipherBytes)))

	// CDN 必须使用强制 HTTP/1.1
	cdnClient := &http.Client{
		Transport: &http.Transport{
			DisableKeepAlives:     true,
			DisableCompression:    true,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 30 * time.Second,
		},
		Timeout: 60 * time.Second,
	}

	cdnResp, err := cdnClient.Do(cdnReq)
	if err != nil {
		return nil, fmt.Errorf("upload to cdn failed: %w", err)
	}
	defer cdnResp.Body.Close()

	if cdnResp.StatusCode < 200 || cdnResp.StatusCode >= 300 {
		bodySnippet, _ := io.ReadAll(io.LimitReader(cdnResp.Body, 512))
		return nil, fmt.Errorf("cdn upload returned status %d: %s", cdnResp.StatusCode, string(bodySnippet))
	}

	// 从 CDN 响应 Header 获取 x-encrypted-param
	encryptedParam := cdnResp.Header.Get("x-encrypted-param")
	if encryptedParam == "" {
		// 某些情况下 Header 为大小写不同，尝试小写遍历
		for k, v := range cdnResp.Header {
			if strings.EqualFold(k, "x-encrypted-param") && len(v) > 0 {
				encryptedParam = v[0]
				break
			}
		}
	}

	if encryptedParam == "" {
		return nil, fmt.Errorf("cdn response missing x-encrypted-param header")
	}

	return &UploadMediaResult{
		EncryptQueryParam: encryptedParam,
		AESKeyHex:         aesKeyHex,
		CipherSize:        cipherSize,
		RawSize:           rawSize,
	}, nil
}

// UploadImageFile 读取本地图片并上传 (mediaType=1)
func (c *Client) UploadImageFile(ctx context.Context, botToken, toUserID, filePath string) (*UploadMediaResult, error) {
	return c.UploadMediaFile(ctx, botToken, toUserID, filePath, 1)
}
