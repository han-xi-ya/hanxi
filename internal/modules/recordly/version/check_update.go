// check_update.go 实现 extapi.UpdateChecker 薄 shim：为宿主级调度器
// （internal/updatewatch）提供"可用更新"感知，点亮四维投影健康维度。
//
// 廉价承诺（契约要求）：本地 = ListInstalled 版本树目录扫描，远程 = 复用
// ListRemote 的 remoteCache（10 分钟 TTL，失败降级旧缓存）——不新建轮子、
// 不写盘、不触发模块懒激活；并发与超时由调度器统一裁决。
package version

import (
	"context"
)

// CheckUpdate 比较"远程稳定通道最新条目 vs 本机已装最新条目"得出可用更新结论。
// 返回语义（extapi.UpdateChecker 契约）：
//   - local/remote 为参与比较的版本串（空串 = 该侧无事实可比）；
//   - hasUpdate 仅当两侧都有值且远程更新为 true；本机无托管资产、远程列表
//     无稳定通道条目一律按"无法判定"返回 false——下载属交付路径，不谎报为更新；
//   - err 仅在远程列表取不到/本地版本树读不了时上抛，调度器据此保持原健康值。
//
// 预发布通道（IsPre）不参与更新判定：update-available 语义是"有兼容官方更新"，
// beta 追新不该给用户普遍点亮信号。
func (m *Manager) CheckUpdate(ctx context.Context) (local, remote string, hasUpdate bool, err error) {
	if err := ctx.Err(); err != nil {
		return "", "", false, err
	}
	releases, err := m.ListRemote(false)
	if err != nil {
		return "", "", false, err
	}
	for _, r := range releases {
		if !r.IsPre {
			remote = r.Version
			break
		}
	}
	if remote == "" {
		return "", "", false, nil
	}
	installed, err := m.ListInstalled()
	if err != nil {
		return "", remote, false, err
	}
	if len(installed) == 0 {
		return "", remote, false, nil // 未安装托管资产：入口是"下载"而非"更新"
	}
	local = installed[0].Version
	// 比较用本包 semver.Compare（含预发布序），不用 platform/versioncmp：
	// 本模块已装版本经 hanxi-meta tag 权威解析，beta 通道安装即 "v1.3.5-beta.N"
	// 形态（见 resolveInstalledVersion 注释）；versioncmp 的段式只认"数字+纯字母尾"，
	// 连字符即整串退化字典序——同核心 stable 追平 beta 会漏报、beta 核心领先
	// stable 会误报引导降级（2026-10-26 A7 死代码线索复核坐实，check_update_pre_test.go
	// 双向钉死）。semver.Compare 自行剥 v 前缀，非规范串退化字典序与旧行为同。
	return local, remote, Compare(remote, local) > 0, nil
}
