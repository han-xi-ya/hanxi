// Package webapp 内置模块：网页应用窗口。
// 用户维护一组网址条目（出厂预置微信文件传输助手网页版），点击即以独立
// WebviewWindow 打开外部 URL（ddnsgo 控制台窗形态）；X 关闭即真销毁，
// "收起"隐藏驻留 + 空闲 TTL 真销毁（quickmenu 踩坑 #53 范式），登录 cookie
// 落共享 WebView2 user data folder，销毁不丢登录。
// 每条网址动态映射为一个 TrayCommand，经既有托盘/轮盘条目配置直达。
package webapp

import (
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"hanxi/internal/extapi"
	"hanxi/internal/platform/windows"
	"hanxi/internal/settings"
)

// 网页窗几何与驻留常量：外观对齐 ddnsgo 控制台窗（ddnsgo/service.go:29-35），
// TTL 对齐 quickmenu 弹窗驻留策略（quickmenu/service.go:29）。
const (
	defaultWindowWidth  = 1120
	defaultWindowHeight = 820
	minWindowWidth      = 760
	minWindowHeight     = 520
	collapseIdleTTL     = 5 * time.Minute
	windowNamePrefix    = "webapp-"
)

// winHandle 单条目存活窗登记：显隐状态机字段 shown 供锁内判定，
// 绝不在此持锁调 IsVisible/Show/Hide/Close（主线程 InvokeSync 锁反转，踩坑 #53）。
type winHandle struct {
	win   *application.WebviewWindow
	shown bool        // true=可见；false=收起隐藏驻留（ttl 在期）
	ttl   *time.Timer // 收起后武装的空闲销毁计时
}

// WebAppService 暴露给 Wails 前端的服务：条目 CRUD 与开窗/收起编排。
type WebAppService struct {
	store *settings.Store
	// openURL "用默认浏览器打开"通道，构造注入便于测试；生产为 rundll32 FileProtocolHandler。
	openURL func(string) error

	// mu 护窗口表；铁律：持锁期间禁止任何 InvokeSync 系窗口调用
	// （Show/Hide/Close/Size/Position），UI 一律锁外执行（踩坑 #53）。
	mu   sync.Mutex
	wins map[string]*winHandle
	// holder 统一调用门持有器（Wave 3）：全部 RPC 导出版经 Enter() 入账，
	// 停用后不得再开/收起网页窗；shutdown/destroy/handleClosed 等生命周期与
	// 窗体事件路径走内部无门版。
	holder *extapi.LeaseHolder
}

// NewWebAppService 创建服务；openURL 传 nil 回退系统默认浏览器实现。构造无网络 IO、无窗口操作。
func NewWebAppService(store *settings.Store, openURL func(string) error, holder *extapi.LeaseHolder) *WebAppService {
	if openURL == nil {
		openURL = windows.OpenURL
	}
	return &WebAppService{store: store, openURL: openURL, wins: make(map[string]*winHandle), holder: holder}
}

// ListEntries 返回全部条目（保持配置顺序）及其运行时窗态。
func (s *WebAppService) ListEntries() ([]WebAppEntryView, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return nil, gateErr
	}
	defer release()

	entries := s.store.GetWebAppEntries()

	s.mu.Lock()
	defer s.mu.Unlock()
	views := make([]WebAppEntryView, 0, len(entries))
	for _, e := range entries {
		v := WebAppEntryView{
			ID:        e.ID,
			Name:      e.Name,
			URL:       e.URL,
			Icon:      e.Icon,
			CreatedAt: e.CreatedAt,
		}
		if h, ok := s.wins[e.ID]; ok {
			v.WindowOpen = h.shown
			v.WindowHidden = !h.shown
		}
		views = append(views, v)
	}
	return views, nil
}

// SaveEntry 新增或更新条目：entryID 为空即新建（服务端定 ID），
// 非空必须命中现有条目（防前端拿着已删 ID 复活幽灵条目）。
// 名称/URL 闸门不过返回用户可读错误；成功返回定稿条目 ID。
func (s *WebAppService) SaveEntry(entryID, name, rawURL, icon string) (string, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return "", gateErr
	}
	defer release()

	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("名称不能为空")
	}
	if len([]rune(name)) > 40 {
		return "", fmt.Errorf("名称过长（上限 40 字）")
	}

	cleanURL, err := validateURL(rawURL)
	if err != nil {
		return "", err
	}
	icon = strings.TrimSpace(icon)

	if entryID == "" {
		entryID = newEntryID()
	} else if _, ok := s.store.GetWebAppEntryByID(entryID); !ok {
		return "", fmt.Errorf("条目不存在或已被删除")
	}

	entry, ok := s.store.GetWebAppEntryByID(entryID)
	if !ok {
		entry = settings.WebAppEntry{ID: entryID}
	}
	entry.Name = name
	entry.URL = cleanURL
	entry.Icon = icon
	if err := s.store.UpsertWebAppEntry(entry); err != nil {
		return "", err
	}

	// 存活窗即时跟新标题（建窗后 Title 恒定格、不随页面漂移，改名只有服务层能推）。
	// 与 Open 占位回填存在良性竞态：改名抢在回填前则 SetTitle 落空（live=nil），
	// 随后建窗以 Store 最新名定格，殊途同归；URL 编辑不导航存活窗，
	// 新地址自下次开窗起生效（有意语义，与 Collapse 复用不刷新同口径）。
	s.mu.Lock()
	var live *application.WebviewWindow
	if h, ok := s.wins[entryID]; ok {
		live = h.win
	}
	s.mu.Unlock()
	if live != nil {
		live.SetTitle(name)
	}
	return entryID, nil
}

// DeleteEntry 删除条目（Store 层同步清理托盘/轮盘死引用），存活窗连带真销毁。
func (s *WebAppService) DeleteEntry(entryID string) error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()

	entryID = strings.TrimSpace(entryID)
	if entryID == "" {
		return fmt.Errorf("条目 ID 不能为空")
	}
	if _, ok := s.store.GetWebAppEntryByID(entryID); !ok {
		return fmt.Errorf("条目不存在或已被删除")
	}
	if err := s.store.DeleteWebAppEntry(entryID); err != nil {
		return err
	}
	s.destroy(entryID, nil, true)
	return nil
}

// OpenExternal 用系统默认浏览器打开条目地址（不进内嵌窗，适合临时跳外链）。
func (s *WebAppService) OpenExternal(entryID string) error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()

	entryID = strings.TrimSpace(entryID)
	entry, ok := s.store.GetWebAppEntryByID(entryID)
	if !ok {
		return fmt.Errorf("条目不存在或已被删除")
	}
	return s.openURL(entry.URL)
}

// ---------- 窗体核心 ----------

// Open 打开（或置顶/恢复）指定条目的网页窗。幂等三态：
// 可见→仅置顶；收起驻留→取消 TTL 热复用秒显；无窗→按需新建。
// 登录 cookie 在共享 WebView2 user data folder，真销毁重建也不丢网页会话。
// 导出版接门：停用/未安装模块不得开出网页窗；托盘/轮盘命令经 registry 派发链
// 先持同模块租约，此处二次入账（计数器语义）不冲突。
func (s *WebAppService) Open(entryID string) error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()

	entryID = strings.TrimSpace(entryID)
	if entryID == "" {
		return fmt.Errorf("条目 ID 不能为空")
	}

	s.mu.Lock()
	h, exists := s.wins[entryID]
	if exists && h.win == nil {
		// 另一调用正在建窗（占位中），窗口稍后自现
		s.mu.Unlock()
		return nil
	}
	if exists {
		if h.ttl != nil {
			h.ttl.Stop()
			h.ttl = nil
		}
		h.shown = true
		win := h.win
		s.mu.Unlock()
		win.Show()
		win.Focus()
		s.emitChanged()
		return nil
	}
	// 先落占位句柄（win=nil）挡住并发重复建窗，建好后回填/失败时回滚
	s.wins[entryID] = &winHandle{}
	s.mu.Unlock()

	entry, ok := s.store.GetWebAppEntryByID(entryID)
	if !ok {
		s.dropPlaceholder(entryID)
		return fmt.Errorf("条目不存在或已被删除")
	}
	app := application.Get()
	if app == nil || app.Window == nil {
		s.dropPlaceholder(entryID)
		return fmt.Errorf("网页窗口需要在应用内打开，请重试")
	}

	win := s.createWindow(app, entry)

	s.mu.Lock()
	if cur, ok := s.wins[entryID]; ok && cur.win == nil {
		cur.win = win
		cur.shown = true
		s.mu.Unlock()
		win.Show()
		win.Focus()
		s.emitChanged()
		return nil
	}
	// 建窗期间条目被 DeleteEntry/shutdown 摘除：弃建（Close 无 Cancel hook，直达真销毁）。
	// Close 走 InvokeSync，须留 goroutine：OnDestroy 在 shutdown 主线程执行时，
	// 若此弃建窗恰好还挂在主线程手里，同线程阻塞等待自身队列会僵住退出链（#56 语境）。
	s.mu.Unlock()
	go win.Close()
	return nil
}

// Collapse 收起条目窗口：Hide + 武装空闲 TTL，到期真销毁归还 WebView2 内存。
// 无窗/已收起时幂等无操作。
func (s *WebAppService) Collapse(entryID string) error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()

	return s.collapse(entryID)
}

// collapse 收起的内部无门版：供带门 Collapse 与 CollapseAll 复用（同链不二次入账）。
func (s *WebAppService) collapse(entryID string) error {
	entryID = strings.TrimSpace(entryID)

	s.mu.Lock()
	h, ok := s.wins[entryID]
	if !ok || h.win == nil || !h.shown {
		s.mu.Unlock()
		return nil
	}
	if h.ttl != nil {
		h.ttl.Stop()
		h.ttl = nil
	}
	h.shown = false
	win := h.win
	s.mu.Unlock()

	// 尺寸/坐标回写必须在 Hide 前后都持窗存活期内拉取（锁外 InvokeSync）
	s.rememberGeometry(entryID, win)
	win.Hide()

	s.mu.Lock()
	if cur, ok := s.wins[entryID]; ok && cur == h && !cur.shown {
		dead := h
		cur.ttl = time.AfterFunc(collapseIdleTTL, func() { s.destroy(entryID, dead, false) })
	}
	s.mu.Unlock()
	s.emitChanged()
	return nil
}

// CollapseAll 收起全部可见网页窗（各自进入 TTL 驻留）。
// 返回 error 恒为 nil：全仓 Wails 服务先例（Dismiss 等）以 error 收尾
// 保持绑定面一致，前端 await 契约不因后续演进突变。
func (s *WebAppService) CollapseAll() error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()

	s.mu.Lock()
	ids := make([]string, 0, len(s.wins))
	for id, h := range s.wins {
		if h.win != nil && h.shown {
			ids = append(ids, id)
		}
	}
	s.mu.Unlock()

	for _, id := range ids {
		_ = s.collapse(id)
	}
	return nil
}

// shutdown 真销毁全部存活窗（模块停用/应用退出），不论显隐。
func (s *WebAppService) shutdown() {
	s.mu.Lock()
	ids := make([]string, 0, len(s.wins))
	for id := range s.wins {
		ids = append(ids, id)
	}
	s.mu.Unlock()

	for _, id := range ids {
		s.destroy(id, nil, true)
	}
}

// createWindow 按条目建外部 URL 子窗。语义要点：
//   - 不注册任何 WindowClosing Cancel hook——X 关闭直达 Wails 内部真销毁
//     默认路径（beta.10 判据与安全性见 TROUBLESHOOTING #56）；
//   - 销毁收尾挂 OnWindowEvent（Wails 以 goroutine 异步派发监听器），
//     回调内只拿 s.mu 摘表，绝不触 UI；
//   - 标题恒定格为条目名（beta.10 无页面 title 同步通路）；
//   - 未记忆坐标时保持零值 WindowCentered 让新窗自动居中。
func (s *WebAppService) createWindow(app *application.App, entry settings.WebAppEntry) *application.WebviewWindow {
	width, height := entry.Width, entry.Height
	if width < minWindowWidth {
		width = defaultWindowWidth
	}
	if height < minWindowHeight {
		height = defaultWindowHeight
	}

	options := application.WebviewWindowOptions{
		Name:             windowNamePrefix + entry.ID,
		Title:            entry.Name,
		Width:            width,
		Height:           height,
		MinWidth:         minWindowWidth,
		MinHeight:        minWindowHeight,
		URL:              entry.URL,
		BackgroundColour: application.NewRGB(245, 246, 248),
	}
	if entry.X != 0 || entry.Y != 0 {
		options.InitialPosition = application.WindowXY
		options.X = entry.X
		options.Y = entry.Y
	}

	win := app.Window.NewWithOptions(options)
	id := entry.ID
	win.OnWindowEvent(events.Common.WindowClosing, func(*application.WindowEvent) {
		s.handleClosed(id, win)
	})
	return win
}

// handleClosed 原生 X 关闭与程序化 Close 共用的收尾监听：指针比对认领自己的
// 句柄（重建竞态下旧监听不误杀新窗），只做摘表与事件广播。
func (s *WebAppService) handleClosed(entryID string, win *application.WebviewWindow) {
	s.mu.Lock()
	h, ok := s.wins[entryID]
	if !ok || h.win != win {
		s.mu.Unlock()
		return
	}
	if h.ttl != nil {
		h.ttl.Stop()
	}
	delete(s.wins, entryID)
	s.mu.Unlock()
	s.emitChanged()
}

// destroy 真销毁条目窗口。expect 非 nil 时仅命中该句柄才动手（TTL 回调防误杀
// 重建窗）；shown 且非 force 时跳过——用户在 TTL 到期前又打开了窗口。
func (s *WebAppService) destroy(entryID string, expect *winHandle, force bool) {
	s.mu.Lock()
	h, ok := s.wins[entryID]
	if !ok || (expect != nil && h != expect) {
		s.mu.Unlock()
		return
	}
	if h.shown && !force {
		s.mu.Unlock()
		return
	}
	if h.ttl != nil {
		h.ttl.Stop()
	}
	delete(s.wins, entryID)
	s.mu.Unlock()

	if h.win != nil {
		s.rememberGeometry(entryID, h.win)
		// 无 Cancel hook：emit 后即走内部真销毁（#56）。Close/内部销毁响应器
		// （InvokeSync markAsDestroyed+impl.close，非主线程即排队等主循环）一律
		// 挪 goroutine 执行：本方法有 shutdown 调用点，可能正处于主线程 OnShutdown
		// 序列内，锁外同步 Close 会阻塞退出链数秒甚至更久。
		win := h.win
		go win.Close()
	}
	s.emitChanged()
}

// rememberGeometry 把窗体当前几何回写条目（下次开窗沿用）。两条静默跳过：
// 已销毁窗 Size 返 0（beta.10 isDestroyed 守门，不可恢复）；条目已删不得复活。
func (s *WebAppService) rememberGeometry(entryID string, win *application.WebviewWindow) {
	width, height := win.Size()
	if width <= 0 || height <= 0 {
		return
	}
	x, y := win.Position()

	entry, ok := s.store.GetWebAppEntryByID(entryID)
	if !ok {
		return
	}
	if entry.Width == width && entry.Height == height && entry.X == x && entry.Y == y {
		return
	}
	entry.Width, entry.Height, entry.X, entry.Y = width, height, x, y
	if err := s.store.UpsertWebAppEntry(entry); err != nil {
		slog.Warn("failed to persist webapp window geometry", "entryId", entryID, "err", err)
	}
}

// dropPlaceholder 回滚建窗失败时留下的占位句柄。
func (s *WebAppService) dropPlaceholder(entryID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if h, ok := s.wins[entryID]; ok && h.win == nil {
		delete(s.wins, entryID)
	}
}

// emitChanged 广播窗态变化（前端列表重拉）。无载荷事件，注册必须 Void。
func (s *WebAppService) emitChanged() {
	if app := application.Get(); app != nil && app.Event != nil {
		app.Event.Emit("webapp:windows-changed")
	}
}

// validateURL 网址闸门：仅放行 http/https 且带主机的绝对地址。
// javascript:/file:/data: 等协议一律拒绝——条目会以独立窗口加载，
// 放行危险 scheme 等于给配置面开本地执行旁路。
func validateURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("网址不能为空")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("网址格式无效: %v", err)
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", fmt.Errorf("仅支持 http/https 网址（需带协议前缀，如 https://）")
	}
	if u.Host == "" {
		return "", fmt.Errorf("网址缺少主机名")
	}
	return u.String(), nil
}

// newEntryID 生成条目 ID：前缀 + UnixNano。与 memo/portscan 同谱（Nano 粒度
// 避免同秒批量导入相撞），出厂预置条目用固定 ID 保证托盘引用跨装机稳定。
func newEntryID() string {
	return fmt.Sprintf("webapp_%d", time.Now().UnixNano())
}
