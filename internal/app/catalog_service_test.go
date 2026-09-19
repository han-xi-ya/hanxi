// catalog_service_test.go 覆盖模块中心后端 RPC 的装配级行为：
// 投影透传、逻辑安装/卸载事务与 receipt 联动（不启动 Wails 应用，
// broadcastModuleChange 的 application.Get()==nil 路径同时被覆盖）。
package app

import (
	"context"
	"testing"

	"hanxi/internal/extapi"
	"hanxi/internal/settings"
)

// stubModule 最小 extapi.Module 实现（生命周期均无资源）。
type stubModule struct {
	id  string
	env extapi.ModuleInfo
}

func (m *stubModule) Info() extapi.ModuleInfo {
	return extapi.ModuleInfo{ID: m.id, Name: "测试模块 " + m.id, Level: extapi.LevelBuiltin}
}
func (m *stubModule) Nav() []extapi.NavEntry       { return nil }
func (m *stubModule) Services() []extapi.Service   { return nil }
func (m *stubModule) OnInit(context.Context) error { return nil }
func (m *stubModule) OnDestroy() error             { return nil }
func (m *stubModule) IsInitialized() bool          { return true }

func newTestAppService(t *testing.T, ids ...string) (*AppService, *extapi.Registry, *settings.ReceiptStore) {
	t.Helper()
	store := settings.NewReceiptStore(t.TempDir())
	registry := extapi.NewRegistry(nil)
	registry.SetReceiptStorage(store)
	mods := make([]extapi.Module, 0, len(ids))
	for _, id := range ids {
		mods = append(mods, &stubModule{id: id})
	}
	if err := registry.Register(mods...); err != nil {
		t.Fatal(err)
	}
	return NewAppService(registry, nil), registry, store
}

func stateOf(t *testing.T, svc *AppService, id string) extapi.ModuleState {
	t.Helper()
	for _, st := range svc.ListModuleStates() {
		if st.ModuleID == id {
			return st
		}
	}
	t.Fatalf("模块 %q 不在投影中", id)
	return extapi.ModuleState{}
}

func TestListModuleStatesProxiesRegistry(t *testing.T) {
	svc, registry, store := newTestAppService(t, "alpha", "beta")
	if err := store.EnsureInstalled([]string{"alpha", "beta"}, extapi.ReceiptBuiltinLogical); err != nil {
		t.Fatal(err)
	}
	states := svc.ListModuleStates()
	if len(states) != 2 || states[0].ModuleID != "alpha" || states[1].ModuleID != "beta" {
		t.Fatalf("投影列表异常: %+v", states)
	}
	for _, st := range states {
		if st.Delivery != extapi.DeliveryInstalled || st.Policy != extapi.PolicyEnabled ||
			st.Schema != extapi.ModuleContractSchema {
			t.Errorf("alpha/beta 初始投影异常: %+v", st)
		}
	}
	_ = registry
}

func TestSetModuleInstalledUninstallRoundTrip(t *testing.T) {
	svc, _, store := newTestAppService(t, "alpha")
	// 迁移基线：注册时未补凭据 → absent/install。
	if st := stateOf(t, svc, "alpha"); st.Delivery != extapi.DeliveryAbsent || st.PrimaryAction != extapi.ActionInstall {
		t.Fatalf("初始应为 absent/install: %+v", st)
	}

	// 安装：installed+enabled+open（事件通道 nil 守卫不 panic）。
	st, err := svc.SetModuleInstalled("alpha", true)
	if err != nil || st == nil {
		t.Fatalf("安装失败: %+v %v", st, err)
	}
	if st.Delivery != extapi.DeliveryInstalled || st.Policy != extapi.PolicyEnabled {
		t.Errorf("安装后投影异常: %+v", st)
	}
	if !store.IsInstalled("alpha") {
		t.Error("安装后 receipt 应落盘")
	}

	// 卸载：absent，数据目录不触碰（此处以 IsInstalled 单侧断言）。
	st, err = svc.SetModuleInstalled("alpha", false)
	if err != nil || st == nil {
		t.Fatalf("卸载失败: %+v %v", st, err)
	}
	if st.Delivery != extapi.DeliveryAbsent || st.PrimaryAction != extapi.ActionInstall {
		t.Errorf("卸载后投影异常: %+v", st)
	}
	if store.IsInstalled("alpha") {
		t.Error("卸载后 receipt 应移除")
	}
}

func TestSetModuleInstalledRejectsUnknown(t *testing.T) {
	svc, _, _ := newTestAppService(t, "alpha")
	if _, err := svc.SetModuleInstalled("ghost", true); err == nil {
		t.Error("未知模块安装必须报错")
	}
}

func TestSetModuleEnabledRespectsInstalledGate(t *testing.T) {
	svc, registry, _ := newTestAppService(t, "alpha")
	// 未安装模块：启用被拒（SetEnabled(true) 的 checkInstalled 门）。
	if err := registry.SetEnabled("alpha", true); err == nil {
		t.Fatal("未安装模块启用应被拒")
	}
	if _, err := svc.SetModuleInstalled("alpha", true); err != nil {
		t.Fatal(err)
	}
	// 安装后可停用；停用后仍未安装判断不回退（receipt 仍在）。
	if err := registry.SetEnabled("alpha", false); err != nil {
		t.Fatalf("已安装模块停用应成功: %v", err)
	}
	st := stateOf(t, svc, "alpha")
	if st.Delivery != extapi.DeliveryInstalled || st.Policy != extapi.PolicyDisabled ||
		st.PrimaryAction != extapi.ActionEnable {
		t.Errorf("停用后应 installed+disabled+enable: %+v", st)
	}
}

func TestListCatalogShape(t *testing.T) {
	items := CatalogItems()
	if len(items) != 41 {
		t.Fatalf("内建目录应 41 项, got %d", len(items))
	}
	for _, it := range items {
		if it.Delivery != extapi.DeliveryBuiltinLogical || it.Owner == "" || it.Name == "" {
			t.Errorf("目录项异常: %+v", it)
		}
		seen := map[extapi.Entrypoint]bool{}
		for _, ep := range it.Entrypoints {
			if seen[ep] {
				t.Errorf("%s entrypoints 重复: %v", it.ID, it.Entrypoints)
			}
			seen[ep] = true
		}
		for _, need := range []extapi.Entrypoint{extapi.EntryRPC, extapi.EntryNavigation, extapi.EntrySearch} {
			if !seen[need] {
				t.Errorf("%s 缺基础入口 %s", it.ID, need)
			}
		}
	}
}
