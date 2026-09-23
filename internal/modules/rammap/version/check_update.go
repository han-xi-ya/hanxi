// check_update.go 实现 extapi.UpdateChecker 薄 shim（宿主更新感知调度器直调）。
//
// 廉价承诺（契约要求）：本地 = ListInstalled 版本树扫描，远程 = 复用
// ListRemote 的 remoteCache（10 分钟 TTL HEAD 探测，失败降级旧缓存）——
// 不新建轮子、不写盘、不触发模块懒激活。
//
// 版本语义特殊性（区别于 GitHub 家族）：令牌为 ISO 日期串（YYYY-MM-DD），
// 字典序即时间序，直接字符串比较；导入件令牌（v1.63/imported-*）非日期形，
// 不参与"有更新"判定（其上游对应物就是"最新版"本身，无法也无须比对）。
package version

import (
	"context"
)

// CheckUpdate 比较"当前上游最新版日期 vs 本机已装最新日期版"。
// 返回语义见 extapi.UpdateChecker 契约；本机无日期形已装版时 hasUpdate=false
// （未安装托管资产：入口是"下载"而非"更新"）。
func (m *Manager) CheckUpdate(ctx context.Context) (local, remote string, hasUpdate bool, err error) {
	if err := ctx.Err(); err != nil {
		return "", "", false, err
	}
	releases, err := m.ListRemote()
	if err != nil || len(releases) == 0 {
		return "", "", false, err
	}
	remote = releases[0].Version
	installed, err := m.ListInstalled()
	if err != nil {
		return "", remote, false, err
	}
	for _, v := range installed {
		if dateToken.MatchString(v.Version) {
			local = v.Version
			break
		}
	}
	if local == "" {
		return "", remote, false, nil
	}
	return local, remote, remote > local, nil
}
