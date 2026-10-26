// check_update.go 实现 extapi.UpdateChecker 薄 shim：为宿主级调度器
// （internal/updatewatch）提供"上游出新版"感知，点亮四维投影健康维度。
//
// 廉价承诺（契约要求）：本地 = ListInstalled 版本树目录扫描，远程 = 复用
// ListRemote 的 remoteCache（10 分钟 TTL，失败降级旧缓存）——不新建轮子、
// 不写盘、不触发模块懒激活；并发与超时由调度器统一裁决。
//
// 与其他托管模块的语义差别：本模块没有自动下载通道，"有更新"的实际动作是
// **打开该版发布页自行取主程序再导入**。信号仍然值得点亮——激活工具的新版
// 支持往往直接决定能不能激活新系统/新 Office，机主需要第一时间知道。
package version

import (
	"context"
	"strings"

	"hanxi/internal/platform/versioncmp"
)

// CheckUpdate 比较"远程稳定通道最新条目 vs 本机已装最新条目"得出可用更新结论。
// 返回语义（extapi.UpdateChecker 契约）：
//   - local/remote 为参与比较的版本串（空串 = 该侧无事实可比）；
//   - hasUpdate 仅当两侧都有值且远程更新为 true；本机无已装版本一律按
//     "无法判定"返回 false——首次安装属交付路径，不谎报为更新；
//   - err 仅在远程列表取不到/本地版本树读不了时上抛，调度器据此保持原健康值。
//
// 预发布通道（IsPre）不参与更新判定。imported- 兜底目录由 ListInstalled 的
// importedRank 闸显式沉底（versioncmp 对非规范形状退化字典序，不闸则
// "imported-…" 反被误判最新），installed[0] 恒为最新数字版本——只剩兜底
// 目录时 local 即该兜底目录，此时判不出"有更新"属如实语义。
func (m *Manager) CheckUpdate(ctx context.Context) (local, remote string, hasUpdate bool, err error) {
	if err := ctx.Err(); err != nil {
		return "", "", false, err
	}
	releases, err := m.ListRemote()
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
		return "", remote, false, nil // 未装任何版本：入口是"导入"而非"更新"
	}
	local = installed[0].Version
	trim := func(v string) string { return strings.TrimPrefix(strings.TrimSpace(v), "v") }
	return local, remote, versioncmp.Compare(trim(remote), trim(local)) > 0, nil
}
