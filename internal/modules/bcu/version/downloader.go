package version

import "fmt"

// assetMirrors 构造直连与镜像下载 URL 候选列表（与 markeron/frpc/ccswitch 同一组镜像前缀）。
// BCU 特有：tag 与资产版本不同形（v6.2 的 release 里资产是 6.2.0），故模板收
// release 自带 tag 而非版本号；首个为主址，其余为同摘要备用传输来源，
// 实际下载/校验/回退纪律已收口内核 artifact.Fetch。
func assetMirrors(tag, assetName string) []string {
	relPath := fmt.Sprintf("%s/%s/releases/download/%s/%s", repoOwner, repoName, tag, assetName)
	return []string{
		"https://github.com/" + relPath,
		"https://ghfast.top/https://github.com/" + relPath,
		"https://gh-proxy.com/https://github.com/" + relPath,
		"https://mirror.ghproxy.com/https://github.com/" + relPath,
	}
}
