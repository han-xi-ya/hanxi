// catalog_service.go 是模块中心的后端 RPC（Wave 1 底座）：
// 四维状态投影与逻辑安装/卸载事务。契约语义见 extapi/catalog.go 与
// docs/adr/ADR-0001，前端所有入口只消费这里的投影，禁止自造第二份状态。
package app

import (
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hanxi/internal/domain"
	"hanxi/internal/extapi"
)

// ListCatalog 返回静态模块目录（身份/交付形态/入口/能力/owner），按 ID 排序。
// 表是生成物（internal/app/catalog.go，基线 scripts/fixture/module_catalog.json），
// 与运行状态分离——瞬时进度变化不重建目录（ADR-0001 §1.2）。
func (s *AppService) ListCatalog() []extapi.ModuleCatalogItem {
	return CatalogItems()
}

// ListModuleStates 返回全部模块的四维状态投影（含派生主操作与摘要），按 ID 排序。
func (s *AppService) ListModuleStates() []extapi.ModuleState {
	return s.registry.ListStates()
}

// SetModuleInstalled 执行逻辑安装/卸载（模块中心"安装/卸载"按钮的后端事务）。
// Phase 1-2 内建模块的交付形态固定为 builtin-logical：安装=登记凭据+默认启用；
// 卸载=drain/析构+移除凭据，用户数据默认保留。宿主内建代码不随卸载释放——
// UI 文案必须如实呈现（delivery kind 由投影强制携带）。
func (s *AppService) SetModuleInstalled(id string, installed bool) (*extapi.ModuleState, error) {
	id = strings.TrimSpace(id)
	var err error
	if installed {
		err = s.registry.Install(id, extapi.ReceiptBuiltinLogical)
	} else {
		err = s.registry.Uninstall(id)
	}
	if err != nil {
		ae := domain.NewAppError(domain.ErrValidation, "模块安装操作失败")
		ae.Cause = err
		return nil, ae
	}

	// 安装态变化会牵动导航、托盘与命令面板，统一广播并热重建托盘。
	s.broadcastModuleChange()

	for _, st := range s.registry.ListStates() {
		if st.ModuleID == id {
			return &st, nil
		}
	}
	return nil, nil
}

// broadcastModuleChange 广播扩展变化并重建托盘菜单（安装/启停共用收口）。
func (s *AppService) broadcastModuleChange() {
	if app := application.Get(); app != nil && app.Event != nil {
		app.Event.Emit("ext:changed")
	}
	if s.trayRebuild != nil {
		s.trayRebuild()
	}
}
