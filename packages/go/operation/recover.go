package operation

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"hanxi/internal/extapi"
)

// Compensation 是宿主注册的补偿分派器：Recover 把每笔未收口事务（含
// compensated）按 (operation, state, phase) 交给它；返回 handled=false 表示
// 没有登记能接管该组合的补偿器（§8.8 "不自动执行"），handled=true 表示已接管，
// 且应尽量当场 Complete 到终态（succeeded/failed/compensated 落账）。
// 补偿器不得执行任何未在 journal 中显式记账的副作用。
type Compensation func(j Journal) (handled bool, err error)

// Report 是一次启动恢复的分拣结果（§8.8）。四个桶互斥。
type Report struct {
	// Resumed：补偿器接管并收口为 succeeded（恢复推进完成）。
	Resumed []Journal
	// RolledBack：补偿器收口为 failed 或 compensated（补偿动作落账）。
	// compensated 仍留在 Pending 里，等待最终落账或重启后的续处理。
	RolledBack []Journal
	// Orphaned：无补偿器接管、补偿器报错、或处理后事务仍未收口（pending/running）
	// ——如实上报、绝不自动执行（§8.8 第 5 条），交宿主呈现给用户处置。
	Orphaned []Journal
	// Quarantined：解析失败/校验不过/transactionId 与文件名不符的 journal 文件。
	// 恢复器已将其改名隔离（*.journal-corrupt，不删除、不猜测内容），
	// 条目仅携带文件名线索与错误说明，其余字段保持零值。
	Quarantined []Journal
}

// snapshot 一次性返回未收口事务（已排序）与损坏文件清单（须由调用方独占写
// 路径：恢复先于业务，§8.8 第 6 条）。
func (s *Store) snapshot() ([]Journal, []string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pendingLocked()
}

// Recover 执行启动恢复扫描（§8.8）：
//
//  1. 扫账本：state ∈ {pending, running, compensated} 的未收口事务按
//     (createdAt, transactionId) 稳定顺序逐笔交给 compensate；已收口
//     （succeeded/failed）的事务是历史账，不进入分派；
//  2. 按补偿器落账后的最终 state 分拣进 Report（映射表见 Compensation 与
//     Report 各桶注释）；handled=false 或补偿器自身报错的一律 Orphaned，
//     绝不代为猜测或执行；
//  3. 无法解析/校验的 journal 文件改名隔离（*.journal-corrupt，保留原始
//     文件供人工取证），计入 Quarantined；
//  4. 磁盘现场（staging/.installing-*/.removing-*/.tmp-*）盘点由调用方另用
//     Store.AbandonedDirs 获取，receipt/active pointer 校验属 artifact 侧——
//     本包不越界处置资产目录。
//
// 纪律（§8.2/§8.8）：恢复发生在模块启动之前、业务调用开放之前。Recover 自身
// 对 Store 只做读取与经补偿器之手的收口写入，要求调用期无并发写事务。
func Recover(s *Store, compensate Compensation) (report Report, err error) {
	if s == nil {
		return report, errors.New("operation: Recover requires a store")
	}
	if compensate == nil {
		return report, errors.New("operation: Recover requires a compensation dispatcher")
	}
	live, corrupts, err := s.snapshot()
	if err != nil {
		return report, err
	}

	for _, path := range corrupts {
		report.Quarantined = append(report.Quarantined, *quarantine(path))
	}

	for _, j := range live {
		handled, herr := compensate(j)
		switch {
		case herr != nil:
			report.Orphaned = append(report.Orphaned, annotate(j, "compensation-failed",
				fmt.Sprintf("补偿器执行失败，事务保持原态待人工处置: %v", herr)))
		case !handled:
			report.Orphaned = append(report.Orphaned, annotate(j, "compensation-unhandled",
				fmt.Sprintf("无补偿器登记接管（operation=%s, state=%s, phase=%s），不自动执行", j.Operation, j.State, j.Phase)))
		default:
			after, gerr := s.Get(j.TransactionID)
			if gerr != nil {
				// 补偿器声称接管但账本读取失败：宁可上报孤立，不猜测结局。
				report.Orphaned = append(report.Orphaned, annotate(j, "compensation-unverifiable",
					fmt.Sprintf("补偿器已接管但无法复核落账: %v", gerr)))
				continue
			}
			switch after.State {
			case TxnSucceeded:
				report.Resumed = append(report.Resumed, after)
			case TxnFailed, TxnCompensated:
				report.RolledBack = append(report.RolledBack, after)
			default: // pending/running：处理后仍未收口
				report.Orphaned = append(report.Orphaned, annotate(after, "compensation-incomplete",
					fmt.Sprintf("补偿器接管后事务仍未收口（state=%s），本次恢复期内未闭环", after.State)))
			}
		}
	}
	return report, nil
}

// quarantine 把一个可疑的 journal 文件改名为 *.journal-corrupt 隔离并返回其
// 报告条目；改名失败（文件已被并发的恢复轮次移走则静默跳过，其余错误上报）
// 时仍返回条目（保留原路径线索），绝不删除任何字节。
func quarantine(path string) *Journal {
	name := filepath.Base(path)
	reason := inspectCorrupt(path)
	entry := &Journal{
		TransactionID: name,
		Error: &extapi.OperationError{
			Code:        "journal-corrupt",
			Message:     fmt.Sprintf("journal 文件无法作为事务账本解析（%s）: %s", reason, path),
			Recoverable: false,
		},
	}
	if _, statErr := os.Stat(path); statErr != nil {
		return entry // 已消失（前轮已隔离），保留报告线索
	}
	target := path + ".journal-corrupt"
	if _, err := os.Stat(target); err == nil {
		target = fmt.Sprintf("%s.%d", target, time.Now().UnixNano())
	}
	if err := os.Rename(path, target); err != nil && !errors.Is(err, os.ErrNotExist) {
		entry.Error.Message = fmt.Sprintf("%s; 隔离改名失败: %v", entry.Error.Message, err)
	}
	return entry
}

// inspectCorrupt 尽力说明损坏原因供取证：解析结果如何都不改写文件。
func inspectCorrupt(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return "不可读取: " + err.Error()
	}
	var j Journal
	if err := j.unmarshalStrict(data); err != nil {
		return "反序列化/键面校验失败: " + err.Error()
	}
	if err := j.validate(); err != nil {
		return "schema 校验失败: " + err.Error()
	}
	return "文件名与 transactionId 不符（拒绝按内容认领）"
}

// annotate 返回带补充错误说明的事务副本（仅用于 Report，不落盘、不改原账）。
func annotate(j Journal, code, message string) Journal {
	out := j
	out.Error = &extapi.OperationError{Code: code, Message: message, Recoverable: true}
	return out
}
