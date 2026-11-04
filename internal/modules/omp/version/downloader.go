package version

import "fmt"

// assetMirrors 构造直连与镜像下载 URL 候选列表（与 rustdesk/ccswitch 同一组镜像前缀）。
// 回退、断流取消与 SHA-256 全核均在 packages/go/artifact.Fetch 内核内完成，
// 本包只提供领域知识（仓库路径与资产名）。version 传上游原始 tag（vX.Y.Z）。
func assetMirrors(version, assetName string) []string {
	relPath := fmt.Sprintf("%s/%s/releases/download/%s/%s", repoOwner, repoName, version, assetName)
	return []string{
		"https://github.com/" + relPath,
		"https://ghfast.top/https://github.com/" + relPath,
		"https://gh-proxy.com/https://github.com/" + relPath,
		"https://mirror.ghproxy.com/https://github.com/" + relPath,
	}
}
