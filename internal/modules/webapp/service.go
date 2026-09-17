// Package webapp 内置模块：网页应用窗口。
// 用户维护一组网址条目（出厂预置微信文件传输助手网页版），点击即以独立
// WebviewWindow 打开外部 URL（ddnsgo 控制台窗形态）；X 关闭即真销毁，
// "收起"隐藏驻留 + 空闲 TTL 真销毁（quickmenu 踩坑 #53 范式），登录 cookie
// 落共享 WebView2 user data folder，销毁不丢登录。
// 每条网址动态映射为一个 TrayCommand，经既有托盘/轮盘条目配置直达。
package webapp

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"hanxi/internal/platform/windows"
	"hanxi/internal/settings"
)

// WebAppService 暴露给 Wails 前端的服务：条目 CRUD 与开窗/收起编排。
type WebAppService struct {
	store *settings.Store
	// openURL "用默认浏览器打开"通道，构造注入便于测试；生产为 rundll32 FileProtocolHandler。
	openURL func(string) error
}

// NewWebAppService 创建服务；openURL 传 nil 回退系统默认浏览器实现。构造无网络 IO、无窗口操作。
func NewWebAppService(store *settings.Store, openURL func(string) error) *WebAppService {
	if openURL == nil {
		openURL = windows.OpenURL
	}
	return &WebAppService{store: store, openURL: openURL}
}

// ListEntries 返回全部条目（保持配置顺序）及其运行时窗态。
func (s *WebAppService) ListEntries() []WebAppEntryView {
	entries := s.store.GetWebAppEntries()
	views := make([]WebAppEntryView, 0, len(entries))
	for _, e := range entries {
		views = append(views, WebAppEntryView{
			ID:        e.ID,
			Name:      e.Name,
			URL:       e.URL,
			Icon:      e.Icon,
			CreatedAt: e.CreatedAt,
		})
	}
	return views
}

// SaveEntry 新增或更新条目：entryID 为空即新建（服务端定 ID），
// 非空必须命中现有条目（防前端拿着已删 ID 复活幽灵条目）。
// 名称/URL 闸门不过返回用户可读错误；成功返回定稿条目 ID。
func (s *WebAppService) SaveEntry(entryID, name, rawURL, icon string) (string, error) {
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
	return entryID, nil
}

// DeleteEntry 删除条目（Store 层同步清理托盘/轮盘死引用）。
func (s *WebAppService) DeleteEntry(entryID string) error {
	entryID = strings.TrimSpace(entryID)
	if entryID == "" {
		return fmt.Errorf("条目 ID 不能为空")
	}
	if _, ok := s.store.GetWebAppEntryByID(entryID); !ok {
		return fmt.Errorf("条目不存在或已被删除")
	}
	return s.store.DeleteWebAppEntry(entryID)
}

// OpenExternal 用系统默认浏览器打开条目地址（不进内嵌窗，适合临时跳外链）。
func (s *WebAppService) OpenExternal(entryID string) error {
	entryID = strings.TrimSpace(entryID)
	entry, ok := s.store.GetWebAppEntryByID(entryID)
	if !ok {
		return fmt.Errorf("条目不存在或已被删除")
	}
	return s.openURL(entry.URL)
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
