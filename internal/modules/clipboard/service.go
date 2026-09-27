package clipboard

import (
	"fmt"
	"image"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hanxi/internal/extapi"
	"hanxi/internal/hotkey"
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

	mu       sync.Mutex
	started  bool
	paused   bool
	listener *clipboardListener
	wailsApp *application.App

	// excluded 敏感排除表**活表**（小写进程名；v1.3.4 裁决归 service 持有，
	// 收口阶段设置页可增删；构造缺省=出厂默认表。入库判定与 GetStatus 回显
	// 同读此表，审计面与执行面不分裂）。
	excluded []string

	// eventSink 事件出口测试缝：非 nil 时事件只进假件不碰 Wails（单测注入）。
	eventSink func(name string, payload any)

	// 系统写手与 DIB 解码缝（默认 setSystemText/DecodeDIB，测试注入假件；
	// DecodeDIB/BlobStore 由 A6 线落地，签名冻结见契约 §10）。
	writeText func(string) error
	decodeDIB func([]byte) (image.Image, error)

	// 浮层（overlay.go）与热键（hotkey.go）簿记：全部走 ovMu（memo sheetMu 同谱），
	// 与数据锁 store.mu、运行态锁 s.mu 互不相涉；Wails 窗口 API 一律锁外调用。
	ovMu      sync.Mutex
	ovWin     *application.WebviewWindow
	ovClosing func()
	ovShown   bool
	ovIdle    *time.Timer
	hk        *hotkey.Registry // 全仓通用热键注册器（装配根注入，未注入只降级不报错）
}

// 事件名常量（契约 §5/§6，前端 adapters/clipboard.ts CLIP_EV 逐字对位）。
// 模块标识 ID 见 module.go（契约 §12.1：ModuleID/目录键/授权键单一事实源）。
const (
	evUpdated = "clipboard:updated"
	evRemoved = "clipboard:removed"
	evPaused  = "clipboard:paused"
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
	return &ClipboardService{
		store:     store,
		holder:    holder,
		writeText: setSystemText,
		decodeDIB: DecodeDIB,
		excluded:  DefaultExcludedExes(),
	}, nil
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
	return s.store.List(q, kind, limit), nil
}

// Get 单条全文：含解密 Text 与图片 BlobData（PNG 原始字节），并记一次使用
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
	return s.store.ClearAll()
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
	return Status{
		Paused:       paused,
		EntryCount:   s.store.Count(),
		BlobBytes:    blobBytes,
		MaxEntries:   maxEntries,
		MaxBlobBytes: maxBlobBytes,
		// 排除表回显**活表实况**（v1.3.4：与入库判定同源，MCP 审计可复验）
		ExcludedExes: s.excludedSnapshot(),
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
	s.emitIngested(final, evicted)
}

// emitIngested 广播 clipboard:updated（新条目与去重顶置同一事件，契约 §5），
// 再逐个补发被淘汰条目的 removed。载荷剥 Text/BlobData、Preview 截 200 rune。
func (s *ClipboardService) emitIngested(final Entry, evicted []Entry) {
	s.emit(evUpdated, cloneEntry(final, true))
	for _, v := range evicted {
		s.emit(evRemoved, RemovedPayload{ID: v.ID})
	}
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
