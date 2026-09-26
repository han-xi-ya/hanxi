// check_update.go 实现 extapi.UpdateChecker 薄 shim：为宿主级调度器
// （internal/updatewatch）提供"可用更新"感知，点亮四维投影健康维度。
// 接入纪律（softver 教训）：托管模块从第一天就挂上雷达，不等装配线补票。
//
// 廉价承诺（契约要求）：本地 = ListInstalled 版本树目录扫描，远程 = 复用
// ListRemote 的 remoteCache（10 分钟 TTL，失败降级旧缓存）——不新建轮子、
// 不写盘、不触发模块懒激活；并发与超时由调度器统一裁决。
//
// 上游日更风暴口径：Piik 发布频繁，更新感知必然长期"红点常亮"——这是事实
// 而非故障；调度频率归 updatewatch，本 shim 只保证"被问到时答案廉价且诚实"。
// 远程列表彻底取不到时错误一律上抛，绝不吞错伪装"无更新"（remote 失败 ≠
// 无更新，调度器据此保持原健康值）。
package version

import (
	"context"
	"strings"

	"hanxi/internal/platform/versioncmp"
)

// CheckUpdate 比较"远程稳定通道最新条目 vs 本机已装最新条目"得出可用更新
// 结论。返回语义（extapi.UpdateChecker 契约）：
//   - local/remote 为参与比较的版本串（空串 = 该侧无事实可比）；
//   - hasUpdate 仅当两侧都有值且远程更新为 true；本机无托管资产、远程列表
//     无条目一律按"无法判定"返回 false——下载属交付路径，不谎报为更新；
//   - err 仅在远程列表取不到/本地版本树读不了时上抛（上抛 ≠ 无更新）。
//
// 预发布通道已在 parseReleasesBody 过滤（连同低于托管下限 v1.1.0 的历史版本），
// 列表取最新条即可。
func (m *Manager) CheckUpdate(ctx context.Context) (local, remote string, hasUpdate bool, err error) {
	if err := ctx.Err(); err != nil {
		return "", "", false, err
	}
	releases, err := m.ListRemote()
	if err != nil {
		return "", "", false, err
	}
	if len(releases) > 0 {
		remote = releases[0].Version
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
	trim := func(v string) string { return strings.TrimPrefix(strings.TrimSpace(v), "v") }
	return local, remote, versioncmp.Compare(trim(remote), trim(local)) > 0, nil
}
