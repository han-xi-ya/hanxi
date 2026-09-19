// updates_service.go 是"可用更新"感知链的宿主 RPC 装配面（Wave 4+ 健康维度）：
// 手动全量感知一轮（RefreshUpdates）与调度器注入。感知结果的呈现零新 RPC——
// 健康维度随 ListModuleStates 既有投影携带（registry.SetHealth → Project），
// 一轮收口经 updates:checked 事件提示前端重拉。契约见 extapi/update.go、
// 调度策略见 internal/updatewatch 包注释。
package app

import (
	"context"
	"fmt"

	"hanxi/internal/extapi"
	"hanxi/internal/updatewatch"
)

// SetUpdateWatcher 装配根注入更新感知调度器（与 SetOperations 同级的装配缝）。
// 未注入（如降级装配/单测）时 RefreshUpdates 如实报错，绝不静默假成功。
func (s *AppService) SetUpdateWatcher(watcher *updatewatch.Scheduler) {
	s.updateWatcher = watcher
}

// RefreshUpdates 手动触发（或加入）一轮全量可用更新感知，阻塞至收口，
// 返回本轮成功判定的模块数。逐模块失败保持原健康值不谎报（updatewatch 口径）；
// 远程列表各有 10 分钟 TTL 缓存，连点不放大网络请求（single-flight 合并）。
func (s *AppService) RefreshUpdates() (int, error) {
	if s.updateWatcher == nil {
		return 0, fmt.Errorf("更新感知调度器未装配")
	}
	return s.updateWatcher.CheckNow(context.Background())
}

// collectUpdateCheckers 从待注册模块集合中收集实现 extapi.UpdateChecker 的
// 模块（键 = 模块 ID）。托管模块的版本引擎 shim 不依赖模块激活，未启用/
// 未安装模块同样参与感知（健康信号对停用模块仍是真实事实，呈现口径由
// 前端按四维投影自行裁决）。
func collectUpdateCheckers(mods []extapi.Module) map[string]extapi.UpdateChecker {
	out := map[string]extapi.UpdateChecker{}
	for _, m := range mods {
		checker, ok := m.(extapi.UpdateChecker)
		if !ok {
			continue
		}
		id := m.Info().ID
		if _, dup := out[id]; dup {
			continue // 重复 ID 已由 registry.Register 拒收，此处仅防御
		}
		out[id] = checker
	}
	return out
}
