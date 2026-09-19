package version

import (
	"fmt"
	"io"
	"net/http"
	"strings"
)

// assetMirrors 构造直连与镜像下载 URL 候选列表（与 markeron/ccswitch 同一组镜像前缀）。
// 路径模板对任意 owner/repo 泛化；首个为主址，其余为同摘要备用传输来源，
// 实际下载/校验/回退纪律已收口内核 artifact.Fetch。
func assetMirrors(version, assetName string) []string {
	relPath := fmt.Sprintf("%s/%s/releases/download/%s/%s", repoOwner, repoName, version, assetName)
	return []string{
		"https://github.com/" + relPath,
		"https://ghfast.top/https://github.com/" + relPath,
		"https://gh-proxy.com/https://github.com/" + relPath,
		"https://mirror.ghproxy.com/https://github.com/" + relPath,
	}
}

// downloadSmall 拉取小体积资产（SHA256SUMS.txt 级别），限制最大读取字节防失控。
// 仅服务官方校验清单交叉比对（第二只眼）；主交付资产的受控下载走内核 artifact.Fetch。
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

// crossCheckSums 用官方 SHA256SUMS.txt 交叉比对安装器校验和（GitHub digest 之外的第二只眼）。
// 清单行格式容错解析：`<64位hex> [可选*或空格]<文件名>`；
// 网络拉取失败仅告警放行（digest 已是官方第一依据），但清单存在且与安装器名匹配却
// 哈希不一致时硬失败——两个官方来源互相矛盾说明下载链路有篡改。
// localSHA 传入内核 Fetch 已双核验证的官方摘要（与重读安装体自哈希等价）；
// urls 由调用方经 mirrors 接缝构造（与安装器同一组候选前缀，失败注入测试
// 可把清单请求一并引到回环假源）。
func crossCheckSums(client *http.Client, urls []string, installerName, localSHA string) error {
	body, err := downloadSmall(client, urls, 64<<10)
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
