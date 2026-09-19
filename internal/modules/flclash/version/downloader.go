package version

import "fmt"

// assetMirrors 构造直连与镜像下载 URL 候选列表（与各模块同一组镜像前缀）。
// 路径模板对任意 owner/repo 泛化；首个为主址，其余为同摘要备用传输来源，
// 实际下载/校验/回退纪律已收口内核 artifact.Fetch。
// tag 带 v 前缀（FlClash 的 release tag 与资产版本同形：v0.8.96 ↔ 0.8.96）。
func assetMirrors(tag, assetName string) []string {
	relPath := fmt.Sprintf("%s/%s/releases/download/%s/%s", repoOwner, repoName, tag, assetName)
	return []string{
		"https://github.com/" + relPath,
		"https://ghfast.top/https://github.com/" + relPath,
		"https://gh-proxy.com/https://github.com/" + relPath,
		"https://mirror.ghproxy.com/https://github.com/" + relPath,
	}
}
