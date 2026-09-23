// downloader.go 传输工具：GitHub 直链 + 加速镜像 URL 模板与本地哈希工具。
// 下载主链无模块 bespoke——上游全资产带官方 digest（实测），完整性一律
// 委托内核 artifact.Fetch（摘要必检 + 镜像回退 + 流式上限），与 markeron
// 同构；本包只保留领域 URL 形状与账本哈希计算。
package version

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
)

// githubMirrors 构造主址与镜像候选列表（首个为主址；路径模板对任意
// owner/repo/tag/asset 泛化，与 markeron/windterm 共用同一组镜像前缀）。
// 注意：GitHub release 下载路径以 tag 原文为段（Termora tag 无 v 前缀）。
func githubMirrors(tag, assetName string) []string {
	relPath := fmt.Sprintf("%s/%s/releases/download/%s/%s", repoOwner, repoName, tag, assetName)
	return []string{
		"https://github.com/" + relPath,
		"https://ghfast.top/https://github.com/" + relPath,
		"https://gh-proxy.com/https://github.com/" + relPath,
		"https://mirror.ghproxy.com/https://github.com/" + relPath,
	}
}

// fileSize 返回文件字节数。
func fileSize(path string) (int64, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return fi.Size(), nil
}

// fileSHA256 计算文件全量 SHA-256（十六进制小写；失败返回空串——仅诊断
// 入账用途，下载完整性由内核 Fetch 流式+落盘双核收口）。
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
