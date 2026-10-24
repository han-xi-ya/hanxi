package clipboard

import (
	"strings"
	"testing"
)

// ---------- v1.8 预览隐私遮蔽（契约 §12 v1.8 闸③兑现） ----------

func mustEngine(t *testing.T, kinds, patterns []string) *maskEngine {
	t.Helper()
	m, err := newMaskEngine(kinds, patterns)
	if err != nil {
		t.Fatalf("newMaskEngine: %v", err)
	}
	return m
}

// TestMaskNilEngineIdentity nil 引擎（测试手装体/未装载形态）=恒等——
// "遮蔽未配置不遮"，与"配置了但装配失败沿用旧引擎"是两种如实。
func TestMaskNilEngineIdentity(t *testing.T) {
	var m *maskEngine
	if m.enabled() {
		t.Fatal("nil 引擎应视为未启用")
	}
	if got := m.apply("13812345678"); got != "13812345678" {
		t.Fatalf("nil apply 应恒等: %s", got)
	}
	empty := mustEngine(t, nil, nil)
	if empty.enabled() || empty.apply("abc") != "abc" {
		t.Fatal("空规则集应短路恒等")
	}
}

// TestMaskKinds 内置四族正反例：保首尾可辨、边界不误咬、X 尾证件、
// 私钥标记、赋值形保留键名只遮值；以及**遮后幂等**（重复 apply 不套娃）。
func TestMaskKinds(t *testing.T) {
	all := mustEngine(t, []string{"phone", "idcard", "email", "secret"}, nil)
	cases := []struct {
		in, want string
	}{
		{"联系 13812345678 谢谢", "联系 138****5678 谢谢"},
		{"非手机号 12812345678", "非手机号 12812345678"},             // 1[3-9] 第二闸
		{"卡号 6222021381234567890", "卡号 6222021381234567890"}, // 长数字串 \b 不误咬中段
		{"身份证 11010119900307888X", "身份证 110***********888X"},
		{"邮箱 user.name+tag@ex.co.cn", "邮箱 u***@ex.co.cn"},
		{"token= abc123def456 rest", "token= **** rest"}, // 键名与分隔保形
		{"sk-abcdefghijklmnopqrstuvwx tail", "sk-**** tail"},
		{"ghp_abcdefghijklmnopqrstuvwxyz123456", "ghp_****"},
		{"-----BEGIN RSA PRIVATE KEY-----", "-----[私钥]-----"},
	}
	for _, c := range cases {
		if got := all.apply(c.in); got != c.want {
			t.Errorf("apply(%q) = %q, want %q", c.in, got, c.want)
		}
		// 幂等：遮后再跑一轮，形态不变
		if got2 := all.apply(all.apply(c.in)); got2 != c.want {
			t.Errorf("二次 apply(%q) 套娃: %q", c.in, got2)
		}
	}

	// 单族开关：kind 关闭即不遮
	phoneOnly := mustEngine(t, []string{"phone"}, nil)
	if got := phoneOnly.apply("a@b.com 13812345678"); got != "a@b.com 138****5678" {
		t.Fatalf("单族失真: %s", got)
	}
}

// TestMaskCustom 自定义规则：内置之后依序整命中替 ****；规则命中自身幂等
// （替换产物不再命中原规则时稳定）。
func TestMaskCustom(t *testing.T) {
	m := mustEngine(t, nil, []string{`\d{6,}`})
	if got := m.apply("订单 88990012 金额 50"); got != "订单 **** 金额 50" {
		t.Fatalf("自定义整命中失真: %s", got)
	}
	// 内置在前、自定义殿后：手机号先保形遮，残串再被 6+ 数字规则二次处理时
	// 形态稳定（"138****5678" 无 6 连数字，不再命中）
	both := mustEngine(t, []string{"phone"}, []string{`\d{6,}`})
	if got := both.apply("13812345678"); got != "138****5678" {
		t.Fatalf("内置+自定义叠加失真: %s", got)
	}
	if _, err := newMaskEngine(nil, []string{"("}); err == nil {
		t.Fatal("坏正则应编译期拒绝（纵深第二闸）")
	}
}

// TestMaskServiceEgress 出口口径三面：List 遮、updated 事件载荷遮、
// **Get 正文永不遮**（回归锚：详情/Paste/Set 依赖明文，未来谁把遮罩
// 挪进 Get，本例即红）。store 明文搜索命中原文=特性面反证。
func TestMaskServiceEgress(t *testing.T) {
	h := newSvcHarness(t)
	defer h.stop()
	eng := mustEngine(t, []string{"phone"}, nil)
	h.svc.mask.Store(eng)

	const raw = "回电 13800001234 安排验收"
	h.svc.handleCapture(textCap(raw))
	id := mustSingleID(t, h)

	rows, err := h.svc.List("", "", 0)
	if err != nil || len(rows) != 1 {
		t.Fatalf("List: %v %v", rows, err)
	}
	if !strings.Contains(rows[0].Preview, "138****1234") {
		t.Fatalf("List 出口未遮: %q", rows[0].Preview)
	}
	// 搜索原文照样命中（遮蔽在出口不在匹配面）
	hits, _ := h.svc.List("13800001234", "", 0)
	if len(hits) != 1 {
		t.Fatalf("明文搜索应命中（特性登记 v1.8）: %v", hits)
	}

	full, err := h.svc.Get(id)
	if err != nil || !strings.Contains(full.Text, raw) || !strings.Contains(full.Preview, raw) {
		t.Fatalf("Get 回归锚失守（正文与详情 preview 必须明文）: %+v %v", full, err)
	}

	upd := h.updatedPayload(t)
	if !strings.Contains(upd.Preview, "138****1234") {
		t.Fatalf("updated 载荷未遮: %q", upd.Preview)
	}
	if upd.Text != "" {
		t.Fatal("updated 载荷本应剥 Text（既有纪律不得回退）")
	}

	// 全关形态：空规则引擎 → 恒等（显式关就不遮，回显与行为一致）
	h.svc.mask.Store(&maskEngine{kinds: map[string]bool{}})
	rows, _ = h.svc.List("", "", 0)
	if !strings.Contains(rows[0].Preview, raw) {
		t.Fatalf("全关形态应回明文: %q", rows[0].Preview)
	}
}

// TestMaskPrefsHotReload SetPrefs 遮蔽域热换：改 kinds 后下一轮 List 即吃
// 新规则（不重启）；GetPrefs 回显生效集（未知 kind 已剔）。
func TestMaskPrefsHotReload(t *testing.T) {
	h := newSvcHarness(t)
	defer h.stop()
	h.attachPrefs(t)
	// 手装体构造不经 NewClipboardService，引擎需显式装配一次（生产路径
	// 在构造函数内一次完成——本例走的就是 SetPrefs 热换路径）
	h.svc.rebuildMask()
	// 首装默认全开：手机号应遮
	const raw = "热线 13900001111"
	h.svc.handleCapture(textCap(raw))

	cur, err := h.svc.GetPrefs()
	if err != nil || len(cur.MaskKinds) != len(allMaskKinds) {
		t.Fatalf("默认应全开: %+v %v", cur, err)
	}
	rows, _ := h.svc.List("", "", 0)
	if !strings.Contains(rows[0].Preview, "139****1111") {
		t.Fatalf("默认全开应遮手机号: %q", rows[0].Preview)
	}

	// 只留 email：手机号转明文可见
	cur.MaskKinds = []string{"email", "bogus"}
	echo, err := h.svc.SetPrefs(cur)
	if err != nil {
		t.Fatalf("SetPrefs: %v", err)
	}
	if strings.Join(echo.MaskKinds, ",") != "email" {
		t.Fatalf("未知 kind 应剔且固定序回显: %v", echo.MaskKinds)
	}
	rows, _ = h.svc.List("", "", 0)
	if !strings.Contains(rows[0].Preview, raw) {
		t.Fatalf("关手机号后应回明文: %q", rows[0].Preview)
	}

	// 自定义正则热进：上屏即遮
	cur = echo
	cur.MaskCustomPatterns = []string{`热线 \d+`}
	if _, err := h.svc.SetPrefs(cur); err != nil {
		t.Fatal(err)
	}
	rows, _ = h.svc.List("", "", 0)
	if strings.Contains(rows[0].Preview, "热线 13900001111") {
		t.Fatalf("自定义规则未生效: %q", rows[0].Preview)
	}
}
