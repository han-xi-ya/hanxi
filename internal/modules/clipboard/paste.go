package clipboard

// R-G1 粘回原窗口（契约 §12 v1.7.1，§4 十法→十一法）：Paste = Set 回填 +
// 把焦点还给**唤出浮层瞬间的前台窗**（pasteTarget）+ SendInput 模拟 Ctrl+V。
//
// 时序关键：showOverlay 在 Show/Focus **之前**记录 GetForegroundWindow——那一
// 瞬前台还是用户正要粘贴的目标窗（hanxi 自己的浮层还没上桌）。主界面行复制钮
// 维持 Set（只回填不抢焦点），Paste 专供浮层选取线。
//
// 钉回失败（句柄失效/前台锁拒绝/前台未落位/按键被拒）一律降级为"已回填请手动
// Ctrl+V"话术如实上抛——回填已经做成，错误只说明自动补全没做成，不吞不 panic。
// 仅 text kind 支持，image/file 返回可读"暂不支持自动粘贴"错误（首版边界）。
//
// 键盘注入走 user32 裸 syscall（INPUT/KEYBDINPUT 自建结构，x/sys 不导出，
// listener.go 同款纪律，零新依赖）；焦点归还复用 platform 的 SetForegroundForce
// （AttachThreadInput 借权谱，浮层置前同一件）。

import (
	"fmt"
	"log/slog"
	"time"
	"unsafe"
)

const (
	// Win32 INPUT/KEYBDINPUT 常量（winuser.h）。
	inputKeyboard       = 1                      // INPUT.type = INPUT_KEYBOARD
	keyEventKeyUp       = 0x0002                 // KEYEVENTF_KEYUP
	vkControl           = 0x11                   // VK_CONTROL（通用值，MapVirtualKey 落左 Ctrl 扫描码）
	vkV                 = 0x56                   // 'V'
	mapVKToVSC          = 0                      // MAPVK_VK_TO_VSC
	pasteKeyInputs      = 4                      // Ctrl↓ V↓ V↑ Ctrl↑
	pasteForegroundWait = 150 * time.Millisecond // 前台落位等待上限（契约 R-G1 ≤150ms）
	pasteForegroundPoll = 10 * time.Millisecond
)

var (
	procSendInputPaste = user32Clip.NewProc("SendInput")
	procMapVirtualKey  = user32Clip.NewProc("MapVirtualKeyW")
)

// winKeybdInput = Win32 KEYBDINPUT（amd64 逐字段对齐：2+2+4+4+uintptr，
// 尾部对齐补到 24 字节）。
type winKeybdInput struct {
	wVk         uint16
	wScan       uint16
	dwFlags     uint32
	dwTime      uint32
	dwExtraInfo uintptr
}

// winInput = Win32 INPUT（amd64 sizeof 恒 40）：type(4) + 联合区前衬到 8 字节
// 对齐(4) + 联合区按最大成员 MOUSEINPUT 撑开(32)。本线只填 KEYBDINPUT(24)，
// 其余 8 字节是联合余位衬垫。SendInput 校验 cbSize，布局错一字节全盘被拒——
// TestPasteInputLayout 以尺寸/偏移断言钉死。
type winInput struct {
	typ      uint32
	_pad     uint32
	ki       winKeybdInput
	_tailPad [8]byte
}

// keyStroke 一次按键步（down/up），构造与投递的中间表。
type keyStroke struct {
	vk   uint16
	scan uint16
	up   bool
}

// buildCtrlVInputs Ctrl+V 四键序纯构造（不发键，单测锁形状）：按住 Ctrl →
// 点按 V → 按原路松开。扫描码由调用方经 MapVirtualKey 查得（跟随活动键盘
// 布局，硬编码假定 QWERTY 对少数布局会落空）。
func buildCtrlVInputs(ctrlScan, vScan uint16) [pasteKeyInputs]winInput {
	steps := [pasteKeyInputs]keyStroke{
		{vk: vkControl, scan: ctrlScan},
		{vk: vkV, scan: vScan},
		{vk: vkV, scan: vScan, up: true},
		{vk: vkControl, scan: ctrlScan, up: true},
	}
	var ins [pasteKeyInputs]winInput
	for i, st := range steps {
		var flags uint32
		if st.up {
			flags = keyEventKeyUp
		}
		ins[i] = winInput{
			typ: inputKeyboard,
			ki:  winKeybdInput{wVk: st.vk, wScan: st.scan, dwFlags: flags},
		}
	}
	return ins
}

// mapScanCode VK → 扫描码（MAPVK_VK_TO_VSC；查失败回 0，SendInput 仍可按 VK 投递）。
func mapScanCode(vk uint16) uint16 {
	sc, _, _ := procMapVirtualKey.Call(uintptr(vk), mapVKToVSC)
	return uint16(sc)
}

// sendCtrlV 默认键手（Paste.sendKeys 缝的真身）：整组一次 SendInput，返回数
// 不足数即投递被拒（非交互桌面/UIPI 屏蔽等），如实报错不重试——粘贴目标不可
// 控地收键比重发更安全。
func sendCtrlV() error {
	ins := buildCtrlVInputs(mapScanCode(vkControl), mapScanCode(vkV))
	r, _, err := procSendInputPaste.Call(
		pasteKeyInputs,
		uintptr(unsafe.Pointer(&ins[0])),
		uintptr(unsafe.Sizeof(ins[0])),
	)
	if r != pasteKeyInputs {
		return fmt.Errorf("SendInput 仅投递 %d/%d 个按键事件（前台或桌面权限受限）: %v", r, pasteKeyInputs, err)
	}
	return nil
}

// waitForegroundWindow 默认前台落位等待（Paste.waitForeground 缝的真身）：
// SetForegroundWindow 成功到前台真正易主有毫秒级间隙，轮询比对目标句柄，
// 上限 ≤150ms（契约 R-G1）——超时不硬发键，宁可降级手动，绝不误注键他人窗口。
func waitForegroundWindow(target uintptr) bool {
	if target == 0 {
		return false
	}
	deadline := time.Now().Add(pasteForegroundWait)
	for {
		if fg, _, _ := procGetFgWindow.Call(); fg == target {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(pasteForegroundPoll)
	}
}

// pasteAutoEligible 自动粘贴资格判定纯函数：目标只在**浮层显示期**有效
// （生命周期随显隐：showOverlay 记录时置位、收起后 ovShown=false 即作废，
// 不养"半小时前的窗口"僵尸目标）；句柄 0 值（记录失败/无前台）无资格。
func pasteAutoEligible(shown bool, target uintptr) bool {
	return shown && target != 0
}

// recordPasteTarget showOverlay 打开瞬间调用（R-G1 时序钉：Show/Focus 之前）：
// 此刻前台还是用户即将粘贴进去的目标窗。取不到前台（0）也如实记账——判资格
// 的 pasteAutoEligible 会把 0 挡在自动线外，降级手动话术。
func (s *ClipboardService) recordPasteTarget() {
	fg, _, _ := procGetFgWindow.Call()
	s.ovMu.Lock()
	s.pasteTarget = fg
	s.ovMu.Unlock()
}

// claimPasteTarget 读取并核验粘贴目标（不消费：同一条目重试 Paste 仍可钉回，
// 失效由 ovShown/句柄有效性在下一环节如实兜底）。锁内只碰 uintptr/bool，
// Wails/Win32 调用全在锁外（ovMu 锁纪律同款）。
func (s *ClipboardService) claimPasteTarget() (uintptr, bool) {
	s.ovMu.Lock()
	target, shown := s.pasteTarget, s.ovShown
	s.ovMu.Unlock()
	return target, pasteAutoEligible(shown, target)
}

// Paste 粘回原窗口（契约 §12 v1.7 R-G1，§4 十一法之一）：取条目 → 仅 text
// 支持（其余返回可读"暂不支持自动粘贴，已可手动回填"错误）→ 复用 Set 的
// 回填通路（store.Get + writeText 缝）→ 焦点还给浮层打开瞬间记录的
// pasteTarget → 等前台落位（≤150ms）→ SendInput Ctrl+V。
//
// 降级矩阵（回填已成，错误只描述自动线缺口，话术统一"请手动 Ctrl+V"）：
//   - 无目标/浮层已收起 → 不抢焦点不发消息键；
//   - SetForegroundForce 失败（句柄失效/前台锁）→ 上抛原因；
//   - 前台未落位目标窗 → 绝不盲发（防误 inject 第三窗口）；
//   - SendInput 不足数 → 上抛。
//
// 无头/应用未运行：调用门（holder.Enter）拒则如实上抛；即便放行，浮层从未
// 打开则无 pasteTarget，走"无目标"降级——全程不 panic。
func (s *ClipboardService) Paste(id string) error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()

	e, ok := s.store.Get(id)
	if !ok {
		return fmt.Errorf("剪贴板条目不存在: %s", id)
	}
	if e.Kind != KindText {
		// 话术单源纪律：不指"点复制按钮"死路（Set 对非 text 同样拒绝）——如实说边界。
		return fmt.Errorf("暂不支持自动粘贴%s类条目：可在主界面详情区预览内容、逐条复制文件路径", kindCN(e.Kind))
	}
	// 回填先行（Set 内部通路同款）：粘贴失败不折损"已写回剪贴板"这半程价值。
	if err := s.writeText(e.Text); err != nil {
		return err
	}
	// 选中即使用：回填做成即记一次使用（焦点/按键环节成败不改此账；契约 §12 v1.7 裁决，
	// 与 Get 同口径 best-effort，失败仅 warn 不拦主链）。
	if _, err := s.store.Touch(id, time.Now()); err != nil {
		slog.Warn("clipboard: Paste 记使用失败", "id", id, "err", err)
	}

	target, ok := s.claimPasteTarget()
	if !ok {
		return fmt.Errorf("已回填系统剪贴板，未捕获到粘贴目标窗口（浮层未开或已收起），请切回原窗口手动 Ctrl+V")
	}
	if err := s.focusWin(target); err != nil {
		return fmt.Errorf("已回填系统剪贴板，焦点归还原窗口失败，请手动 Ctrl+V: %w", err)
	}
	if !s.waitForeground(target) {
		return fmt.Errorf("已回填系统剪贴板，原窗口 %dms 内未成为前台，请手动 Ctrl+V", int(pasteForegroundWait/time.Millisecond))
	}
	if err := s.sendKeys(); err != nil {
		return fmt.Errorf("已回填系统剪贴板，模拟粘贴按键失败，请手动 Ctrl+V: %w", err)
	}
	return nil
}
