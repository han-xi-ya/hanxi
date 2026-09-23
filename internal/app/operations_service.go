// operations_service.go 是 Wave 4-B 在途操作观察面的后端 RPC：
// 模块中心详情/首页最近任务/诊断视图经 ListOperations 消费共享内核
// operation.Hub 的统一载荷（extapi.Operation，Wave 0 冻结契约），
// 崩溃遗留的 resumable 事务经 DismissResumable 由用户显式忽略收口。
// 双样本接入：markeron / rufus 的托管资产安装事务（managed-declarative）。
package app

import (
	"fmt"
	"strings"

	"hanxi/internal/extapi"
	"hanxi/internal/ops"
	"hanxi/packages/go/artifact"
	"hanxi/packages/go/operation"
)

// listOperationsRecentLimit Recent 合并窗口：Active 全量之外，再并入最近 30 条
// （含 NewHub 回灌的 resumable 合成记录），防止长会话下终态记录无界增长。
const listOperationsRecentLimit = 30

// SetOperations 装配根注入操作观察面与 resumable 忽略通道（Wave 4-B）。
// hub 恒非 nil（纯内存降级由装配根保证）；dismiss 为装配根组装的背书收口函数。
func (s *AppService) SetOperations(hub *operation.Hub, dismiss func(txnID string) error) {
	s.opHub = hub
	s.opDismiss = dismiss
}

// ListOperations 返回在途与近期操作记录：Active（queued/running，按登记序）
// 与 Recent(30)（终态 + resumable 回灌，按 StartedAt 倒序）合并去重。
// 前端不依赖组件内存——刷新后从后端恢复全部现态（§2.3）。
func (s *AppService) ListOperations() []extapi.Operation {
	if s.opHub == nil {
		return nil
	}
	active := s.opHub.Active()
	out := make([]extapi.Operation, 0, len(active)+listOperationsRecentLimit)
	seen := make(map[string]bool, len(active)+listOperationsRecentLimit)
	for _, op := range active {
		out = append(out, op)
		seen[op.ID] = true
	}
	for _, op := range s.opHub.Recent("", listOperationsRecentLimit) {
		if seen[op.ID] {
			continue
		}
		out = append(out, op)
		seen[op.ID] = true
	}
	return out
}

// CancelOperation 用户取消一笔在途托管资产事务（N26）：按（模块, 事务 ID）
// 精确定位并只发取消信号——事务自身持 ctx（P0 批 2b），下载/解包链即时中断，
// 收口仍由模块 worker 的 2b 纪律完成（journal 以 operation-cancelled 落账、
// 后台租约归还、半截残件按既有背书清理）。本 RPC 不做任何"代为收口"的越权
// 动作：取消是信号，不是结果。ID 失配（页面快照过期）如实拒绝，不盲杀新事务。
func (s *AppService) CancelOperation(moduleID, txnID string) error {
	moduleID = strings.TrimSpace(moduleID)
	txnID = strings.TrimSpace(txnID)
	if moduleID == "" || txnID == "" {
		return fmt.Errorf("取消需要模块 ID 与事务 ID 同时在场")
	}
	return ops.CancelModuleTxn(moduleID, txnID)
}

// DismissResumable 用户显式忽略一笔未收口事务：按背书清理该事务的托管现场
// （staging/.removing-<txnID> 同名目录），journal 以 failed 收口，并从观察面
// 摘除 resumable 回灌记录。对已成功/已失败收口的账本拒绝翻案（审计单收口）。
func (s *AppService) DismissResumable(txnID string) error {
	if s.opDismiss == nil {
		return fmt.Errorf("操作账本未初始化，无法忽略残留事务")
	}
	txnID = strings.TrimSpace(txnID)
	if txnID == "" {
		return fmt.Errorf("事务 ID 不能为空")
	}
	return s.opDismiss(txnID)
}

// makeDismissResumable 组装忽略通道（装配根布线；RPC 入口见 DismissResumable）：
// store 缺失（账本打开失败的降级模式）时如实拒绝——降级期内本就没有可忽略的账。
func makeDismissResumable(store *operation.Store, hub *operation.Hub, versionTrees map[string]*artifact.Tree) func(txnID string) error {
	return func(txnID string) error {
		if store == nil {
			return fmt.Errorf("journal 账本未初始化，忽略操作不可用")
		}
		j, err := store.Get(txnID)
		if err != nil {
			return fmt.Errorf("操作记录不存在或不可读: %w", err)
		}
		// 背书清理：模块无托管版本树（如未来 builtin-logical 账）时自然 no-op
		if err := ops.CleanTxnResidue(versionTrees[j.ModuleID], txnID); err != nil {
			return err
		}
		if err := store.Complete(txnID, string(operation.TxnFailed), &extapi.OperationError{
			Code:        "resumable-dismissed",
			Message:     "用户确认忽略该未收口事务，事务现场已按背书清理",
			Recoverable: false,
		}); err != nil {
			return fmt.Errorf("收口失败（事务可能已收口或对已收口账拒绝翻案）: %w", err)
		}
		if hub != nil {
			hub.ForgetResumable(txnID)
		}
		ops.BroadcastChanged()
		return nil
	}
}

// JournalHealth journal 账本健康状态（P0 批 2a 观察面暴露）：降级即托管写
// 事务闸门关闭；前端据此挂"账本降级"横幅并置灰安装入口（批 3 接线）。
type JournalHealth struct {
	OK     bool   `json:"ok"`
	Reason string `json:"reason,omitempty"`
}

// GetJournalHealth 报告 journal 账本健康：degraded 原因如实透传（含处置指引——
// 重启 Hanxi 恢复账本；只读功能不受影响）。
func (s *AppService) GetJournalHealth() JournalHealth {
	if reason := ops.JournalDegraded(); reason != nil {
		return JournalHealth{OK: false, Reason: reason.Error() + "（重启 Hanxi 可恢复账本；只读功能不受影响）"}
	}
	return JournalHealth{OK: true}
}
