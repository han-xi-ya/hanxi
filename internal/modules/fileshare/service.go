package fileshare

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"log/slog"

	"github.com/wailsapp/wails/v3/pkg/application"
	"hanxi/internal/extapi"
	"hanxi/internal/notify"
	"hanxi/internal/platform"
)

// defaultShareDirName 默认共享的宿主目录名（用户「下载」夹）。
// 与 wechat/wsl 两处同款就地字面量：等 Lane Y 的公共下载目录解析落地后再统一收口，
// 本模块不跨包引依赖。
const defaultShareDirName = "Downloads"

// FileShareService 面向 Wails 前端与核心调度的服务层
type FileShareService struct {
	plat         platform.Platform
	mu           sync.RWMutex
	lifecycleMu  sync.Mutex
	config       ShareConfig
	server       *Server
	wailsApp     *application.App
	onDropToMemo func(title, content string, tags []string) error
	// holder 统一调用门持有器（Wave 3）：RPC 导出版全部经 Enter() 入账；
	// server.go 的 HTTP 处理与投递回调不走 service 方法面，不接门。
	holder *extapi.LeaseHolder
}

// NewFileShareService 实例化服务
func NewFileShareService(plat platform.Platform, holder *extapi.LeaseHolder) *FileShareService {
	// 默认配置
	homeDir, _ := os.UserHomeDir()
	defaultShare := filepath.Join(homeDir, defaultShareDirName)
	if _, err := os.Stat(defaultShare); os.IsNotExist(err) {
		defaultShare = homeDir
	}

	return &FileShareService{
		plat:   plat,
		holder: holder,
		config: ShareConfig{
			Port:            DefaultSharePort,
			SharePath:       defaultShare,
			AllowUpload:     true,
			AllowTextDrop:   true,
			AutoSaveToMemo:  true,
			MaxUploadSizeMB: 0,
		},
	}
}

// SetWailsApp 设置 Wails App 引用以便发送事件。
// 装配布线: Go 直调路径,不得依赖运行态(见 ADR-0001 Wave 3 注记)——不接调用门。
func (s *FileShareService) SetWailsApp(app *application.App) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.wailsApp = app
}

// SetMemoHook 注册投递自动进入备忘录的钩子。
// 装配布线: Go 直调路径,不得依赖运行态(见 ADR-0001 Wave 3 注记)——不接调用门；
// 钩子本体是 memo.MemoService.QuickCreate（RPC 导出版带门）：memo 停用时投递
// 写入被门拒绝，回调侧如实记录并上浮通知，不静默吞。
func (s *FileShareService) SetMemoHook(hook func(title, content string, tags []string) error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onDropToMemo = hook
}

// GetConfig 获取当前配置
func (s *FileShareService) GetConfig() (ShareConfig, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return ShareConfig{}, gateErr
	}
	defer release()

	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.config, nil
}

// SaveConfig 保存并应用配置
func (s *FileShareService) SaveConfig(cfg ShareConfig) error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()

	s.mu.Lock()
	defer s.mu.Unlock()

	if cfg.SharePath == "" {
		return fmt.Errorf("共享目录不能为空")
	}
	if cfg.MaxUploadSizeMB < 0 {
		return fmt.Errorf("单文件上传上限不能小于 0")
	}
	info, err := os.Stat(cfg.SharePath)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("共享目录不存在或不可读: %s", cfg.SharePath)
	}

	s.config = cfg
	if s.server != nil {
		s.server.UpdateConfig(cfg)
		s.emitEvent("fileshare:status", s.getStatusLocked())
	}
	return nil
}

// StartServer 启动局域网快传服务
func (s *FileShareService) StartServer() (ServerStatus, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return ServerStatus{}, gateErr
	}
	defer release()

	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.server != nil && s.server.IsRunning() {
		return s.getStatusLocked(), nil
	}

	server := NewServer(s.config, func(item DropItem) {
		s.emitEvent("fileshare:text-dropped", item)
		notify.Info("fileshare", "收到文本投递", fmt.Sprintf("来自 %s: %s", item.SenderIP, item.Content), "/ext/fileshare")

		s.mu.RLock()
		autoSave := s.config.AutoSaveToMemo
		hook := s.onDropToMemo
		s.mu.RUnlock()

		if autoSave && hook != nil {
			title := fmt.Sprintf("来自 %s 的投递", item.SenderIP)
			if item.IsURL {
				title = fmt.Sprintf("网页收藏 (%s)", item.SenderIP)
			}
			// 钩子即 memo.MemoService.QuickCreate（带门导出版）：memo 停用/被拒时
			// 返回错误。投递文本本身已入投递箱可查，但写库失败不得静默吞——
			// 记日志并通知用户（可到投递箱手动导入便签）。
			if herr := hook(title, item.Content, []string{"#手机投递", "#Inbox"}); herr != nil {
				slog.Warn("fileshare: 投递自动写入便签失败", "err", herr)
				notify.Error(ID, "投递未写入便签",
					fmt.Sprintf("文本已收入投递箱，但自动存便签失败：%v", herr), "/ext/fileshare")
			}
		}
	}, func(evt TransferEvent) {
		s.emitEvent("fileshare:transfer", evt)
		if evt.Type == "upload" && evt.Success {
			notify.Success("fileshare", "文件上传完成", fmt.Sprintf("来自 %s: %s", evt.ClientIP, evt.Filename), "/ext/fileshare")
		}
		// 每次传输后同步推送最新状态，保证电脑端统计卡实时刷新
		s.mu.RLock()
		status := s.getStatusLocked()
		s.mu.RUnlock()
		s.emitEvent("fileshare:status", status)
	})

	port, err := server.Start()
	if err != nil {
		return ServerStatus{}, err
	}

	s.server = server
	s.config.Port = port

	status := s.getStatusLocked()
	s.emitEvent("fileshare:status", status)
	return status, nil
}

// StopServer 停止快传服务（RPC 导出版：接统一调用门）。
func (s *FileShareService) StopServer() error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()

	return s.stopServer()
}

// stopServer 停服的内部无门版：Module.OnDestroy 在 stopping 态必须无条件停服
// 释放监听端口，不能经门（门此时恒拒）。
func (s *FileShareService) stopServer() error {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()

	s.mu.Lock()
	server := s.server
	if server == nil {
		s.mu.Unlock()
		return nil
	}
	// 先摘除当前代，使回调和状态查询不再把正在停止的服务报告为运行中。
	s.server = nil
	s.mu.Unlock()

	err := server.Stop()

	s.mu.RLock()
	status := s.getStatusLocked()
	s.mu.RUnlock()
	s.emitEvent("fileshare:status", status)
	return err
}

// GetServerStatus 获取服务当前状态
func (s *FileShareService) GetServerStatus() (ServerStatus, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return ServerStatus{}, gateErr
	}
	defer release()

	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.getStatusLocked(), nil
}

// getStatusLocked 内部获取状态 (需在持有锁情况下调用)
func (s *FileShareService) getStatusLocked() ServerStatus {
	isRunning := s.server != nil && s.server.IsRunning()
	activeURLs := make([]string, 0)

	if isRunning {
		endpoints := s.getNetworkEndpointsInternal(s.config.Port)
		for _, ep := range endpoints {
			activeURLs = append(activeURLs, ep.URL)
		}
	}

	var activeConns, uploads, downloads, upBytes, downBytes int64
	var upRate, downRate float64
	var startedAt string
	if isRunning {
		activeConns = atomic.LoadInt64(&s.server.activeConnections)
		uploads = atomic.LoadInt64(&s.server.uploadCount)
		downloads = atomic.LoadInt64(&s.server.downloadCount)
		upBytes = atomic.LoadInt64(&s.server.upBytes)
		downBytes = atomic.LoadInt64(&s.server.downBytes)
		upRate, downRate = s.server.currentRates()
		startedAt = s.server.startedAt.Format(time.RFC3339)
	}

	return ServerStatus{
		IsRunning:         isRunning,
		Port:              s.config.Port,
		SharePath:         s.config.SharePath,
		AllowUpload:       s.config.AllowUpload,
		AllowTextDrop:     s.config.AllowTextDrop,
		AutoSaveToMemo:    s.config.AutoSaveToMemo,
		ActiveURLs:        activeURLs,
		ActiveConnections: activeConns,
		UploadCount:       uploads,
		DownloadCount:     downloads,
		UploadBytes:       upBytes,
		DownloadBytes:     downBytes,
		UploadRate:        upRate,
		DownloadRate:      downRate,
		StartedAt:         startedAt,
	}
}

// GetNetworkEndpoints 获取所有可访问的局域网接入点
func (s *FileShareService) GetNetworkEndpoints() ([]NetworkEndpoint, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return nil, gateErr
	}
	defer release()

	s.mu.RLock()
	port := s.config.Port
	s.mu.RUnlock()
	return s.getNetworkEndpointsInternal(port), nil
}

func (s *FileShareService) getNetworkEndpointsInternal(port int) []NetworkEndpoint {
	if s.plat == nil || s.plat.Network() == nil {
		return []NetworkEndpoint{{
			InterfaceName: "Localhost",
			IP:            "127.0.0.1",
			URL:           fmt.Sprintf("http://127.0.0.1:%d", port),
			IsDefault:     true,
		}}
	}

	adapters, err := s.plat.Network().Adapters()
	if err != nil {
		return []NetworkEndpoint{{
			InterfaceName: "Localhost",
			IP:            "127.0.0.1",
			URL:           fmt.Sprintf("http://127.0.0.1:%d", port),
			IsDefault:     true,
		}}
	}

	endpoints := make([]NetworkEndpoint, 0)
	for _, a := range adapters {
		if !a.IsUp || a.IsLoopback {
			continue
		}
		for _, ip := range a.IPv4 {
			if ip == "127.0.0.1" || ip == "0.0.0.0" {
				continue
			}
			endpoints = append(endpoints, NetworkEndpoint{
				InterfaceName: a.Name,
				IP:            ip,
				URL:           fmt.Sprintf("http://%s:%d", ip, port),
				IsDefault:     a.Gateway != "",
			})
		}
	}

	if len(endpoints) == 0 {
		endpoints = append(endpoints, NetworkEndpoint{
			InterfaceName: "Localhost",
			IP:            "127.0.0.1",
			URL:           fmt.Sprintf("http://127.0.0.1:%d", port),
			IsDefault:     true,
		})
	}

	return endpoints
}

// GetDropInbox 获取投递箱列表
func (s *FileShareService) GetDropInbox() ([]DropItem, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return nil, gateErr
	}
	defer release()

	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.server == nil {
		return []DropItem{}, nil
	}
	s.server.mu.RLock()
	defer s.server.mu.RUnlock()
	res := make([]DropItem, len(s.server.dropInbox))
	copy(res, s.server.dropInbox)
	return res, nil
}

// DeleteDropItem 删除单条投递记录
func (s *FileShareService) DeleteDropItem(id string) error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()

	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.server == nil {
		return nil
	}
	s.server.mu.Lock()
	defer s.server.mu.Unlock()

	filtered := make([]DropItem, 0, len(s.server.dropInbox))
	for _, item := range s.server.dropInbox {
		if item.ID != id {
			filtered = append(filtered, item)
		}
	}
	s.server.dropInbox = filtered
	return nil
}

// ClearDropInbox 清空投递箱
func (s *FileShareService) ClearDropInbox() error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()

	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.server == nil {
		return nil
	}
	s.server.mu.Lock()
	defer s.server.mu.Unlock()
	s.server.dropInbox = make([]DropItem, 0)
	return nil
}

// ChooseDirectory 调起操作系统原生目录选择对话框
func (s *FileShareService) ChooseDirectory() (string, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return "", gateErr
	}
	defer release()

	app := application.Get()
	if app == nil || app.Dialog == nil {
		return "", fmt.Errorf("系统对话框服务不可用")
	}

	dialog := app.Dialog.OpenFile().
		SetTitle("选择局域网共享根目录").
		CanChooseDirectories(true).
		CanChooseFiles(false)

	// 如果当前已有配置目录且有效，作为初始目录
	s.mu.RLock()
	curPath := s.config.SharePath
	s.mu.RUnlock()
	if curPath != "" {
		if info, err := os.Stat(curPath); err == nil && info.IsDir() {
			dialog.SetDirectory(curPath)
		}
	}

	selected, err := dialog.PromptForSingleSelection()
	if err != nil {
		return "", fmt.Errorf("选择目录失败: %w", err)
	}
	return selected, nil
}

func (s *FileShareService) emitEvent(name string, data any) {
	if s.wailsApp != nil && s.wailsApp.Event != nil {
		s.wailsApp.Event.Emit(name, data)
	} else if app := application.Get(); app != nil && app.Event != nil {
		app.Event.Emit(name, data)
	}
}
