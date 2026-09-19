package version

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
)

// 说明：下载主流程（多源回退、流式 SHA-256、字节双核、原子落位临时件）已收口
// 至内核 artifact.Fetch；本文件只保留 Rufus 领域辅助——镜像 URL 模板与
// 模块侧断言/哈希/拷贝工具。

// assetMirrors 构造直连与镜像下载 URL 候选列表（与 ccswitch/rustdesk 同一组镜像前缀）。
func assetMirrors(version, assetName string) []string {
	relPath := fmt.Sprintf("%s/%s/releases/download/%s/%s", repoOwner, repoName, version, assetName)
	return []string{
		"https://github.com/" + relPath,
		"https://ghfast.top/https://github.com/" + relPath,
		"https://gh-proxy.com/https://github.com/" + relPath,
		"https://mirror.ghproxy.com/https://github.com/" + relPath,
	}
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

// verifyPEMagic 校验文件头 MZ 魔数：单文件 exe 无 zip 布局可自检，
// PE 魔数是"落地文件确实是可执行体而非 HTML 错误页/截断残片"的最低断言。
func verifyPEMagic(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	var head [2]byte
	if _, err := io.ReadFull(f, head[:]); err != nil {
		return fmt.Errorf("读取文件头失败: %w", err)
	}
	if head[0] != 'M' || head[1] != 'Z' {
		return fmt.Errorf("文件不是有效的 Windows 可执行体（MZ 头缺失，疑似镜像返回了错误页）")
	}
	return nil
}

// copyFileTo 流式拷贝（导入搬运用；本地文件间复制不涉及网络）。
func copyFileTo(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
