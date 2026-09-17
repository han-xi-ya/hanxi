package softver

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hanxi/internal/platform/versioncmp"
)

// EventDirScan 目录大小扫描进度事件名（app.go 注册载荷类型）。
const EventDirScan = "softver:dir-scan"

// scanProgressInterval 运行中进度的推送节流（终态必发，不限流）。
const scanProgressInterval = 250 * time.Millisecond

// urlOpener 平台外呼最小面（wsl/envcheck 同款解耦，单测注入 fake）。
type urlOpener interface {
	OpenURL(url string) error
}

// localData 一次本机探测的产物（探测函数按平台注入，Windows 出全量，其余出错误）。
type localData struct {
	Installs []LocalInstall
	Dirs     []DirSlot
	Notes    []string
}

// SoftverService Wails 绑定服务：微信（首个跟踪目标）的本机双口径版本、
// 目录槽位与大小、官方最新版对照。探测/取页函数一律字段注入，
// 单测替换后即可离线断言；页面进入零隐式外呼（官方取数只在显式 RefreshOfficial）。
type SoftverService struct {
	opener     urlOpener
	probeLocal func() (localData, error)
	fetchPage  func(context.Context) (string, error)
	walk       func(context.Context, string, func(scanStat, string)) (scanStat, error)
	emit       func(name string, payload any)
	reveal     func(path string) error

	mu sync.Mutex
	// local 最近一次探测结果：StartDirScan/CancelDirScan/RevealDir 只认这里的
	// 槽位 ID（路径由后端解析，前端传任意串一律拒绝——envcheck 红线同款）。
	local localData
	// sizes 扫描结果缓存（key=槽位 ID，跨次探测稳定）：只存终态成功值，
	// 取消/失败不留半成品；重扫覆盖旧值。
	sizes map[string]*DirSize
	// scans 进行中的扫描取消通道。
	scans map[string]context.CancelFunc

	official    *OfficialRelease
	officialErr string
}

// NewSoftverService 构造服务并按平台挂载探测默认值。
func NewSoftverService(opener urlOpener) *SoftverService {
	s := &SoftverService{
		opener:    opener,
		fetchPage: fetchUpdatesPage, // 官方页抓取跨平台通用；本机探测按平台挂载
		walk:      walkDirSize,
		emit:      emitEvent,
		reveal:    revealInExplorer,
		sizes:     map[string]*DirSize{},
		scans:     map[string]context.CancelFunc{},
	}
	attachPlatformDefaults(s)
	return s
}

// dirSlotID 槽位稳定键：kind + 小写规范化路径（"|" 为 Windows 路径非法字符，
// 跨次探测与扫描缓存挂接都用它；RevealDir/StartDirScan 只认完整 ID）。
func dirSlotID(kind, path string) string {
	return kind + "|" + strings.ToLower(filepath.Clean(path))
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

// ---- 对外绑定面 ----

// Snapshot 返回页面全量数据：本机探测（快、无网络）× 官方缓存 × 对比结论。
// 目录槽位自动挂接缓存的扫描结果；不隐式发起官方页抓取。
func (s *SoftverService) Snapshot() (Snapshot, error) {
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
		Notes:         data.Notes,
	}, nil
}

// RefreshOfficial 显式抓取并解析官方更新页；成功更新缓存，失败缓存原因
// （页面结构改版即失配——前端据 OfficialError 降级为"打开官方页"，不猜不编）。
func (s *SoftverService) RefreshOfficial() (OfficialRelease, error) {
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
	if s.opener == nil {
		return errors.New("打开官方页失败: 平台能力不可用")
	}
	if err := s.opener.OpenURL(UpdatesPageURL); err != nil {
		return fmt.Errorf("打开官方页失败: %w", err)
	}
	return nil
}

// StartDirScan 异步扫描指定目录槽位大小（终态/进度经 softver:dir-scan 事件推送）。
// id 必须来自最近一次 Snapshot 的槽位列表；同槽位扫描中拒绝重入。
func (s *SoftverService) StartDirScan(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("目录槽位 ID 不能为空")
	}
	s.mu.Lock()
	if len(s.local.Dirs) == 0 {
		s.mu.Unlock()
		// 冷启动直调（未先 Snapshot）：补一次探测建立槽位面
		if _, err := s.Snapshot(); err != nil {
			return err
		}
		s.mu.Lock()
	}
	slot, ok := s.findSlotLocked(id)
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("未知目录槽位 %q（请刷新后重试）", id)
	}
	if !slot.Exists {
		s.mu.Unlock()
		return fmt.Errorf("目录不存在，无法扫描：%s", slot.Path)
	}
	if _, busy := s.scans[id]; busy {
		s.mu.Unlock()
		return fmt.Errorf("该目录正在扫描中，请稍候或先取消")
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.scans[id] = cancel
	s.mu.Unlock()

	go s.runScan(id, slot.Path, ctx, cancel)
	return nil
}

func (s *SoftverService) runScan(id, path string, ctx context.Context, cancel context.CancelFunc) {
	// recover 兜底：扫描协程若意外 panic 也必须发终态事件——否则前端该槽位
	// 永远停在"进行中"（defer LIFO 下本函数最后执行，登记与取消通道已先清）。
	defer func() {
		if r := recover(); r != nil {
			s.emit(EventDirScan, ScanProgress{ID: id, State: "error", Message: fmt.Sprintf("扫描异常中止: %v", r)})
		}
	}()
	defer cancel()
	defer func() {
		s.mu.Lock()
		delete(s.scans, id)
		s.mu.Unlock()
	}()

	var lastEmit time.Time
	throttled := func(stat scanStat, current string) {
		if time.Since(lastEmit) < scanProgressInterval {
			return
		}
		lastEmit = time.Now()
		s.emit(EventDirScan, ScanProgress{
			ID: id, State: "running", Bytes: stat.Bytes, Files: stat.Files,
			Dirs: stat.Dirs, Skipped: stat.Skipped, Current: current,
		})
	}
	stat, err := s.walk(ctx, path, throttled)
	// 先摘扫描登记、再发终态事件：前端收到终态即可立即重扫/取消，
	// 不留"事件已到一个登记未清"的窗口（defer 兜底 panic 路径）。
	s.mu.Lock()
	delete(s.scans, id)
	s.mu.Unlock()
	switch {
	case errors.Is(err, context.Canceled):
		s.emit(EventDirScan, ScanProgress{
			ID: id, State: "canceled", Bytes: stat.Bytes, Files: stat.Files,
			Dirs: stat.Dirs, Skipped: stat.Skipped, Message: "已取消，本次结果未缓存",
		})
	case err != nil:
		s.emit(EventDirScan, ScanProgress{ID: id, State: "error", Message: err.Error()})
	default:
		now := time.Now().Format(time.RFC3339)
		size := &DirSize{Bytes: stat.Bytes, Files: stat.Files, Dirs: stat.Dirs, Skipped: stat.Skipped, ScannedAt: now}
		s.mu.Lock()
		s.sizes[id] = size
		s.mu.Unlock()
		s.emit(EventDirScan, ScanProgress{
			ID: id, State: "done", Bytes: size.Bytes, Files: size.Files,
			Dirs: size.Dirs, Skipped: size.Skipped, ScannedAt: now,
		})
	}
}

// CancelDirScan 请求取消指定槽位进行中的扫描（半成品不缓存）。
func (s *SoftverService) CancelDirScan(id string) error {
	s.mu.Lock()
	cancel, ok := s.scans[strings.TrimSpace(id)]
	s.mu.Unlock()
	if !ok {
		return errors.New("该目录当前没有进行中的扫描")
	}
	cancel()
	return nil
}

// RevealDir 在资源管理器中打开槽位目录（路径经后端槽位面解析获得，拒收任意路径）。
func (s *SoftverService) RevealDir(id string) error {
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
