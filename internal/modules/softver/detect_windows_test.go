//go:build windows

package softver

import "testing"

// 注册表口径的命中/排除规则（真机实证样本 + 反例）：
// 本机 4.0 命中 WOW6432Node 根下子键 "Weixin"（DisplayName"微信"）。

func TestWechatInstallMatch(t *testing.T) {
	cases := []struct {
		child, display string
		want           bool
	}{
		{"Weixin", "微信", true},           // 本机 4.0 实证
		{"WeChat", "微信", true},           // 3.x 常见形态
		{"WeChat", "WeChat", true},       // 英文版
		{"wechat", "WEIXIN", true},       // 大小写不敏感
		{"Tencent WeChat", "微信", true},   // DisplayName 精确命中本体：子键名变体照收
		{"WXWork", "企业微信", false},        // 另一产品，卡片边界排除
		{"Weixin", "企业微信（WeCom）", false}, // DisplayName 命中企业微信必须排除
		{"RustDesk", "RustDesk", false},  // 无关软件
		{"", "", false},
	}
	for _, c := range cases {
		if got := wechatInstallMatch(c.child, c.display); got != c.want {
			t.Errorf("wechatInstallMatch(%q,%q) = %v, 期望 %v", c.child, c.display, got, c.want)
		}
	}
}

func TestGenerationOf(t *testing.T) {
	if g := generationOf("Weixin", "微信"); g != "4.x (Weixin)" {
		t.Errorf("generationOf(Weixin) = %q", g)
	}
	if g := generationOf("WeChat", "微信"); g != "3.x (WeChat)" {
		t.Errorf("generationOf(WeChat) = %q", g)
	}
	if g := generationOf("Tencent WeChat", "微信"); g != "未知代际" {
		t.Errorf("非常见子键名应如实未知，got %q", g)
	}
}

func TestFixDuplicateIDs(t *testing.T) {
	installs := []LocalInstall{{ID: "wechat"}, {ID: "wechat"}, {ID: "weixin"}}
	fixDuplicateIDs(installs)
	if installs[2].ID != "weixin" {
		t.Errorf("唯一 ID 不应被改名: %q", installs[2].ID)
	}
	if installs[0].ID == installs[1].ID || installs[0].ID == "" || installs[1].ID == "" {
		t.Errorf("撞车 ID 未消歧: %q / %q", installs[0].ID, installs[1].ID)
	}
}

// TestProbeLocalRealMachine 真机冒烟：只断言"探测不崩、形状合法"，
// 不断言具体版本（机器装了什么是环境事实，不是本包契约）。
func TestProbeLocalRealMachine(t *testing.T) {
	data, err := probeLocal()
	if err != nil {
		t.Fatalf("probeLocal: %v", err)
	}
	for _, in := range data.Installs {
		if in.ID == "" || in.Generation == "" {
			t.Errorf("安装记录缺稳定键/代际: %+v", in)
		}
		if len(in.Sources) > 0 && in.BestVersion == "" {
			t.Errorf("有读数却无主版本: %+v", in)
		}
		found := false
		for _, src := range in.Sources {
			if src.Primary {
				found = true
			}
			if src.Value == "" || src.Kind == "" {
				t.Errorf("口径读数不合法: %+v", src)
			}
		}
		if len(in.Sources) > 0 && !found {
			t.Errorf("有读数却无主口径标记: %+v", in.Sources)
		}
	}
	seen := map[string]bool{}
	for _, d := range data.Dirs {
		if d.ID == "" || d.Path == "" {
			t.Errorf("槽位缺 ID/路径: %+v", d)
		}
		if seen[d.ID] {
			t.Errorf("槽位 ID 撞车: %q", d.ID)
		}
		seen[d.ID] = true
	}
	if len(data.Dirs) == 0 {
		t.Error("至少应产出两代数据目录候选槽位")
	}
	t.Logf("真机读数：installs=%+v", data.Installs)
	for _, d := range data.Dirs {
		t.Logf("真机槽位：%s kind=%s exists=%v active=%v origin=%s", d.Path, d.Kind, d.Exists, d.Active, d.Origin)
	}
}
