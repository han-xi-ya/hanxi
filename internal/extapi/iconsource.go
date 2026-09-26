// iconsource.go 是"运行期本机图标提取"的可选能力契约（N27 红线三件套）。
//
// 背景：rammap/recordly/vscode 的厂商图标因许可红线永不入仓（见
// docs/THIRD_PARTY_NOTICES.md「运行期本机提取」节），改由运行期从机主本机
// 已装的官方载荷 exe 就地提取、仅本地使用。集中式 RuntimeIconService
// （internal/app）经本契约向各模块取"提取源是哪只 exe"，免三家各开一条
// RPC 通道；模块侧实现只回路径不做 IO 重活。
//
// 与调用门的关系：本方法是**只读纯函数语义**（返回托管目录内的既有文件
// 路径，不触碰进程与网络），供集中服务在"启用+已装"裁决通过后直调；
// 集中服务刻意不走 Registry.Acquire 的懒激活全量门——给一枚图标叫醒
// 一个睡眠模块是不可接受的副作用（口径详见 runtimeicon_service.go）。
package extapi

// IconSourceProvider 可选契约：模块声明其真图标的本机提取源 exe 路径
// （通常 = 当前使用版本的载荷主程序）。载荷不在位（未安装/已卸载）时
// 如实返回 error，由集中服务落负缓存、前端回落矢量徽标。
type IconSourceProvider interface {
	IconSourceExe() (string, error)
}

// HasModule 报告注册表中是否存在该 ID 的模块（集中服务区分"模块不存在"
// 与"模块未挂提取源"两类事实用）。
func (r *Registry) HasModule(moduleID string) bool {
	_, ok := r.wrapper(moduleID)
	return ok
}

// IconSourceProviderOf 按模块 ID 取实现了 IconSourceProvider 的模块实例；
// 模块不存在或未实现该契约返回 false。
func (r *Registry) IconSourceProviderOf(moduleID string) (IconSourceProvider, bool) {
	wrapper, ok := r.wrapper(moduleID)
	if !ok {
		return nil, false
	}
	provider, ok := wrapper.Module.(IconSourceProvider)
	return provider, ok
}
