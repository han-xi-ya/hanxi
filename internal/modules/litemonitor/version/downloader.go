package version

import "fmt"

// assetMirrors 构造直连与镜像下载 URL 候选列表（与 markeron/frpc/ccswitch 同一组镜像前缀）。
// GitHub release 资产域名（release-assets.githubusercontent.com）在部分网络环境
// 间歇性 DNS 失败（侦查阶段实测 curl exit 56），多镜像回退是必需而非锦上添花。
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
