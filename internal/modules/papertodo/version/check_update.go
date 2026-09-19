// check_update.go 实现 extapi.UpdateChecker 薄 shim：为宿主级调度器
// （internal/updatewatch）提供"可用更新"感知，点亮四维投影健康维度。
//
// 廉价承诺（契约要求）：本地 = ListInstalled 托管目录扫描，远程 = 复用
// ListRemote 的 remoteCache（10 分钟 TTL，失败降级旧缓存）——不新建轮子、
// 不写盘、不触发模块懒激活；并发与超时由调度器统一裁决。
package version

import (
	"context"
	"strings"

	"hanxi/internal/platform/versioncmp"
)

// CheckUpdate 比较"远程最新条目 vs 本机已装最新条目"得出可用更新结论。
// 远程列表在构建期已剔除 draft/prerelease/非规范 tag 与双变体资产不齐项，
// 首条即可用的最新正式版。返回语义（extapi.UpdateChecker 契约）：
//   - local/remote 为参与比较的版本串（空串 = 该侧无事实可比）；
//   - hasUpdate 仅当两侧都有值且远程更新为 true；本机无托管资产按"无法判定"
//     返回 false——下载属交付路径，不谎报为更新；
//   - err 仅在远程列表取不到/本地扫描失败时上抛，调度器据此保持原健康值。
//
// 本机权威版本可能是 hanxi-meta tag（v3.31）或 PE FileVersion（3.31.0.0）
// 两种形状，versioncmp 段数比较天然兼容（"3.31" == "3.31.0.0" 视为本机更新，
// 不误报）。
func (m *Manager) CheckUpdate(ctx context.Context) (local, remote string, hasUpdate bool, err error) {
	if err := ctx.Err(); err != nil {
		return "", "", false, err
	}
	releases, err := m.ListRemote()
	if err != nil {
		return "", "", false, err
	}
	if len(releases) == 0 {
		return "", "", false, nil
	}
	remote = releases[0].Version
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
