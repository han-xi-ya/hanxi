package ocr

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/wailsapp/wails/v3/pkg/application"

	"hanxi/internal/history"
	"hanxi/internal/modules/ocr/instance"
	"hanxi/internal/modules/ocr/snip"
	"hanxi/internal/notify"
	"hanxi/internal/platform"
	"hanxi/internal/settings"
)

const (
	watchInterval = 5 * time.Second // 服务状态探测周期（与前端兜底轮询同频）

	statusTimeout    = 2 * time.Second  // /api/status 探测
	recognizeTimeout = 35 * time.Second // 上游引擎超时 30s + 余量
	shutdownTimeout  = 5 * time.Second  // 优雅退出尝试
	portReleaseWait  = 3 * time.Second

	cacheFreshWindow = 15 * time.Second // 探测缓存视为新鲜的窗口

	// maxPasteImageBytes 对齐上游 /api/ocr/upload 的 64MB 硬上限。
	maxPasteImageBytes = 64 << 20
	// maxPreviewBytes 预览 dataURL 上限（超限不给预览，识别本身不受影响）。
	maxPreviewBytes = 24 << 20
)

// OcrService 向前端暴露 hanxi-ocr 本地服务的托管启停、状态探测与图片识别转发。
// 定位边界：识别能力全部在上游服务内，本服务只做"探活 + 转发 + 生命周期"，
// 不重复实现上游功能面（与 ddnsgo 托管口径一致）。
type OcrService struct {
	plat    platform.Platform
	store   *ocrStore
	engine  *instance.Engine
	client  *http.Client // 回环专用：Proxy 显式置 nil，防系统代理污染（netx 教训）
	exeDir  string       // Hanxi 主程序目录（同级 ../hanxi-ocr、../hanxi-ocr-paddle 自动发现锚点）
	dataDir string       // 数据目录（ocr-engines/ PP-OCR 引擎主发现锚点）
	tmpDir  string       // 粘贴/拖拽图片落盘目录 RuntimeDir()/ocr
	snip    snip.Snipper // 框选截屏识别原语（测试可打桩）

	history         *history.Store // 统一历史记录（nil=未接线，静默不记）
	historyFullText func() bool    // Q1 档位：识别全文是否入库（装配根注入，nil 视为开）

	watchMu   sync.Mutex
	watching  bool
	watchStop chan struct{}

	snipMu   sync.Mutex // 截屏识别防重入（TryLock 语义见 SnipAndRecognize）
	cardMu   sync.Mutex // 悬浮卡窗口与最近结果
	card     *application.WebviewWindow
	lastSnip SnipResult

	cardDragMu   sync.Mutex    // 悬浮卡拖拽会话：同一时刻至多一个跟手 goroutine
	cardDragStop chan struct{} // 非 nil 表示拖拽进行中

	hotkeyMu      sync.Mutex        // 保护热键绑定通道注入（装配根写、RPC 读）
	hotkeyBinding SnipHotkeyBinding // 剪贴板识图热键落实通道（未注入 = 纯配置读写）

	mu        sync.Mutex
	probe     probeCache
	lastEmit  ServiceState // 上一次广播的状态（去重防抖）
	lastPaste string       // 最近一次粘贴落盘文件（新粘贴替换删除，目录不膨胀）
}

// probeCache /api/status 最近一次探测结果缓存。
// engine / engineMode 为上游如实上报的引擎标识（双引擎契约 v0.4，计划 §3.1），
// 前端据此显示当前引擎型号。
type probeCache struct {
	online        bool
	version       string
	engine        string
	engineMode    string
	engineError   string // 上游可选 status.error：仅引擎带病（如 paddle 自检失败）时非空
	engineRunning bool
	hung          bool
	checkedAt     time.Time
}

func NewOcrService(plat platform.Platform) *OcrService {
	paths := settings.GetPaths()
	svc := &OcrService{
		plat:   plat,
		store:  newOcrStore(paths.StateDir()),
		client: &http.Client{Transport: &http.Transport{Proxy: nil}}, // 超时走 per-call ctx
		exeDir: exeDirOf(),
		// dataDir 是 ocr-engines/ 引擎组件的发现锚（二进制目录，非状态文件，
		// 不随 state/ 收纳走），保持指数据根。
		dataDir: paths.DataDir(),
		tmpDir:  filepath.Join(paths.RuntimeDir(), "ocr"),
		snip:    snip.New(),
	}
	svc.engine = instance.NewEngine(plat.Job(), instance.NewProbe(), instance.Callbacks{
		OnState: svc.emitInstanceState,
	})
	return svc
}

func exeDirOf() string {
	if exe, err := os.Executable(); err == nil {
		return filepath.Dir(exe)
	}
	return "."
}

// SetHistory 注入统一历史存储与"全文入库"档位读取器（装配根接线，
// 照 memo↔fileshare SetMemoHook 先例）。fullText 为 config.json 开关的实时读取
// 闭包（Q1：默认开=全文入库；关=只记图片路径与摘要）；nil 视为开。
func (s *OcrService) SetHistory(h *history.Store, fullText func() bool) {
	s.history = h
	s.historyFullText = fullText
}

// ---------- 状态模型与事件 ----------

// addr 当前设定服务地址。
func (s *OcrService) addr() string { return fmt.Sprintf("127.0.0.1:%d", s.store.GetListenPort()) }

// resolveEngineExe 按指定引擎解析组件路径（登记件为空时走各自自动发现锚点）。
func (s *OcrService) resolveEngineExe(id string) (path string, fromStore bool, err error) {
	return resolveServiceExe(s.exeDir, s.dataDir, id, s.store.GetEnginePath(id))
}

// resolveActiveExe 按当前活跃引擎解析组件路径——启停与截屏拉起的唯一取径
// （计划 §5.4：单活语义下所有拉起目标恒等于 active 引擎的解析结果）。
func (s *OcrService) resolveActiveExe() (path string, fromStore bool, err error) {
	return s.resolveEngineExe(s.store.GetActiveEngine())
}

// buildState 引擎快照 + 探测缓存合并为前端状态模型。
func (s *OcrService) buildState(snap instance.Snapshot) ServiceState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buildStateLocked(snap)
}

func (s *OcrService) buildStateLocked(snap instance.Snapshot) ServiceState {
	managed := snap.State == instance.StateRunning
	fresh := time.Since(s.probe.checkedAt) <= cacheFreshWindow
	online := managed || (fresh && s.probe.online)
	st := ServiceState{
		State:      string(snap.State),
		Online:     online,
		Managed:    managed,
		External:   snap.External,
		PID:        snap.PID,
		ListenAddr: s.addr(),
		Version:    s.probe.version,
		Engine:     s.probe.engine,
		EngineMode: s.probe.engineMode,
		EngineID:   s.store.GetActiveEngine(),
		Error:      snap.Error,
	}
	if managed && snap.ListenAddr != "" {
		st.ListenAddr = snap.ListenAddr
	}
	if fresh || managed {
		st.EngineRunning = s.probe.engineRunning
		st.EngineError = s.probe.engineError
		st.Hung = s.probe.hung
		if !s.probe.checkedAt.IsZero() {
			st.CheckedAt = s.probe.checkedAt.Format("2006-01-02 15:04:05")
		}
	}
	// ExePath/ExeAuto 恒描述活跃引擎（未安装时 ExePath 留空，原因由 GetEngines 给）
	active := s.store.GetActiveEngine()
	if exe, _, err := s.resolveEngineExe(active); err == nil {
		st.ExePath = exe
	}
	st.ExeAuto = strings.TrimSpace(s.store.GetEnginePath(active)) == ""
	return st
}

// emitInstanceState 引擎状态迁移 → 事件 ocr:service-state；failed 附带桌面通知。
func (s *OcrService) emitInstanceState(snap instance.Snapshot) {
	slog.Debug("ocr instance state", "state", snap.State, "pid", snap.PID, "external", snap.External)
	if snap.State == instance.StateFailed && snap.Error != "" {
		notify.Error("ocr", "hanxi-ocr 服务异常", snap.Error, "/ext/ocr")
	}
	s.mu.Lock()
	st := s.buildStateLocked(snap)
	s.mu.Unlock()
	s.emitStateIfChanged(st)
}

func (s *OcrService) emitStateIfChanged(st ServiceState) {
	s.mu.Lock()
	changed := st != s.lastEmit
	if changed {
		s.lastEmit = st
	}
	s.mu.Unlock()
	if changed {
		if app := application.Get(); app != nil && app.Event != nil {
			app.Event.Emit("ocr:service-state", st)
		}
	}
}

// activate 启动后台状态感知：5s 周期刷新外部实例 + 契约探测。
func (s *OcrService) activate() {
	// 粘贴临时目录：开机清扫上次崩溃残留，重建干净目录
	_ = os.RemoveAll(s.tmpDir)
	_ = os.MkdirAll(s.tmpDir, 0755)

	s.watchMu.Lock()
	defer s.watchMu.Unlock()
	if s.watching {
		return
	}
	s.watching = true
	stop := make(chan struct{})
	s.watchStop = stop
	go func() {
		t := time.NewTicker(watchInterval)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				s.refresh()
			}
		}
	}()
}

// Shutdown 模块销毁：停 watch；随退联动开启时强杀托管实例（限时通道，
// 不走上游优雅退出——OnDestroy 必须快速返回，照 ddnsgo 决策）。
func (s *OcrService) Shutdown() {
	s.watchMu.Lock()
	if s.watchStop != nil {
		close(s.watchStop)
		s.watchStop = nil
	}
	s.watching = false
	s.watchMu.Unlock()

	if s.store.GetFollowOnExit() {
		_ = s.engine.Stop()
	}
}

// refresh 一次完整状态刷新（探测 + external 校正 + 事件广播）。
func (s *OcrService) refresh() {
	addr := s.addr()
	s.engine.RefreshExternal(addr)
	s.probeStatus(addr)
	s.emitStateIfChanged(s.buildState(s.engine.Snapshot()))
}

// probeStatus GET /api/status 更新探测缓存。name 判别按契约名集合放宽（计划 §5.1：
// 微信版与 PP-OCR 开源版同名 "hanxi-ocr"）；engine / engine_mode 收进缓存并经
// ServiceState 透传给前端显示当前引擎。
func (s *OcrService) probeStatus(addr string) probeCache {
	ctx, cancel := context.WithTimeout(context.Background(), statusTimeout)
	defer cancel()

	var out probeCache
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+"/api/status", nil)
	if err == nil {
		resp, e := s.client.Do(req)
		if e == nil {
			defer resp.Body.Close()
			var v struct {
				OK            bool   `json:"ok"`
				Name          string `json:"name"`
				Version       string `json:"version"`
				Engine        string `json:"engine"`
				EngineMode    string `json:"engine_mode"`
				EngineError   string `json:"error"` // 双引擎契约 v0.4 可选字段（微信件无此字段，缺省空串）
				EngineRunning bool   `json:"engine_running"`
				EngineHung    bool   `json:"engine_hung"`
			}
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
			if resp.StatusCode == http.StatusOK && json.Unmarshal(body, &v) == nil && v.OK && isContractName(v.Name) {
				out = probeCache{
					online: true, version: v.Version, engine: v.Engine, engineMode: v.EngineMode,
					engineError: v.EngineError, engineRunning: v.EngineRunning, hung: v.EngineHung, checkedAt: time.Now(),
				}
			}
		}
	}
	s.mu.Lock()
	s.probe = out
	s.mu.Unlock()
	// 在线实例（托管或外部）的版本回写活跃引擎注册表（同值静默跳过，探测周期
	// 不产生写盘放大）；引擎未在线不动登记值，避免把离线猜测当事实。
	if out.online && out.version != "" {
		switch s.engine.Snapshot().State {
		case instance.StateRunning, instance.StateExternal:
			_ = s.store.SetEngineVersion(s.store.GetActiveEngine(), out.version)
		}
	}
	return out
}

// ---------- 前端 API：状态与启停 ----------

// GetStatus 返回合并状态（先做一次同步探测，弥补轮询间隙与首帧）。
func (s *OcrService) GetStatus() (ServiceState, error) {
	s.refresh()
	return s.buildState(s.engine.Snapshot()), nil
}

// StartService 冷启动托管实例（external 占位转为幂等说明而非报错）。
func (s *OcrService) StartService() (ControlOutcome, error) {
	snap := s.engine.Snapshot()
	switch snap.State {
	case instance.StateRunning:
		return ControlOutcome{Action: "already-running", Message: "服务已在运行（Hanxi 托管）"}, nil
	case instance.StateStarting:
		return ControlOutcome{Action: "starting", Message: "服务启动中，请稍候"}, nil
	case instance.StateExternal:
		return ControlOutcome{Action: "external", External: true,
			Message: "外部 hanxi-ocr 已在服务，识别照常、启停不接管"}, nil
	}

	exe, _, err := s.resolveActiveExe() // 拉起目标恒为活跃引擎解析结果（计划 §5.4）
	if err != nil {
		return ControlOutcome{}, err
	}
	addr := s.addr()
	if err := s.engine.Start(instance.StartOptions{
		Exe: exe, ListenAddr: addr,
		Detached: !s.store.GetFollowOnExit(),
	}); err != nil {
		if snap2 := s.engine.Snapshot(); snap2.State == instance.StateExternal {
			return ControlOutcome{Action: "external", External: true, Message: err.Error()}, nil
		}
		return ControlOutcome{}, err // failed 已由引擎广播 + notify
	}
	notify.Success("ocr", "识别服务已启动", "hanxi-ocr 正在监听 "+addr, "/ext/ocr")
	return ControlOutcome{Action: "started", Message: "hanxi-ocr 服务已启动（" + addr + "）"}, nil
}

// StopService 停止托管实例：优先上游优雅退出（POST /api/shutdown），兜底强杀；
// external 不越权（进程归属不在本引擎）。
func (s *OcrService) StopService() (ControlOutcome, error) {
	snap := s.engine.Snapshot()
	switch snap.State {
	case instance.StateStopped:
		return ControlOutcome{Action: "already-stopped", Message: "服务未在运行"}, nil
	case instance.StateExternal:
		return ControlOutcome{Action: "external-unmanaged", External: true,
			Message: "当前是外部自行启动的实例，请在其运行环境中退出（Hanxi 不接管外部进程）"}, nil
	}

	addr := s.addr()
	s.gracefulShutdown(addr)
	if !s.waitAddrReleased(addr, portReleaseWait) {
		if err := s.engine.Stop(); err != nil {
			return ControlOutcome{}, err
		}
	} else {
		_ = s.engine.Stop() // 幂等收敛状态（进程已自退，等 wait() 收尾）
	}
	s.refresh()
	return ControlOutcome{Action: "stopped", Message: "hanxi-ocr 服务已停止"}, nil
}

// gracefulShutdown 尽力调用优雅退出端点；一切失败静默（后续兜底强杀接管）。
func (s *OcrService) gracefulShutdown(addr string) {
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+addr+"/api/shutdown", nil)
	if err != nil {
		return
	}
	if resp, e := s.client.Do(req); e == nil {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<10))
		_ = resp.Body.Close()
	}
}

// waitAddrReleased 轮询直至端口不可连（进程真正退场）。
func (s *OcrService) waitAddrReleased(addr string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if !s.engine.PortOpen(addr) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(150 * time.Millisecond)
	}
}

// Logs 返回最近 n 行托管实例输出（排障）。
func (s *OcrService) Logs(n int) ([]string, error) {
	if n <= 0 {
		n = 100
	}
	if n > instance.LogCapacityHint {
		n = instance.LogCapacityHint
	}
	return s.engine.Logs(n), nil
}

// ---------- 前端 API：识别转发 ----------

// RecognizeImage 转发图片路径给上游识别。一切业务失败折进 Outcome.Error
// （中文人话），error 通道留给程序性错误。
// 统一历史：识别动作的唯一记录点在 recognizeImage 的 defer 单点（成功与失败同记），
// 截屏链路经 snip 来源标记复用同一记录点，勿二处插。
func (s *OcrService) RecognizeImage(path string) (OcrOutcome, error) {
	return s.recognizeImage(path, "ui")
}

func (s *OcrService) recognizeImage(path string, source string) (result OcrOutcome, err error) {
	defer func() { s.recordHistory(path, source, result, err) }()
	var empty OcrOutcome
	p := strings.TrimSpace(path)
	if p == "" {
		return OcrOutcome{Error: "图片路径不能为空"}, nil
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return OcrOutcome{Error: "路径解析失败: " + err.Error()}, nil
	}
	st, err := os.Stat(abs)
	if err != nil || st.IsDir() {
		return OcrOutcome{Error: "图片不存在: " + abs}, nil
	}
	addr := s.addr()
	body, err := json.Marshal(map[string]string{"path": abs})
	if err != nil {
		return empty, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), recognizeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+addr+"/api/ocr", bytes.NewReader(body))
	if err != nil {
		return empty, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return OcrOutcome{Error: fmt.Sprintf("无法连接 hanxi-ocr 服务（%s），请先启动服务", addr)}, nil
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	var v struct {
		Ok        bool   `json:"ok"`
		ElapsedMs int64  `json:"elapsed_ms"`
		Text      string `json:"text"`
		Error     string `json:"error"`
		Lines     []struct {
			Text string  `json:"text"`
			X    float64 `json:"x"`
			Y    float64 `json:"y"`
		} `json:"lines"`
	}
	if e := json.Unmarshal(raw, &v); e != nil {
		return OcrOutcome{Error: fmt.Sprintf("服务返回无法解析（HTTP %d）", resp.StatusCode)}, nil
	}
	if !v.Ok || resp.StatusCode >= 400 {
		msg := v.Error
		if msg == "" {
			msg = fmt.Sprintf("服务返回异常（HTTP %d）", resp.StatusCode)
		}
		return OcrOutcome{Error: msg}, nil
	}
	out := OcrOutcome{Ok: true, Text: v.Text, ElapsedMs: v.ElapsedMs}
	for _, l := range v.Lines {
		out.Lines = append(out.Lines, OcrLine{Text: l.Text, X: int(l.X), Y: int(l.Y)})
	}
	return out, nil
}

// recordHistory 识别动作统一历史落点（defer 单点，成败同记）。
// Q1 档位：全文开关关闭时只记图片路径与摘要、不存识别文本（截图常含聊天记录，
// 隐私风险面收在这一个读取点）；空路径的入参拒绝不入库，防非法调用刷桶。
// Save 失败仅静默（历史是尽力而为的副作用，公共包已记日志）。
func (s *OcrService) recordHistory(path, source string, result OcrOutcome, err error) {
	if s.history == nil || strings.TrimSpace(path) == "" {
		return
	}
	base := filepath.Base(strings.TrimSpace(path))
	rec := history.Record{FuncType: ID, Input: path, Extra: source}
	if err == nil && result.Ok {
		n := utf8.RuneCountInString(result.Text)
		if s.fullTextOn() {
			rec.Summary = fmt.Sprintf("识别 %s → %d 字", base, n)
			rec.Output = result.Text
		} else {
			rec.Summary = fmt.Sprintf("识别 %s → %d 字（全文未记录）", base, n)
			rec.Extra += "|nofull"
		}
	} else {
		msg := result.Error
		if msg == "" && err != nil {
			msg = err.Error()
		}
		rec.Summary = "识别失败 · " + base
		rec.Output = msg
		rec.Extra += "|fail"
	}
	_ = s.history.Save(rec)
}

// fullTextOn 全文档位实时读取（未注入读取器视为开，对齐 Q1 默认全文）。
func (s *OcrService) fullTextOn() bool {
	return s.historyFullText == nil || s.historyFullText()
}

// ---------- 前端 API：图片三通道 ----------

// PickImageDialog 打开系统图片选择对话框；取消返回空串。
func (s *OcrService) PickImageDialog() (string, error) {
	app := application.Get()
	if app == nil {
		return "", fmt.Errorf("应用实例不可用")
	}
	dialog := app.Dialog.OpenFile()
	dialog.SetTitle("选择要识别的图片")
	dialog.AddFilter("图片 (*.png *.jpg *.jpeg *.bmp *.webp *.gif *.tif *.tiff)", "*.png;*.jpg;*.jpeg;*.bmp;*.webp;*.gif;*.tif;*.tiff")
	dialog.AddFilter("所有文件 (*.*)", "*.*")
	return dialog.PromptForSingleSelection()
}

// InspectImage 校验对话框所选图片并生成预览（不落盘、temporary=false）。
func (s *OcrService) InspectImage(path string) (ImageRef, error) {
	p := strings.TrimSpace(path)
	if p == "" {
		return ImageRef{}, fmt.Errorf("图片路径不能为空")
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return ImageRef{}, err
	}
	st, err := os.Stat(abs)
	if err != nil || st.IsDir() {
		return ImageRef{}, fmt.Errorf("图片不存在: %s", abs)
	}
	ref := ImageRef{Path: abs, Name: filepath.Base(abs), Size: st.Size()}
	if ext := strings.ToLower(filepath.Ext(abs)); !imgExtOK[ext] {
		return ImageRef{}, fmt.Errorf("不支持的图片格式 %s（支持 PNG/JPG/BMP/WEBP/GIF/TIFF）", ext)
	}
	if ref.Size <= maxPreviewBytes {
		if data, e := os.ReadFile(abs); e == nil {
			ref.PreviewURL = buildDataURL(mimeForExt(filepath.Ext(abs)), data)
		}
	}
	return ref, nil
}

// SavePastedImage 拖拽/粘贴通道：File→dataURL 解码落盘 RuntimeDir()/ocr，
// 返回统一 ImageRef（temporary=true）。替换删除上一张粘贴临时件，目录不膨胀。
func (s *OcrService) SavePastedImage(fileName, dataURL string) (ImageRef, error) {
	data, mime, err := decodeDataURL(dataURL)
	if err != nil {
		return ImageRef{}, err
	}
	if int64(len(data)) > maxPasteImageBytes {
		return ImageRef{}, fmt.Errorf("图片超过 %d MB 上限，请改用「选择图片」按路径识别", maxPasteImageBytes>>20)
	}
	ext, ok := imageExtForMIME(mime)
	if !ok {
		return ImageRef{}, fmt.Errorf("剪贴板内容不是支持的图片格式 (%s)", mime)
	}
	base := sanitizeImageName(fileName)
	if base == "" {
		base = "粘贴图片_" + time.Now().Format("20060102_150405")
	}
	if err := os.MkdirAll(s.tmpDir, 0755); err != nil {
		return ImageRef{}, fmt.Errorf("创建临时目录失败: %w", err)
	}
	path := filepath.Join(s.tmpDir, fmt.Sprintf("%d-%s%s", time.Now().UnixNano(), base, ext))
	if err := os.WriteFile(path, data, 0644); err != nil {
		return ImageRef{}, fmt.Errorf("写入临时图片失败: %w", err)
	}

	s.mu.Lock()
	old := s.lastPaste
	s.lastPaste = path
	s.mu.Unlock()
	if old != "" && old != path {
		_ = os.Remove(old) // 只删自家临时件
	}

	ref := ImageRef{Path: path, Name: filepath.Base(path), Size: int64(len(data)), Temporary: true}
	if int64(len(data)) <= maxPreviewBytes {
		ref.PreviewURL = buildDataURL(mime, data)
	}
	return ref, nil
}

// ---------- 前端 API：设置项 ----------

// GetServiceExePath 返回当前生效的服务程序路径（活跃引擎的自动发现结果或登记件）。
func (s *OcrService) GetServiceExePath() (string, error) {
	exe, _, err := s.resolveActiveExe()
	return exe, err
}

// SetServiceExePath 设定活跃引擎的登记路径；""=恢复自动发现。返回当前生效值。
// （双引擎口径：本方法作用于 active 引擎注册件；另一引擎的登记走导入分流。）
func (s *OcrService) SetServiceExePath(path string) (string, error) {
	if err := s.store.SetEnginePath(s.store.GetActiveEngine(), path); err != nil {
		return "", err
	}
	return s.GetServiceExePath()
}

// BrowseServiceExeDialog 打开系统对话框挑选 hanxi-ocr.exe。
func (s *OcrService) BrowseServiceExeDialog() (string, error) {
	app := application.Get()
	if app == nil {
		return "", fmt.Errorf("应用实例不可用")
	}
	dialog := app.Dialog.OpenFile()
	dialog.SetTitle("选择 hanxi-ocr.exe")
	dialog.AddFilter("hanxi-ocr 服务 (*.exe)", "*.exe")
	return dialog.PromptForSingleSelection()
}

// GetListenPort 服务端口（默认 53120）。
func (s *OcrService) GetListenPort() (int, error) { return s.store.GetListenPort(), nil }

// SetListenPort 设定端口并落盘；托管实例运行中返回 pending（下次启动生效）。
func (s *OcrService) SetListenPort(port int) (string, error) {
	if err := s.store.SetListenPort(port); err != nil {
		return "", err
	}
	switch s.engine.Snapshot().State {
	case instance.StateRunning, instance.StateStarting:
		return "pending", nil
	}
	s.refresh()
	return "applied", nil
}

// GetFollowOnExit 返回"随 Hanxi 退出一起关闭"开关。
func (s *OcrService) GetFollowOnExit() (bool, error) { return s.store.GetFollowOnExit(), nil }

// GetAutoCopy 返回「截屏识别后自动复制文字」开关（默认 true）。
func (s *OcrService) GetAutoCopy() (bool, error) { return s.store.GetAutoCopy(), nil }

// SetAutoCopy 设定自动复制开关（下一次截屏识别起生效）。
func (s *OcrService) SetAutoCopy(v bool) error { return s.store.SetAutoCopy(v) }

// SetFollowOnExit 设定开关（已运行实例的联动在下一次启动时生效）。
func (s *OcrService) SetFollowOnExit(v bool) error { return s.store.SetFollowOnExit(v) }
