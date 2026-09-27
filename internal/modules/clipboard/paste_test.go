package clipboard

import (
	"fmt"
	"strings"
	"testing"
	"time"
	"unsafe"
)

// ---------- R-G1 Paste 单测（契约 §12 v1.7.1）----------
// 断言面：INPUT/KEYBDINPUT 结构构造（永不真发键）、粘贴目标资格纯函数、
// 非 text 拒绝、降级矩阵与缝编排（focusWin/sendKeys/waitForeground 假件）。

// TestPasteInputLayout Win32 注入结构逐字节钉死（amd64）：SendInput 校验
// cbSize=sizeof(INPUT)=40，KEYBDINPUT=24、ki 偏移 8——错一字节全盘被拒；
// Ctrl+V 键序 = Ctrl↓ V↓ V↑ Ctrl↑，抬起步必须带 KEYEVENTF_KEYUP。
func TestPasteInputLayout(t *testing.T) {
	if unsafe.Sizeof(winKeybdInput{}) != 24 {
		t.Fatalf("KEYBDINPUT 应 24 字节，得 %d", unsafe.Sizeof(winKeybdInput{}))
	}
	if unsafe.Sizeof(winInput{}) != 40 {
		t.Fatalf("INPUT 应 40 字节，得 %d", unsafe.Sizeof(winInput{}))
	}
	if unsafe.Offsetof(winInput{}.ki) != 8 {
		t.Fatalf("联合区应落在偏移 8，得 %d", unsafe.Offsetof(winInput{}.ki))
	}

	ins := buildCtrlVInputs(0x1D, 0x2F)
	want := []struct {
		vk, scan uint16
		flags    uint32
	}{
		{vkControl, 0x1D, 0},
		{vkV, 0x2F, 0},
		{vkV, 0x2F, keyEventKeyUp},
		{vkControl, 0x1D, keyEventKeyUp},
	}
	for i, w := range want {
		g := ins[i]
		if g.typ != inputKeyboard {
			t.Fatalf("第 %d 项 type 应为 INPUT_KEYBOARD，得 %d", i, g.typ)
		}
		if g.ki.wVk != w.vk || g.ki.wScan != w.scan || g.ki.dwFlags != w.flags {
			t.Fatalf("第 %d 项按键不符: got vk=%#x scan=%#x flags=%#x want vk=%#x scan=%#x flags=%#x",
				i, g.ki.wVk, g.ki.wScan, g.ki.dwFlags, w.vk, w.scan, w.flags)
		}
	}
}

// TestPasteAutoEligible 粘贴目标资格纯函数：目标只在浮层显示期有效（生命
// 周期随显隐），句柄 0 值永不自动。
func TestPasteAutoEligible(t *testing.T) {
	cases := []struct {
		name   string
		shown  bool
		target uintptr
		want   bool
	}{
		{"浮层显示且句柄在", true, 0x1024, true},
		{"浮层未开有旧账", false, 0x1024, false},
		{"浮层开着但没记到窗", true, 0, false},
		{"双缺", false, 0, false},
	}
	for _, c := range cases {
		if got := pasteAutoEligible(c.shown, c.target); got != c.want {
			t.Fatalf("%s: want %v got %v", c.name, c.want, got)
		}
	}
}

// TestPasteRejectsNonTextAndMissing image/file 返回"暂不支持自动粘贴+详情区
// 指引"话术且不回填不抢焦点；不存在条目如实报错。调用门拒（无头停用形态）
// 同样如实上抛且零副作用。话术回归锚（R-F1 审查发现）：拒绝消息严禁指认
// "点复制按钮"死路——Set 对非 text 同样拒绝，二源话术必矛盾。
func TestPasteRejectsNonTextAndMissing(t *testing.T) {
	h := newSvcHarness(t)
	defer h.stop()

	h.svc.handleCapture(capture{kind: KindFile, files: []string{`C:\tmp\a.txt`}, at: time.Now()})
	fileID := mustSingleID(t, h)

	err := h.svc.Paste(fileID)
	if err == nil || !strings.Contains(err.Error(), "暂不支持自动粘贴") || !strings.Contains(err.Error(), "详情区预览") {
		t.Fatalf("file 条目应回可读拒绝: %v", err)
	}
	if strings.Contains(err.Error(), "复制按钮") {
		t.Fatalf("拒绝话术不得指认 Set 死路（非 text 的 Set 同样被拒）: %v", err)
	}
	if len(h.writes) != 0 {
		t.Fatalf("拒绝路径不得回填剪贴板: %v", h.writes)
	}

	if err := h.svc.Paste("deadbeef"); err == nil || !strings.Contains(err.Error(), "不存在") {
		t.Fatalf("缺条目应如实报错: %v", err)
	}

	// 调用门拒（模块停用/未启动态的既有门接法）：错误原样上抛，零回填。
	h.svc.holder.SetGate(denyGate{})
	if err := h.svc.Paste(fileID); err == nil || !strings.Contains(err.Error(), "门已拒") {
		t.Fatalf("调用门拒应上抛: %v", err)
	}
	if len(h.writes) != 0 {
		t.Fatalf("门拒路径不得回填: %v", h.writes)
	}
}

// TestPasteDegradeNoTarget 无目标三态（浮层未开 / 记不到窗 / 收起后旧账）：
// 回填照做（Set 半程价值保留），错误话术统一"请手动 Ctrl+V"，绝不触缝。
func TestPasteDegradeNoTarget(t *testing.T) {
	h := newSvcHarness(t)
	defer h.stop()

	focused := false
	h.svc.focusWin = func(uintptr) error { focused = true; return nil }
	h.svc.sendKeys = func() error { return fmt.Errorf("不应发键") }

	h.svc.handleCapture(textCap("粘回原窗口"))
	id := mustSingleID(t, h)

	// 浮层从未开：ovShown=false、pasteTarget=0
	if err := h.svc.Paste(id); err == nil || !strings.Contains(err.Error(), "请切回原窗口手动 Ctrl+V") {
		t.Fatalf("无目标应降级: %v", err)
	}
	if len(h.writes) != 1 || h.writes[0] != "粘回原窗口" {
		t.Fatalf("降级路径应已回填: %v", h.writes)
	}
	if focused {
		t.Fatal("无目标不得抢焦点")
	}

	// 浮层开着但记录失败（句柄 0）
	h.svc.ovMu.Lock()
	h.svc.ovShown, h.svc.pasteTarget = true, 0
	h.svc.ovMu.Unlock()
	if err := h.svc.Paste(id); err == nil || !strings.Contains(err.Error(), "手动 Ctrl+V") {
		t.Fatalf("句柄 0 应降级: %v", err)
	}

	// 浮层收起后的旧账：句柄在但 ovShown=false，生命周期随显隐作废
	h.svc.ovMu.Lock()
	h.svc.ovShown, h.svc.pasteTarget = false, 0xfeed
	h.svc.ovMu.Unlock()
	if err := h.svc.Paste(id); err == nil || !strings.Contains(err.Error(), "手动 Ctrl+V") {
		t.Fatalf("收起后旧账应降级: %v", err)
	}
	if focused {
		t.Fatal("降级三态全程不得抢焦点")
	}
}

// TestPasteFlowAndFailures 缝编排全矩阵：成功链 = 回填→抢焦→前台落位→发键；
// 抢焦失败/前台不落位/发键失败三处降级各归其位，错误如实上抛且话术含
// "手动 Ctrl+V"；前台不落位时绝不触键（防误 inject 第三窗口）。
func TestPasteFlowAndFailures(t *testing.T) {
	h := newSvcHarness(t)
	defer h.stop()

	h.svc.handleCapture(textCap("hello paste"))
	id := mustSingleID(t, h)
	h.svc.ovMu.Lock()
	h.svc.ovShown, h.svc.pasteTarget = true, 0x4242
	h.svc.ovMu.Unlock()

	step := func(focus func(uintptr) error, wait func(uintptr) bool, send func() error) error {
		h.svc.focusWin, h.svc.waitForeground, h.svc.sendKeys = focus, wait, send
		return h.svc.Paste(id)
	}

	// 成功链
	var gotHandle uintptr
	sent := 0
	if err := step(func(hwnd uintptr) error { gotHandle = hwnd; return nil },
		func(hwnd uintptr) bool { return hwnd == 0x4242 },
		func() error { sent++; return nil }); err != nil {
		t.Fatalf("成功链不应报错: %v", err)
	}
	if gotHandle != 0x4242 || sent != 1 {
		t.Fatalf("成功链编排失真: handle=%#x sent=%d", gotHandle, sent)
	}

	// 抢焦失败（句柄失效/前台锁）：回填已成、不发键
	if err := step(func(uintptr) error { return fmt.Errorf("SetForegroundWindow 返回 FALSE") },
		func(uintptr) bool { return true },
		func() error { sent++; return nil }); err == nil ||
		!strings.Contains(err.Error(), "焦点归还原窗口失败") || !strings.Contains(err.Error(), "FALSE") {
		t.Fatalf("抢焦失败应上抛原因: %v", err)
	}

	// 前台未落位：不发键
	if err := step(func(uintptr) error { return nil },
		func(uintptr) bool { return false },
		func() error { sent++; return nil }); err == nil ||
		!strings.Contains(err.Error(), "未成为前台") {
		t.Fatalf("前台未落位应降级: %v", err)
	}

	// 发键失败：错误如实包装
	if err := step(func(uintptr) error { return nil },
		func(uintptr) bool { return true },
		func() error { return fmt.Errorf("SendInput 仅投递 0/4") }); err == nil ||
		!strings.Contains(err.Error(), "0/4") {
		t.Fatalf("发键失败应上抛: %v", err)
	}
	if sent != 1 {
		t.Fatalf("三处失败路径均不得发键，sent=%d", sent)
	}
	if len(h.writes) != 4 {
		t.Fatalf("四趟 Paste 应四趟回填，得 %v", h.writes)
	}
}

// TestPasteRecordHeadlessSafe recordPasteTarget 只读前台句柄不发键不碰窗口，
// 无头调用安全；与 claimPasteTarget 同锁同账（浮层未开时记到什么都不自动）。
func TestPasteRecordHeadlessSafe(t *testing.T) {
	h := newSvcHarness(t)
	defer h.stop()

	h.svc.recordPasteTarget()
	if _, ok := h.svc.claimPasteTarget(); ok {
		t.Fatal("浮层未开时 claim 不应有自动资格")
	}
	h.svc.ovMu.Lock()
	h.svc.ovShown = true
	h.svc.ovMu.Unlock()
	target, ok := h.svc.claimPasteTarget()
	if want := pasteAutoEligible(true, target); ok != want {
		t.Fatalf("claim 与资格纯函数口径分裂: target=%#x ok=%v", target, ok)
	}
}

// ---------- 测试件 ----------

// denyGate 调用门拒假件（无模块停用真门的单测态下模拟"停用/未启动"拒接）。
type denyGate struct{}

func (denyGate) Acquire(string) (func(), error) {
	return nil, fmt.Errorf("门已拒：模块停用中")
}

// mustSingleID 取库内唯一条目的 ID。
func mustSingleID(t *testing.T, h *svcHarness) string {
	t.Helper()
	entries := h.svc.store.List("", "all", 0)
	if len(entries) != 1 {
		t.Fatalf("期望库内 1 条，得 %d", len(entries))
	}
	return entries[0].ID
}

// TestPasteRecordsUsage 选中即使用（契约 §12 v1.7 裁决）：回填做成即记一次
// 使用，与 Get 同口径 best-effort；降级路径（焦点/按键环节失败）回填同样已成，
// 账照记——成败只影响"自动补粘"半程，不改"用了这条"的事实。
func TestPasteRecordsUsage(t *testing.T) {
	h := newSvcHarness(t)
	defer h.stop()
	h.svc.handleCapture(textCap("记一次使用"))
	id := mustSingleID(t, h)
	before, _ := h.svc.store.Get(id)

	h.svc.focusWin = func(uintptr) error { return nil }
	h.svc.sendKeys = func() error { return nil }
	h.svc.waitForeground = func(uintptr) bool { return true }
	h.svc.ovMu.Lock()
	h.svc.ovShown, h.svc.pasteTarget = true, 0x1234
	h.svc.ovMu.Unlock()

	if err := h.svc.Paste(id); err != nil {
		t.Fatalf("成功链 Paste: %v", err)
	}
	mid, _ := h.svc.store.Get(id)
	if mid.UseCount != before.UseCount+1 {
		t.Fatalf("成功粘贴应记一次使用: %d→%d", before.UseCount, mid.UseCount)
	}

	// 降级链（无目标）：回填成功、自动补粘失败——账仍要 +1。
	h.svc.ovMu.Lock()
	h.svc.ovShown = false
	h.svc.ovMu.Unlock()
	if err := h.svc.Paste(id); err == nil || !strings.Contains(err.Error(), "手动 Ctrl+V") {
		t.Fatalf("无目标应降级: %v", err)
	}
	after, _ := h.svc.store.Get(id)
	if after.UseCount != mid.UseCount+1 {
		t.Fatalf("降级路径回填已成，使用账照记: %d→%d", mid.UseCount, after.UseCount)
	}
}
