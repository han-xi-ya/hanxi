package clipboard

// 顺序粘贴队列（契约 §12 v1.8，借鉴 TieZ sequential paste）：多条文本历史
// 排成 FIFO，全局热键（默认 Ctrl+Alt+X，键位归 prefs）逐条贴到**当前前台
// 应用**——高频搬运场景（把一串散碎内容依次挪进同一目标）不必每轮重开浮层。
//
// 与 Paste 的分工（裁决 §12 v1.8 第 9 条）：Paste 服务浮层选取线——回填 +
// 焦点还给浮层打开瞬间的 pasteTarget + 模拟 Ctrl+V；QueueStep 服务"人已坐在
// 目标里按键"的语境——当前前台就是目标，**不还焦点、不等前台落位**，直接
// 回填 + 发键。自家写入由既有 owner-pid 回环抑制静默跳过，不入库、无需新机制。
//
// 队列裁决一览：内存簿记不持久（重启即弃，临时工作集不与 index.json 缠账）；
// 仅 text 准入（入队即拒，即时反馈）；入队后被删/淘汰/清扫 → 贴出时惰性
// 跳过并摘除（运行期事件链够不着"将来"，peek 校验是唯一可靠时点）；同 id
// 幂等不重复；上限 50；writeText 失败不消费（重试无害）；sendKeys 失败照常
// 前进（回填已成半程，回插会令"手动补贴后再按热键"双贴）；Paste 成功不消费
// 队列（两条线互不折损进度）。

import (
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"
)

// maxQueueLen 队列容量闸：超限如实拒绝（自用批搬场景 50 已是"一次大搬迁"，
// 更深队列靠反复入队衔接，不做翻页工程）。
const maxQueueLen = 50

// QueueChangedPayload clipboard:queue:changed 载荷：全量 id 快照（前端徽标
// 序号即数组序；淘汰/删除失同步由快照天然纠，不做增量 diff 协议）。
// 导出系 Wails 精确类型闸要求（RemovedPayload 同谱）。
type QueueChangedPayload struct {
	IDs []string `json:"ids"`
}

// QueueHotkeyState 队列热键实况回显（ocr SnipHotkeyState 同谱：Registered 读
// Registry 实况供设置面显示"在位/冲突降级"，Accel ""=停用）。
type QueueHotkeyState struct {
	Accel      string `json:"accel"`
	Registered bool   `json:"registered"`
}

// ---------- 内部簿记（s.mu 域；广播一律锁外） ----------

// queueSnapshot 当前队列拷贝，恒非 nil（wire 面 [] 与 null 的线形归一在源头
// 解决，桥层零特判）。
func (s *ClipboardService) queueSnapshot() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string{}, s.queue...)
}

// queueLen 当前队列长度（GetStatus 供数）。
func (s *ClipboardService) queueLen() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.queue)
}

// queueAddInner 幂等入队：已在队内不重复；容量闸如实拒绝。返回是否变化。
func (s *ClipboardService) queueAddInner(id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if slices.Contains(s.queue, id) {
		return false, nil // 幂等：状态未变，不广播
	}
	if len(s.queue) >= maxQueueLen {
		return false, fmt.Errorf("粘贴队列已满（上限 %d 项），请先贴出或清空", maxQueueLen)
	}
	s.queue = append(s.queue, id)
	return true, nil
}

// queueRemoveInner 按 id 摘除（任意位）；不在队内静默幂等。返回是否变化。
func (s *ClipboardService) queueRemoveInner(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, q := range s.queue {
		if q != id {
			continue
		}
		s.queue = append(s.queue[:i], s.queue[i+1:]...)
		return true
	}
	return false
}

// queueClearInner 清队，返回是否确有内容被清。
func (s *ClipboardService) queueClearInner() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := len(s.queue)
	s.queue = nil
	return n > 0
}

// queuePrune 条目消失联动摘除（Delete/ClearAll/容量淘汰/按应用清扫/删后贴
// 共用）：changed 时调用方锁外广播。ids 空零成本早退。
func (s *ClipboardService) queuePrune(ids []string) bool {
	if len(ids) == 0 {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	drop := func(x string) bool { return slices.Contains(ids, x) }
	kept := s.queue[:0]
	changed := false
	for _, q := range s.queue {
		if drop(q) {
			changed = true
			continue
		}
		kept = append(kept, q)
	}
	s.queue = kept
	return changed
}

// resetQueue 停机弃队（stop 收口，不广播——事件出口已随模块停用收拢）。
func (s *ClipboardService) resetQueue() {
	s.mu.Lock()
	s.queue = nil
	s.queueBusy = false
	s.mu.Unlock()
}

// ---------- 队列 RPC 面（全部经统一调用门） ----------

// QueueAdd 加入队列尾（仅 text 准入，入队即拒并给可读话术；同 id 幂等）。
func (s *ClipboardService) QueueAdd(id string) error {
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
		return fmt.Errorf("粘贴队列仅支持文本条目（该条为%s）；图片/文件请在浮层单条处理", kindCN(e.Kind))
	}
	changed, err := s.queueAddInner(id)
	if err != nil {
		return err
	}
	if changed {
		s.emitQueueChanged()
	}
	return nil
}

// QueueRemove 从队列摘除一条（条目本体不动；不在队内幂等静默）。
func (s *ClipboardService) QueueRemove(id string) error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()

	if s.queueRemoveInner(id) {
		s.emitQueueChanged()
	}
	return nil
}

// QueueClear 清空队列（条目本体都在历史里，清队不删账）。
func (s *ClipboardService) QueueClear() error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()

	if s.queueClearInner() {
		s.emitQueueChanged()
	}
	return nil
}

// QueueList 队列全量 id 快照（浮层徽标序号即数组序）。
func (s *ClipboardService) QueueList() ([]string, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return nil, gateErr
	}
	defer release()
	return s.queueSnapshot(), nil
}

// QueueStep 贴出队首到当前前台（RPC 面 = 门 + 转调；热键回调走无门内部版）。
func (s *ClipboardService) QueueStep() error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	return s.queueStep()
}

// queueStep 逐步贴出核心（热键 goroutine 与 RPC 共用）：
//  1. 单发闸：上一次贴出未走完（回填/发键含毫秒级等待）时再按直接拒——连击
//     不得并发改写系统剪贴板（settling 浮层同谱）；
//  2. peek 队首：条目已不存在/非 text（理论不可达）→ 摘除广播后跳下一项，
//     整队皆陈 → 清队如实报错；
//  3. writeText 失败 = 未消费：队列原样，再按可重试（回填是半程价值的地基，
//     没打地基不推进进度）；
//  4. 回填成功即消费点：弹队首 + Touch（"用了这条"记账，Paste 同谱
//     best-effort）+ 广播快照；
//  5. sendKeys 失败：内容已在剪贴板，**照常前进**（不回插）——话术指路
//     "手动 Ctrl+V 后再按热键续"；成功则按 prefs 联动删后贴。
//
// 全程不还焦点、不等前台落位：按键语境里当前前台就是目标；若此刻前台其实
// 是 hanxi 自己的窗口（用户忘了切走），Ctrl+V 落进自家界面——如实接受，
// 与用户在目标窗手动按 Ctrl+V 等价，无安全增量。
func (s *ClipboardService) queueStep() error {
	s.mu.Lock()
	if s.queueBusy {
		s.mu.Unlock()
		return fmt.Errorf("上一次队列贴出尚未完成，请稍候再按")
	}
	s.queueBusy = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.queueBusy = false
		s.mu.Unlock()
	}()

	for {
		head := ""
		s.mu.Lock()
		if len(s.queue) > 0 {
			head = s.queue[0]
		}
		s.mu.Unlock()
		if head == "" {
			return fmt.Errorf("粘贴队列为空")
		}

		e, ok := s.store.Get(head)
		if !ok || e.Kind != KindText {
			// 陈旧队首：摘除+广播，继续试下一项
			if s.queueRemoveInner(head) {
				s.emitQueueChanged()
			}
			continue
		}
		if err := s.writeText(e.Text); err != nil {
			return fmt.Errorf("队列首项回填失败（进度未推进，可再按重试）: %w", err)
		}

		// 消费点：回填已成即推进（sendKeys 成败不再回滚——见头注裁决 5）
		if s.queueRemoveInner(head) {
			s.emitQueueChanged()
		}
		if _, err := s.store.Touch(head, time.Now()); err != nil {
			slog.Warn("clipboard: 队列贴出记使用失败", "id", head, "err", err)
		}

		if err := s.sendKeys(); err != nil {
			left := s.queueLen()
			hint := "队尾已无待贴项"
			if left > 0 {
				hint = fmt.Sprintf("再按队列热键继续余下 %d 项", left)
			}
			return fmt.Errorf("已回填队列首项，模拟粘贴按键失败，请手动 Ctrl+V，%s: %w", hint, err)
		}
		s.applyDeleteAfterPaste(e)
		return nil
	}
}

// ---------- 队列热键对（ocr snip 键位对同谱：校验即绑定、败不落盘） ----------

// validateQueueHotkey 形态校验纯函数：去空白；非空必含修饰键（"+" 分隔），
// 空串=显式停用。真实可用性以 Registry 注册结果为准（冲突拒绝不落盘）。
func validateQueueHotkey(raw string) (string, error) {
	norm := strings.TrimSpace(raw)
	if norm != "" && !strings.Contains(norm, "+") {
		return norm, fmt.Errorf("热键 %q 无效：需至少一个修饰键（如 Ctrl+Alt+X），留空表示停用热键", norm)
	}
	return norm, nil
}

// GetQueueHotkey 队列热键实况（Accel=prefs 现值，Registered=Registry 在位
// 实况——"存了但没绑上"的分叉态由双字段如实暴露）。
func (s *ClipboardService) GetQueueHotkey() (QueueHotkeyState, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return QueueHotkeyState{}, gateErr
	}
	defer release()
	return s.queueHotkeyState(), nil
}

func (s *ClipboardService) queueHotkeyState() QueueHotkeyState {
	accel := s.prefs.queueHotkey()
	registered := false
	if accel != "" {
		if r := s.hotkeyRegistry(); r != nil {
			registered = r.Registered(queueHotkeySlot)
		}
	}
	return QueueHotkeyState{Accel: accel, Registered: registered}
}

// SetQueueHotkey 改键事务：形态校验 → **先绑定成功才落盘**（抢键失败保持
// 旧绑定旧配置，杜绝"设置页显示新键、系统里旧键还在按"的分叉）；空串=停用
// （解绑+落盘）。应用未接线注册器时如实报错不落盘。
func (s *ClipboardService) SetQueueHotkey(accel string) (QueueHotkeyState, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return QueueHotkeyState{}, gateErr
	}
	defer release()

	norm, err := validateQueueHotkey(accel)
	if err != nil {
		return QueueHotkeyState{}, err
	}
	if err := s.applyQueueHotkey(norm); err != nil {
		return QueueHotkeyState{}, fmt.Errorf("热键 %q 绑定失败，保持原键位: %w", norm, err)
	}
	if err := s.prefs.setQueueHotkey(norm); err != nil {
		// 键已绑而配置未存：下次启动会回旧键位——如实上浮让重按保存，
		// 不做"静默吞配置写失败"的乐观申报。
		return QueueHotkeyState{}, err
	}
	return s.queueHotkeyState(), nil
}
