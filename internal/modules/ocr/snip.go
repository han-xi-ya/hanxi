package ocr

// ---------- 前端 API：框选截屏识别（轮盘命令 / 页面按钮共用全链） ----------
//
// 链路：确保上游在线 → 唤起系统截屏覆盖层 → 以剪贴板接图 → 落临时件 → 转发识别
// → 悬浮结果卡（按开关自动复制）。覆盖层覆写剪贴板是方案固有代价，
// 用户取消/失败时把原本文本快照回写还原。

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"hanxi/internal/modules/ocr/instance"
	"hanxi/internal/modules/ocr/snip"
)

const (
	snipWaitTimeout = 45 * time.Second // 等待用户完成框选的上限（超时按取消）
	snipStartWait   = 15 * time.Second // 恰有并发启动流程时轮询等待就绪
)

// SnipAndRecognize 唤起系统截屏覆盖层（ms-screenclip:），接住框选图像识别，
// 成功后弹悬浮卡并按开关自动复制文字。业务失败以中文 error 返回
// （命令派发链路与前端按钮的提示系统会接住）；用户放弃选区静默返回 Cancelled。
func (s *OcrService) SnipAndRecognize() (SnipResult, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return SnipResult{}, gateErr
	}
	defer release()
	if !s.snipMu.TryLock() {
		return SnipResult{}, fmt.Errorf("截屏识别正在进行中，请稍候")
	}
	defer s.snipMu.Unlock()

	if err := s.ensureOnlineForSnip(); err != nil {
		return SnipResult{}, err
	}

	// 原生截屏以剪贴板交付：先快照文本 → 清空 → 等新图。覆盖层本就覆写
	// 剪贴板（方案固有代价），用户取消时把原文本回写还原。
	oldText, hadText := s.snip.SnapshotText()
	if err := s.snip.Empty(); err != nil {
		return SnipResult{}, err
	}
	if err := s.snip.InvokeOverlay(); err != nil {
		s.restoreSnipText(oldText, hadText)
		return SnipResult{}, fmt.Errorf("唤起系统截屏失败: %w", err)
	}
	pngBytes, found, err := snip.WaitFor(s.snip, snipWaitTimeout)
	if err != nil {
		s.restoreSnipText(oldText, hadText)
		return SnipResult{}, err
	}
	if !found {
		s.restoreSnipText(oldText, hadText) // 取消/超时：还原文本快照，静默退场
		return SnipResult{Cancelled: true}, nil
	}

	return s.finishSnipRecognition(pngBytes)
}

// RecognizeClipboardImage 剪贴板识图（默认热键 Ctrl+Alt+T / 轮盘/托盘命令
// ocr/snip-clipboard / 页面按钮共用）：剪贴板已有图 → 直接识别——不弹覆盖层、
// 不清写用户剪贴板（与 SnipAndRecognize 互补：那边"先截后识"，这边"先复制后识"，
// 贴 snipaste 工作流）。成功后悬浮卡 + 按开关自动复制。
// 歧义处理：GrabImage 只认图像格式，复制的文字/文件一律如实报"剪贴板中无图片"。
func (s *OcrService) RecognizeClipboardImage() (SnipResult, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return SnipResult{}, gateErr
	}
	defer release()
	if !s.snipMu.TryLock() {
		return SnipResult{}, fmt.Errorf("识别正在进行中，请稍候")
	}
	defer s.snipMu.Unlock()

	// 先取图再保服务在线：剪贴板没图是最常见失败，快速反馈不白拉引擎；
	// 极端持锁竞态下 GrabImage 报错也按"读不到图"给重按指引（openClipboard 已内置重试）。
	pngBytes, found, err := s.snip.GrabImage()
	if err != nil {
		return SnipResult{}, fmt.Errorf("读取剪贴板图像失败，请重试: %w", err)
	}
	if !found {
		return SnipResult{}, fmt.Errorf("剪贴板中没有图片：请先复制截图或图片（复制文字、文件不算）")
	}
	if err := s.ensureOnlineForSnip(); err != nil {
		return SnipResult{}, err
	}
	return s.finishSnipRecognition(pngBytes)
}

// finishSnipRecognition 识别公共收尾：落临时件 → 转发识别 → 按开关自动复制 →
// 悬浮卡。调用方须持有 snipMu（两条截屏入口共用）。
func (s *OcrService) finishSnipRecognition(pngBytes []byte) (SnipResult, error) {
	if err := os.MkdirAll(s.tmpDir, 0755); err != nil {
		return SnipResult{}, fmt.Errorf("创建临时目录失败: %w", err)
	}
	path := filepath.Join(s.tmpDir, fmt.Sprintf("snip-%d.png", time.Now().UnixNano()))
	if err := os.WriteFile(path, pngBytes, 0644); err != nil {
		return SnipResult{}, fmt.Errorf("写入截屏临时件失败: %w", err)
	}
	// 复用识别链路的 defer 记录点，来源标 snip（勿二处插历史落点）。
	out, err := s.recognizeImage(path, "snip")
	if err != nil {
		return SnipResult{}, err
	}
	if !out.Ok {
		return SnipResult{}, fmt.Errorf("识别失败: %s", out.Error)
	}
	res := SnipResult{Ok: true, Text: out.Text, LineCount: len(out.Lines), ElapsedMs: out.ElapsedMs}
	// 空文本也出卡：让用户确认"截到了但没字"而非链路故障（卡片按 text 空态展示）
	if s.store.GetAutoCopy() && res.Text != "" {
		res.Copied = s.snip.WriteText(res.Text) == nil
	}
	s.showSnipCard(res)
	return res, nil
}

func (s *OcrService) restoreSnipText(text string, had bool) {
	if had && strings.TrimSpace(text) != "" {
		_ = s.snip.WriteText(text)
	}
}

// ensureOnlineForSnip 截屏前保证上游服务在线：running/external 直接满足；
// starting 轮询等待（并发启动流）；stopped/failed 拉起托管（无组件给导入指引）。
func (s *OcrService) ensureOnlineForSnip() error {
	switch st := s.engine.Snapshot().State; st {
	case instance.StateRunning, instance.StateExternal:
		return nil
	case instance.StateStarting:
		deadline := time.Now().Add(snipStartWait)
		for time.Now().Before(deadline) {
			time.Sleep(150 * time.Millisecond)
			switch s.engine.Snapshot().State {
			case instance.StateRunning, instance.StateExternal:
				return nil
			case instance.StateFailed, instance.StateStopped:
				return fmt.Errorf("服务启动未能完成，请到文字识别页查看")
			}
		}
		return fmt.Errorf("服务启动超时，请到文字识别页重试")
	}
	exe, _, err := s.resolveActiveExe() // 拉起目标 = 活跃引擎解析结果（计划 §5.4）
	if err != nil {
		return fmt.Errorf("截屏识别需要 hanxi-ocr 组件：请先在文字识别页导入")
	}
	if err := s.engine.Start(instance.StartOptions{
		Exe: exe, ListenAddr: s.addr(),
		Detached: !s.store.GetFollowOnExit(),
	}); err != nil {
		if snap := s.engine.Snapshot(); snap.State == instance.StateExternal {
			return nil // 并发下外部实例抢位：同样可用
		}
		return err
	}
	s.refresh()
	return nil
}
