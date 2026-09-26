package version

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"
)

// dbxSiteDownloadBase 上游官网发布通道前缀（dl.dbxio.com）。
// 诚实申明：该源为侦查发现，未实测验证可用性与同步时延，仅作镜像回退家族
// 的排后备位；与 GitHub 直链共享同一官方 digest 校验闸，坏源/旧包都进不来。
const dbxSiteDownloadBase = "https://dl.dbxio.com/releases/latest/"

// assetMirrors 构造直连与镜像下载 URL 候选列表（与 markeron/ccswitch 同一组
// 镜像前缀；首个为主址，其余为同摘要备用传输来源，实际下载/校验/回退纪律
// 已收口内核 artifact.Fetch）。路径模板对任意 owner/repo 泛化；末位追加
// 上游官网 dl.dbxio.com 备位（未实测源，见 dbxSiteDownloadBase 注释）。
func assetMirrors(version, assetName string) []string {
	relPath := fmt.Sprintf("%s/%s/releases/download/%s/%s", repoOwner, repoName, version, assetName)
	return []string{
		"https://github.com/" + relPath,
		"https://ghfast.top/https://github.com/" + relPath,
		"https://gh-proxy.com/https://github.com/" + relPath,
		"https://mirror.ghproxy.com/https://github.com/" + relPath,
		dbxSiteDownloadBase + assetName,
	}
}

// fileSHA256 计算文件全量 SHA-256（十六进制小写）。
// 用途：exe 落位/漂移复查摘要记账（账外漂移护栏的信任锚点）与
// portable-update.json 旁证复核，不参与下载校验主流程
// （下载完整性由内核 artifact.Fetch 的官方摘要双核收口）。
func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// verifySHA256 文件实测摘要与期望值核对（漂移复查用；大小写不敏感）。
func verifySHA256(path, want string) error {
	got, err := fileSHA256(path)
	if err != nil {
		return err
	}
	if !strings.EqualFold(got, want) {
		return fmt.Errorf("sha256 不匹配：期望 %s，实际 %s", want, got)
	}
	return nil
}
