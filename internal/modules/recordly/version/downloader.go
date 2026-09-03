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

// assetMirrors 构造直连与镜像下载 URL 候选列表（与 ccswitch/markeron 同一组镜像前缀）。
func assetMirrors(version, assetName string) []string {
	relPath := fmt.Sprintf("%s/%s/releases/download/%s/%s", repoOwner, repoName, version, assetName)
	return []string{
		"https://github.com/" + relPath,
		"https://ghfast.top/https://github.com/" + relPath,
		"https://gh-proxy.com/https://github.com/" + relPath,
		"https://mirror.ghproxy.com/https://github.com/" + relPath,
	}
}

// downloadTo 依次尝试候选 URL 下载到目标文件，支持重试与镜像故障转移。
func downloadTo(client *http.Client, urls []string, dest string, onProgress func(done int64)) error {
	const maxRetries = 2
	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * time.Second)
		}
		for _, u := range urls {
			err := tryDownloadSingle(client, u, dest, onProgress)
			if err == nil {
				return nil
			}
			lastErr = err
		}
	}
	if lastErr != nil {
		return fmt.Errorf("所有下载源与重试均失败: %w", lastErr)
	}
	return fmt.Errorf("所有下载源均失败")
}

func tryDownloadSingle(client *http.Client, url, dest string, onProgress func(done int64)) error {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent)

	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusOK:
	case resp.StatusCode >= 300 && resp.StatusCode < 400:
		return fmt.Errorf("unexpected redirect to %s", resp.Header.Get("Location"))
	case resp.StatusCode >= 400:
		return fmt.Errorf("HTTP %d from %s", resp.StatusCode, url)
	}

	buf := make([]byte, 64*1024)
	var done int64
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := out.Write(buf[:n]); werr != nil {
				return werr
			}
			done += int64(n)
			if onProgress != nil {
				onProgress(done)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}
	return nil
}

// downloadSmall 拉取小体积资产（SHA256SUMS.txt 级别），限制最大读取字节防失控。
func downloadSmall(client *http.Client, urls []string, maxBytes int64) ([]byte, error) {
	var lastErr error
	for _, u := range urls {
		req, err := http.NewRequest(http.MethodGet, u, nil)
		if err != nil {
			lastErr = err
			continue
		}
		req.Header.Set("User-Agent", userAgent)
		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxBytes))
		resp.Body.Close()
		if readErr != nil {
			lastErr = readErr
			continue
		}
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("HTTP %d from %s", resp.StatusCode, u)
			continue
		}
		return body, nil
	}
	return nil, lastErr
}

// fileSize 返回文件字节数。
func fileSize(path string) (int64, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return fi.Size(), nil
}

// fileSHA256 计算文件 sha256（下载校验与 SHA256SUMS 交叉比对的实际值来源）。
func fileSHA256(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}

// verifySHA256 校验文件 sha256 是否与期望一致（大小写不敏感）。
func verifySHA256(path, want string) error {
	got := fileSHA256(path)
	if got == "" {
		return fmt.Errorf("无法读取下载文件")
	}
	if !strings.EqualFold(got, want) {
		return fmt.Errorf("sha256 不匹配：期望 %s，实际 %s", want, got)
	}
	return nil
}

// crossCheckSums 用官方 SHA256SUMS.txt 交叉比对安装器校验和（GitHub digest 之外的第二只眼）。
// 清单行格式容错解析：`<64位hex> [可选*或空格]<文件名>`；
// 网络拉取失败仅告警放行（digest 已是官方第一依据），但清单存在且与安装器名匹配却
// 哈希不一致时硬失败——两个官方来源互相矛盾说明下载链路有篡改。
func crossCheckSums(client *http.Client, version, installerName, localSHA string) error {
	body, err := downloadSmall(client, assetMirrors(version, sumsAssetName), 64<<10)
	if err != nil {
		// 清单缺失/网络失败不阻断：降级为单一官方源校验
		return nil
	}
	return checkSumsBody(body, installerName, localSHA)
}

// checkSumsBody 清单内容解析与比对（纯函数，单测直接注入样例清单）。
func checkSumsBody(body []byte, installerName, localSHA string) error {
	var want string
	var wantForAny string
	for _, line := range strings.Split(string(body), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		hash := strings.ToLower(fields[0])
		if len(hash) != 64 || !isHex(hash) {
			continue
		}
		name := strings.ToLower(strings.TrimLeft(fields[len(fields)-1], "*"))
		if wantForAny == "" && strings.HasSuffix(name, ".exe") {
			wantForAny = hash // 兜底：清单只含一个 exe 时直接用它
		}
		if name == strings.ToLower(installerName) {
			want = hash
		}
	}
	if want == "" {
		want = wantForAny
	}
	if want == "" {
		return nil // 清单里找不到可比对条目（上游格式突变），放行交给 digest
	}
	if !strings.EqualFold(want, localSHA) {
		return fmt.Errorf("SHA256SUMS.txt 交叉比对失败：清单 %s，实际 %s", want, localSHA)
	}
	return nil
}

func isHex(s string) bool {
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
