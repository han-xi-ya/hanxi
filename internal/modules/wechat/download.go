package wechat

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const maxInboundAttachmentSize int64 = 512 << 20

func (c *Client) DownloadInboundFile(ctx context.Context, media InboundMedia, expectedSize int64) ([]byte, error) {
	if strings.TrimSpace(media.EncryptQueryParam) == "" {
		return nil, fmt.Errorf("文件下载参数为空")
	}
	if expectedSize < 0 || expectedSize > maxInboundAttachmentSize {
		return nil, fmt.Errorf("文件大小超出限制")
	}

	downloadURL := c.cdnBase + "/download?encrypted_query_param=" + url.QueryEscape(media.EncryptQueryParam)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return nil, fmt.Errorf("创建下载请求失败: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("下载微信文件失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("微信文件 CDN 返回 HTTP %d", resp.StatusCode)
	}

	limit := maxInboundAttachmentSize + 16 + 1
	ciphertext, err := io.ReadAll(io.LimitReader(resp.Body, limit))
	if err != nil {
		return nil, fmt.Errorf("读取微信文件失败: %w", err)
	}
	if int64(len(ciphertext)) >= limit {
		return nil, fmt.Errorf("微信文件超过大小限制")
	}

	key, err := parseInboundAESKey(media.AESKey)
	if err != nil {
		return nil, err
	}
	plaintext, err := aesEcbDecrypt(ciphertext, key)
	if err != nil {
		return nil, fmt.Errorf("解密微信文件失败: %w", err)
	}
	return plaintext, nil
}

func parseInboundAESKey(encoded string) ([]byte, error) {
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil {
		return nil, fmt.Errorf("解析微信文件密钥失败: %w", err)
	}
	if len(decoded) == 16 {
		return decoded, nil
	}
	if len(decoded) == 32 {
		key, err := hex.DecodeString(string(decoded))
		if err != nil {
			return nil, fmt.Errorf("解析微信文件十六进制密钥失败: %w", err)
		}
		if len(key) == 16 {
			return key, nil
		}
	}
	return nil, fmt.Errorf("微信文件密钥长度异常: %d", len(decoded))
}

func writeFileAtomically(target string, data []byte) error {
	dir := filepath.Dir(target)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("创建保存目录失败: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".hubkit-wechat-*.tmp")
	if err != nil {
		return fmt.Errorf("创建临时文件失败: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("写入文件失败: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("同步文件失败: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("关闭文件失败: %w", err)
	}
	if err := os.Rename(tmpName, target); err != nil {
		// Windows 不允许 Rename 覆盖现有文件；保存对话框已完成覆盖确认。
		if removeErr := os.Remove(target); removeErr != nil && !os.IsNotExist(removeErr) {
			return fmt.Errorf("替换已有文件失败: %w", removeErr)
		}
		if retryErr := os.Rename(tmpName, target); retryErr != nil {
			return fmt.Errorf("保存文件失败: %w", retryErr)
		}
	}
	return nil
}

func defaultDownloadsDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, "Downloads")
}

func attachmentTimeout() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 2*time.Minute)
}
