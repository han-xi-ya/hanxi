package snapshot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"hanxi/internal/notify"
	"hanxi/internal/settings"
)

// CheckpointService 历史版本服务（绑定面）。
type CheckpointService struct {
	paths *settings.Paths
	store *settings.Store

	mu      sync.Mutex
	eng     engine // nil = 未 Start（tick 与 RPC 都以"引擎未就绪"降级处理）
	gitOK   bool   // Start 时探测结论缓存（状态行展示用，不每次重探）
	running bool   // tick 循环存活
	stopCh  chan struct{}
	// 触发源状态（三源一闸，PLAN §3.2）
	deactivated  atomic.Bool // 失焦/隐藏/最小化置位，恢复焦点清零
	manual       atomic.Bool // 「立即快照」按钮 / 退出补拍直通标记
	inFlight     atomic.Bool // 单飞闸：忙即跳过，不制造排队假象（wsl tryBegin 同款）
	lastCommitAt time.Time
	scannedMT    time.Time // 上次检查点落定时的白名单 mtime 水位（脏判定基线）

	failMu           sync.Mutex
	consecutiveFails int
	failureWarned    bool

	// memoRestorer 便签热恢复钩子（装配根注入 memo.MemoService.RestoreFile）：
	// memo/ 文件回写后同步内存换装 + emit memo:changed，热生效无感。
	memoRestorer atomic.Pointer[func(id string, data []byte) error]
}

// New 构造服务（纯装配无 IO；探测与引擎选择在 Start）。
func New(paths *settings.Paths, store *settings.Store) *CheckpointService {
	return &CheckpointService{paths: paths, store: store}
}

// SetMemoRestorer 注入便签热恢复回调（仅装配根调用；nil 安全）。
func (s *CheckpointService) SetMemoRestorer(fn func(id string, data []byte) error) {
	if fn == nil {
		s.memoRestorer.Store(nil)
		return
	}
	s.memoRestorer.Store(&fn)
}

// snapshotDir 数据根下的快照驻留目录（git-dir 与影子备份共同的老家）。
func (s *CheckpointService) snapshotDir() string {
	return filepath.Join(s.paths.DataDir(), snapshotsDirName)
}

// config 读生效偏好（非法值回出厂：idle ≥30s 地板、interval ≥1min，防设置面误操）。
func (s *CheckpointService) config() settings.SnapshotConfig {
	if s.store == nil {
		return settings.DefaultSettings().Snapshot
	}
	cfg := s.store.Get().Snapshot
	if cfg.IdleSeconds < 30 {
		cfg.IdleSeconds = 300
	}
	if cfg.IntervalMinutes < 1 {
		cfg.IntervalMinutes = 5
	}
	return cfg
}

// ---------- 生命周期 ----------

// Start 探测引擎并起 5s tick（装配根在窗口建好后调用一次；重复调用 no-op）。
func (s *CheckpointService) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return
	}
	s.running = true

	s.eng, s.gitOK = s.pickEngine()

	stop := make(chan struct{})
	s.stopCh = stop
	go func() {
		t := time.NewTicker(tickInterval)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				s.tick()
			}
		}
	}()
}

// Stop 停 tick 循环（装配根 cleanup 调用；不 flush——退出补拍在 OnShutdown 链上先行）。
func (s *CheckpointService) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return
	}
	s.running = false
	close(s.stopCh)
}

// pickEngine 探测并选择引擎；git 可用但仓库初始化失败同样静默转降级链（PLAN §3.3）。
func (s *CheckpointService) pickEngine() (eng engine, gitAvailable bool) {
	probe := detectGit()
	if probe.Available {
		ge, err := newGitEngine(s.paths.DataDir(), s.snapshotDir(), probe)
		if err != nil {
			slog.Error("snapshot: git 仓库初始化失败，本次进程降级影子拷贝", "err", err)
		} else {
			slog.Info("snapshot: 历史版本以 git 模式运行", "git", probe.Path, "version", probe.Version)
			return ge, true
		}
	} else {
		slog.Info("snapshot: 未探测到可用 git，等待影子拷贝降级链接入")
	}
	return nil, probe.Available
}

// ---------- 触发源（三源一闸） ----------

// NoteDeactivated 主窗失焦/隐藏/最小化 → 置失活标记（等价一次"用户离开"事件）。
// 由装配根挂 Wails 窗口事件；驻托盘后不再有失焦事件，故 Hide/Minimise 同样打点。
func (s *CheckpointService) NoteDeactivated() { s.deactivated.Store(true) }

// NoteActivated 窗口重新聚焦/显示 → 清零失活标记（编辑期不打断用户）。
func (s *CheckpointService) NoteActivated() { s.deactivated.Store(false) }

// CheckpointNow 手动「立即快照」（Q5 拍板）：绕过触发判定与间隔闸，仍受无变更
// skip 与单飞闸约束。异步执行（UI 稍后重拉列表即可）。
func (s *CheckpointService) CheckpointNow() error {
	s.mu.Lock()
	eng := s.eng
	s.mu.Unlock()
	if eng == nil {
		return errors.New("历史版本服务尚未启动")
	}
	if !s.inFlight.CompareAndSwap(false, true) {
		return errors.New("上一次快照尚未完成，请稍后再试")
	}
	go func() {
		defer s.inFlight.Store(false)
		s.runCheckpoint(eng)
	}()
	return nil
}

// FlushOnExit 退出前同步补最后一发（挂 OnShutdown 链，3s 闸门卡死不拖退出）。
func (s *CheckpointService) FlushOnExit() {
	s.mu.Lock()
	eng := s.eng
	s.mu.Unlock()
	if eng == nil {
		return
	}
	if !s.inFlight.CompareAndSwap(false, true) {
		return // tick 正在跑：退出路径不抢，放弃补拍
	}
	defer s.inFlight.Store(false)

	done := make(chan struct{})
	go func() {
		defer close(done)
		s.runCheckpoint(eng)
	}()
	select {
	case <-done:
	case <-time.After(shutdownFlushBound):
		slog.Warn("snapshot: 退出前快照超时，放弃等待（数据仍在盘上，仅少一个版本）")
	}
}

// tick 5s 巡检：白名单 mtime 脏判定 + 三源触发 + 间隔闸门。
func (s *CheckpointService) tick() {
	cfg := s.config()
	maxMT, found := ScanMtime(s.paths.DataDir())

	s.mu.Lock()
	eng := s.eng
	in := tickInput{
		Enabled:      cfg.Enabled,
		Force:        s.manual.Load(),
		Deactivated:  s.deactivated.Load(),
		Found:        found,
		MaxMT:        maxMT,
		ScannedMT:    s.scannedMT,
		LastCommitAt: s.lastCommitAt,
		Idle:         time.Duration(cfg.IdleSeconds) * time.Second,
		Interval:     time.Duration(cfg.IntervalMinutes) * time.Minute,
		InFlight:     s.inFlight.Load(),
	}
	shouldRun := decideTick(in, time.Now()) == tickGo
	s.mu.Unlock()

	if eng == nil || !shouldRun {
		return
	}
	if !s.inFlight.CompareAndSwap(false, true) {
		return
	}
	s.manual.Store(false)
	go func() {
		defer s.inFlight.Store(false)
		s.runCheckpoint(eng)
	}()
}

// tickDecision decideTick 结论。
type tickDecision int

const (
	tickGo       tickDecision = iota
	skipDisabled              // 总开关关闭
	skipQuiet                 // 白名单自上次检查点以来无 mtime 变化
	skipBusy                  // 单飞闸忙
	skipActive                // 编辑期未空闲且无失活/手动触发
	skipInterval              // 距上次提交不足最小间隔（防灌碎历史，下个 tick 再试）
)

// decideTick 触发判定纯函数（单测表驱动穷举）。顺序即优先级：
// 开关 → 忙闸 → 手动直通 → 脏判定 → 触发源 → 间隔闸。
// 注意：tick 持锁调用本函数并据结果决定是否抢占 inFlight，判定与抢闸之间的
// 竞态由 CompareAndSwap 兜底（忙则放弃，下个 tick 再来）。
func decideTick(in tickInput, now time.Time) tickDecision {
	switch {
	case !in.Enabled:
		return skipDisabled
	case in.InFlight:
		return skipBusy
	case in.Force:
		return tickGo
	case !in.Found || !in.MaxMT.After(in.ScannedMT):
		return skipQuiet
	case !in.Deactivated && now.Sub(in.MaxMT) < in.Idle:
		return skipActive // 还在写：等空闲或失焦
	case !in.LastCommitAt.IsZero() && now.Sub(in.LastCommitAt) < in.Interval:
		return skipInterval
	default:
		return tickGo
	}
}

// tickInput decideTick 的判定输入（时间一律外灌，便于测试）。
type tickInput struct {
	Enabled      bool
	Force        bool
	Deactivated  bool
	Found        bool
	MaxMT        time.Time
	ScannedMT    time.Time
	LastCommitAt time.Time
	Idle         time.Duration
	Interval     time.Duration
	InFlight     bool
}

// runCheckpoint 执行一次检查点：无变更 skip，提交成功后推进水位。
// 失败静默 slog.Error + 首次一条 notify.Warning（去重），连续失败自愈。
func (s *CheckpointService) runCheckpoint(eng engine) {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()

	changes, err := eng.changes(ctx)
	if err != nil {
		s.noteFailure(eng, "检查变更", err)
		return
	}
	if len(changes) == 0 {
		// 内容相同（原子写原样重写）或已被上一发覆盖：推进水位，停止空转重试。
		s.advanceWatermark()
		return
	}
	if err := eng.commit(ctx, changes); err != nil {
		s.noteFailure(eng, "提交检查点", err)
		return
	}
	s.advanceWatermark()
	s.failMu.Lock()
	s.consecutiveFails, s.failureWarned = 0, false
	s.failMu.Unlock()
	slog.Debug("snapshot: 检查点已固化", "mode", eng.mode(), "files", len(changes))
}

// advanceWatermark 记录提交时刻并推进 mtime 水位（水位取当前扫描值：commit 期间的
// 新写入 mtime 更大，下一拍 After(水位) 仍成立，不丢触发）。
func (s *CheckpointService) advanceWatermark() {
	mt, found := ScanMtime(s.paths.DataDir())
	s.mu.Lock()
	s.lastCommitAt = time.Now()
	if found && mt.After(s.scannedMT) {
		s.scannedMT = mt
	}
	s.mu.Unlock()
	s.deactivated.Store(false) // 本轮"用户离开"触发已被消费
}

// noteFailure 失败分级：仅记日志；首次失败发一条去重通知；连续失败达阈值
// 请引擎自愈（git 重 init，防仓库损坏后静默烧 tick）。
func (s *CheckpointService) noteFailure(eng engine, stage string, err error) {
	slog.Error("snapshot: "+stage+"失败", "mode", eng.mode(), "err", err)
	s.failMu.Lock()
	s.consecutiveFails++
	warn := !s.failureWarned
	s.failureWarned = true
	rebuild := s.consecutiveFails >= repoRebuildFails
	s.failMu.Unlock()
	if warn {
		notify.Warning("snapshot", "历史版本快照失败",
			fmt.Sprintf("%s：%v（不影响数据使用，详见日志）", stage, err), "/settings/snapshot")
	}
	if rebuild {
		slog.Warn("snapshot: 连续失败达阈值，请引擎自愈重建", "mode", eng.mode(), "fails", s.consecutiveFails)
		ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
		defer cancel()
		eng.heal(ctx)
		s.failMu.Lock()
		s.consecutiveFails = 0
		s.failMu.Unlock()
	}
}

// ---------- 绑定面 RPC（"历史版本"浏览 / 预览 / 恢复） ----------

// GetStatus 分区状态行。
func (s *CheckpointService) GetStatus() StatusInfo {
	s.mu.Lock()
	eng := s.eng
	last := s.lastCommitAt
	gitOK := s.gitOK
	s.mu.Unlock()

	cfg := s.config()
	st := StatusInfo{
		Enabled:         cfg.Enabled,
		IdleSeconds:     cfg.IdleSeconds,
		IntervalMinutes: cfg.IntervalMinutes,
		SnapshotDir:     s.snapshotDir(),
		GitAvailable:    gitOK,
	}
	if eng != nil {
		st.Mode = eng.mode()
		ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
		defer cancel()
		if revs, err := eng.revisions(ctx, maxListRevisions); err == nil {
			st.RevisionCount = len(revs)
		}
	}
	if !last.IsZero() {
		st.LastCommitAt = last.Format(time.RFC3339)
	}
	return st
}

// SetPreferences 保存可调偏好（非法值回出厂默认，与 config() 同口径）。
func (s *CheckpointService) SetPreferences(p Preferences) error {
	if p.IdleSeconds < 30 {
		p.IdleSeconds = 300
	}
	if p.IntervalMinutes < 1 {
		p.IntervalMinutes = 5
	}
	if s.store == nil {
		return nil
	}
	return s.store.Update(func(cfg *settings.AppSettings) {
		cfg.Snapshot = settings.SnapshotConfig{
			Enabled:         p.Enabled,
			IdleSeconds:     p.IdleSeconds,
			IntervalMinutes: p.IntervalMinutes,
		}
	})
}

// ListRevisions 历史版本列表（新→旧）。
func (s *CheckpointService) ListRevisions() ([]Revision, error) {
	eng, err := s.engineReady()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	revs, err := eng.revisions(ctx, maxListRevisions)
	if err != nil {
		return nil, err
	}
	if revs == nil {
		revs = []Revision{}
	}
	return revs, nil
}

// RevisionDetail 某版本包含的文件清单（预览弹窗首层）。
func (s *CheckpointService) RevisionDetail(id string) ([]RevisionFile, error) {
	eng, err := s.engineReady()
	if err != nil {
		return nil, err
	}
	if err := checkRevisionID(id); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	files, err := eng.revisionFiles(ctx, id)
	if err != nil {
		return nil, err
	}
	if files == nil {
		files = []RevisionFile{}
	}
	return files, nil
}

// PreviewFile 单文件内容预览（≤512KB 文本）。
func (s *CheckpointService) PreviewFile(id, path string) (FilePreview, error) {
	eng, err := s.engineReady()
	if err != nil {
		return FilePreview{}, err
	}
	if err := checkRevisionID(id); err != nil {
		return FilePreview{}, err
	}
	rel, err := normalizeWhitelistPath(path)
	if err != nil {
		return FilePreview{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	data, err := eng.file(ctx, id, rel)
	if err != nil {
		return FilePreview{}, err
	}
	fe := FilePreview{Path: rel, Size: int64(len(data))}
	if len(data) > maxPreviewBytes {
		fe.Content = string(data[:maxPreviewBytes])
		fe.Truncated = true
	} else {
		fe.Content = string(data)
	}
	return fe, nil
}

// RestoreFile 单文件回滚。memo/ 下的便签走热恢复（内存换装 + memo:changed 事件，
// 重启无感）；config.json / state JSON 直写盘后 notify 提示重启生效——
// 运行中内存态与盘面对齐的热回滚 v1 不做（违背 settings.Update 不变式）。
func (s *CheckpointService) RestoreFile(id, path string) error {
	eng, err := s.engineReady()
	if err != nil {
		return err
	}
	if err := checkRevisionID(id); err != nil {
		return err
	}
	rel, err := normalizeWhitelistPath(path)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	data, err := eng.file(ctx, id, rel)
	if err != nil {
		return err
	}

	if strings.HasPrefix(rel, memoPrefix) {
		if restorer := s.memoRestorer.Load(); restorer != nil {
			memoID := strings.TrimSuffix(rel[strings.LastIndex(rel, "/")+1:], ".md")
			if memoID == "" {
				return fmt.Errorf("非法便签路径: %s", rel)
			}
			return (*restorer)(memoID, data)
		}
	}
	return s.writeRestored(rel, data)
}

// writeRestored 白名单文件原子回写数据根（tmp+rename，与 jsonstore 同构防半途断电；
// 不走 jsonstore.Save——恢复的是原样字节，可能是 md 也可能含 JSON 校验外形态）。
func (s *CheckpointService) writeRestored(rel string, data []byte) error {
	target := filepath.Join(s.paths.DataDir(), filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return err
	}
	tmp := fmt.Sprintf("%s.snapshot-restore.%d", target, os.Getpid())
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	if err := os.Rename(tmp, target); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	notify.Info("snapshot", "历史版本已写回",
		fmt.Sprintf("%s 已恢复到所选版本，重启 Hanxi 后生效（便签除外，即时生效）。", rel), "/settings/snapshot")
	return nil
}

// OpenHistoryDir 打开快照驻留目录（降级模式 UI 退化的唯一动作，Q3 拍板）。
func (s *CheckpointService) OpenHistoryDir() error {
	dir := s.snapshotDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	return exec.Command("explorer.exe", filepath.Clean(dir)).Start()
}

// engineReady 引擎访问的统一门卫（未 Start 给出可读错误而非 panic）。
func (s *CheckpointService) engineReady() (engine, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.eng == nil {
		return nil, errors.New("历史版本服务尚未启动")
	}
	return s.eng, nil
}

// checkRevisionID 版本 ID 形态校验：git hash（hex 7~40）或备份时间戳目录名。
func checkRevisionID(id string) error {
	id = strings.TrimSpace(id)
	if gitHexRe.MatchString(id) || backupIDRe.MatchString(id) {
		return nil
	}
	return fmt.Errorf("非法的版本标识: %s", id)
}

// normalizeWhitelistPath 恢复/预览路径安全闸门：仅白名单相对路径放行，
// 拒绝绝对路径与 .. 穿越（参数全部过白名单，wsl 操作面同款纪律）。
func normalizeWhitelistPath(p string) (string, error) {
	rel := filepath.ToSlash(strings.TrimSpace(p))
	if rel == "" || strings.HasPrefix(rel, "/") || filepath.IsAbs(p) {
		return "", fmt.Errorf("非法的快照路径: %s", p)
	}
	for _, seg := range strings.Split(rel, "/") {
		if seg == ".." || seg == "" {
			return "", fmt.Errorf("非法的快照路径: %s", p)
		}
	}
	if !Whitelisted(rel) {
		return "", fmt.Errorf("路径不在历史版本白名单内: %s", p)
	}
	return rel, nil
}
