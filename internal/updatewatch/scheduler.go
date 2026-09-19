// Package updatewatch 是宿主级"可用更新"感知调度器（Wave 4+ 健康维度收口）。
//
// 职责边界：本包只做"调度 + 裁决写入"——遍历实现了 extapi.UpdateChecker 的
// 托管模块，逐个廉价比较（本地账目 + 带缓存远程列表，实现约束见契约注释），
// 依据结果写 registry.SetHealth("update-available"|"current")——update-available
// 时附带上游新版本号（投影展示"→ 新版"，不参与状态机裁决），并广播
// updates:checked 事件提示前端重拉 ListModuleStates 投影。前端零新 RPC、
// 不持久化第二份真相（ADR-0001 投影唯一权威源纪律）。
//
// 调度策略（工具箱"按需"习惯）：
//   - 启动后一次性延迟首检（默认 60s，让路启动风暴），不建常驻 ticker——
//     远程列表各有 10 分钟 TTL 缓存，周期性重查的收益低于常驻 IO 与
//     GitHub 匿名限流（60 次/小时/IP）的配额成本；用户主动刷新经
//     AppService.RefreshUpdates RPC 触发，随开随查；
//   - 全轮并发 ≤3（镜像回退链最坏单模块 ~48s，避免同时压满出口带宽与
//     GitHub API），全局超时兜底（默认 5 分钟），单模块超时/失败静默：
//     保持该模块原健康值不清除、不谎报（失败≠无更新）；
//   - 并发触发合并：同轮 in-flight 时第二个 CheckNow 加入等待同一结果
//     （single-flight 语义），不重复起网络请求。
package updatewatch

import (
	"context"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hanxi/internal/extapi"
)

// 调度参数默认值（装配根 New 生效；测试可覆盖）。
const (
	defaultStartDelay  = 60 * time.Second  // 启动后首检延迟：让路启动链
	defaultSweepBudget = 5 * time.Minute   // 单轮全局超时：镜像回退链最坏情况的收口闸门
	defaultCheckBudget = 60 * time.Second  // 单模块超时：4 镜像 ×12s HTTP 上限的余量
	maxConcurrency     = 3                 // 并发上限（任务口径：并发≤3）
	EventChecked       = "updates:checked" // 一轮感知结束（Void），前端据此重拉状态投影
)

// Scheduler 更新感知调度器：注册表 + 模块检查器集合 + 结果缓存（state 目录）。
type Scheduler struct {
	registry *extapi.Registry
	checkers map[string]extapi.UpdateChecker

	cache *cache

	startDelay  time.Duration
	sweepBudget time.Duration
	checkBudget time.Duration
	concurrency int

	stopOnce sync.Once
	stopCh   chan struct{}

	mu      sync.Mutex
	running *round // in-flight 合并：并发 CheckNow 共享同一轮结果
}

// round 单轮感知：done 关闭后 checked 可读（同轮 join 者等待该通道）。
type round struct {
	done    chan struct{}
	checked int
}

// New 构造调度器。checkers 为装配根从注册模块中类型断言收集的实现集合
// （键 = 模块 ID）；stateDir 为持久化结果缓存目录（重启后首帧回灌，
// 见 cache.go），空串 = 不缓存。零 IO，可安全在装配早期构造。
func New(registry *extapi.Registry, checkers map[string]extapi.UpdateChecker, stateDir string) *Scheduler {
	return &Scheduler{
		registry:    registry,
		checkers:    checkers,
		cache:       newCache(stateDir),
		startDelay:  defaultStartDelay,
		sweepBudget: defaultSweepBudget,
		checkBudget: defaultCheckBudget,
		concurrency: maxConcurrency,
		stopCh:      make(chan struct{}),
	}
}

// Restore 应用启动时（装配根、wails 运行前）把上次缓存的 update-available
// 回灌 registry，供前端首帧投影即时点亮；无任何其他副作用。返回回灌条数。
func (s *Scheduler) Restore() int {
	applied := 0
	for id, remote := range s.cache.load().AvailableSet() {
		// 缓存里带的版本号一并回灌（旧格式迁移或检查时未拿到版本时为空串，
		// 仅点亮 update-available，不谎报版本）。
		s.registry.SetHealth(id, extapi.HealthUpdateAvailable, remote)
		applied++
	}
	if applied > 0 {
		slog.Info("updatewatch: 已按缓存回灌可用更新标记", "modules", applied)
	}
	return applied
}

// Start 挂出一次性延迟首检 goroutine：stopCh 关闭则放弃；不注册任何周期任务。
// 装配根在应用启动路径调用一次即可。
func (s *Scheduler) Start() {
	if len(s.checkers) == 0 {
		return
	}
	go func() {
		timer := time.NewTimer(s.startDelay)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-s.stopCh:
			return
		}
		if _, err := s.CheckNow(context.Background()); err != nil {
			slog.Info("updatewatch: 启动首检未完整收口", "err", err)
		}
	}()
}

// Stop 取消尚未触发的启动首检（装配根退出链调用）。已在途的轮次由其自身
// 预算收口，不无限等待——检查均为读路径廉价操作，残留无资源风险。
func (s *Scheduler) Stop() {
	s.stopOnce.Do(func() { close(s.stopCh) })
}

// CheckNow 执行（或加入）一轮完整感知，阻塞至轮次收口，返回本轮成功判定
// 的模块数。join 者拿到与发起者相同的计数；ctx 取消只结束等待，不中止
// 已在途的网络链（与实现方"可并发重入"契约一致）。
func (s *Scheduler) CheckNow(ctx context.Context) (int, error) {
	s.mu.Lock()
	if s.running != nil {
		r := s.running
		s.mu.Unlock()
		select {
		case <-r.done:
			return r.checked, nil
		case <-ctx.Done():
			return 0, ctx.Err()
		}
	}
	r := &round{done: make(chan struct{})}
	s.running = r
	s.mu.Unlock()

	checked := s.sweep(r)
	s.mu.Lock()
	s.running = nil
	s.mu.Unlock()
	close(r.done)
	return checked, nil
}

// sweep 单轮感知主体：并发受限 + 全局超时 + 逐模块健康裁决 + 结果缓存 + 事件广播。
func (s *Scheduler) sweep(r *round) int {
	ctx, cancel := context.WithTimeout(context.Background(), s.sweepBudget)
	defer cancel()

	ids := make([]string, 0, len(s.checkers))
	for id := range s.checkers {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		checked int
		result  = map[string]verdict{} // moduleID → 本轮结论（仅成功判定的模块入账）
	)
	sem := make(chan struct{}, s.concurrency)
	for _, id := range ids {
		checker := s.checkers[id]
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done(): // 全局超时：排队者即刻收手，不发起新比较
				return
			}
			local, remote, hasUpdate, err := s.checkOne(ctx, id, checker)
			if err != nil {
				// 失败静默：保持原健康值（既有 update-available 不被清除，
				// 网络不可达绝不降级成"无更新"谎报）。
				slog.Info("updatewatch: 模块更新感知失败，保持原健康值", "module", id, "err", err)
				return
			}
			health := extapi.HealthCurrent
			if hasUpdate {
				health = extapi.HealthUpdateAvailable
			}
			// remote 只在 update-available 时被 Registry 记录（展示用上游
			// 新版本）；current 判定连带清掉旧版本残留。
			s.registry.SetHealth(id, health, remote)
			mu.Lock()
			checked++
			result[id] = verdict{hasUpdate: hasUpdate, remote: remote}
			if hasUpdate {
				slog.Info("updatewatch: 检测到可用更新", "module", id, "local", local, "remote", remote)
			}
			mu.Unlock()
		}()
	}
	wg.Wait()

	s.cache.merge(result)
	if app := application.Get(); app != nil && app.Event != nil {
		app.Event.Emit(EventChecked)
	}
	r.checked = checked
	slog.Info("updatewatch: 一轮可用更新感知收口", "checked", checked, "total", len(ids))
	return checked
}

// outcome 单模块比较结果（checkOne 的 goroutine 回传载荷）。
type outcome struct {
	local, remote string
	hasUpdate     bool
	err           error
}

// checkOne 以单模块预算执行比较：ListRemote 链不接受 ctx，超时即放弃等待
// （在途 HTTP 由客户端自身 12s 逐级限时，自然收口，无 goroutine 长期滞留）。
func (s *Scheduler) checkOne(ctx context.Context, id string, checker extapi.UpdateChecker) (string, string, bool, error) {
	cctx, cancel := context.WithTimeout(ctx, s.checkBudget)
	defer cancel()
	ch := make(chan outcome, 1)
	go func() {
		local, remote, hasUpdate, err := checker.CheckUpdate(cctx)
		ch <- outcome{local: local, remote: remote, hasUpdate: hasUpdate, err: err}
	}()
	select {
	case o := <-ch:
		return o.local, o.remote, o.hasUpdate, o.err
	case <-cctx.Done():
		return "", "", false, cctx.Err()
	}
}
