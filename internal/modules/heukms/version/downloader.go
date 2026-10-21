package version

import "fmt"

// assetMirrors 构造直连与镜像下载 URL 候选列表（与 markeron/frpc/ccswitch/litemonitor
// 同一组镜像前缀）。GitHub release 资产域名（release-assets.githubusercontent.com）
// 在部分网络环境间歇性 DNS 失败（本模块侦查阶段实测 curl 需重试），多镜像回退是
// 必需而非锦上添花；路径模板对任意 owner/repo 泛化，实际下载/校验/回退纪律
// 已收口内核 artifact.Fetch（镜像只作同摘要的备用传输来源，不信任镜像本身）。
func assetMirrors(version, assetName string) []string {
	relPath := fmt.Sprintf("%s/%s/releases/download/%s/%s", repoOwner, repoName, version, assetName)
	return []string{
		"https://github.com/" + relPath,
		"https://ghfast.top/https://github.com/" + relPath,
		"https://gh-proxy.com/https://github.com/" + relPath,
		"https://mirror.ghproxy.com/https://github.com/" + relPath,
	}
}
