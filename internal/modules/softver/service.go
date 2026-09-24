package softver

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hanxi/internal/extapi"
	"hanxi/internal/notify"
	"hanxi/internal/platform/versioncmp"
)

// EventDirScan 目录大小扫描进度事件名（app.go 注册载荷类型）。
const EventDirScan = "softver:dir-scan"

// EventInstallerDownload 安装包下载进度事件名（app.go 注册载荷类型）。
// 声明与 s.emit 调用同文件——composition contract 的事件提取按"常量定义与
// emit 调用同文件"解析，跨文件引用会漏收（download.go 承载下载引擎本体）。
const EventInstallerDownload = "softver:installer-download"

// scanProgressInterval 运行中进度的推送节流（终态必发，不限流）。
const scanProgressInterval = 250 * time.Millisecond

// maxConcurrentDirScans 是目录 Walk 的全局并发上限。机械盘/大目录并发 Walk
// 会放大随机 IO，默认串行并按入队顺序调度。
const maxConcurrentDirScans = 1

// urlOpener 平台外呼最小面（wsl/envcheck 同款解耦，单测注入 fake）。
type urlOpener interface {
	OpenURL(url string) error
}

// installerIntegrityNote 下载完成后的如实校验声明（UI 与事件消息共用单一来源）：
// 官方直链从不旁挂摘要值，能核的只有字节数与可执行文件头，绝不冒充"校验通过"。
const installerIntegrityNote = "官方直链未提供校验值：已核对传输字节数与可执行文件头（MZ），未做 SHA-256 校验"

// downloadJob 一次进行中的安装包下载（单槽位：同一时刻至多一个下载，
// 路径与直链在登记时定死，goroutine 不再回读可变的官方缓存）。
type downloadJob struct {
	ctx      context.Context
	cancel   context.CancelFunc
	url      string
	version  string
	fileName string
	destPath string
}

// localData 一次本机探测的产物（探测函数按平台注入，Windows 出全量，其余出错误）。
type localData struct {
	Installs []LocalInstall
	Dirs     []DirSlot
	Notes    []string
}

// scanJob 是一次已登记的目录扫描。任务从入队起即拥有独立 context，因而
// 排队期间也能取消；pathKey 按最终路径归一化，用于拦截不同槽位指向同一目录。
type scanJob struct {
	id      string
	path    string
	pathKey string
	ctx     context.Context
	cancel  context.CancelFunc
	running bool
}

// SoftverService Wails 绑定服务：微信（首个跟踪目标）的本机双口径版本、
// 目录槽位与大小、官方最新版对照与安装包直连下载（N38：拿完包走人，
// 不托管安装/启动）。探测/取页/下载函数一律字段注入，单测替换后即可离线
// 断言；页面进入零隐式外呼（官方取数与下载都只在显式动作触发）。
// 业务 RPC 方法经 holder.Enter() 接入统一调用门（Wave 3）；扫描/下载 goroutine
// 与生命周期取消（cancelAllDirScans/cancelActiveDownload）走未接门的内部路径。
type SoftverService struct {
	holder        *extapi.LeaseHolder
	opener        urlOpener
	probeLocal    func() (localData, error)
	fetchPage     func(context.Context) (string, error)
	walk          func(context.Context, string, func(scanStat, string)) (scanStat, error)
	canonicalPath func(string) (string, error)
	emit          func(name string, payload any)
	reveal        func(path string) error

	// 下载安装包面（N38）：目录解析与"资源管理器定位文件"按平台注入，
	// downloadFile 字段注入后单测不碰网络。
	downloadsDir func() (string, error)
	revealFile   func(path string) error
	downloadFile installerDownloader

	mu sync.Mutex
	// local 最近一次探测结果：StartDirScan/CancelDirScan/RevealDir 只认这里的
	// 槽位 ID（路径由后端解析，前端传任意串一律拒绝——envcheck 红线同款）。
	local localData
	// sizes 扫描结果缓存（key=槽位 ID，跨次探测稳定）：只存终态成功值，
	// 取消/失败不留半成品；重扫覆盖旧值。
	sizes map[string]*DirSize
	// scans 包含排队与运行中的任务；scanQueue 保持 FIFO 公平性，runningScans
	// 只统计真正占用 Walk 槽位的任务。activePaths 防止槽位别名重复扫描同一路径。
	scans        map[string]*scanJob
	scanQueue    []*scanJob
	activePaths  map[string]string
	runningScans int

	// dl 进行中的安装包下载（nil = 空闲）；downloaded 最近一次成功落位记录。
	dl         *downloadJob
	downloaded *InstallerFile

	official    *OfficialRelease
	officialErr string
}

// NewSoftverService 构造服务并按平台挂载探测默认值。
func NewSoftverService(opener urlOpener, holder *extapi.LeaseHolder) *SoftverService {
	s := &SoftverService{
		holder:        holder,
		opener:        opener,
		fetchPage:     fetchUpdatesPage, // 官方页抓取跨平台通用；本机探测按平台挂载
		walk:          walkDirSize,
		canonicalPath: canonicalPathKey,
		emit:          emitEvent,
		reveal:        revealInExplorer,
		revealFile:    revealFileInExplorer,
		downloadFile:  downloadInstallerFile,
		sizes:         map[string]*DirSize{},
		scans:         map[string]*scanJob{},
		activePaths:   map[string]string{},
	}
	attachPlatformDefaults(s)
	return s
}

// dirSlotID 槽位稳定键：kind + 小写规范化路径（"|" 为 Windows 路径非法字符，
// 跨次探测与扫描缓存挂接都用它；RevealDir/StartDirScan 只认完整 ID）。
func dirSlotID(kind, path string) string {
	return kind + "|" + strings.ToLower(filepath.Clean(path))
}

// canonicalPathKey 解析已存在目录的最终路径并转成不区分大小写的比较键。
// EvalSymlinks 在 Windows 会展开 symlink/junction；Abs 同时消除相对路径别名。
func canonicalPathKey(path string) (string, error) {
	resolved, err := filepath.EvalSymlinks(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return "", err
	}
	return strings.ToLower(filepath.Clean(resolved)), nil
}

// emitEvent 经 Wails 事件总线推送；无 app 实例（单测/装配前）静默跳过。
func emitEvent(name string, payload any) {
	if app := application.Get(); app != nil && app.Event != nil {
		app.Event.Emit(name, payload)
	}
}

// revealInExplorer 资源管理器打开目录（包级变量，单测替换避免真拉 explorer）。
var revealInExplorer = func(path string) error {
	return exec.Command("explorer.exe", filepath.Clean(path)).Start()
}

// revealFileInExplorer 资源管理器打开父目录并高亮定位文件（explorer /select
// 习语，同 windows.RevealFile；包级变量便于单测替换）。
var revealFileInExplorer = func(path string) error {
	return exec.Command("explorer.exe", "/select,"+filepath.Clean(path)).Start()
}

// ---- 对外绑定面 ----

// Snapshot 返回页面全量数据：本机探测（快、无网络）× 官方缓存 × 对比结论。
// 目录槽位自动挂接缓存的扫描结果；不隐式发起官方页抓取。
func (s *SoftverService) Snapshot() (Snapshot, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return Snapshot{}, gateErr
	}
	defer release()
	return s.querySnapshot()
}

// querySnapshot Snapshot 的内部共用版：不接调用门，供 RPC 门后与
// StartDirScan 冷启动自调等已在门内/门外的内部路径复用。
func (s *SoftverService) querySnapshot() (Snapshot, error) {
	data, err := s.probeLocal()
	if err != nil {
		return Snapshot{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// 拷贝槽位再挂缓存：probeLocal 的实现方可能复用包级切片，不隔层改写它的返回物。
	dirs := append([]DirSlot(nil), data.Dirs...)
	for i := range dirs {
		dirs[i].Size = s.sizes[dirs[i].ID]
	}
	data.Dirs = dirs
	s.local = data
	off := s.official
	offErr := s.officialErr
	scanning := make([]string, 0, len(s.scans))
	for id := range s.scans {
		scanning = append(scanning, id)
	}
	return Snapshot{
		ProbedAt:      time.Now().Format(time.RFC3339),
		Installs:      data.Installs,
		Dirs:          data.Dirs,
		Official:      off,
		OfficialError: offErr,
		Update:        updateHintFor(data.Installs, off),
		Scanning:      scanning,
		Downloading:   s.dl != nil,
		Downloaded:    s.downloaded,
		Notes:         data.Notes,
	}, nil
}

// RefreshOfficial 显式抓取并解析官方更新页；成功更新缓存，失败缓存原因
// （页面结构改版即失配——前端据 OfficialError 降级为"打开官方页"，不猜不编）。
func (s *SoftverService) RefreshOfficial() (OfficialRelease, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return OfficialRelease{}, gateErr
	}
	defer release()
	ctx, cancel := context.WithTimeout(context.Background(), officialFetchTimeout+5*time.Second)
	defer cancel()
	rel, err := s.fetchOfficial(ctx)
	s.mu.Lock()
	if err != nil {
		s.officialErr = err.Error()
		s.mu.Unlock()
		return OfficialRelease{}, err
	}
	s.official = rel
	s.officialErr = ""
	s.mu.Unlock()
	return *rel, nil
}

func (s *SoftverService) fetchOfficial(ctx context.Context) (*OfficialRelease, error) {
	body, err := s.fetchPage(ctx)
	if err != nil {
		return nil, err
	}
	rel, err := parseOfficialPage(body)
	if err != nil {
		return nil, err
	}
	rel.FetchedAt = time.Now().Format(time.RFC3339)
	return &rel, nil
}

// OpenUpdatesPage 拉起浏览器打开官方更新页（官方通道失配时的保底动线）。
func (s *SoftverService) OpenUpdatesPage() error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	if s.opener == nil {
		return errors.New("打开官方页失败: 平台能力不可用")
	}
	if err := s.opener.OpenURL(UpdatesPageURL); err != nil {
		return fmt.Errorf("打开官方页失败: %w", err)
	}
	return nil
}

// StartDirScan 异步扫描指定目录槽位大小（终态/进度经 softver:dir-scan 事件推送）。
// id 必须来自最近一次 Snapshot 的槽位列表；不同 ID 若解析到同一最终路径也拒绝重入。
// 任务统一进入 FIFO 队列，最多 maxConcurrentDirScans 个 Walk 同时运行。
func (s *SoftverService) StartDirScan(id string) error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("目录槽位 ID 不能为空")
	}
	s.mu.Lock()
	if len(s.local.Dirs) == 0 {
		s.mu.Unlock()
		// 冷启动直调（未先 Snapshot）：补一次探测建立槽位面（已在门内，走内部版）
		if _, err := s.querySnapshot(); err != nil {
			return err
		}
		s.mu.Lock()
	}
	slot, ok := s.findSlotLocked(id)
	s.mu.Unlock()
	if !ok {
		return fmt.Errorf("未知目录槽位 %q（请刷新后重试）", id)
	}
	if !slot.Exists {
		return fmt.Errorf("目录不存在，无法扫描：%s", slot.Path)
	}

	pathKey, err := s.canonicalPath(slot.Path)
	if err != nil {
		return fmt.Errorf("解析目录最终路径失败: %w", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	job := &scanJob{id: id, path: slot.Path, pathKey: pathKey, ctx: ctx, cancel: cancel}

	s.mu.Lock()
	if _, busy := s.scans[id]; busy {
		s.mu.Unlock()
		cancel()
		return errors.New("该目录已在排队或扫描中，请稍候或先取消")
	}
	if activeID, busy := s.activePaths[pathKey]; busy {
		s.mu.Unlock()
		cancel()
		return fmt.Errorf("同一目录已由槽位 %q 排队或扫描，已拒绝重复任务", activeID)
	}
	s.scans[id] = job
	s.activePaths[pathKey] = id
	s.scanQueue = append(s.scanQueue, job)
	s.mu.Unlock()

	s.emit(EventDirScan, ScanProgress{ID: id, State: "queued", Message: "已排队等待扫描"})
	s.dispatchScans()
	return nil
}

// dispatchScans 按 FIFO 顺序占用全局 Walk 槽位。事件在启动 goroutine 前发出，
// 保证观察者不会先看到进度、后看到 running 状态。
func (s *SoftverService) dispatchScans() {
	var ready []*scanJob
	s.mu.Lock()
	for s.runningScans < maxConcurrentDirScans && len(s.scanQueue) > 0 {
		job := s.scanQueue[0]
		s.scanQueue = s.scanQueue[1:]
		if s.scans[job.id] != job { // 已在排队期间取消
			continue
		}
		job.running = true
		s.runningScans++
		ready = append(ready, job)
	}
	s.mu.Unlock()
	for _, job := range ready {
		s.emit(EventDirScan, ScanProgress{ID: job.id, State: "running", Message: "正在扫描"})
		go s.runScan(job)
	}
}

func (s *SoftverService) runScan(job *scanJob) {
	released := false
	defer job.cancel()
	defer func() {
		if r := recover(); r != nil {
			if !released {
				s.releaseScan(job, nil)
				s.emit(EventDirScan, ScanProgress{ID: job.id, State: "error", Message: fmt.Sprintf("扫描异常中止: %v", r)})
			}
			s.dispatchScans()
		}
	}()

	var lastEmit time.Time
	throttled := func(stat scanStat, current string) {
		if time.Since(lastEmit) < scanProgressInterval {
			return
		}
		lastEmit = time.Now()
		s.emit(EventDirScan, ScanProgress{
			ID: job.id, State: "running", Bytes: stat.Bytes, Files: stat.Files,
			Dirs: stat.Dirs, Skipped: stat.Skipped, Current: current,
		})
	}
	stat, err := s.walk(job.ctx, job.path, throttled)

	var size *DirSize
	state := "done"
	message := ""
	now := ""
	switch {
	case errors.Is(err, context.Canceled):
		state = "canceled"
		message = "已取消，本次结果未缓存"
	case err != nil:
		state = "error"
		message = err.Error()
	default:
		now = time.Now().Format(time.RFC3339)
		size = &DirSize{Bytes: stat.Bytes, Files: stat.Files, Dirs: stat.Dirs, Skipped: stat.Skipped, ScannedAt: now}
	}
	s.releaseScan(job, size)
	released = true
	s.emit(EventDirScan, ScanProgress{
		ID: job.id, State: state, Bytes: stat.Bytes, Files: stat.Files,
		Dirs: stat.Dirs, Skipped: stat.Skipped, Message: message, ScannedAt: now,
	})
	s.dispatchScans()
}

// releaseScan 释放登记与全局 Walk 槽位；成功结果与释放在同一临界区提交。
func (s *SoftverService) releaseScan(job *scanJob, size *DirSize) {
	s.mu.Lock()
	if s.scans[job.id] == job {
		delete(s.scans, job.id)
		delete(s.activePaths, job.pathKey)
		if job.running {
			s.runningScans--
		}
		if size != nil {
			s.sizes[job.id] = size
		}
	}
	s.mu.Unlock()
}

// CancelDirScan 请求取消指定槽位排队或运行中的扫描（半成品不缓存）。
func (s *SoftverService) CancelDirScan(id string) error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	id = strings.TrimSpace(id)
	s.mu.Lock()
	job, ok := s.scans[id]
	if !ok {
		s.mu.Unlock()
		return errors.New("该目录当前没有排队或进行中的扫描")
	}
	if job.running {
		s.mu.Unlock()
		job.cancel()
		return nil
	}
	delete(s.scans, id)
	delete(s.activePaths, job.pathKey)
	s.removeQueuedScanLocked(job)
	s.mu.Unlock()
	job.cancel()
	s.emit(EventDirScan, ScanProgress{ID: id, State: "canceled", Message: "已取消排队任务，本次结果未缓存"})
	s.dispatchScans()
	return nil
}

// cancelAllDirScans 是模块生命周期使用的包内取消入口，不扩展 Wails 绑定面。
// 排队任务立即发 canceled；运行任务由 Walk 响应 context 后发终态。
func (s *SoftverService) cancelAllDirScans() {
	var queued []*scanJob
	var running []*scanJob
	s.mu.Lock()
	for _, job := range s.scans {
		if job.running {
			running = append(running, job)
			continue
		}
		queued = append(queued, job)
		delete(s.scans, job.id)
		delete(s.activePaths, job.pathKey)
	}
	s.scanQueue = nil
	s.mu.Unlock()
	for _, job := range queued {
		job.cancel()
		s.emit(EventDirScan, ScanProgress{ID: job.id, State: "canceled", Message: "已取消排队任务，本次结果未缓存"})
	}
	for _, job := range running {
		job.cancel()
	}
}

func (s *SoftverService) removeQueuedScanLocked(target *scanJob) {
	for i, job := range s.scanQueue {
		if job == target {
			s.scanQueue = append(s.scanQueue[:i], s.scanQueue[i+1:]...)
			return
		}
	}
}

// RevealDir 在资源管理器中打开槽位目录（路径经后端槽位面解析获得，拒收任意路径）。
func (s *SoftverService) RevealDir(id string) error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	s.mu.Lock()
	slot, ok := s.findSlotLocked(strings.TrimSpace(id))
	s.mu.Unlock()
	if !ok {
		return fmt.Errorf("未知目录槽位 %q（请刷新后重试）", id)
	}
	if !slot.Exists {
		return fmt.Errorf("目录不存在：%s", slot.Path)
	}
	if err := s.reveal(slot.Path); err != nil {
		return fmt.Errorf("打开目录失败: %w", err)
	}
	return nil
}

// findSlotLocked 在槽位面查 ID（调用方须持 s.mu）。
func (s *SoftverService) findSlotLocked(id string) (DirSlot, bool) {
	for _, d := range s.local.Dirs {
		if d.ID == id {
			return d, true
		}
	}
	return DirSlot{}, false
}

// ---- 下载安装包（N38：只搬包到下载目录，不托管安装/启动） ----

// StartInstallerDownload 异步下载官方直链安装包到系统下载目录（进度/终态经
// softver:installer-download 事件推送）。直链取官方缓存读数，未先 RefreshOfficial
// 或页面改版没解析出直链一律如实报错——不猜链接。同一时刻只允许一个下载。
// 刻意不提供"运行安装器"：边界止于把包交还用户（打开位置由 RevealInstallerFile 负责）。
func (s *SoftverService) StartInstallerDownload() error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()

	s.mu.Lock()
	if s.dl != nil {
		s.mu.Unlock()
		return errors.New("已有安装包下载在进行中，请稍候或先取消")
	}
	off := s.official
	if off == nil || off.DownloadURL == "" {
		s.mu.Unlock()
		return errors.New("尚未获取到官方下载直链，请先点「获取官方最新版」（页面改版解析不出直链时只能复制/打开官方页）")
	}
	if s.downloadsDir == nil {
		s.mu.Unlock()
		return errors.New("下载目录能力不可用")
	}
	dir, err := s.downloadsDir()
	if err != nil {
		s.mu.Unlock()
		return err
	}
	fileName := installerFileName(off.DownloadURL, off.Version)
	destPath, err := uniqueDestPath(dir, fileName)
	if err != nil {
		s.mu.Unlock()
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	job := &downloadJob{ctx: ctx, cancel: cancel, url: off.DownloadURL, version: off.Version, fileName: filepath.Base(destPath), destPath: destPath}
	s.dl = job
	s.mu.Unlock()

	s.emit(EventInstallerDownload, InstallerProgress{State: "downloading", FileName: job.fileName, Message: "开始下载官方安装包"})
	go s.runInstallerDownload(job)
	return nil
}

// runInstallerDownload 执行下载并广播进度：进度事件按 250ms 节流、终态必发；
// 失败/取消清理登记但绝不下发"成功"，done 只在字节双核 + MZ 断言通过后发出。
func (s *SoftverService) runInstallerDownload(job *downloadJob) {
	defer job.cancel()
	defer func() {
		if r := recover(); r != nil {
			if s.clearDownload(job) {
				s.emit(EventInstallerDownload, InstallerProgress{State: "error", FileName: job.fileName, Message: fmt.Sprintf("下载异常中止: %v", r)})
				notify.Error("softver", "安装包下载失败", "下载安装包时发生内部错误", navRoute)
			}
		}
	}()

	var lastEmit time.Time
	bytes, err := s.downloadFile(job.ctx, job.url, job.destPath, func(done, total int64) {
		if time.Since(lastEmit) < downloadProgressInterval {
			return
		}
		lastEmit = time.Now()
		s.emit(EventInstallerDownload, InstallerProgress{State: "downloading", FileName: job.fileName, Done: done, Total: total})
	})

	switch {
	case err != nil && (errors.Is(err, context.Canceled) || errors.Is(job.ctx.Err(), context.Canceled)):
		s.clearDownload(job)
		s.emit(EventInstallerDownload, InstallerProgress{State: "canceled", FileName: job.fileName, Message: "已取消下载，半截临时文件已清理"})
	case err != nil:
		s.clearDownload(job)
		s.emit(EventInstallerDownload, InstallerProgress{State: "error", FileName: job.fileName, Message: err.Error()})
		notify.Error("softver", "安装包下载失败", err.Error(), navRoute)
	default:
		file := &InstallerFile{
			Path: job.destPath, FileName: filepath.Base(job.destPath), Version: job.version,
			Bytes: bytes, DownloadedAt: time.Now().Format(time.RFC3339), Note: installerIntegrityNote,
		}
		s.finishDownload(job, file)
		s.emit(EventInstallerDownload, InstallerProgress{
			State: "done", FileName: file.FileName, Done: bytes, Total: bytes,
			Message: fmt.Sprintf("下载完成：%s（%s）", file.FileName, installerIntegrityNote), File: file,
		})
		notify.Success("softver", "安装包下载完成", installerIntegrityNote+"；安装请自行双击运行，本工具不代管。", navRoute)
	}
}

// clearDownload 撤下下载登记（仅当登记的仍是本任务，防误清新任务）。
func (s *SoftverService) clearDownload(job *downloadJob) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dl == job {
		s.dl = nil
		return true
	}
	return false
}

// finishDownload 同一临界区提交"成功记录 + 撤下进行中登记"（快照回显原子）。
func (s *SoftverService) finishDownload(job *downloadJob, file *InstallerFile) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dl == job {
		s.dl = nil
	}
	s.downloaded = file
}

// CancelInstallerDownload 请求取消进行中的下载（临时件由下载器清理；
// canceled 终态事件由执行协程统一发出，此处不重复播报）。
func (s *SoftverService) CancelInstallerDownload() error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	s.mu.Lock()
	job := s.dl
	s.mu.Unlock()
	if job == nil {
		return errors.New("当前没有进行中的安装包下载")
	}
	job.cancel()
	return nil
}

// RevealInstallerFile 在资源管理器中定位最近一次下载的安装包（"打开位置"）。
// 路径只认后端成功落位记录，拒收任意路径——与 RevealDir 的红线一致。
func (s *SoftverService) RevealInstallerFile() error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	s.mu.Lock()
	file := s.downloaded
	s.mu.Unlock()
	if file == nil {
		return errors.New("尚未成功下载安装包")
	}
	if _, err := os.Stat(file.Path); err != nil {
		return fmt.Errorf("已下载的文件不存在（可能被移动或删除）：%s", file.Path)
	}
	if s.revealFile == nil {
		return errors.New("打开位置能力不可用")
	}
	if err := s.revealFile(file.Path); err != nil {
		return fmt.Errorf("打开下载位置失败: %w", err)
	}
	return nil
}

// cancelActiveDownload 是模块生命周期使用的包内取消入口（OnDestroy），
// 不扩展 Wails 绑定面；终态事件仍由执行协程发出。
func (s *SoftverService) cancelActiveDownload() {
	s.mu.Lock()
	job := s.dl
	s.mu.Unlock()
	if job != nil {
		job.cancel()
	}
}

// ---- 纯函数 helpers（单测直打） ----

// updateHintFor 本机主口径最高版本 × 官方最新版对照。
// 官方页只出三段（如 4.1.15）而本机常带四段构建号（4.1.15.9）：
// versioncmp"段数多者胜"让 4.1.15.9 ≥ 4.1.15，不会把同版本误报为可更新。
func updateHintFor(installs []LocalInstall, official *OfficialRelease) *UpdateHint {
	localBest := ""
	for _, in := range installs {
		v := in.BestVersion
		if v == "" {
			continue
		}
		if localBest == "" || versioncmp.Compare(v, localBest) > 0 {
			localBest = v
		}
	}
	switch {
	case localBest == "":
		return &UpdateHint{Status: "noLocal", Message: "未在本机检测到微信安装"}
	case official == nil || official.Version == "":
		return &UpdateHint{Status: "noOfficial", LocalVersion: localBest, Message: "尚未获取官方最新版本"}
	case versioncmp.Compare(official.Version, localBest) > 0:
		return &UpdateHint{
			Status: "available", LocalVersion: localBest, OfficialVersion: official.Version,
			Message: fmt.Sprintf("官方已有新版本 %s（本机 %s），可用直链下载升级", official.Version, localBest),
		}
	default:
		return &UpdateHint{
			Status: "latest", LocalVersion: localBest, OfficialVersion: official.Version,
			Message: fmt.Sprintf("本机 %s 不低于官方最新 %s，已是最新", localBest, official.Version),
		}
	}
}
