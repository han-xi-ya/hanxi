// gate_matrix_test.go 钉死 Wave 3 调用门口径：quickmenu 全部 RPC 导出方法
// ① 门拒绝时如实上抛同一错误（禁止空值/空表静默消化）；
// ② 拒绝不得产生业务副作用（store 原样）；
// ③ 成功路径 acquire/release 收支平衡（drain 以租约归零为门，泄露即停用卡死）。
package quickmenu

import (
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"

	"hanxi/internal/extapi"
	"hanxi/internal/settings"
)

type countGate struct {
	deny     error
	acquired atomic.Int64
	released atomic.Int64
}

func (g *countGate) Acquire(string) (func(), error) {
	g.acquired.Add(1)
	if g.deny != nil {
		return nil, g.deny
	}
	return func() { g.released.Add(1) }, nil
}

func newGateSvc(t *testing.T, g extapi.Gate) (*QuickMenuService, *settings.Store) {
	t.Helper()
	store, err := settings.NewStore(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	holder := extapi.NewLeaseHolder(ID)
	if g != nil {
		holder.SetGate(g)
	}
	return NewQuickMenuService(store, nil, holder), store
}

// TestGateDeniedRPCsUpstreamError 11 个导出方法逐一被拒：返回的错误必须是门
// 给出的同一 sentinel（errors.Is 身份），且写类方法不得留下任何盘上痕迹。
func TestGateDeniedRPCsUpstreamError(t *testing.T) {
	sentinel := errors.New("gate: module disabled")
	gate := &countGate{deny: sentinel}
	svc, store := newGateSvc(t, gate)

	// 预置基线事实，验证"拒即无副作用"
	if err := store.SetQuickMenuTwoTier(true); err != nil {
		t.Fatal(err)
	}
	if err := store.SetQuickMenuTrigger(300, 10); err != nil {
		t.Fatal(err)
	}
	skin0 := store.GetQuickMenuSkin()

	cases := []struct {
		name string
		call func() error
	}{
		{"GetStatus", func() error { _, err := svc.GetStatus(); return err }},
		{"GetTwoTier", func() error { _, err := svc.GetTwoTier(); return err }},
		{"SetTwoTier", func() error { return svc.SetTwoTier(false) }},
		{"GetSkin", func() error { _, err := svc.GetSkin(); return err }},
		{"SetSkin", func() error { _, err := svc.SetSkin(Skin{Preset: "ink"}); return err }},
		{"GetTriggerConfig", func() error { _, _, err := svc.GetTriggerConfig(); return err }},
		{"SetTriggerConfig", func() error { _, _, err := svc.SetTriggerConfig(999, 999); return err }},
		{"ListItems", func() error { _, err := svc.ListItems(); return err }},
		{"Launch", func() error { return svc.Launch([]int{0}) }},
		{"OpenSettings", func() error { return svc.OpenSettings() }},
		{"Dismiss", func() error { return svc.Dismiss() }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := c.call(); !errors.Is(err, sentinel) {
				t.Errorf("%s 门拒绝时应原样上抛 sentinel，实际 err=%v", c.name, err)
			}
		})
	}

	// 副作用为零：两个写方法（SetTwoTier/SetTriggerConfig/SetSkin）被拒后，
	// store 读数必须仍是基线（300/10、twoTier=true、皮肤出厂）。
	if hold, move := store.GetQuickMenuTrigger(); hold != 300 || move != 10 {
		t.Errorf("SetTriggerConfig 被拒后 store 参数被改写: (%d,%d), want (300,10)", hold, move)
	}
	if !store.GetQuickMenuTwoTier() {
		t.Error("SetTwoTier 被拒后开关被改写")
	}
	if sk := store.GetQuickMenuSkin(); sk != skin0 {
		t.Errorf("SetSkin 被拒后皮肤账被改写: %+v, want 基线 %+v", sk, skin0)
	}
	if gate.acquired.Load() != int64(len(cases)) {
		t.Errorf("每方法应且仅应敲门一次: acquired=%d want %d", gate.acquired.Load(), len(cases))
	}
	if gate.released.Load() != 0 {
		t.Errorf("拒绝路径不应有 release 入账（未取得却释放）: %d", gate.released.Load())
	}
}

// TestGateAllowedReleasesLease 放行门下的成功调用必须逐笔归还租约。
func TestGateAllowedReleasesLease(t *testing.T) {
	gate := &countGate{}
	svc, store := newGateSvc(t, gate)
	if err := store.SetWheelMenu([]settings.TrayMenuItem{
		{Type: settings.TrayItemRoute, Ref: "/settings", Enabled: true},
	}); err != nil {
		t.Fatal(err)
	}

	calls := []func() error{
		func() error { _, err := svc.GetStatus(); return err },
		func() error { _, err := svc.GetSkin(); return err },
		func() error { _, _, err := svc.GetTriggerConfig(); return err },
		func() error { _, err := svc.ListItems(); return err },
		func() error { return svc.Dismiss() },
		func() error { return svc.SetTwoTier(true) },
		func() error { _, err := svc.SetSkin(Skin{Preset: "veil"}); return err },
		func() error { _, _, err := svc.SetTriggerConfig(320, 12); return err },
		func() error { return svc.Launch([]int{0}) }, // 合法索引：route /settings 非模块引用，导航回调对无应用实例无害
	}
	for i, fn := range calls {
		if err := fn(); err != nil {
			t.Fatalf("第 %d 个成功调用报错: %v", i, err)
		}
	}
	if gate.acquired.Load() != int64(len(calls)) || gate.released.Load() != int64(len(calls)) {
		t.Errorf("租约收支不平衡: acquired=%d released=%d want %d/%d",
			gate.acquired.Load(), gate.released.Load(), len(calls), len(calls))
	}
}
