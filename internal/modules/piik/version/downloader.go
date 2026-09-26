package version

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"
)

// assetMirrors 构造直连与镜像下载 URL 候选列表（与 markeron/dbx/ccswitch
// 同一组镜像前缀；首个为主址，其余为同摘要备用传输来源，实际下载/校验/
// 回退纪律已收口内核 artifact.Fetch）。路径模板对任意 owner/repo 泛化。
//
// Gitee 镜像备位注记（诚实申明，契约要求挂账）：曾考虑追加 Gitee 镜像
// releases 作备用传输源，但镜像仓地址与同步时延均未经实测验证——备位
// 槽位保留、暂不启用。伪造或猜测镜像 URL 比留空更危险（把用户流量导向
// 陌生域名）；下载完整性已由 GitHub digest 单一信任根收口，未来实测
// 确认后在此追加候选址即可，校验闸保证坏源/旧包一律进不来。
func assetMirrors(version, assetName string) []string {
	relPath := fmt.Sprintf("%s/%s/releases/download/%s/%s", repoOwner, repoName, version, assetName)
	return []string{
		"https://github.com/" + relPath,
		"https://ghfast.top/https://github.com/" + relPath,
		"https://gh-proxy.com/https://github.com/" + relPath,
		"https://mirror.ghproxy.com/https://github.com/" + relPath,
	}
}

// fileSHA256 计算文件全量 SHA-256（十六进制小写）。
// 用途：主 exe 落位/漂移复查摘要记账（账外漂移护栏的信任锚点），不参与
// 下载校验主流程（下载完整性由内核 artifact.Fetch 的官方摘要双核收口）。
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
