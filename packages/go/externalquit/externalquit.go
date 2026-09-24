// Package externalquit 实现"外部实例（非本会话启动进程）退出"的分档执行器
// （N3 终裁 2026-09-21，见 docs/plans/PLAN_W1_DECISIONS §1）：
//
// 统一动作序列——优雅信号（若模块有官方通道）→ 轮询观察宽限期 → 身份复核
// （防 PID 复用）→ 按档处置：
//   - PolicyForceFree（低损档）：优雅不生效即直接强杀，不打扰用户
//     （适用 snipaste/everything 这类"死了秒恢复"的工具）；
//   - PolicyConfirmForce（打扰档，新托管未声明时的默认档）：强杀前必须经
//     Confirm 回调向用户明示风险并获同意，拒绝即回指引——绝不闷头杀。
//
// 强杀原语复用 platform.ProcessAPI.KillVerified（红线检查 + 令牌复核 +
// PID 复用防护 + UIPI 权限失败显式化），本包不做任何"绕过复核"的快捷路径。
// 与 supervisor 内核的分工：内核维持"外部实例绝不由引擎强杀"的保守边界
// （Stop 返回 ErrExternal），分档政策是模块服务层的显式决策，经本包执行——
// 两者不冲突：内核管自家进程树，本包管"越权但有裁决"的外部通道。
//
// 平台差异经由注入的 ProcessAPI 消化，本包零平台代码、零框架依赖，
// Linux/容器可直接单测。
package externalquit

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"time"

	"hanxi/internal/platform"
)

// Policy 外部实例退出档位（N3 终裁分档）。
type Policy string

const (
	// PolicyForceFree 低损档：优雅信号不生效即直接强杀，无需用户确认。
	PolicyForceFree Policy = "force-free"
	// PolicyConfirmForce 打扰档：强杀前必须取得用户明确同意（Confirm 回调）。
	PolicyConfirmForce Policy = "confirm-force"
)

// Method 执行结果归因。
const (
	MethodGraceful    = "graceful-request" // 优雅信号在宽限期内生效
	MethodAlreadyGone = "already-exited"   // 动手前进程已消失（含复核窗口竞态）
	MethodForced      = "forced"           // 强杀成功（含 confirm 后执行）
	MethodDeclined    = "declined-confirm" // 打扰档用户拒绝（或确认通道缺失）
	MethodBlocked     = "blocked-elevated" // 权限/红线拦截（典型：目标以管理员运行被 UIPI 挡）
	MethodOwnership   = "ownership-lost"   // 身份复核不匹配，拒杀上报（防 PID 复用误伤）
	// 引擎层守卫归因（QuitExternalOf 前置检查产物，与 Method* 同处一表供模块映射文案）：
	MethodNotExternal     = "not-external"      // 快照已非 external 态（并发撤场），不动手
	MethodProbeMissingPID = "probe-missing-pid" // 探针未取得实例身份，拒执行（防把"没身份"谎报成"已退出"）
)

// ExternalQuitRequest 引擎层一次外部退出所需的全部事实（N3 收尾：四份逐字
// QuitExternal 克隆的守卫+组装序列收编于此，模块只留锁、快照取数与结果映射）。
type ExternalQuitRequest struct {
	// External：当前引擎快照是否 external 态——false 即拒（内核绝不由此通道动自家进程）。
	External bool
	// PID/ExePath/StartedAt：探针报告的实例身份（PID=0 视为身份不全，拒执行）。
	PID       uint32
	ExePath   string
	StartedAt time.Time
	// 分档三件套（语义同 Quit 入参）。
	Policy  Policy
	Risk    string
	Confirm func(risk string) bool
	// Proc 同 Deps.Proc。
	Proc platform.ProcessAPI
	// Graceful 优雅通道（典型 WM_CLOSE 按 PID 投递）：nil = 无优雅信号。
	// 以 pid 入参传递身份，回调内禁止回读引擎可变态（快照取数留在调用方）。
	Graceful func(ctx context.Context, pid uint32) error
	// Grace 观察宽限（<=0 走 Deps 缺省 2.5s）。
	Grace time.Duration
}

// QuitExternalOf 引擎层外部退出统一入口：守卫（非 external / 身份不全）→
// token 组装 → 委托 Quit。守卫归因用 MethodNotExternal / MethodProbeMissingPID，
// 调用方（service）据此映射指引文案。
func QuitExternalOf(ctx context.Context, req ExternalQuitRequest) (Result, error) {
	if !req.External {
		return Result{Method: MethodNotExternal}, nil
	}
	if req.PID == 0 {
		return Result{Method: MethodProbeMissingPID}, nil
	}
	token := platform.VerifyToken{PID: req.PID, ExePath: req.ExePath, StartedAt: req.StartedAt}
	deps := Deps{Proc: req.Proc, Risk: req.Risk, Confirm: req.Confirm, Grace: req.Grace}
	if req.Graceful != nil {
		graceful := req.Graceful
		deps.Graceful = func(c context.Context) error { return graceful(c, req.PID) }
	}
	return Quit(ctx, token, req.Policy, deps)
}

// Deps 执行器依赖（全部由模块服务层注入）。
type Deps struct {
	// Proc 必注：进程查询与安全查杀通道。
	Proc platform.ProcessAPI
	// Graceful 模块的官方优雅退出通道（命令行 -quit 转发 / 信使 / WM_CLOSE
	// 等尽力而为的信号）。nil = 该外部实例无优雅通道，直接按档处置。
	// 返回错误不终止流程（信号尽力而为，成败以"进程是否消失"为准）。
	Graceful func(ctx context.Context) error
	// Grace 优雅信号后的观察宽限（缺省兜底 2.5s，与 snipaste closeGracePeriod 同口径）。
	Grace time.Duration
	// PollInterval 宽限期内探测进程消失的轮询间隔（缺省 100ms）。
	PollInterval time.Duration
	// Risk 打扰档确认弹窗的风险文案（如"正在向 U 盘写入"）。
	Risk string
	// Confirm 打扰档用户确认回调：true=同意强杀。nil 一律按拒绝处理
	// （宁可退回指引，不可未经同意越权）。force-free 档不调用本回调。
	Confirm func(risk string) bool
	// Now/Sleep 时钟注入位（单测压缩等待窗口；nil 用真实时钟）。
	Now   func() time.Time
	Sleep func(d time.Duration) <-chan time.Time
}

// Result 一次外部退出执行的结果。
type Result struct {
	Stopped bool   // 目标进程是否已终止（含优雅生效与强杀成功）
	Forced  bool   // 是否走了强杀通道
	Method  string // Method* 常量归因
}

// 执行器缺省值。
const (
	defaultGrace         = 2500 * time.Millisecond
	defaultPollInterval  = 100 * time.Millisecond
	gracefulSignalBudget = 2 * time.Second // Graceful 回调自身的超时上限
)

// Quit 按 N3 终裁序列执行一次外部实例退出。token 来自探针报告的
// PID/ExePath/StartedAt（调用方构造，本包信任其来源）。
// error 仅在"需要用户进一步知情"时非空（ownership-lost 拒杀、系统级查询
// 失败）；declined/blocked 属正常业务结果，Result.Method 归因、error 为 nil。
func Quit(ctx context.Context, token platform.VerifyToken, policy Policy, deps Deps) (Result, error) {
	if deps.Proc == nil {
		return Result{}, errors.New("externalquit: Deps.Proc 未注入")
	}
	grace := deps.Grace
	if grace <= 0 {
		grace = defaultGrace
	}
	interval := deps.PollInterval
	if interval <= 0 {
		interval = defaultPollInterval
	}

	// 第一拍：优雅信号尽力而为——投递成功与否都不下结论，只认"进程消失"。
	if deps.Graceful != nil {
		sctx, cancel := context.WithTimeout(ctx, gracefulSignalBudget)
		_ = deps.Graceful(sctx)
		cancel()
		if gone := waitGone(ctx, deps, token.PID, grace, interval); gone {
			return Result{Stopped: true, Method: MethodGraceful}, nil
		}
	}

	// 第二拍：动手前身份复核。查询失败按"已消失"处理（与内核 killSequence
	// 同口径）；查到了但身份不符，宁可拒杀上报也绝不误伤复用同 PID 的陌生进程。
	current, err := deps.Proc.Query(token.PID)
	if err != nil {
		return Result{Stopped: true, Method: MethodAlreadyGone}, nil
	}
	if tokenMismatch(token, current) {
		return Result{Method: MethodOwnership},
			errors.New("外部实例身份复核失败（PID 可能已被复用），已拒绝终止以避免误伤其他进程")
	}

	// 第三拍：按档处置。
	if policy == PolicyConfirmForce {
		if deps.Confirm == nil || !deps.Confirm(deps.Risk) {
			return Result{Method: MethodDeclined}, nil
		}
	}
	if err := deps.Proc.KillVerified(ctx, token, true); err != nil {
		switch {
		case errors.Is(err, platform.ErrProcessNotFound):
			return Result{Stopped: true, Method: MethodAlreadyGone}, nil
		case errors.Is(err, platform.ErrAccessDenied), errors.Is(err, platform.ErrProtectedProcess):
			// UIPI/红线拦截：能力边界如实上报，由模块层降级为"仅指引"。
			return Result{Stopped: false, Method: MethodBlocked}, nil
		case errors.Is(err, platform.ErrTokenMismatch):
			return Result{Method: MethodOwnership},
				errors.New("外部实例身份复核失败（PID 可能已被复用），已拒绝终止以避免误伤其他进程")
		default:
			return Result{Method: MethodForced}, err
		}
	}
	return Result{Stopped: true, Forced: true, Method: MethodForced}, nil
}

// waitGone 宽限期内轮询进程消失；ctx 取消提前收口（判"未消失"，上层复核兜底）。
func waitGone(ctx context.Context, deps Deps, pid uint32, grace, interval time.Duration) bool {
	sleep := deps.Sleep
	if sleep == nil {
		sleep = time.After
	}
	deadline := time.NewTimer(grace)
	defer deadline.Stop()
	for {
		select {
		case <-ctx.Done():
			return false
		case <-deadline.C:
			_, err := deps.Proc.Query(pid)
			return err != nil // 末位复探一次，覆盖轮询与超时边界竞态
		case <-sleep(interval):
			if _, err := deps.Proc.Query(pid); err != nil {
				return true
			}
		}
	}
}

// tokenMismatch 与 supervisor/snipaste verifyToken 同形口径：路径大小写不敏感
// 比对，启动时间 ±1s 容差；空字段不参与比对（探针拿不到即降级为 PID 单因子）。
func tokenMismatch(want platform.VerifyToken, got platform.ProcInfo) bool {
	if want.ExePath != "" && got.ExePath != "" && !strings.EqualFold(filepath.Clean(want.ExePath), filepath.Clean(got.ExePath)) {
		return true
	}
	if !want.StartedAt.IsZero() && !got.StartedAt.IsZero() {
		diff := want.StartedAt.Sub(got.StartedAt)
		if diff < -time.Second || diff > time.Second {
			return true
		}
	}
	return false
}
