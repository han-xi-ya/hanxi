package version

import (
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

func downloadTo(client *http.Client, urls []string, dest string, onProgress func(int64)) error {
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		for _, url := range urls {
			if err := tryDownload(client, url, dest, onProgress); err == nil {
				return nil
			} else {
				lastErr = err
			}
		}
		if attempt == 0 {
			time.Sleep(time.Second)
		}
	}
	return fmt.Errorf("所有下载源均失败: %w", lastErr)
}

func tryDownload(client *http.Client, url, dest string, onProgress func(int64)) error {
	req, err := http.NewRequest(http.MethodGet, url, nil)
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
