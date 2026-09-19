package version

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
)

// assetMirrors 构造直连与镜像下载 URL 候选列表（与 ccswitch/markeron/frpc 同一组镜像前缀）。
// 实测经验：国内网络直连 github.com 拉 release 资产常在数 MB 处 Connection reset，
// gh-proxy 系镜像可稳定跑完且与官方 digest 校验一致。镜像只是同一官方摘要的
// 备用传输来源，不作信任根（摘要必检收口在内核 artifact.Fetch）。
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
// 注意：仅用于诊断入账链（exe 自哈希写进落位账本），不参与下载校验主流程
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
