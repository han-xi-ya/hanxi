package version

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
)

// 说明：下载主流程（多源回退、流式 SHA-256、字节双核、原子落位临时件）已收口
// 至内核 artifact.Fetch；本文件只保留 MangoDisk 领域辅助——镜像 URL 模板与
// 模块侧哈希/尺寸/拷贝工具。

// assetMirrors 构造直连与镜像下载 URL 候选列表（与 markeron/frpc 同一组镜像前缀）。
func assetMirrors(version, assetName string) []string {
	relPath := fmt.Sprintf("%s/%s/releases/download/%s/%s", repoOwner, repoName, version, assetName)
	return []string{
		"https://github.com/" + relPath,
		"https://ghfast.top/https://github.com/" + relPath,
		"https://gh-proxy.com/https://github.com/" + relPath,
		"https://mirror.ghproxy.com/https://github.com/" + relPath,
	}
}

// fileSize 返回文件字节数（装机与声明对齐断言用）。
func fileSize(path string) (int64, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	return fi.Size(), nil
}

// fileSHA256 计算文件 sha256（仅作账本诊断记录；下载校验使用官方 release digest，
// 由内核 artifact.Fetch 流式 + 落盘双核收口）。
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
