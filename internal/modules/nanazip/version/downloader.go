package version

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

func assetMirrors(version, assetName string) []string {
	direct := fmt.Sprintf("https://github.com/%s/%s/releases/download/%s/%s", repoOwner, repoName, version, assetName)
	return []string{direct, "https://ghfast.top/" + direct, "https://gh-proxy.com/" + direct, "https://mirror.ghproxy.com/" + direct}
}

// downloadTo 依次尝试直连与各镜像源。
//
// ctx 贯穿（与 douzy/guoheview/rustdesk 同谱）：取消在**换源之前、重试退避间隙、
// 传输读循环内**（经 http.NewRequestWithContext 断流）均即时响应。旧形态既没有 ctx，
// 又用裸 time.Sleep 做退避，模块停用/退出时这轮下载与 1s 退避都只能干等。
func downloadTo(ctx context.Context, client *http.Client, urls []string, dest string, onProgress func(int64)) error {
	if ctx == nil {
		ctx = context.Background()
	}
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		for _, url := range urls {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := tryDownload(ctx, client, url, dest, onProgress); err == nil {
				return nil
			} else {
				if ctxErr := ctx.Err(); ctxErr != nil {
					return ctxErr // 取消优先于"源失败"，别把断流当故障再重试一轮
				}
				lastErr = err
			}
		}
		if attempt == 0 {
			select {
			case <-time.After(time.Second):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return fmt.Errorf("所有下载源均失败: %w", lastErr)
}

func tryDownload(ctx context.Context, client *http.Client, url, dest string, onProgress func(int64)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()
	buf := make([]byte, 64*1024)
	var done int64
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, err := out.Write(buf[:n]); err != nil {
				return err
			}
			done += int64(n)
			if onProgress != nil {
				onProgress(done)
			}
		}
		if readErr == io.EOF {
			return nil
		}
		if readErr != nil {
			return readErr
		}
	}
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func verifySHA256(path, want string) error {
	got, err := fileSHA256(path)
	if err != nil {
		return err
	}
	if !strings.EqualFold(got, want) {
		return fmt.Errorf("sha256 不匹配：期望 %s，实际 %s", want, got)
	}
	return nil
}
