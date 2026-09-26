// Package dirstats 提供磁盘占用的只读统计公共件（W3：N11 数据根占用可视化 +
// N14 envcheck 本体/依赖家底共用，"新功能第一天走公共包"纪律的首个落点）。
//
// 设计约束（来自 N11/N14 登记时的痛点）：
//   - 巨型目录友好（Everything 索引、快照库、Go module cache 动辄百万文件）：
//     支持时间预算，超时截断为 Partial——Bytes 如实标记"下限估算"，绝不谎报全量；
//   - 环路/双计防御：符号链接与 Windows 重解析点（junction 在 Go 的 ReadDir
//     里同报 ModeSymlink）恒不计数、恒不进入；
//   - 坏文件不炸场：单条目 stat 失败计数跳过（errorCount），walk 继续；
//     根不存在/不可读经 Err 如实上报（调用方决定展示语义）；
//   - 兄弟子目录并发度量（MeasureChildren）：共享同一绝对截止时间，预算耗尽
//     的后续子目录快速短路为 Partial，不排队烧 CPU。
//
// 纯标准库零依赖、无平台代码，Linux/容器可直接单测；Windows 真机行为差异
// （reparse 点覆盖度）列为验收项。
package dirstats

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// 缺省参数（单测可经 Options 覆写）。
const (
	defaultCheckEvery = 256 // 每 N 条目查一次预算/取消（百万文件下 ctx.Err 摊销）
	defaultWorkers    = 8   // MeasureChildren 并发子目录上限
)

// Options 统计行为参数。零值 = 全部缺省（无时限、并发池 8）。
type Options struct {
	// TimeBudget 挂钟上限；0 = 不限。超限截断并将 Partial 置真。
	TimeBudget time.Duration
	// CheckEvery 预算检查的条目间隔；0 = 缺省 256。
	CheckEvery int
	// MaxWorkers MeasureChildren 的并发上限；0 = 缺省 8。
	// 注：符号链接/重解析点恒不进入（filepath.WalkDir 原生不跟随，无开关）。
	MaxWorkers int
}

func (o Options) checkEvery() int {
	if o.CheckEvery > 0 {
		return o.CheckEvery
	}
	return defaultCheckEvery
}

func (o Options) workers() int {
	if o.MaxWorkers > 0 {
		return o.MaxWorkers
	}
	return defaultWorkers
}

// Stats 一次目录度量的结果。Partial/ErrorCount 是诚实性字段：截断与坏条目
// 必须透出给 UI（"≥ 此值"话术），不得静默冒充精确全量。
type Stats struct {
	Bytes      int64 `json:"bytes"`
	Files      int64 `json:"files"`
	Dirs       int64 `json:"dirs"`
	ErrorCount int64 `json:"errorCount"` // 读取元信息失败的条目数（已跳过）
	Partial    bool  `json:"partial"`    // 预算/取消截断：Bytes 为下限估算
	// Err 根级失败（不存在/不可读/非目录）；此时计数字段无意义。
	Err error `json:"-"`
}

// errAbortWalk 预算耗尽的内部哨兵：经 WalkDir 错误链上抛以中止遍历。
var errAbortWalk = errors.New("dirstats: budget exhausted")

// Measure 度量 root 目录的占用（含 root 自身计 1 个 Dir）。不 panic；
// root 不存在返回 Stats{Err: fs.ErrNotExist 包装}。
func Measure(root string, opt Options) Stats {
	return measure(context.Background(), root, opt)
}

// MeasureWith 带外部 ctx 的单目录度量：供"多目录共享一份总预算"的编排方
// （如 envcheck 空间家底：一次 RPC 扫多个缓存目录，全场限时而非每目录限时）。
func MeasureWith(ctx context.Context, root string, opt Options) Stats {
	return measure(ctx, root, opt)
}

// MeasureBudgeted 带挂钟预算的单目录度量：预算耗尽即截断并置 Partial
// （Bytes 为下限估算，由调用方决定呈现语义）。budget <= 0 等同不限时。
func MeasureBudgeted(root string, budget time.Duration) Stats {
	if budget <= 0 {
		return measure(context.Background(), root, Options{})
	}
	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()
	return measure(ctx, root, Options{})
}

// measure 带 ctx 的内部实现（MeasureChildren 的共享截止时间经 ctx 下发）。
func measure(ctx context.Context, root string, opt Options) Stats {
	var s Stats
	var st os.FileInfo
	var err error
	if st, err = os.Stat(root); err != nil {
		s.Err = err
		return s
	}
	if !st.IsDir() {
		s.Err = &fs.PathError{Op: "measure", Path: root, Err: errors.New("不是目录")}
		return s
	}
	var visited int
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			s.ErrorCount++ // 目录不可读等局部损伤：计数继续，不中断全场
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil // 链接/重解析点不计不追（WalkDir 原生不跟随目录链接）
		}
		if d.IsDir() {
			s.Dirs++
		} else {
			info, ierr := d.Info()
			if ierr != nil {
				s.ErrorCount++
				return nil
			}
			s.Files++
			s.Bytes += info.Size()
		}
		visited++
		if visited%opt.checkEvery() == 0 {
			select {
			case <-ctx.Done():
				s.Partial = true
				return errAbortWalk
			default:
			}
		}
		return nil
	})
	if walkErr != nil && !errors.Is(walkErr, errAbortWalk) {
		s.Err = walkErr // 非预算类错误（理论罕见）如实上报
	}
	return s
}

// Child 一个一级子项的度量结果（文件项 Dirs 恒 0、IsDir 假）。
type Child struct {
	Name  string `json:"name"`
	IsDir bool   `json:"isDir"`
	Stats
}

// MeasureChildren 列出 root 的一级子项并逐个度量，按 Bytes 降序（同值按名）。
// TimeBudget 为整场共享：截止时刻一次定死，预算耗尽后未开始/进行中的子项
// 标 Partial（Bytes 下限），列表仍完整返回——"每个便携软件占多大"的
// 一屏总览不因个别巨目录而整页失踪。
func MeasureChildren(root string, opt Options) ([]Child, error) {
	ctx := context.Background()
	if opt.TimeBudget > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opt.TimeBudget)
		defer cancel()
	}
	return measureChildren(ctx, root, opt)
}

// measureChildren ctx 版内部实现（预算共享与"预取消"路径均可被单测确定驱动）。
func measureChildren(ctx context.Context, root string, opt Options) ([]Child, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	children := make([]Child, 0, len(entries))
	for _, e := range entries {
		isLink := e.Type()&os.ModeSymlink != 0
		c := Child{Name: e.Name(), IsDir: e.IsDir() && !isLink}
		switch {
		case c.IsDir: // 目录交下方并发度量
		case isLink: // 顶层符号链接/重解析点：跳过计数，仅保留名目（Partial 语义不适用）
		default:
			if info, ierr := e.Info(); ierr == nil {
				c.Stats = Stats{Files: 1, Bytes: info.Size()}
			} else {
				c.Stats = Stats{ErrorCount: 1}
			}
		}
		children = append(children, c)
	}

	// 并发度量目录子项：worker 池限并发，坏目录经 Stats.Err 局部降级。
	var idx sync.WaitGroup
	sem := make(chan struct{}, opt.workers())
	for i := range children {
		if !children[i].IsDir {
			continue
		}
		select {
		case <-ctx.Done(): // 预算已尽：剩余目录直接 Partial 短路
			children[i].Stats = Stats{Partial: true, Dirs: 1}
			continue
		case sem <- struct{}{}:
		}
		idx.Add(1)
		go func(i int) {
			defer idx.Done()
			defer func() { <-sem }()
			children[i].Stats = measure(ctx, filepath.Join(root, children[i].Name), opt)
		}(i)
	}
	idx.Wait()

	sort.Slice(children, func(a, b int) bool {
		if children[a].Bytes != children[b].Bytes {
			return children[a].Bytes > children[b].Bytes
		}
		return children[a].Name < children[b].Name
	})
	return children, nil
}
