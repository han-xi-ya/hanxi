package memo

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// memo 库迁移：state/memo.json 整库 → memo/<id>.md 每条一文件（PLAN_SNAPSHOT §3.4）。
// 判据幂等（仓内无 schema 版本号先例，走 ocr 双写文化同款"判据幂等"）：
//
//	需要迁移 ⇔ 旧库 memo.json 存在 && memo/ 下没有已提交的分条文件
//
// 流程与崩溃安全边界：
//  1. 逐条写暂存目录 `<数据根>/.memo-migrating-<pid>/`（与目标同卷，rename 免拷贝；
//     暂存前缀带点且在白名单外，快照/巡检都看不见它）；
//  2. **改名 memo.json → memo.json.migrated 即提交点**：此前任何失败，旧库原样在位，
//     重跑一次即再来；此后崩溃由"续跑分支"把暂存文件搬完；
//  3. 暂存文件逐条 rename 进 memo/（单文件 rename 原子；重入时同名即收敛，幂等）。
//
// 分叉防御：memo.json 与已提交 memo/*.md 并存（只可能来自手工回滚快照等异常）时
// 告警并以文件库为权威，绝不自动二选一合并。

// migratedSuffix 提交点留底后缀（快照白名单排除该文件；Q6 两个版本周期后删旧链）。
const migratedSuffix = ".migrated"

// stagingPrefix 暂存目录前缀（数据根下、白名单外；启动清扫孤儿暂存）。
const stagingPrefix = ".memo-migrating-"

// corruptInfix 旧库损坏隔离副本 infix（<legacyPath>.corrupt-<时间戳>，取证用；
// ClearAll 全删时连同销毁——副本里就是旧便签正文）。
const corruptInfix = ".corrupt-"

// memoNeedsMigration 迁移判据（纯函数，供单测直接喂路径）。
func memoNeedsMigration(legacyPath, memoDir string) (bool, error) {
	if _, err := os.Stat(legacyPath); err != nil {
		if os.IsNotExist(err) {
			return false, nil // 无旧库：从未有数据或已迁移完成
		}
		return false, err
	}
	hasCommitted, err := memoDirHasFiles(memoDir)
	if err != nil {
		return false, err
	}
	if hasCommitted {
		slog.Error("memo: 旧 memo.json 与已提交文件库并存（快照手工回滚？），以文件库为权威，旧库原地保留")
		return false, nil
	}
	return true, nil
}

// memoDirHasFiles memo/ 下是否存在已提交的分条文件（只认直接子层 *.md，
// 暂存目录/杂项不算）。
func memoDirHasFiles(memoDir string) (bool, error) {
	entries, err := os.ReadDir(memoDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") && memoFileNameRe.MatchString(e.Name()) {
			return true, nil
		}
	}
	return false, nil
}

// migrateMemoToFiles 执行一次迁移（读取旧库走真实 Store loader）。
func migrateMemoToFiles(legacyPath, memoDir string) (committed bool, err error) {
	return migrateMemoWithLoader(legacyPath, memoDir, func() ([]MemoItem, error) {
		st, serr := NewStore(legacyPath)
		if serr != nil {
			return nil, serr
		}
		return st.Load()
	})
}

// migrateMemoWithLoader 迁移主体（loader 注入便于单测喂各种旧库形态）。
// 返回 committed=true 当且仅当提交点已过；committed=false 时旧库必然原样在位。
func migrateMemoWithLoader(legacyPath, memoDir string, load func() ([]MemoItem, error)) (committed bool, err error) {
	// 数据根 = memoDir 的上级；暂存与目标同卷（rename 免拷贝）
	staging := filepath.Join(filepath.Dir(memoDir), stagingPrefix+fmt.Sprint(os.Getpid()))

	// —— 续跑分支：提交点已过（旧库已改名）但暂存未搬完（上次崩在第 3 步）——
	if _, serr := os.Stat(legacyPath); errors.Is(serr, fs.ErrNotExist) {
		if _, derr := os.Stat(staging); derr == nil {
			return true, finishStagingMove(staging, memoDir)
		}
		return false, nil // 无旧库亦无半程：无事可做（绝不让 loader 重建空库）
	}

	items, lerr := load()
	if lerr != nil {
		return false, fmt.Errorf("读取旧便签库失败，迁移中止（旧库原样保留）: %w", lerr)
	}
	if len(items) == 0 {
		// 空库无逐条价值，但提交点仍要走（否则永远判定"待迁移"反复空转）
		if err := os.MkdirAll(memoDir, 0755); err != nil {
			return false, err
		}
		return commitLegacy(legacyPath)
	}

	if err := os.MkdirAll(staging, 0755); err != nil {
		return false, err
	}
	defer func() {
		if !committed {
			_ = os.RemoveAll(staging) // 提交点前失败：清暂存，旧库未动即原样回退
		}
	}()
	for _, it := range items {
		data, eerr := EncodeMemo(it)
		if eerr != nil {
			return false, eerr
		}
		if werr := writeFileAtomic(filepath.Join(staging, it.ID+".md"), data); werr != nil {
			return false, werr
		}
	}

	committed, err = commitLegacy(legacyPath)
	if err != nil {
		return false, err
	}
	if derr := finishStagingMove(staging, memoDir); derr != nil {
		// 提交点已过：暂存仍在位，下次启动续跑分支收口，这里只报失败事实
		return true, derr
	}
	return true, nil
}

// commitLegacy 提交点：memo.json → memo.json.migrated（旧文件保留可人工回退）。
func commitLegacy(legacyPath string) (bool, error) {
	migrated := legacyPath + migratedSuffix
	if _, err := os.Stat(migrated); err == nil {
		// 留底已存在（极端：手工恢复 memo.json 又撞迁移）：新留底带时间戳不覆盖
		migrated = fmt.Sprintf("%s%s-%s", legacyPath, migratedSuffix, time.Now().Format("20060102-150405"))
	}
	if err := os.Rename(legacyPath, migrated); err != nil {
		return false, fmt.Errorf("迁移提交点改名失败（旧库未动）: %w", err)
	}
	return true, nil
}

// finishStagingMove 把暂存目录逐条搬进 memo/ 并收掉暂存（幂等可重入）。
func finishStagingMove(staging, memoDir string) error {
	if err := os.MkdirAll(memoDir, 0755); err != nil {
		return err
	}
	entries, err := os.ReadDir(staging)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // 暂存已不在：视为已完成
		}
		return err
	}
	var failed []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		src := filepath.Join(staging, e.Name())
		dst := filepath.Join(memoDir, e.Name())
		if rerr := os.Rename(src, dst); rerr != nil {
			if errors.Is(rerr, fs.ErrExist) {
				// 目标同名：重入场景，内容同源，删暂存副本即收敛
				if os.Remove(src) != nil {
					failed = append(failed, e.Name())
				}
				continue
			}
			failed = append(failed, e.Name())
			continue
		}
	}
	if len(failed) > 0 {
		return fmt.Errorf("部分便签搬运失败（暂存保留待续跑）: %s", strings.Join(failed, ", "))
	}
	return os.Remove(staging)
}

// sweepStaleStaging 启动清扫遗留暂存目录（非本进程 pid）：
// 旧库仍在 → 暂存是失败残骸，删除；旧库已改名 → 暂存是待续跑半程，搬完。
func sweepStaleStaging(dataDir, memoDir, legacyPath string) {
	entries, err := os.ReadDir(dataDir)
	if err != nil {
		return
	}
	_, lerr := os.Stat(legacyPath)
	legacyGone := errors.Is(lerr, fs.ErrNotExist)
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), stagingPrefix) {
			continue
		}
		if strings.TrimPrefix(e.Name(), stagingPrefix) == fmt.Sprint(os.Getpid()) {
			continue // 本进程暂存由迁移主流程自管
		}
		staging := filepath.Join(dataDir, e.Name())
		if legacyGone {
			slog.Warn("memo: 续跑上次中断的便签迁移暂存", "staging", staging)
			if ferr := finishStagingMove(staging, memoDir); ferr != nil {
				slog.Error("memo: 暂存续跑失败（旧库已改名，未搬运条目在暂存目录仍在位）", "err", ferr)
			}
		} else {
			slog.Info("memo: 清扫上次迁移失败遗留的暂存目录", "staging", staging)
			_ = os.RemoveAll(staging)
		}
	}
}
