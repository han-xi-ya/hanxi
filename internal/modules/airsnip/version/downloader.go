package version

import "fmt"

// assetMirrors 构造直连与镜像下载 URL 候选列表（与 markeron/frpc/ccswitch/litemonitor
// 同一组镜像前缀）。GitHub release 资产域名（release-assets.githubusercontent.com）
// 在部分网络环境间歇性 DNS 失败（侦查阶段本机实测 curl exit 56/http 000），
// 多镜像回退是必需而非锦上添花——上游自己的自动更新也内置三个 gh-proxy 镜像
// （src/update/endpoints.rs），可见"GitHub 直连不稳"是上游公认事实。
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
