package softver

// 目录大小扫描引擎：纯 WalkDir 聚合（不做并发分片——几十 GB 树的重 IO
// 已经贴着磁盘转，并行只添噪声），取消走 context、进度回调由调用方节流。
// 口径：目录自身按 4KB 量级计（Windows os.DirEntry 的 Info 不含目录实质大小），
// 文件计逻辑大小；符号链接/junction 不下探（Windows 上 junction 会以目录形态
// 出现且可能成环，宁少计不跑飞）；无权限条目计入 Skipped，结果偏小如实标注。

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
)

// scanStat 一次扫描的聚合读数。
type scanStat struct {
	Bytes   int64
	Files   int64
	Dirs    int64
	Skipped int64
}

// maxWalkDepth junction 成环的最后防线（正常业务树远浅于此）。
const maxWalkDepth = 48

// walkDirSize 统计 root 目录树的占用。onProgress 在每条目处理后被调用
// （调用方自行节流），返回的 current 为最近触及路径。取消时返回包装了
// context.Canceled 的错误，已扫描部分不缓存。
func walkDirSize(ctx context.Context, root string, onProgress func(stat scanStat, current string)) (scanStat, error) {
	var stat scanStat
	rootDepth := depthOf(root) // 深度限制的起算基准
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, werr error) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if werr != nil {
			if path == root {
				return werr // 根都进不去：硬失败，不装"扫完了"
			}
			stat.Skipped++ // 子树不可读（权限/占用）：跳过并记账，不中断整体
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil // 链接不跟随（防环）
		}
		if d.IsDir() {
			if depthOf(path)-rootDepth > maxWalkDepth {
				return filepath.SkipDir
			}
			stat.Dirs++
		} else {
			info, ierr := d.Info()
			if ierr != nil {
				stat.Skipped++
			} else {
				stat.Bytes += info.Size()
				stat.Files++
			}
		}
		if onProgress != nil {
			onProgress(stat, path)
		}
		return nil
	})
	// context.Canceled 原样上抛（调用方区分取消与真失败），其余错误透传。
	return stat, err
}

// depthOf 路径段数（以分隔符计，用于深度限制）。
func depthOf(p string) int {
	n := 0
	for i := 0; i < len(p); i++ {
		if p[i] == filepath.Separator || p[i] == '/' {
			n++
		}
	}
	return n
}
