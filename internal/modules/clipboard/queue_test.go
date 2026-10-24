package clipboard

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hanxi/internal/hotkey"
)

// ---------- v1.8 顺序粘贴队列（契约 §12 v1.8 第 8/9 条） ----------

// queueHarness 起一个缝全通的队列试验台：focusWin 注入"被调即 t.Fatal"闭包
// ——钉死 QueueStep ≠ Paste 的核心语义（不还焦点、用户已坐在目标里）。
func queueHarness(t *testing.T) (*svcHarness, *[]string, *int) {
	t.Helper()
	h := newSvcHarness(t)
	h.svc.focusWin = func(uintptr) error {
		t.Fatal("QueueStep 不得抢焦点/还原焦点（当前前台即目标，v1.8 裁决）")
		return nil
	}
	h.svc.waitForeground = func(uintptr) bool {
		t.Fatal("QueueStep 不得等前台落位")
		return true
	}
	var sent int
	h.svc.sendKeys = func() error { sent++; return nil }
	return h, &h.writes, &sent
}

func captureID(t *testing.T, h *svcHarness, text string) string {
	t.Helper()
	h.svc.handleCapture(textCap(text))
	list := h.svc.store.List("", "text", 0)
	for _, e := range list {
		if e.Preview == text {
			return e.ID
		}
	}
	t.Fatalf("未找到条目 %q", text)
	return ""
}

// injectQueue 队内直塞（绕开 QueueAdd 的存在性验真）：专造"入队后被账本
// 删掉/淘汰"的陈旧竞态兜底场景——服务联动摘除被绕过，只剩贴出时 peek 一跳。
func injectQueue(h *svcHarness, ids ...string) {
	h.svc.mu.Lock()
	h.svc.queue = append(h.svc.queue, ids...)
	h.svc.mu.Unlock()
}

func queueIDs(t *testing.T, h *svcHarness) []string {
	t.Helper()
	ids, err := h.svc.QueueList()
	if err != nil {
		t.Fatalf("QueueList: %v", err)
	}
	return ids
}

// TestQueueAddAdmission 准入面：text 入队、去重与序保持；非 text 入队即拒
// （话术给浮层指引）；不存在条目拒；同 id 幂等不重复、不重复广播；上限 50。
func TestQueueAddAdmission(t *testing.T) {
	h, _, _ := queueHarness(t)
	defer h.stop()

	idA := captureID(t, h, "队首 A")
	idB := captureID(t, h, "队尾 B")
	if err := h.svc.QueueAdd(idA); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.QueueAdd(idB); err != nil {
		t.Fatal(err)
	}
	if got := queueIDs(t, h); len(got) != 2 || got[0] != idA || got[1] != idB {
		t.Fatalf("FIFO 序失真: %v", got)
	}
	eventsBefore := h.countEvents(evQueueChanged)
	if err := h.svc.QueueAdd(idA); err != nil {
		t.Fatal(err)
	}
	if got := queueIDs(t, h); len(got) != 2 {
		t.Fatalf("同 id 应幂等: %v", got)
	}
	if h.countEvents(evQueueChanged) != eventsBefore {
		t.Fatal("幂等入队不应重复广播")
	}

	// 非 text 拒：file 条目话术指路浮层
	h.svc.handleCapture(capture{kind: KindFile, files: []string{`C:\tmp\x.txt`}, at: time.Now()})
	fileID := ""
	for _, e := range h.svc.store.List("", "file", 0) {
		fileID = e.ID
	}
	err := h.svc.QueueAdd(fileID)
	if err == nil || !strings.Contains(err.Error(), "仅支持文本") || !strings.Contains(err.Error(), "浮层") {
		t.Fatalf("非 text 应入队即拒并给指引: %v", err)
	}
	if err := h.svc.QueueAdd("deadbeef"); err == nil || !strings.Contains(err.Error(), "不存在") {
		t.Fatalf("缺条目应报错: %v", err)
	}

	// 上限 50：直塞垫满，第 51 条（真实且未在队的新条目）拒
	pads := make([]string, 0, maxQueueLen-2)
	for i := range maxQueueLen - 2 {
		pads = append(pads, fmt.Sprintf("pad-%02d", i))
	}
	injectQueue(h, pads...)
	idC := captureID(t, h, "第三条")
	if err := h.svc.QueueAdd(idC); err == nil || !strings.Contains(err.Error(), "已满") {
		t.Fatalf("超上限应拒: %v", err)
	}
	if err := h.svc.QueueClear(); err != nil {
		t.Fatal(err)
	}
}

// TestQueueStepConsumesInOrder 逐步贴出全链：序=FIFO、每步 writeText+sendKeys
// 各一次、Touch 记账、queue:changed 快照收敛；贴空后如实报"队列为空"。
func TestQueueStepConsumesInOrder(t *testing.T) {
	h, writes, sent := queueHarness(t)
	defer h.stop()

	idA := captureID(t, h, "第一条")
	idB := captureID(t, h, "第二条")
	_ = h.svc.QueueAdd(idA)
	_ = h.svc.QueueAdd(idB)

	if err := h.svc.QueueStep(); err != nil {
		t.Fatalf("step1: %v", err)
	}
	if len(*writes) != 1 || (*writes)[0] != "第一条" || *sent != 1 {
		t.Fatalf("step1 编排失真: writes=%v sent=%d", *writes, *sent)
	}
	if got := queueIDs(t, h); len(got) != 1 || got[0] != idB {
		t.Fatalf("step1 应消费队首: %v", got)
	}
	e, _ := h.svc.store.Get(idA)
	if e.UseCount < 1 {
		t.Fatal("队列贴出应记使用（Paste 同谱）")
	}
	if err := h.svc.QueueStep(); err != nil {
		t.Fatalf("step2: %v", err)
	}
	if err := h.svc.QueueStep(); err == nil || !strings.Contains(err.Error(), "为空") {
		t.Fatalf("空队应可读报错: %v", err)
	}
}

// TestQueueStepFailures writeText 失败=进度不推进可重试；sendKeys 失败=
// 照常前进（回填已成半程，话术含"手动 Ctrl+V"与余数）；陈旧队首惰性跳。
func TestQueueStepFailures(t *testing.T) {
	h, _, _ := queueHarness(t)
	defer h.stop()

	idA := captureID(t, h, "甲")
	idB := captureID(t, h, "乙")
	_ = h.svc.QueueAdd(idA)
	_ = h.svc.QueueAdd(idB)

	// 回填失败：不消费
	h.svc.writeText = func(string) error { return fmt.Errorf("OpenClipboard 抢占失败") }
	err := h.svc.QueueStep()
	if err == nil || !strings.Contains(err.Error(), "进度未推进") {
		t.Fatalf(`writeText 失败应报"进度未推进": %v`, err)
	}
	if got := queueIDs(t, h); len(got) != 2 {
		t.Fatalf("回填失败不得消费: %v", got)
	}
	h.svc.writeText = func(text string) error { h.writes = append(h.writes, text); return nil }

	// 发键失败：消费照常 + 话术含余数与手动指引
	h.svc.sendKeys = func() error { return fmt.Errorf("SendInput 0/4") }
	err = h.svc.QueueStep()
	if err == nil || !strings.Contains(err.Error(), "手动 Ctrl+V") || !strings.Contains(err.Error(), "余下 1 项") {
		t.Fatalf("sendKeys 失败应前进并给指引: %v", err)
	}
	if got := queueIDs(t, h); len(got) != 1 || got[0] != idB {
		t.Fatalf("sendKeys 失败应已消费队首: %v", got)
	}

	// 陈旧队首：条目被账本级删除（绕过服务联动，模拟"入队后被淘汰"竞态兜底），
	// 后方还有真条目——跳陈旧、贴下一项，一气呵成不报错
	h.svc.sendKeys = func() error { return nil }
	if _, err := h.svc.store.Delete(idB); err != nil {
		t.Fatal(err)
	}
	idC := captureID(t, h, "丙")
	if err := h.svc.QueueAdd(idC); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.QueueStep(); err != nil {
		t.Fatalf("陈旧项应被跳过后贴出下一项，不该直接失败: %v", err)
	}
	if last := h.writes[len(h.writes)-1]; last != "丙" {
		t.Fatalf("跳陈旧后应贴出丙，实际末笔=%q", last)
	}
	// 整队皆陈 → 清队如实报错（直塞绕开存在性验真，专造陈旧竞态）
	injectQueue(h, "ghost-1", "ghost-2")
	err = h.svc.QueueStep()
	if err == nil || !strings.Contains(err.Error(), "为空") {
		t.Fatalf("整队陈旧应跳完报空: %v", err)
	}
	if got := queueIDs(t, h); len(got) != 0 {
		t.Fatalf("幽灵队应被清尽: %v", got)
	}
}

// TestQueuePruneLinks 四联动：Delete 摘挂名、ClearAll 清队、淘汰清单进
// emitIngested 即 prune（容量/清扫两源共用一口）、删后贴自摘。
func TestQueuePruneLinks(t *testing.T) {
	h, _, _ := queueHarness(t)
	defer h.stop()

	idA := captureID(t, h, "留名甲")
	idB := captureID(t, h, "留名乙")
	_ = h.svc.QueueAdd(idA)
	_ = h.svc.QueueAdd(idB)

	if err := h.svc.Delete(idA); err != nil {
		t.Fatal(err)
	}
	if got := queueIDs(t, h); len(got) != 1 || got[0] != idB {
		t.Fatalf("Delete 应联动摘除队列挂名: %v", got)
	}

	// emitIngested 淘汰联动口（容量淘汰与清扫的 evicted 都汇这里）
	victim := captureID(t, h, "将被淘汰")
	_ = h.svc.QueueAdd(victim)
	h.svc.emitIngested(Entry{ID: "fresh-irrelevant", Kind: KindText}, []Entry{{ID: victim}})
	if got := queueIDs(t, h); len(got) != 1 || got[0] != idB {
		t.Fatalf("淘汰联动失守: %v", got)
	}

	if err := h.svc.ClearAll(); err != nil {
		t.Fatal(err)
	}
	if got := queueIDs(t, h); len(got) != 0 {
		t.Fatalf("ClearAll 应清队: %v", got)
	}

	// stop 弃队（不持久裁决）
	_ = h.svc.QueueAdd(idB)
	h.stop()
	if got := queueIDs(t, h); len(got) != 0 {
		t.Fatalf("停机必须弃队: %v", got)
	}
}

// TestQueueBusyGate 单发闸：上一贴的 writeText 尚未返回时再按直接拒，
// 释放后正常——连击不得并发改写系统剪贴板。
func TestQueueBusyGate(t *testing.T) {
	h, _, _ := queueHarness(t)
	defer h.stop()
	idA := captureID(t, h, "慢回填")
	_ = h.svc.QueueAdd(idA)

	release := make(chan struct{})
	started := make(chan struct{})
	h.svc.writeText = func(string) error {
		close(started)
		<-release
		return nil
	}

	stepDone := make(chan error, 1)
	go func() { stepDone <- h.svc.queueStep() }()
	<-started
	if err := h.svc.queueStep(); err == nil || !strings.Contains(err.Error(), "尚未完成") {
		t.Fatalf("在途期再按应被单发闸拒: %v", err)
	}
	close(release)
	if err := <-stepDone; err != nil {
		t.Fatalf("首贴应成: %v", err)
	}
	// 闸释放后可再走（此时队已空——报"为空"而非"尚未完成"）
	if err := h.svc.queueStep(); err == nil || !strings.Contains(err.Error(), "为空") {
		t.Fatalf("释放后应正常执行: %v", err)
	}
}

// TestQueueDeleteAfterPaste 队列 × 删后贴合成：QueueStep 成功 + 开关开 →
// 条目销账、removed 广播、队列快照同步（队首已消费，prune 幂等）。
func TestQueueDeleteAfterPaste(t *testing.T) {
	h, _, _ := queueHarness(t)
	defer h.stop()
	pstore := h.attachPrefs(t)
	if _, err := pstore.Set(Prefs{DeleteAfterPaste: true}); err != nil {
		t.Fatal(err)
	}
	idA := captureID(t, h, "贴完就销")
	_ = h.svc.QueueAdd(idA)
	if err := h.svc.QueueStep(); err != nil {
		t.Fatalf("step: %v", err)
	}
	if _, ok := h.svc.store.Get(idA); ok {
		t.Fatal("队列贴出同样吃删后贴偏好")
	}
	if h.countEvents(evRemoved) != 1 {
		t.Fatalf("removed 应一发: %v", h.eventNames())
	}
}

// TestQueueHotkeyTransaction 改键事务（校验即绑定、败不落盘、""=停用）：
// 换键成功→Registry 实况与 prefs 同步；冲突→旧键位旧配置原样；非法形态拒；
// 停用→解绑+落盘显式空；GetQueueHotkey 双字段如实。
func TestQueueHotkeyTransaction(t *testing.T) {
	h := newSvcHarness(t)
	defer h.stop()
	pstore := h.attachPrefs(t)
	fb := newFakeHotkeyBackend()
	fb.conflictKeys["Ctrl+Alt+R"] = true
	reg := hotkey.NewRegistry(fb)
	h.svc.setHotkeyRegistry(reg)
	if err := h.svc.start(); err != nil {
		t.Fatal(err)
	}
	// start 按 prefs 默认键绑定 X
	st, err := h.svc.GetQueueHotkey()
	if err != nil || st.Accel != defaultQueueHotkey || !st.Registered {
		t.Fatalf("start 应绑默认键: %+v %v", st, err)
	}

	// 成功换键：绑定与配置两头动
	st, err = h.svc.SetQueueHotkey("Ctrl+Alt+Q")
	if err != nil || st.Accel != "Ctrl+Alt+Q" || !st.Registered {
		t.Fatalf("换键失真: %+v %v", st, err)
	}
	if pstore.queueHotkey() != "Ctrl+Alt+Q" {
		t.Fatal("换键未落盘")
	}

	// 冲突：绑定败→不落盘、旧键位存续（"存了但没绑上"分叉零容忍）
	if _, err := h.svc.SetQueueHotkey("Ctrl+Alt+R"); err == nil || !strings.Contains(err.Error(), "保持原键位") {
		t.Fatalf("冲突应拒并保旧: %v", err)
	}
	if pstore.queueHotkey() != "Ctrl+Alt+Q" || !fb.IsRegistered("Ctrl+Alt+Q") {
		t.Fatalf("冲突后旧键位失守: prefs=%q reg=%v", pstore.queueHotkey(), fb.registered)
	}

	// 非法形态（无修饰键）拒
	if _, err := h.svc.SetQueueHotkey("X"); err == nil || !strings.Contains(err.Error(), "修饰键") {
		t.Fatalf("非法键位应拒: %v", err)
	}

	// ""=停用：解绑 + 显式空落盘（重开不再绑——装载面指针语义）
	st, err = h.svc.SetQueueHotkey("")
	if err != nil || st.Accel != "" || st.Registered {
		t.Fatalf("停用失真: %+v %v", st, err)
	}
	if fb.IsRegistered(defaultQueueHotkey) || fb.IsRegistered("Ctrl+Alt+Q") {
		t.Fatalf("停用后不得留绑: %v", fb.registered)
	}
	// 指针字段可辨性反证：装载要喂**目录**（newClipPrefsStore 内部拼文件名）
	reopened := newClipPrefsStore(filepath.Dir(pstore.filePath))
	if reopened.queueHotkey() != "" {
		t.Fatalf("停用态应显式持久: %q", reopened.queueHotkey())
	}
}

// TestQueueStepHeadlessSafe 无头/门拒形态：调用门拒上抛零副作用；真前台
// 动作全走缝（writeText/sendKeys 假件），单测永不触真 Win32。
func TestQueueStepHeadlessSafe(t *testing.T) {
	h := newSvcHarness(t)
	defer h.stop()
	h.svc.holder.SetGate(denyGate{})
	if err := h.svc.QueueStep(); err == nil || !strings.Contains(err.Error(), "门已拒") {
		t.Fatalf("门拒应上抛: %v", err)
	}
	if err := h.svc.QueueAdd("x"); err == nil || !strings.Contains(err.Error(), "门已拒") {
		t.Fatalf("QueueAdd 门拒应上抛: %v", err)
	}
}
