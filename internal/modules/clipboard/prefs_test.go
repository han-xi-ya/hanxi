package clipboard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ---------- 偏好纯函数与持久化（契约 §12 v1.8） ----------

// TestClipPrefsNilFallback nil 接收者全量回落默认：测试手装体不接 prefs 的
// 路径天然安全（memo quickMemoStore 同谱，存量 service_test 零改动的依据）。
func TestClipPrefsNilFallback(t *testing.T) {
	var s *clipPrefsStore
	if s.maxEntries() != defaultMaxEntries || s.maxBlobBytes() != defaultMaxBlobBytes {
		t.Fatalf("容量默认失真: %d %d", s.maxEntries(), s.maxBlobBytes())
	}
	if s.deleteAfterPaste() {
		t.Fatal("deleteAfterPaste 默认应为关")
	}
	if got := s.maskKinds(); len(got) != len(allMaskKinds) {
		t.Fatalf("遮蔽默认应全开: %v", got)
	}
	// 提交 2 激活后：默认键 Ctrl+Alt+X（机主拍板；""=显式停用另由
	// normalizePersisted 指针路径与 setQueueHotkey 表达）
	if s.queueHotkey() != defaultQueueHotkey {
		t.Fatalf("队列热键默认应为 %q，得 %q", defaultQueueHotkey, s.queueHotkey())
	}
	if _, err := s.Set(Prefs{}); err == nil {
		t.Fatal("nil 存储的 Set 应如实报错")
	}
}

// TestClipPrefsFreshReadonly 缺文件回落默认且**构造不落盘**（偏好面连基线
// 写都不必有——NewStore 落盘副作用的镜像纪律）。
func TestClipPrefsFreshReadonly(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	s := newClipPrefsStore(dir)
	if s.maxEntries() != defaultMaxEntries {
		t.Fatalf("缺文件应回落默认: %d", s.maxEntries())
	}
	if _, err := os.Stat(filepath.Join(dir, "clipboard-prefs.json")); err == nil {
		t.Fatal("构造期不应产生偏好文件")
	}
}

// TestClipPrefsNormalize 校验/钳制/规范化纯函数表驱动：数值越界静默钳、
// exe 补 .exe 去重剔出厂、天数≤0 即删规则、遮蔽 nil=全开/空表=全关可辨、
// 坏正则整表拒绝。
func TestClipPrefsNormalize(t *testing.T) {
	p, err := normalizeClipPrefs(Prefs{
		MaxEntries: 99999,
		MaxBlobMB:  -5,
		ExcludedCustom: []string{
			" WeChat ", "wechat.exe", "1Password.exe", "", "dash-no-dot",
		},
		AppRetentionDays: map[string]int{
			"OLD.EXE":  7,    // 大写键 → 小写
			"zero.exe": 0,    // ≤0 → 规则删除
			"far.exe":  9999, // 越界 → 钳 365
		},
		MaskKinds:          []string{"phone", "bogus", "PHONE"}, // 未知丢弃+去重
		MaskCustomPatterns: []string{"  ", "(", "(ok)"},
	})
	if err == nil || !strings.Contains(err.Error(), "(") {
		t.Fatalf("坏正则应整表拒绝上浮: %v", err)
	}
	if _, err := normalizeClipPrefs(Prefs{MaskCustomPatterns: []string{"(ok)", "["}}); err == nil {
		t.Fatal("任一坏正则都应拒绝")
	}

	p, err = normalizeClipPrefs(Prefs{
		MaxEntries:     99999,
		MaxBlobMB:      -5,
		ExcludedCustom: []string{" WeChat ", "wechat.exe", "1Password.exe", "", "dash-no-dot"},
		AppRetentionDays: map[string]int{
			"OLD.EXE": 7, "zero.exe": 0, "far.exe": 9999,
		},
		MaskKinds:          []string{"phone", "bogus", "PHONE"},
		MaskCustomPatterns: []string{"(ok)", "  "},
	})
	if err != nil {
		t.Fatalf("合法表不应被拒: %v", err)
	}
	if p.MaxEntries != maxMaxEntries || p.MaxBlobMB != minMaxBlobMB {
		t.Fatalf("越界应静默钳制: %d %d", p.MaxEntries, p.MaxBlobMB)
	}
	if strings.Join(p.ExcludedCustom, ",") != "wechat.exe,dash-no-dot.exe" {
		t.Fatalf("exe 规范化失真（补 .exe/去重/剔出厂/空项）: %v", p.ExcludedCustom)
	}
	if p.AppRetentionDays["old.exe"] != 7 || p.AppRetentionDays["far.exe"] != retentionMaxDays {
		t.Fatalf("保留天数钳制失真: %v", p.AppRetentionDays)
	}
	if _, hit := p.AppRetentionDays["zero.exe"]; hit {
		t.Fatal("天数≤0 应视为删除该规则")
	}
	if strings.Join(p.MaskKinds, ",") != "phone" {
		t.Fatalf("遮蔽 kind 应固定序过滤: %v", p.MaskKinds)
	}
	if len(p.MaskCustomPatterns) != 1 || p.MaskCustomPatterns[0] != "(ok)" {
		t.Fatalf("自定义规则去空去重失真: %v", p.MaskCustomPatterns)
	}

	// 未设置（零值）与显式形态的分界：0 值回落默认；MaskKinds 空表=全关
	// 保持非 nil（回显/再存盘不翻回全开）。
	d, err := normalizeClipPrefs(Prefs{})
	if err != nil || d.MaxEntries != defaultMaxEntries || d.MaxBlobMB != defaultMaxBlobMB {
		t.Fatalf("零值应回落默认: %+v %v", d, err)
	}
	off, err := normalizeClipPrefs(Prefs{MaskKinds: []string{}})
	if err != nil || off.MaskKinds == nil || len(off.MaskKinds) != 0 {
		t.Fatalf("空表=全关须保持非 nil 空集: %#v %v", off.MaskKinds, err)
	}
}

// TestClipPrefsRoundTrip Set 落盘→重开装载一致；全关遮蔽显式往返；
// setQueueHotkey 与 SetPrefs 分域互不踩。
func TestClipPrefsRoundTrip(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	s := newClipPrefsStore(dir)
	if _, err := s.Set(Prefs{
		MaxEntries: 800, MaxBlobMB: 300, DeleteAfterPaste: true,
		ExcludedCustom: []string{"pgadmin"}, AppRetentionDays: map[string]int{"slack.exe": 30},
		MaskKinds: []string{}, MaskCustomPatterns: []string{`\d{4}`},
	}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	// 用非默认键做往返（默认也是 X——同值往返是假绿）
	if err := s.setQueueHotkey("Ctrl+Alt+Q"); err != nil {
		t.Fatalf("setQueueHotkey: %v", err)
	}

	reopened := newClipPrefsStore(dir)
	if reopened.maxEntries() != 800 || reopened.maxBlobBytes() != int64(300)<<20 {
		t.Fatalf("容量往返失真: %d", reopened.maxEntries())
	}
	if !reopened.deleteAfterPaste() {
		t.Fatal("开关往返失真")
	}
	if strings.Join(reopened.excludedCustom(), ",") != "pgadmin.exe" {
		t.Fatalf("custom 往返失真: %v", reopened.excludedCustom())
	}
	if reopened.appRetentionDays()["slack.exe"] != 30 {
		t.Fatalf("保留表往返失真: %v", reopened.appRetentionDays())
	}
	if kinds := reopened.maskKinds(); len(kinds) != 0 {
		t.Fatalf("全关遮蔽应显式保持空: %#v", kinds)
	}
	if reopened.queueHotkey() != "Ctrl+Alt+Q" {
		t.Fatalf("热键往返失真: %q", reopened.queueHotkey())
	}
	// 快照拷贝隔离：改副本不伤运行态
	snap := reopened.snapshot()
	snap.ExcludedCustom[0] = "tampered.exe"
	if reopened.excludedCustom()[0] != "pgadmin.exe" {
		t.Fatal("snapshot 未隔离")
	}
}

// TestClipPrefsBadFileFallsBack 坏文件回落默认不落盘；盘上坏正则剔除后其余
// 装载（装载面不报错——报错没人接，运行面拒绝即可）。
func TestClipPrefsBadFileFallsBack(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "clipboard-prefs.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := newClipPrefsStore(dir)
	if s.maxEntries() != defaultMaxEntries {
		t.Fatalf("坏文件应回落默认: %d", s.maxEntries())
	}

	if err := os.WriteFile(path, []byte(`{"maxEntries":700,"maskCustomPatterns":["(","(good)"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	s2 := newClipPrefsStore(dir)
	if s2.maxEntries() != 700 {
		t.Fatalf("坏正则剔除不应连坐数值域: %d", s2.maxEntries())
	}
	if len(s2.maskCustomPatterns()) != 0 {
		t.Fatalf("含坏正则的自定义域应整域剔除: %v", s2.maskCustomPatterns())
	}
}

// TestMergeExcludedTables 活表装配唯一通路：出厂在前、custom 并入去重，
// 出厂 7 项结构上删不掉（v1.3.4"不得裁撤"的形态级兑现）。
func TestMergeExcludedTables(t *testing.T) {
	merged := mergeExcludedTables([]string{"wechat.exe", "1Password.exe", "keepass.exe"})
	if len(merged) != len(sensitiveDefaultUsers)+1 {
		t.Fatalf("并入去重失真: %v", merged)
	}
	for _, must := range sensitiveDefaultUsers {
		found := false
		for _, m := range merged {
			if strings.EqualFold(m, must) {
				found = true
			}
		}
		if !found {
			t.Fatalf("出厂项丢失: %s", must)
		}
	}
	cleared := mergeExcludedTables(nil)
	if len(cleared) != len(sensitiveDefaultUsers) {
		t.Fatalf("custom 清空应只剩出厂表: %v", cleared)
	}
}

// ---------- 服务面集成（GetPrefs/SetPrefs/清扫/删后贴） ----------

// attachPrefs 给手装 harness 接上真偏好存储（默认目录=临时 state）。
func (h *svcHarness) attachPrefs(t *testing.T) *clipPrefsStore {
	t.Helper()
	p := newClipPrefsStore(filepath.Join(t.TempDir(), "state"))
	h.svc.prefs = p
	return p
}

// TestClipServicePrefsGateAndDefaults 手装体 prefs==nil：GetPrefs 回显默认
// 值（wire 形态完整），SetPrefs 如实报"未就绪"——不造半生效态。
func TestClipServicePrefsGateAndDefaults(t *testing.T) {
	h := newSvcHarness(t)
	defer h.stop()

	p, err := h.svc.GetPrefs()
	if err != nil {
		t.Fatalf("GetPrefs: %v", err)
	}
	if p.MaxEntries != defaultMaxEntries || len(p.ExcludedFactory) != len(sensitiveDefaultUsers) {
		t.Fatalf("nil prefs 应回显默认: %+v", p)
	}
	if _, err := h.svc.SetPrefs(Prefs{MaxEntries: 600}); err == nil {
		t.Fatal("未接存储时 SetPrefs 应如实报错")
	}
}

// TestClipServiceSetPrefsApplies SetPrefs 热应用全链：钳制回显即生效值、
// 降容当场裁并反映到 GetStatus、custom 排除表即时生效、清空后出厂与
// password 硬规则仍拦、坏正则整表拒且运行态不动、QueueHotkey 入参忽略。
func TestClipServiceSetPrefsApplies(t *testing.T) {
	h := newSvcHarness(t)
	defer h.stop()
	pstore := h.attachPrefs(t)

	// 灌 6 条（默认闸内），再降到生效最小 50 不可行——用 ApplyLimits 直验过
	// 裁；此处验证"回显即生效"与排除/硬规则链。
	h.svc.handleCapture(textCap("一条普通记录"))

	echo, err := h.svc.SetPrefs(Prefs{
		MaxEntries: 99999, ExcludedCustom: []string{"WeChat"},
		AppRetentionDays: map[string]int{"x.exe": 1}, QueueHotkey: "乱填",
	})
	if err != nil {
		t.Fatalf("SetPrefs: %v", err)
	}
	if echo.MaxEntries != maxMaxEntries {
		t.Fatalf("应回显钳制后的生效值: %d", echo.MaxEntries)
	}
	if echo.QueueHotkey != pstore.queueHotkey() {
		t.Fatalf("QueueHotkey 入参应被忽略保旧值: %q vs %q", echo.QueueHotkey, pstore.queueHotkey())
	}
	if max, _ := h.svc.store.CurrentLimits(); max != maxMaxEntries {
		t.Fatalf("store 未吃到新闸: %d", max)
	}

	// custom 排除即时生效：wechat.exe（listener 侧小写形态）来源不入库
	h.svc.handleCapture(capture{kind: KindText, text: "微信复制的内容",
		sourceApp: "微信", sourceExe: "wechat.exe", at: time.Now()})
	if n := h.svc.store.Count(); n != 1 {
		t.Fatalf("custom 排除未生效: count=%d", n)
	}
	// password 子串硬规则与表无关：custom 清空仍拦（走"读全量→改域→
	// 发全量"的前端同款姿势——整表替换语义下漏发域=回落默认，全量表
	// 才保住其他域）
	setPrefsPatch(t, h.svc, func(p *Prefs) { p.ExcludedCustom = []string{} })
	h.svc.handleCapture(capture{kind: KindText, text: "不该入库",
		sourceExe: "somepasswordmgr.exe", at: time.Now()})
	h.svc.handleCapture(capture{kind: KindText, text: "也不该入库",
		sourceExe: "1password.exe", at: time.Now()})
	if n := h.svc.store.Count(); n != 1 {
		t.Fatalf("出厂表/password 硬规则失守: count=%d", n)
	}
	st, err := h.svc.GetStatus()
	if err != nil || len(st.ExcludedExes) != len(sensitiveDefaultUsers) {
		t.Fatalf("GetStatus 活表回显失真: %+v %v", st, err)
	}

	// 坏正则整表拒：运行态（含数值域/排除表域）一律不动
	before := h.svc.prefs.wirePrefs()
	cur := before
	cur.MaskCustomPatterns = []string{"("}
	if _, err := h.svc.SetPrefs(cur); err == nil {
		t.Fatal("坏正则应整表拒绝")
	}
	again := h.svc.prefs.wirePrefs()
	if again.MaxEntries != before.MaxEntries || len(again.ExcludedCustom) != 0 {
		t.Fatalf("拒绝后运行态被半写入: %+v", again)
	}
}

// setPrefsPatch 整表语义模拟器：读现全量→改指定域→发全量（真实前端
// "本地持全表、改一项发全表"的行为）。零值域会被 SetPrefs 重置为默认——
// 这条语义的另一半由 normalizeClipPrefs(Prefs{}) 的默认回落用例钉住。
func setPrefsPatch(t *testing.T, svc *ClipboardService, patch func(*Prefs)) Prefs {
	t.Helper()
	cur, err := svc.GetPrefs()
	if err != nil {
		t.Fatalf("GetPrefs: %v", err)
	}
	patch(&cur)
	out, err := svc.SetPrefs(cur)
	if err != nil {
		t.Fatalf("SetPrefs: %v", err)
	}
	return out
}

// TestClipServiceSweepTriggers 清扫两处触发点：start() 一次 + 真正新增入库
// 顺带；去重顶置不空转（final.ID≠来 ID）。
func TestClipServiceSweepTriggers(t *testing.T) {
	h := newSvcHarness(t)
	pstore := h.attachPrefs(t)
	if _, err := pstore.Set(Prefs{AppRetentionDays: map[string]int{"test.exe": 1}}); err != nil {
		t.Fatal(err)
	}
	// 预置一条"十天前"的旧条目（直存账本，绕开服务时间）
	old := entryText("old-1", "陈年旧账")
	old.SourceExe = "test.exe"
	old.CreatedAt = time.Now().Add(-10 * 24 * time.Hour).UnixMilli()
	if _, _, err := h.svc.store.Add(old, time.Now()); err != nil {
		t.Fatal(err)
	}

	// 触发点一：start（harness 构造已 start 过一轮——那时还没有规则；
	// 再 start 一次模拟"带规则启动"）
	if err := h.svc.start(); err != nil {
		t.Fatal(err)
	}
	if _, ok := h.svc.store.Get("old-1"); ok {
		t.Fatal("start 清扫触发点未生效")
	}

	// 触发点二：新增入库顺带
	old2 := entryText("old-2", "又来一笔陈账")
	old2.SourceExe = "test.exe"
	old2.CreatedAt = time.Now().Add(-10 * 24 * time.Hour).UnixMilli()
	if _, _, err := h.svc.store.Add(old2, time.Now()); err != nil {
		t.Fatal(err)
	}
	h.svc.handleCapture(textCap("今天的新复制"))
	if _, ok := h.svc.store.Get("old-2"); ok {
		t.Fatal("入库顺带清扫未生效")
	}
	// removed 事件应覆盖两笔被扫条目
	removed := 0
	for _, e := range h.events {
		if e.name == evRemoved {
			removed++
		}
	}
	if removed != 2 {
		t.Fatalf("removed 广播数失真: %d（events=%v）", removed, h.eventNames())
	}
	// 去重顶置路径不空转：再复制同文（顶置，无新增）——old 域已空，无从
	// 再扫，事件数不变即证明未多发
	before := len(h.events)
	h.svc.handleCapture(textCap("今天的新复制"))
	if len(h.events) != before+1 { // 只有一条 updated
		t.Fatalf("去重顶置不应附带清扫事件: +%d", len(h.events)-before)
	}
	h.stop()
}
