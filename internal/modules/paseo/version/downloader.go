package version

import "fmt"

// assetMirrors 构造直连与镜像下载 URL 候选列表（与 ccswitch/recordly 同一组镜像前缀）。
// 注意：release 下载 URL 大小写敏感，本函数以 tag 原文与资产原名直拼，不可
// lowercase 归一；首个为主址，其余为同摘要备用传输来源，实际下载/校验/回退
// 纪律已收口内核 artifact.Fetch。
func assetMirrors(tag, assetName string) []string {
	relPath := fmt.Sprintf("%s/%s/releases/download/%s/%s", repoOwner, repoName, tag, assetName)
	return []string{
		"https://github.com/" + relPath,
		"https://ghfast.top/https://github.com/" + relPath,
		"https://gh-proxy.com/https://github.com/" + relPath,
		"https://mirror.ghproxy.com/https://github.com/" + relPath,
	}
}
