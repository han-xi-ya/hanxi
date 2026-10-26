package clipboard

import (
	"fmt"
	"image"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hanxi/internal/extapi"
	"hanxi/internal/hotkey"
	hw "hanxi/internal/platform/windows"
	"hanxi/internal/settings"
)

// ClipboardService 剪贴板历史业务服务（Wails 绑定面）。方法集逐字冻结于契约 §4；
// 事件名与载荷见 §5。构造签名/生命周期与 memo.MemoService 同谱：装配根在收口
// 阶段接线（New → SetWailsApp → Module.SetHotkeyRegistry → OnInit start / OnDestroy stop），
// 本线不碰 app.go。
//
// 锁纪律（三把锁各司其职，绝不互相嵌套调用 Wails API）：
//   - store.mu：entries 账本（store 内部自理，本层只见返回值拷贝）；
//   - s.mu：生命周期与运行态（started/paused/监听引用/wailsApp/eventSink）；
//   - s.ovMu：浮层与热键簿记（见 overlay.go/hotkey.go，memo sheetMu 同谱）。
type ClipboardService struct {
	store  *Store
	holder *extapi.LeaseHolder

	// prefs 用户偏好（<StateDir>/clipboard-prefs.json，契约 §12 v1.8）：
	// 构造期只读装载并应用到 store/活表；测试手装体可为 nil——全部访问器
	// nil 接收者回落默认，存量测试零改动。
	prefs *clipPrefsStore

	mu       sync.Mutex
	started  bool
	paused   bool
	listener *clipboardListener
	wailsApp *application.App

	// queue 顺序粘贴队列（queue.go，v1.8）：id FIFO，仅 text；内存簿记不
	// 持久（重启即弃）。簿记归 s.mu 运行态域，事件广播一律锁外（emit 内
	// 取 s.mu，嵌套死锁纪律与 SetPaused 同款）；queueBusy 单发闸防连击。
	queue     []string
	queueBusy bool

	// excluded 敏感排除表**活表**（小写进程名；v1.3.4 裁决归 service 持有，
	// v1.8 SetPrefs 落笔可增删 custom 段（出厂 7 项结构上删不掉）；构造缺省=
	// 出厂默认表。入库判定与 GetStatus 回显同读此表，审计面与执行面不分裂）。
	excluded []string

	// eventSink 事件出口测试缝：非 nil 时事件只进假件不碰 Wails（单测注入）。
	eventSink func(name string, payload any)

	// mask 预览遮蔽引擎（mask.go，v1.8）：atomic 热换读侧无锁；未装载/测试
	// 手装体为 nil=恒等（遮蔽未配置不遮，与三缝假件同谱）。
	mask atomic.Pointer[maskEngine]

	// 系统写手与 DIB 解码缝（默认 setSystemText/DecodeDIB，测试注入假件；
	// DecodeDIB/BlobStore 由 A6 线落地，签名冻结见契约 §10）。
	writeText func(string) error
	decodeDIB func([]byte) (image.Image, error)

	// R-G1 粘贴执行缝（paste.go，Paste 十一法）：抢焦点/模拟键/前台落位等待，
	// 默认走真 Win32（SetForegroundForce/SendInput/轮询），测试注入假件——
	// 与 writeText 同谱，单测永不真发键。
	focusWin       func(uintptr) error
	sendKeys       func() error
	waitForeground func(uintptr) bool

	// 浮层（overlay.go）与热键（hotkey.go）簿记：全部走 ovMu（memo sheetMu 同谱），
	// 与数据锁 store.mu、运行态锁 s.mu 互不相涉；Wails 窗口 API 一律锁外调用。
	ovMu      sync.Mutex
	ovWin     *application.WebviewWindow
	ovClosing func()
	ovShown   bool
	ovIdle    *time.Timer
	// R-G1 粘贴目标：浮层打开瞬间（Show/Focus 前）的前台窗句柄，即用户即将
	// 粘贴进去的窗口。簿记随 ovMu；生命周期随浮层显隐——recordPasteTarget
	// 在 showOverlay 记录时置位，收起后由 ovShown=false 判资格作废（paste.go）。
	pasteTarget uintptr
	hk          *hotkey.Registry // 全仓通用热键注册器（装配根注入，未注入只降级不报错）
}

// 事件名常量（契约 §5/§6，前端 adapters/clipboard.ts CLIP_EV 逐字对位）。
// 模块标识 ID 见 module.go（契约 §12.1：ModuleID/目录键/授权键单一事实源）。
const (
	evUpdated = "clipboard:updated"
	evRemoved = "clipboard:removed"
	evPaused  = "clipboard:paused"
	// evQueueChanged 顺序粘贴队列全量快照事件（v1.8，载荷 QueueChangedPayload；
	// 入队/摘除/清队/贴出消费/联动 prune 五源共用，前端徽标与余数恒跟快照）。
	evQueueChanged = "clipboard:queue:changed"
	// 浮层清稿事件名 overlayOpeningEv 见 overlay.go（契约 §6）。
)

// RemovedPayload/PausedPayload clipboard:removed 与 clipboard:paused 的载荷形态。
// 导出是装配闸要求：Wails beta.10 对 RegisterEvent[T] 做精确类型匹配，装配根
// 按本类型注册事件，未导出则无法引用（收口清单 W1 事件类型闸）。
type RemovedPayload struct {
	ID string `json:"id"`
}
type PausedPayload struct {
	Paused bool `json:"paused"`
}

// NewClipboardService 实例化服务：建 <DataDir>/clipboard 库（含坏库隔离），
// 装载历史。构造期只读盘与库基线维护，不开监听不碰热键。
//
// 无头表禁令钉注（契约 §12 v1.2.4，安全级）：NewStore 构造即建目录/空库回写
// 是 GUI 侧刻意设计，因此**本模块禁止进入无头 registry/mcpModules 懒激活**——
// 懒实例化会毁掉无头面"零落盘"承诺。无头侧（MCP hanxi_clipboard_search）走
// internal/mcp 独立的只读装载器谱系（A3 tools_clipboard.go），不依赖本包与
// NewClipboardService，天然无此副作用；启用门照 memo 先例走 config.json 直读
// enabled+receipt，由主控收口执行。
func NewClipboardService(paths *settings.Paths, holder *extapi.LeaseHolder) (*ClipboardService, error) {
	dir := filepath.Join(paths.DataDir(), "clipboard")
	blobs := NewBlobStore(filepath.Join(dir, "blobs"))
	store, err := NewStore(dir, blobs, nil, nil)
	if err != nil {
		return nil, err
	}
	// 偏好装载（v1.8，构造只读不落盘）并一次性应用：容量闸走 ApplyLimits
	// 快路径（存量超限才动盘——上一轮调低的设置重启后不该复活越限条目，
	// 这条慢路径正好把它裁掉）；排除表活表 = 出厂 ∪ custom（v1.3.4 归
	// service 持有的承诺由设置面兑现）。
	prefs := newClipPrefsStore(paths.StateDir())
	if evicted, err := store.ApplyLimits(prefs.maxEntries(), prefs.maxBlobBytes()); err != nil {
		slog.Warn("clipboard: 启动应用偏好容量闸失败（按现行值继续，下次入库再触发淘汰）", "err", err)
	} else if len(evicted) > 0 {
		slog.Info("clipboard: 启动按偏好容量闸淘汰存量条目", "count", len(evicted))
	}
	svc := &ClipboardService{
		store:          store,
		holder:         holder,
		prefs:          prefs,
		writeText:      setSystemText,
		decodeDIB:      DecodeDIB,
		focusWin:       hw.SetForegroundForce,
		sendKeys:       sendCtrlV,
		waitForeground: waitForegroundWindow,
		excluded:       mergeExcludedTables(prefs.excludedCustom()),
	}
	// 遮蔽引擎一次性装配（v1.8；装载面已剔除坏正则，此处失败只可能是
	// 未知形态——warn 留痕按恒等运行，绝不装"以为遮了"的假安全）
	if eng, err := newMaskEngine(prefs.maskKinds(), prefs.maskCustomPatterns()); err == nil {
		svc.mask.Store(eng)
	} else {
		slog.Warn("clipboard: 遮蔽引擎装配失败，预览按明文渲染（修好偏好后重启生效）", "err", err)
	}
	return svc, nil
}

// excludedSnapshot 排除表活表快照（副本）：GetStatus 回显与设置页初值共用；
// 活表未配置时回落出厂默认，回显口径与入库判定严格一致。
func (s *ClipboardService) excludedSnapshot() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.excluded) == 0 {
		return DefaultExcludedExes()
	}
	return append([]string(nil), s.excluded...)
}

// matchExcludedSource 入库判定唯一通路：只读活表（s.mu 保护，与设置面写方
// 互斥）；活表未初始化（测试手装体/装配缺省）回落出厂默认——排除表是安全向
// 兜底，未配置一律取严不取宽。本包无包级同名判定路径，判定口径只有这一条。
func (s *ClipboardService) matchExcludedSource(exeLower string) bool {
	s.mu.Lock()
	table := s.excluded
	s.mu.Unlock()
	if len(table) == 0 {
		table = sensitiveDefaultUsers
	}
	return matchExcluded(exeLower, table)
}

// SetWailsApp 设置 Wails App 引用（装配布线 Go 直调路径，不接调用门，
// memo 同款纪律）。
func (s *ClipboardService) SetWailsApp(app *application.App) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.wailsApp = app
}

// ---------- 契约 §4 服务面（全部经统一调用门） ----------

// List 历史列表：q 小写子串匹配（Preview/SourceApp/Files/AutoTags 拼接串，
// 非正则），kind 空/"all" 不过滤，limit≤500；出口不含 Text/BlobData。
func (s *ClipboardService) List(q, kind string, limit int) ([]Entry, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return nil, gateErr
	}
	defer release()
	rows := s.store.List(q, kind, limit)
	// v1.8 预览遮蔽：store 吃明文匹配（搜索原文照样命中），出口统一遮形
	//（mask.go 头注"一条路遮全"；Get 正文不经此处，永不遮）
	if eng := s.mask.Load(); eng.enabled() {
		for i := range rows {
			rows[i].Preview = eng.apply(rows[i].Preview)
		}
	}
	return rows, nil
}

// Get 单条全文：含完整正文 Text 与图片 BlobData（PNG 原始字节），并记一次使用
// （useCount++/lastUsedAt）。blob 回读失败如实上浮（元数据页仍可走 List）。
func (s *ClipboardService) Get(id string) (Entry, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return Entry{}, gateErr
	}
	defer release()

	e, err := s.store.Touch(id, time.Now())
	if err != nil {
		return Entry{}, err
	}
	if e.Kind == KindImage && e.Blob != "" {
		data, lerr := s.store.blobs.Load(blobRelOf(e))
		if lerr != nil {
			return Entry{}, fmt.Errorf("读取图片内容失败: %w", lerr)
		}
		e.BlobData = data
	}
	return e, nil
}

// Set 回填系统剪贴板。首版仅支持 text（OpenClipboard 重试 ≤5 内含于写手）；
// image/file 返回可读的"暂不支持"错误——如实告知，不装成功。回填不记使用
// （使用口径只算 Get，契约 §4）。
func (s *ClipboardService) Set(id string) error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()

	e, ok := s.store.Get(id)
	if !ok {
		return fmt.Errorf("剪贴板条目不存在: %s", id)
	}
	switch e.Kind {
	case KindText:
		return s.writeText(e.Text)
	default:
		return fmt.Errorf("暂不支持将%s类条目回填系统剪贴板（首版仅支持文本，图片可在详情页另存、文件可右键复制路径）", kindCN(e.Kind))
	}
}

// CreateText 手建文本片段（manual=true，不淘汰；来源记 hanxi）。走与监听入库
// 同一去重/钳制/敏感判定通道，事件照常广播。
func (s *ClipboardService) CreateText(text string) (Entry, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return Entry{}, gateErr
	}
	defer release()

	t := strings.TrimSpace(text)
	if t == "" {
		return Entry{}, fmt.Errorf("片段内容不能为空")
	}
	now := time.Now()
	id, err := newEntryID()
	if err != nil {
		return Entry{}, err
	}
	e := Entry{
		ID:        id,
		Hash:      hashOf(KindText, t, nil, nil),
		Kind:      KindText,
		Text:      t,
		Preview:   previewOf(KindText, t, nil, 0, 0),
		ByteSize:  int64(len(t)),
		SourceApp: "hanxi",
		AutoTags:  AutoTags(t),
		Sensitive: looksSecret(t),
		Manual:    true,
		CreatedAt: now.UnixMilli(),
	}
	final, evicted, err := s.store.Add(e, now)
	if err != nil {
		return Entry{}, err
	}
	s.emitIngested(final, evicted)
	return final, nil
}

// TogglePin 翻转钉选（豁免 LRU 淘汰），返回更新后的完整拷贝。
func (s *ClipboardService) TogglePin(id string) (Entry, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return Entry{}, gateErr
	}
	defer release()
	return s.store.TogglePin(id)
}

// Delete 删单条（图片连带 blob，先删 blob 成功才动账本），广播 removed。
func (s *ClipboardService) Delete(id string) error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()

	if _, err := s.store.Delete(id); err != nil {
		return err
	}
	s.emit(evRemoved, RemovedPayload{ID: id})
	// v1.8 队列联动：条目没了，队内挂名同步摘除
	if s.queuePrune([]string{id}) {
		s.emitQueueChanged()
	}
	return nil
}

// ClearAll 历史与 blobs 全清（托盘一键擦除走这里）。
// 事件口径按契约 §5 括注由实现方标注：清空**不发**逐条 removed——数百个
// {id} 事件对前端是风暴，正确姿势是前端收到 ClearAll 成功后全量重拉（或经
// GetStatus 对账）；清空语义达成后逐条动画无意义。
func (s *ClipboardService) ClearAll() error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()
	if err := s.store.ClearAll(); err != nil {
		return err
	}
	// v1.8 队列联动：全清账本后队列必然尽成陈旧，直接清队广播（贴出时
	// peek 惰性跳是兜底，不是省掉显式收口的理由）
	if s.queueClearInner() {
		s.emitQueueChanged()
	}
	return nil
}

// SetPaused 暂停/恢复监听入库（历史仍可查；恢复后新复制即刻跟进）。
func (s *ClipboardService) SetPaused(paused bool) error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()

	s.mu.Lock()
	s.paused = paused
	s.mu.Unlock()
	s.emit(evPaused, PausedPayload{Paused: paused})
	return nil
}

// GetPrefs 用户偏好回显（契约 §12 v1.8）：返回**生效值**（钳制/规范化后的
// 实际运行态）而非请求原值——越界值保存后立即读回规范形，面板自然对齐。
func (s *ClipboardService) GetPrefs() (Prefs, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return Prefs{}, gateErr
	}
	defer release()
	return s.prefs.wirePrefs(), nil
}

// SetPrefs 整表校验并原子落盘，成功后**同一调用内热应用**（v1.8）：
// 写盘 → ApplyLimits（降容当场裁，静默不逐发 removed，前端保存成功后重拉）
// → 换排除表活表。失败（坏正则/超数量闸/落盘败）整表不动、错误如实上浮，
// 运行态保持旧值——"以为设了其实没设"不接受。QueueHotkey 域外：入参忽略、
// 现值保留（改键只走 SetQueueHotkey，校验即绑定事务在热键域）。
// mask 域同调用内经 rebuildMask 热换生效（坏正则已在 normalize 拒掉，不入库）。
func (s *ClipboardService) SetPrefs(in Prefs) (Prefs, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return Prefs{}, gateErr
	}
	defer release()

	in.QueueHotkey = s.prefs.queueHotkey() // 分域保护（见 Prefs 头注）
	applied, err := s.prefs.Set(in)
	if err != nil {
		return Prefs{}, err
	}
	if _, err := s.store.ApplyLimits(applied.MaxEntries, int64(applied.MaxBlobMB)<<20); err != nil {
		slog.Warn("clipboard: 偏好已落盘但容量淘汰执行失败（新限值已生效，下次入库重试）", "err", err)
	}
	s.mu.Lock()
	s.excluded = mergeExcludedTables(applied.ExcludedCustom)
	s.mu.Unlock()
	s.rebuildMask() // v1.8 遮蔽域热换（normalize 已验正则，此处编译失败沿用旧引擎）
	return s.prefs.wirePrefs(), nil
}

// GetStatus 运行态快照（监听暂停态、库容量实况、钳制上限与排除表回显）。
func (s *ClipboardService) GetStatus() (Status, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return Status{}, gateErr
	}
	defer release()

	s.mu.Lock()
	paused := s.paused
	s.mu.Unlock()
	blobBytes, err := s.store.BlobBytes()
	if err != nil {
		// 总量读不成实况归零但如实带错（前端状态区显示 "-"）
		return Status{}, fmt.Errorf("读取 blobs 总量失败: %w", err)
	}
	// 申报值即执行值：回显生效容量闸（v1.8 可调后，前端 clipStatusLine
	// "条目 x/上限" 的分母自动跟随，无需感知 prefs）。
	curMaxEntries, curMaxBlobBytes := s.store.CurrentLimits()
	return Status{
		Paused:       paused,
		EntryCount:   s.store.Count(),
		BlobBytes:    blobBytes,
		MaxEntries:   curMaxEntries,
		MaxBlobBytes: curMaxBlobBytes,
		// 排除表回显**活表实况**（v1.3.4：与入库判定同源，MCP 审计可复验）
		ExcludedExes: s.excludedSnapshot(),
		// v1.8 队列余数（状态行/「更多」菜单"清空队列"enabled 判据）
		QueueLen: s.queueLen(),
	}, nil
}

// ---------- 监听入库链（listener goroutine 直调，不属 RPC 面不接门） ----------

// handleCapture 捕获入库口：暂停/未启动即丢；排除表命中**不入库**；secret
// 启发式命中入库置 sensitive；单图 >16MiB 跳过不入库（如实 warn）。
func (s *ClipboardService) handleCapture(c capture) {
	s.mu.Lock()
	running := s.started && !s.paused
	s.mu.Unlock()
	if !running {
		return
	}
	if s.matchExcludedSource(c.sourceExe) {
		slog.Debug("clipboard: 来源命中排除表，内容不入库", "exe", c.sourceExe)
		return
	}

	switch c.kind {
	case KindText:
		s.ingestText(c)
	case KindImage:
		s.ingestImage(c)
	case KindFile:
		s.ingestFiles(c)
	}
}

func (s *ClipboardService) ingestText(c capture) {
	if strings.TrimSpace(c.text) == "" {
		return // 全空白复制纯属噪音，不入库
	}
	// v1.4.2 尺寸闸：单条 text >8MiB 拒入库（与图片 16MiB 同谱，warn 如实；
	// 整库 index.json 原子重写形态下，巨型文本会拖垮每次提交）
	if int64(len(c.text)) > maxTextBytes {
		slog.Warn("clipboard: 单条文本超过 8MiB 上限，跳过入库", "bytes", len(c.text),
			"source", c.sourceExe)
		return
	}
	e, err := s.newEntryBase(KindText, c)
	if err != nil {
		slog.Warn("clipboard: 文本条目装配失败", "err", err)
		return
	}
	e.Text = c.text
	e.Preview = previewOf(KindText, c.text, nil, 0, 0)
	e.ByteSize = int64(len(c.text))
	e.AutoTags = AutoTags(c.text)
	e.Sensitive = looksSecret(c.text)
	s.addAndBroadcast(e, c.at)
}

func (s *ClipboardService) ingestFiles(c capture) {
	e, err := s.newEntryBase(KindFile, c)
	if err != nil {
		slog.Warn("clipboard: 文件条目装配失败", "err", err)
		return
	}
	e.Files = c.files
	e.Preview = previewOf(KindFile, "", c.files, 0, 0)
	total := int64(0)
	for _, f := range c.files {
		total += int64(len(f))
	}
	e.ByteSize = total
	s.addAndBroadcast(e, c.at)
}

// ingestImage 图片链路（依赖 A6 件）：>16MiB 先拦（连解码都不做）→ hash 预查
// 命中即顶置（不重复解码/落盘）→ DecodeDIB → BlobStore.SaveImage（内容寻址幂等）。
func (s *ClipboardService) ingestImage(c capture) {
	if int64(len(c.dib)) > maxImageBytes {
		slog.Warn("clipboard: 单张图片超过 16MiB 上限，跳过入库", "bytes", len(c.dib),
			"source", c.sourceExe)
		return
	}
	hash := hashOf(KindImage, "", nil, c.dib)
	if existing, ok := s.store.FindHash(hash); ok {
		touched, err := s.store.Touch(existing.ID, c.at)
		if err != nil {
			slog.Warn("clipboard: 重复图片顶置记账失败", "id", existing.ID, "err", err)
			return
		}
		s.emitIngested(touched, nil)
		return
	}

	img, err := s.decodeDIB(c.dib)
	if err != nil {
		slog.Warn("clipboard: CF_DIB 解码失败，图片不入库", "err", err)
		return
	}
	rel, w, h, err := s.store.blobs.SaveImage(img)
	if err != nil {
		slog.Warn("clipboard: 图片内容落盘失败，不入库", "err", err)
		return
	}
	e, err := s.newEntryBase(KindImage, c)
	if err != nil {
		slog.Warn("clipboard: 图片条目装配失败", "err", err)
		return
	}
	e.Blob = blobRelPrefix + rel
	e.Width, e.Height = w, h
	e.Preview = previewOf(KindImage, "", nil, w, h)
	if png, lerr := s.store.blobs.Load(rel); lerr == nil {
		e.ByteSize = int64(len(png))
	} else {
		slog.Warn("clipboard: 图片字节数回读失败，ByteSize 以 DIB 原始大小记账", "err", lerr)
		e.ByteSize = int64(len(c.dib))
	}
	s.addAndBroadcast(e, c.at)
}

// newEntryBase 条目公共底：ID/Kind/Hash/来源/时间戳。Hash 统一在此按 capture
// 语义字节现算（契约 §2 口径唯一落点），调用方不再自行传入。
func (s *ClipboardService) newEntryBase(kind Kind, c capture) (Entry, error) {
	id, err := newEntryID()
	if err != nil {
		return Entry{}, err
	}
	return Entry{
		ID:        id,
		Hash:      hashOf(kind, c.text, c.files, c.dib),
		Kind:      kind,
		SourceApp: c.sourceApp,
		SourceExe: c.sourceExe,
		CreatedAt: c.at.UnixMilli(),
	}, nil
}

func (s *ClipboardService) addAndBroadcast(e Entry, at time.Time) {
	final, evicted, err := s.store.Add(e, at)
	if err != nil {
		slog.Error("clipboard: 条目入库落盘失败", "err", err)
		return
	}
	// v1.8 按应用保留清扫触发点之二（只跟**真正新增**的条目——去重顶置
	// 不空转；final.ID 与来条目不一致即命中去重）。与容量淘汰并进同一轮
	// removed 广播。
	if final.ID == e.ID {
		evicted = append(evicted, s.sweepExpired()...)
	}
	s.emitIngested(final, evicted)
}

// sweepExpired 按应用保留天数清扫（触发点共用；规则表空即零成本早退）。
// 失败 warn 不上浮——清扫是维护性动作，入库/启动主链不因它折损。
func (s *ClipboardService) sweepExpired() []Entry {
	days := s.prefs.appRetentionDays()
	if len(days) == 0 {
		return nil
	}
	victims, err := s.store.SweepExpired(time.Now(), days)
	if err != nil {
		slog.Warn("clipboard: 按应用保留清扫失败", "err", err)
		return nil
	}
	return victims
}

// emitIngested 广播 clipboard:updated（新条目与去重顶置同一事件，契约 §5），
// 再逐个补发被淘汰条目的 removed。载荷剥 Text/BlobData、Preview 截 200 rune。
// v1.8 队列联动：淘汰/清扫清单同步 prune 挂名条目（容量淘汰、体积淘汰、
// 按应用清扫三源都汇到这一口）。
func (s *ClipboardService) emitIngested(final Entry, evicted []Entry) {
	payload := cloneEntry(final, true)
	// v1.8 事件载荷与 List 出口同谱遮形（实时新条目不得裸奔明文 preview）
	if eng := s.mask.Load(); eng.enabled() {
		payload.Preview = eng.apply(payload.Preview)
	}
	s.emit(evUpdated, payload)
	ids := make([]string, 0, len(evicted))
	for _, v := range evicted {
		s.emit(evRemoved, RemovedPayload{ID: v.ID})
		ids = append(ids, v.ID)
	}
	if s.queuePrune(ids) {
		s.emitQueueChanged()
	}
}

// emitQueueChanged 队列全量快照广播（queue.go 簿记、本口发射——事件名 const
// 与 emit 调用同文件是装配根 composition contract 扫描器的识别前提）。
// 调用方须已释放 s.mu：emit 内部取 s.mu 读出口，嵌套即死锁（SetPaused 同谱）。
func (s *ClipboardService) emitQueueChanged() {
	s.emit(evQueueChanged, QueueChangedPayload{IDs: s.queueSnapshot()})
}

// ---------- 事件出口 ----------

// emit Wails Event.Emit 封装：测试缝优先；应用未运行（无头）静默丢弃并 debug
// 留痕——事件面无消费者可寻，丢弃是正确行为而非错误。
func (s *ClipboardService) emit(name string, payload any) {
	s.mu.Lock()
	sink, app := s.eventSink, s.wailsApp
	s.mu.Unlock()
	if sink != nil {
		sink(name, payload)
		return
	}
	if app != nil && app.Event != nil {
		app.Event.Emit(name, payload)
		return
	}
	if a := application.Get(); a != nil && a.Event != nil {
		a.Event.Emit(name, payload)
		return
	}
	slog.Debug("clipboard: 应用未运行，事件无出口", "event", name)
}

// kindCN 错误话术里的 Kind 中文名。
func kindCN(k Kind) string {
	switch k {
	case KindImage:
		return "图片"
	case KindFile:
		return "文件"
	default:
		return "文本"
	}
}
