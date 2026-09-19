// update.go 定义"可用更新"感知链的可选契约（Wave 4+ 托管模块健康维度）。
//
// 背景：ModuleState.Health 的四维投影（catalog.go）此前内建逻辑模块恒
// current、update-available 属"无真实来源的隐藏维度"；本契约让托管模块
// 把各自版本引擎已有的"远程列表 vs 本机账目"能力暴露给宿主级调度器
// internal/updatewatch，由其裁决写入 registry.SetHealth，点亮首页与
// 模块中心的"有更新"信号。前端零新 RPC——状态仍只从 ListModuleStates 消费。
package extapi

import "context"

// UpdateChecker 是模块级可选契约：实现方可被宿主更新感知调度器批量调用。
//
// 实现约束（调度器按此假设并发 21+ 模块，违反即放大为启动风暴）：
//   - 必须廉价：只读本地安装账目（版本目录扫描）+ 一次带缓存的远程列表
//     （复用各模块 remoteCache 的 10 分钟 TTL），不得自建轮询、不得触发
//     模块懒激活（OnInit）、不得写盘；
//   - 可并发重入：调度器并发上限内多模块同时调用，同模块一轮至多一次；
//   - ctx 仅用于超时/取消传播，实现方拿不到也应尽快返回。
//
// 返回语义：
//   - local/remote 为参与比较的版本串（空串 = 该侧无事实，如本机未装托管资产）；
//   - hasUpdate 仅当两侧均有值且远程更新时为 true；本机无安装、远程列表
//     无稳定通道条目一律 false（"无可比事实"≠"有更新"，不谎报）；
//   - err 非 nil 表示本轮该模块判定失败——调用方必须保持其原健康值，
//     不得据此清除既有 update-available 信号。
type UpdateChecker interface {
	CheckUpdate(ctx context.Context) (local, remote string, hasUpdate bool, err error)
}
