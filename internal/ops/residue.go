package ops

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"hanxi/packages/go/artifact"
	"hanxi/packages/go/operation"
)

// StalePartAge 文件面收尸的残件最小年龄：远大于任何模块的 Fetch 预算
// （现值 10-15 分钟），启动扫描只针对崩溃/强杀遗留，绝不与在途下载抢地盘。
const StalePartAge = 24 * time.Hour

// CleanTxnResidue 按事务"背书"清理托管版本树内属于该事务的现场目录：
// staging（.tmp-<txnID>）与隔离待删（.removing-<txnID>）两个精确同名目录。
//
// 纪律：只按 transactionId 派生的名字点对点清理，绝不按 .tmp-/.removing- 前缀
// 扫全根——versions 根是 markeron/rufus 等多模块共享目录，前缀扫根会误伤
// 其他事务/其他模块的合法现场。无背书孤儿（无未收口事务对应的前缀目录）
// 不在本函数职责内：如实 Report、不动盘（§8.8"不自动执行"）。
//
// tree 为 nil（模块无托管版本树）视为无需清理；txnID 必须过版本令牌白名单，
// 防经损坏账本的恶意名触发任意路径删除。删除走 os.RemoveAll：目录不存在
// 即成功，天然幂等（恢复重放安全）。
func CleanTxnResidue(tree *artifact.Tree, txnID string) error {
	if tree == nil {
		return nil
	}
	if err := artifact.ValidateVersionToken(txnID); err != nil {
		return fmt.Errorf("事务 ID 无效，拒绝清理: %w", err)
	}
	var errs []error
	for _, name := range []string{
		operation.DirStagingPrefix + txnID,
		operation.DirRemovingPrefix + txnID,
	} {
		if err := os.RemoveAll(filepath.Join(tree.Root, name)); err != nil {
			errs = append(errs, fmt.Errorf("清理事务现场 %s 失败: %w", name, err))
		}
	}
	return errors.Join(errs...)
}

// ListUnbackedTxnDirs 盘点版本树根下带事务前缀（.tmp-* / *<name>.removing-*）
// 的现场目录中，事务后缀不在给定背书集合（已知未收口事务 ID）内的名字——
// 仅供恢复期"如实上报、不动盘"的诊断；删除决策永远留给有账本背书的
// CleanTxnResidue 调用方。
//
// 注：Tree.Remove 的隔离目录形如 <版本目录>.removing-<纳秒>，其"事务后缀"
// 是进程内时间戳而非 journal 事务 ID，天然无背书——按约定只上报不动手，
// 是否收尸由宿主结合账本与模块策略另行决定。
func ListUnbackedTxnDirs(tree *artifact.Tree, backed map[string]bool) []string {
	if tree == nil {
		return nil
	}
	entries, err := os.ReadDir(tree.Root)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		txn := ""
		switch {
		case strings.HasPrefix(name, operation.DirStagingPrefix):
			txn = strings.TrimPrefix(name, operation.DirStagingPrefix)
		case strings.Contains(name, operation.DirRemovingPrefix):
			txn = name[strings.LastIndex(name, operation.DirRemovingPrefix)+len(operation.DirRemovingPrefix):]
		default:
			continue
		}
		if !backed[txn] {
			out = append(out, name)
		}
	}
	return out
}

// CleanStaleDownloadParts 启动恢复阶段的文件面收尸：对给定目录（installers/、
// versions/ 根）扫描一轮 artifact Fetch 强杀遗留的 `.part-<hex>` 临时件，
// 只删超龄（StalePartAge）普通文件，目录/符号链接拒删（语义见
// artifact.CleanStaleParts）。与 CleanTxnResidue 的目录面背书清理互补，
// 收口 ADR-0002 §4"文件面收尸"遗留项；删除清单落 slog 供追溯。
// 本清理不依赖 journal（账本降级为无账模式时照常执行），删除天然幂等。
func CleanStaleDownloadParts(dirs []string) []string {
	removed := artifact.CleanStaleParts(dirs, StalePartAge)
	if len(removed) > 0 {
		slog.Info("ops: 已清理 Fetch 强杀遗留的 .part-* 下载残件",
			"count", len(removed), "files", strings.Join(removed, "; "))
	}
	return removed
}
