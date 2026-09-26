package quickmenu

import (
	"os"
	"path/filepath"
	"testing"

	"hanxi/internal/extapi"
	"hanxi/internal/settings"
)

// 皮肤账后端化（收权批）表测：normalizeSkin 纯归一（坏值归正不报错，与前端
// wheelSkin.ts 语义同源）+ 经 store 的缺省兼容/钳域/重启持久全链路。

func TestNormalizeSkin(t *testing.T) {
	cases := []struct {
		name string
		in   Skin
		want Skin
	}{
		{"出厂值原样", Skin{Preset: "frost", FaceAlpha: 100, Stroke: 55, FollowModuleColor: true},
			Skin{Preset: "frost", FaceAlpha: 100, Stroke: 55, FollowModuleColor: true}},
		{"全缺省（空 struct=旧配置无账）", Skin{},
			Skin{Preset: "frost", FaceAlpha: 100, Stroke: 0, FollowModuleColor: false}},
		{"未知预设回落 frost", Skin{Preset: "neon", FaceAlpha: 80, Stroke: 40},
			Skin{Preset: "frost", FaceAlpha: 80, Stroke: 40}},
		{"透明度越下界钳 35", Skin{Preset: "ink", FaceAlpha: 10, Stroke: 60},
			Skin{Preset: "ink", FaceAlpha: 35, Stroke: 60}},
		{"透明度越上界钳 100", Skin{Preset: "veil", FaceAlpha: 150, Stroke: 60},
			Skin{Preset: "veil", FaceAlpha: 100, Stroke: 60}},
		{"负透明度=未写语义回落出厂", Skin{FaceAlpha: -7, Stroke: 50},
			Skin{Preset: "frost", FaceAlpha: 100, Stroke: 50}},
		{"描边 0 是合法用户值不归出厂", Skin{Preset: "ink", FaceAlpha: 90, Stroke: 0},
			Skin{Preset: "ink", FaceAlpha: 90, Stroke: 0}},
		{"描边钳进 0..100", Skin{Stroke: 999},
			Skin{Preset: "frost", FaceAlpha: 100, Stroke: 100}},
		{"负描边钳 0", Skin{Stroke: -5},
			Skin{Preset: "frost", FaceAlpha: 100, Stroke: 0}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := normalizeSkin(c.in); got != c.want {
				t.Errorf("normalizeSkin(%+v) = %+v, want %+v", c.in, got, c.want)
			}
		})
	}
}

func newSkinSvc(t *testing.T, rawConfig string) (*QuickMenuService, string) {
	t.Helper()
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.json")
	if rawConfig != "" {
		if err := os.WriteFile(cfgFile, []byte(rawConfig), 0o644); err != nil {
			t.Fatalf("写旧配置失败: %v", err)
		}
	}
	store, err := settings.NewStore(cfgFile)
	if err != nil {
		t.Fatalf("NewStore 失败: %v", err)
	}
	return &QuickMenuService{store: store, holder: extapi.NewLeaseHolder("quickmenu")}, cfgFile
}

// 缺省兼容：老 JSON 完全没有 quickMenuSkin 键 → 读出出厂素瓷盘，不报错。
func TestGetSkinLegacyDefaults(t *testing.T) {
	svc, _ := newSkinSvc(t, `{"theme":"dark","logRetainDays":3}`)
	got, err := svc.GetSkin()
	if err != nil {
		t.Fatalf("GetSkin 报错: %v", err)
	}
	want := Skin{Preset: "frost", FaceAlpha: 100, Stroke: 55, FollowModuleColor: false}
	if got != want {
		t.Errorf("旧账 GetSkin = %+v, want 出厂 %+v", got, want)
	}
}

// 缺省兼容（半缺）：quickMenuSkin 只有 preset → 缺的子字段仍落出厂（解码进默认副本）。
func TestGetSkinPartialKeys(t *testing.T) {
	svc, _ := newSkinSvc(t, `{"quickMenuSkin":{"preset":"ink","followModuleColor":true}}`)
	got, err := svc.GetSkin()
	if err != nil {
		t.Fatalf("GetSkin 报错: %v", err)
	}
	want := Skin{Preset: "ink", FaceAlpha: 100, Stroke: 55, FollowModuleColor: true}
	if got != want {
		t.Errorf("半缺账 GetSkin = %+v, want %+v", got, want)
	}
}

// 盘上坏值读取侧归正：预设野值/透明度越界一律钳白名单与合法域，GetSkin 永不报错。
func TestGetSkinDirtyValuesNormalized(t *testing.T) {
	svc, _ := newSkinSvc(t, `{"quickMenuSkin":{"preset":"neon","faceAlpha":5,"stroke":999}}`)
	got, err := svc.GetSkin()
	if err != nil {
		t.Fatalf("GetSkin 报错: %v", err)
	}
	want := Skin{Preset: "frost", FaceAlpha: 35, Stroke: 100}
	if got != want {
		t.Errorf("坏值盘 GetSkin = %+v, want 归正 %+v", got, want)
	}
}

// 写侧钳域 + 回显 + 落盘持久 + 重启稳定：SetSkin 收到的生效值即 GetSkin 所见，
// 重新开 store 模拟重启后仍一致（NAS 随身口径的持久底线）。
func TestSetSkinClampsAndPersists(t *testing.T) {
	svc, cfgFile := newSkinSvc(t, "")
	echo, err := svc.SetSkin(Skin{Preset: "bogus", FaceAlpha: 150, Stroke: -5, FollowModuleColor: true})
	if err != nil {
		t.Fatalf("SetSkin 报错: %v", err)
	}
	want := Skin{Preset: "frost", FaceAlpha: 100, Stroke: 0, FollowModuleColor: true}
	if echo != want {
		t.Fatalf("SetSkin 回显 = %+v, want 归正 %+v", echo, want)
	}
	got, err := svc.GetSkin()
	if err != nil || got != want {
		t.Fatalf("写后读 = %+v (err %v), want %+v", got, err, want)
	}
	reopened, err := settings.NewStore(cfgFile)
	if err != nil {
		t.Fatalf("重开 store 失败: %v", err)
	}
	svc2 := &QuickMenuService{store: reopened, holder: extapi.NewLeaseHolder("quickmenu")}
	if again, _ := svc2.GetSkin(); again != want {
		t.Errorf("重启后皮肤 = %+v, want 持久 %+v", again, want)
	}
}

// store 缺失的降级路径：读=出厂盘（弹窗不断皮），写=如实拒。
func TestSkinNilStore(t *testing.T) {
	svc := &QuickMenuService{holder: extapi.NewLeaseHolder("quickmenu")}
	got, err := svc.GetSkin()
	if err != nil {
		t.Fatalf("nil store GetSkin 不应报错: %v", err)
	}
	if got.Preset != "frost" || got.FaceAlpha != 100 {
		t.Errorf("nil store GetSkin = %+v, want 出厂降级", got)
	}
	if _, err := svc.SetSkin(Skin{Preset: "ink"}); err == nil {
		t.Error("nil store SetSkin 应如实拒")
	}
}
