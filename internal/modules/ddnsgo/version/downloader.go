package version

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
)

// assetMirrors 构造直连与镜像下载 URL 候选列表（首个为主址；与 ccswitch/
// markeron/frpc 同一组镜像前缀）。下载/摘要/字节数双核已收口内核 artifact.Fetch，
// 本包只保留"镜像只是同摘要的备用传输来源"这一领域 URL 模板。
func assetMirrors(version, assetName string) []string {
	relPath := fmt.Sprintf("%s/%s/releases/download/%s/%s", repoOwner, repoName, version, assetName)
	return []string{
		"https://github.com/" + relPath,
		"https://ghfast.top/https://github.com/" + relPath,
		"https://gh-proxy.com/https://github.com/" + relPath,
		"https://mirror.ghproxy.com/https://github.com/" + relPath,
	}
}

// fileSHA256 计算文件全量 SHA-256（十六进制小写）。
// 注意：仅用于落位账本的 exe 诊断摘要（AssetSHA256），不参与下载校验主流程
// （下载完整性已由内核 artifact.Fetch 的官方摘要双核收口）。
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
