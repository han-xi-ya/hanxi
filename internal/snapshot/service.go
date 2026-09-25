package snapshot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"hanxi/internal/notify"
	"hanxi/internal/settings"
)

// dataRootPaths CheckpointService 需要的数据根最小能力面（*settings.Paths 天然
// 满足；收窄成单方法便于测试注入临时目录造景，与全局单例解耦）。
type dataRootPaths interface {
	DataDir() string
}

// CheckpointService 历史版本服务（绑定面）。
type CheckpointService struct {
	paths dataRootPaths
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
	scanMtimeFn  func() (time.Time, bool)

	failMu           sync.Mutex
	consecutiveFails int
	failureWarned    bool

	// memoRestorer 便签热恢复钩子（装配根注入 memo.MemoService.RestoreFile）：
	// memo/ 文件回写后同步内存换装 + emit memo:changed，热生效无感。
	memoRestorer atomic.Pointer[func(id, content string) error]
	// memoTitle 便签标题只读映射钩子（装配根注入 memo.MemoService 侧查询）：
	// ListFiles 左栏把 memo/<id>.md 显示为标题，避免裸文件名串（N33 §2）。
	// 与 memoRestorer 同款 DI，规避 snapshot→memo 包引用。
	memoTitle atomic.Pointer[func(relPath string) (string, bool)]
}

// New 构造服务（纯装配无 IO；探测与引擎选择在 Start）。
// nil paths 显式落为接口 nil（防"类型非 nil 值为 nil"的哑指针陷阱）。
func New(paths *settings.Paths, store *settings.Store) *CheckpointService {
	s := &CheckpointService{store: store}
	if paths != nil {
		s.paths = paths
	}
	return s
}

// SetMemoRestorer 注入便签热恢复回调（仅装配根调用；nil 安全）。
func (s *CheckpointService) SetMemoRestorer(fn func(id, content string) error) {
	if fn == nil {
		s.memoRestorer.Store(nil)
		return
	}
	s.memoRestorer.Store(&fn)
}

// SetMemoTitleResolver 注入便签标题映射回调（仅装配根调用；nil 安全）。
func (s *CheckpointService) SetMemoTitleResolver(fn func(relPath string) (string, bool)) {
	if fn == nil {
		s.memoTitle.Store(nil)
		return
	}
	s.memoTitle.Store(&fn)
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

func (s *CheckpointService) scanMtime() (time.Time, bool) {
	if s.scanMtimeFn != nil {
		return s.scanMtimeFn()
	}
	return ScanMtime(s.paths.DataDir())
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
		slog.Info("snapshot: 未探测到可用 git，历史版本降级影子拷贝")
	}
	return newBackupEngine(s.paths.DataDir(), s.snapshotDir()), probe.Available
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
	maxMT, found := s.scanMtime()

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

	// 水位必须在检查/提交前固定。此后发生的写入无论是否恰好被本轮引擎捕获，
	// mtime 都仍高于该水位，下一拍会再次核验，不能被提交后的重扫吞掉。
	watermark, watermarkFound := s.scanMtime()
	changes, err := eng.changes(ctx)
	if err != nil {
		s.noteFailure(eng, "检查变更", err)
		return
	}
	if len(changes) == 0 {
		// 内容相同（原子写原样重写）或已被上一发覆盖：推进水位，停止空转重试。
		s.advanceWatermark(watermark, watermarkFound)
		return
	}
	if err := eng.commit(ctx, changes); err != nil {
		s.noteFailure(eng, "提交检查点", err)
		return
	}
	s.advanceWatermark(watermark, watermarkFound)
	s.failMu.Lock()
	s.consecutiveFails, s.failureWarned = 0, false
	s.failMu.Unlock()
	slog.Debug("snapshot: 检查点已固化", "mode", eng.mode(), "files", len(changes))
}

// advanceWatermark 记录提交时刻，并仅推进到本轮开始前采集的 mtime。
// commit 期间的新写保持在水位之后，下个 tick 必会再次核验。
func (s *CheckpointService) advanceWatermark(mt time.Time, found bool) {
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

// PreviewFile 单文件内容预览（≤512KB 文本）。既有绑定面，语义 = 当前版本预览；
// 新页一律走 PreviewRevision（可指定历史版本，两模式统一读面）。
func (s *CheckpointService) PreviewFile(id, path string) (FilePreview, error) {
	return s.PreviewRevision(path, id)
}

// PreviewRevision 读取某白名单文件的正文：revision 空 = 盘上当前内容（两模式
// 同口径直读，git index 受提交时序干扰不可作"现值"），非空 = 指定历史版本
// （引擎读面，被删文件在其最后存在版本同样可读——热修复 A 的"看被删内容"）。
// ≤512KB 截断照旧。
func (s *CheckpointService) PreviewRevision(path string, revision string) (FilePreview, error) {
	rel, err := normalizeWhitelistPath(path)
	if err != nil {
		return FilePreview{}, err
	}
	revision = strings.TrimSpace(revision)

	var data []byte
	if revision == "" {
		if s.paths == nil {
			return FilePreview{}, errors.New("历史版本服务尚未启动")
		}
		// 磁盘边界自检（链接/reparse 拒绝），再读
		if err := validateWhitelistPathOnDisk(s.paths.DataDir(), rel, false); err != nil {
			return FilePreview{}, err
		}
		data, err = os.ReadFile(filepath.Join(s.paths.DataDir(), filepath.FromSlash(rel)))
		if err != nil {
			return FilePreview{}, fmt.Errorf("读取当前内容失败: %w", err)
		}
	} else {
		eng, eerr := s.engineReady()
		if eerr != nil {
			return FilePreview{}, eerr
		}
		if err := checkRevisionID(revision); err != nil {
			return FilePreview{}, err
		}
		ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
		defer cancel()
		data, err = eng.file(ctx, revision, rel)
		if err != nil {
			return FilePreview{}, err
		}
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

// FileHistory 单文件时间线（新→旧，只含该文件有变化的版本）。limit 观察窗
// 在 maxListRevisions 内取值（≤0 或超限回 50）；备份模式事件由相邻清单差集
// 读时算（A/M/D 真事件，非硬造）。返回恒为非 nil 切片。
func (s *CheckpointService) FileHistory(path string, limit int) ([]FileRevision, error) {
	eng, err := s.engineReady()
	if err != nil {
		return nil, err
	}
	rel, err := normalizeWhitelistPath(path)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > maxListRevisions {
		limit = maxListRevisions
	}
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	revs, err := eng.fileHistory(ctx, rel, maxListRevisions)
	if err != nil {
		return nil, err
	}
	if len(revs) > limit {
		revs = revs[:limit]
	}
	if revs == nil {
		revs = []FileRevision{}
	}
	return revs, nil
}

// ListFiles 受保文件清单（文件为轴左栏）：引擎观察窗内出现过变化的路径 ∪
// 盘上现存白名单文件（新写未拍也有行，版本数如实为 0）。Alive 以盘上现状为准
// （已删除文件保留历史可见，恢复链见 FileHistory+RestoreFile）；Display 对
// memo/ 走标题 resolver，其余回落文件名（批 C 上中文名表）。
func (s *CheckpointService) ListFiles() ([]TrackedFile, error) {
	eng, err := s.engineReady()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	recs, err := eng.fileChanges(ctx, maxListRevisions)
	if err != nil {
		return nil, err
	}
	perFile := aggregateFileEvents(recs)

	// 盘上现状合并：白名单枚举给 Alive 与"从未入版"文件的最近 mtime。
	// 枚举失败/未装配 paths 时降级为"以事件流口径为准"（末事件非 D 即视为在盘）。
	disk := map[string]time.Time{}
	diskReliable := false
	if s.paths != nil {
		if files, derr := enumerateWhitelist(s.paths.DataDir(), false); derr == nil {
			diskReliable = true
			for _, f := range files {
				disk[f.Rel] = f.Info.ModTime()
			}
		}
	}

	out := make([]TrackedFile, 0, len(perFile)+len(disk))
	for rel, events := range perFile {
		_, alive := disk[rel]
		if !diskReliable {
			// events 恒非空且新→旧（aggregateFileEvents 保证）：降级口径取末次事件形态
			alive = events[0].Status != "D"
		}
		out = append(out, TrackedFile{
			Path:       rel,
			Display:    s.fileDisplay(rel),
			Group:      fileGroup(rel),
			Revisions:  len(events),
			LastChange: events[0].Time,
			Alive:      alive,
		})
	}
	for rel, de := range disk {
		if _, seen := perFile[rel]; seen {
			continue
		}
		out = append(out, TrackedFile{
			Path:       rel,
			Display:    s.fileDisplay(rel),
			Group:      fileGroup(rel),
			Revisions:  0,
			LastChange: de.Format(time.RFC3339),
			Alive:      true,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		gi, gj := groupRank(out[i].Group), groupRank(out[j].Group)
		if gi != gj {
			return gi < gj
		}
		if out[i].Display != out[j].Display {
			return out[i].Display < out[j].Display
		}
		return out[i].Path < out[j].Path
	})
	return out, nil
}

// fileDisplay 标题 resolver 优先，映射不到回落文件名（未注入钩子 = 一律文件名）。
func (s *CheckpointService) fileDisplay(rel string) string {
	if fn := s.memoTitle.Load(); fn != nil {
		if title, ok := (*fn)(rel); ok && strings.TrimSpace(title) != "" {
			return title
		}
	}
	if i := strings.LastIndex(rel, "/"); i >= 0 {
		return rel[i+1:]
	}
	return rel
}

// fileGroup / groupRank 三组归类：便签（主场景）→ 工作台设置 → 模块状态。
func fileGroup(rel string) string {
	switch {
	case strings.HasPrefix(rel, memoPrefix):
		return "memo"
	case rel == rootConfig:
		return "config"
	case strings.HasPrefix(rel, statePrefix):
		return "state"
	default:
		return "state" // 白名单收口后不可达，兜底归状态组
	}
}

func groupRank(group string) int {
	switch group {
	case "memo":
		return 0
	case "config":
		return 1
	case "state":
		return 2
	default:
		return 3
	}
}

// DiffFile 单文件新旧对照（≤512KB 各截断；行 diff 由前端 textdiff 计算）。
func (s *CheckpointService) DiffFile(id, path string) (FileDiff, error) {
	eng, err := s.engineReady()
	if err != nil {
		return FileDiff{}, err
	}
	if err := checkRevisionID(id); err != nil {
		return FileDiff{}, err
	}
	rel, err := normalizeWhitelistPath(path)
	if err != nil {
		return FileDiff{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	raw, err := eng.fileDiff(ctx, id, rel)
	if err != nil {
		return FileDiff{}, err
	}
	fd := FileDiff{Path: rel, Status: raw.Status, Summary: raw.Summary}
	if len(raw.Old) > maxPreviewBytes {
		fd.Old = raw.Old[:maxPreviewBytes]
		fd.OldTruncated = true
	} else {
		fd.Old = raw.Old
	}
	if len(raw.New) > maxPreviewBytes {
		fd.New = raw.New[:maxPreviewBytes]
		fd.NewTruncated = true
	} else {
		fd.New = raw.New
	}
	return fd, nil
}

// RestoreFile 单文件回滚。memo/ 下的便签走热恢复（内存换装 + memo:changed 事件，
// 重启无感）；config.json / state JSON 发布 pending restore 包，由下次启动在内存态
// 构造前应用，避免运行时盘面与 Store 内存分叉。
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
			return (*restorer)(memoID, string(data))
		}
	}
	return StagePendingRestore(s.paths.DataDir(), rel, data)
}

// writeRestoredFile 仅保留供既有原子写测试与兼容调用；config/state 的 RPC 恢复已改为
// StagePendingRestore，启动前由 ApplyPendingRestores 应用。
func writeRestoredFile(dataDir, rel string, data []byte) error {
	target := filepath.Join(dataDir, filepath.FromSlash(rel))
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
