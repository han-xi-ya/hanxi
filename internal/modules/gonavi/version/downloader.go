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

	"hanxi/packages/go/netx"
)

// assetMirrors 构造直连与镜像下载 URL 候选列表（与 markeron/ccswitch 同一组
// 镜像前缀；禁用上游 latest.json 内私有 VPS 镜像 URL——下载一律 GitHub 直链
// + 既有家族镜像回退通道）。路径模板对任意 owner/repo 泛化；首个为主址，
// 其余为同摘要备用传输来源，实际下载/校验/回退纪律已收口内核 artifact.Fetch。
func assetMirrors(version, assetName string) []string {
	relPath := fmt.Sprintf("%s/%s/releases/download/%s/%s", repoOwner, repoName, version, assetName)
	return []string{
		"https://github.com/" + relPath,
		"https://ghfast.top/https://github.com/" + relPath,
		"https://gh-proxy.com/https://github.com/" + relPath,
		"https://mirror.ghproxy.com/https://github.com/" + relPath,
	}
}

// downloadSmall 拉取小体积资产（SHA256SUMS 清单级别），限制最大读取字节防失控。
// 仅服务备用官方摘要源解析（GitHub digest 缺失时）；主交付资产的受控下载
// （重试 + 镜像回退 + 流式上限 + 摘要双核）走内核 artifact.Fetch。
// 传输层取混合闸形态（回环恒定直连 + 外网走 netx 代理链），与内核 Fetch
// 同闸——失败注入测试把清单请求引到回环假源时不受系统代理干扰。
func downloadSmall(urls []string, maxBytes int64) ([]byte, error) {
	client := &http.Client{
		Timeout:   30 * time.Second,
		Transport: &http.Transport{Proxy: netx.LoopbackAwareProxyFunc()},
	}
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

// sumsDigest 从官方 SHA256SUMS 清单内容解析指定资产名的期望摘要（备用
// 第一层信任根）。清单行格式容错解析：`<64位hex> [可选*或空格]<文件名>`，
// 与 recordly.checkSumsBody 同方言。找不到可比对条目即报错——备用源解析
// 失败没有第三只眼，宁拒不猜。纯函数，单测直接注入样例清单。
func sumsDigest(body []byte, assetName string) (string, error) {
	want := ""
	for _, line := range strings.Split(string(body), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		hash := strings.ToLower(fields[0])
		if !sha256HexRe.MatchString(hash) {
			continue
		}
		name := strings.ToLower(strings.TrimLeft(fields[len(fields)-1], "*"))
		if name == strings.ToLower(assetName) {
			want = hash
		}
	}
	if want == "" {
		return "", fmt.Errorf("官方 SHA256SUMS 清单中未找到 %s 的可比对条目", assetName)
	}
	return want, nil
}

// fileSHA256 计算文件全量 SHA-256（十六进制小写）。
// 用途：exe 落位/漂移复查摘要记账（账外漂移护栏的信任锚点），
// 不参与下载校验主流程（下载完整性由内核 artifact.Fetch 的
// 官方摘要双核收口）。
func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// verifySHA256 文件实测摘要与期望值核对（漂移复查用；大小写不敏感）。
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
