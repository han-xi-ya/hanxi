// check_update.go 实现 extapi.UpdateChecker 薄 shim：为宿主级调度器
// （internal/updatewatch）提供"可用更新"感知，点亮四维投影健康维度。
//
// 廉价承诺（契约要求）：本地 = ListInstalled 便携版本树扫描，远程 = 复用
// ListRemote(portable) 的 portableCache（官方更新网关 feed，10 分钟 TTL）——
// 不新建轮子、不写盘、不触发模块懒激活；并发与超时由调度器统一裁决。
package version

import (
	"context"
	"strings"

	"hanxi/internal/platform/versioncmp"
)

// CheckUpdate 比较"远程便携通道最新条目 vs 本机已装便携版最新条目"得出可用
// 更新结论。双形态口径：安装版（installer）经注册表探测、本机至多一份且升级
// 由用户自行安装官方安装器完成，不属托管版本树事实面，不参与本判定；托管的
// 便携形态无安装记录时按"无法判定"返回 false（下载属交付路径，不谎报为更新）。
// 返回语义（extapi.UpdateChecker 契约）：
//   - local/remote 为参与比较的版本串（空串 = 该侧无事实可比）；
//   - hasUpdate 仅当两侧都有值且远程更新为 true；
//   - err 仅在远程 feed 取不到/本地版本树读不了时上抛，调度器据此保持原健康值。
func (m *Manager) CheckUpdate(ctx context.Context) (local, remote string, hasUpdate bool, err error) {
	if err := ctx.Err(); err != nil {
		return "", "", false, err
	}
	releases, err := m.ListRemote(FormPortable)
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
