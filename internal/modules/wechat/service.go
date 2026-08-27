package wechat

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"hubkit/internal/settings"
)

// WechatService 暴露给 Wails 前端的服务（支持多微信账号并发管理与收发路由）
type WechatService struct {
	defaultClient *Client
	listeners     map[string]*Listener
	mu            sync.RWMutex
	store         *settings.Store
}

func NewWechatService(store *settings.Store) *WechatService {
	cfg := store.GetWechatConfig()
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "https://ilinkai.weixin.qq.com"
	}
	defaultClient := NewClient(baseURL)

	svc := &WechatService{
		defaultClient: defaultClient,
		listeners:     make(map[string]*Listener),
		store:         store,
	}

	return svc
}

// getClientForAccount 获取或创建特定账号的 Client
func (s *WechatService) getClientForAccount(baseURL string) *Client {
	if baseURL == "" || baseURL == s.defaultClient.baseURL {
		return s.defaultClient
	}
	return NewClient(baseURL)
}

// InitOnDemand 按需懒初始化：用户进入页面或首次调用时拉起所有已配置账号的后台监听
func (s *WechatService) InitOnDemand() {
	accounts := s.store.GetWechatAccounts()
	for _, acc := range accounts {
		if acc.BotToken != "" {
			_ = s.StartAccountListener(acc.ID)
		}
	}
}

// Destroy 销毁模块并彻底停止所有后台监听 Goroutine
func (s *WechatService) Destroy() {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, l := range s.listeners {
		l.Stop()
	}
	s.listeners = make(map[string]*Listener)
}

// ListAccounts 获取所有账号及其运行时状态
func (s *WechatService) ListAccounts() []WechatAccountState {
	accounts := s.store.GetWechatAccounts()
	s.mu.RLock()
	defer s.mu.RUnlock()

	res := make([]WechatAccountState, 0, len(accounts))
	for _, acc := range accounts {
		isListening := false
		if l, ok := s.listeners[acc.ID]; ok {
			isListening = l.IsRunning()
		}
		res = append(res, WechatAccountState{
			ID:                    acc.ID,
			RemarkName:            acc.RemarkName,
			BotToken:              acc.BotToken,
			IlinkBotID:            acc.IlinkBotID,
			IlinkUserID:           acc.IlinkUserID,
			ContextToken:          acc.ContextToken,
			ContextTokenUpdatedAt: acc.ContextTokenUpdatedAt,
			TargetUserID:          acc.TargetUserID,
			BaseURL:               acc.BaseURL,
			CreatedAt:             acc.CreatedAt,
			IsListening:           isListening,
		})
	}
	return res
}

// GetState 获取全局/主账号运行时状态（兼容旧前端接口）
func (s *WechatService) GetState() WechatState {
	accounts := s.ListAccounts()
	var first WechatAccountState
	if len(accounts) > 0 {
		first = accounts[0]
	}

	return WechatState{
		IsLoggedIn:            first.BotToken != "",
		BotToken:              first.BotToken,
		IlinkBotID:            first.IlinkBotID,
		IlinkUserID:           first.IlinkUserID,
		ContextToken:          first.ContextToken,
		ContextTokenUpdatedAt: first.ContextTokenUpdatedAt,
		TargetUserID:          first.TargetUserID,
		IsListening:           first.IsListening,
		Accounts:              accounts,
	}
}

// UpdateAccount 更新账号基本信息（备注名、目标用户 ID、BaseURL）
func (s *WechatService) UpdateAccount(id, remarkName, targetUserID, baseURL string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("账号 ID 不能为空")
	}

	return s.store.Update(func(c *settings.AppSettings) {
		for i, acc := range c.WechatAccounts {
			if acc.ID == id {
				if remarkName != "" {
					c.WechatAccounts[i].RemarkName = strings.TrimSpace(remarkName)
				}
				c.WechatAccounts[i].TargetUserID = strings.TrimSpace(targetUserID)
				if baseURL != "" {
					c.WechatAccounts[i].BaseURL = strings.TrimSpace(baseURL)
				}
				break
			}
		}
	})
}

// DeleteAccount 删除指定微信账号并停止其长轮询
func (s *WechatService) DeleteAccount(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("账号 ID 不能为空")
	}

	s.StopAccountListener(id)
	return s.store.DeleteWechatAccount(id)
}

// StartAccountListener 启动指定账号的后台监听
func (s *WechatService) StartAccountListener(accountID string) error {
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return fmt.Errorf("账号 ID 不能为空")
	}

	acc, ok := s.store.GetWechatAccountByID(accountID)
	if !ok {
		return fmt.Errorf("未找到指定微信账号: %s", accountID)
	}
	if acc.BotToken == "" {
		return fmt.Errorf("该账号尚未绑定有效凭据")
	}

	s.mu.Lock()
	l, exists := s.listeners[accountID]
	if !exists {
		client := s.getClientForAccount(acc.BaseURL)
		l = NewListener(accountID, client, s.store)
		s.listeners[accountID] = l
	}
	s.mu.Unlock()

	return l.Start()
}

// StopAccountListener 停止指定账号的后台监听
func (s *WechatService) StopAccountListener(accountID string) bool {
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return false
	}

	s.mu.Lock()
	l, exists := s.listeners[accountID]
	s.mu.Unlock()

	if exists && l != nil {
		l.Stop()
		return true
	}
	return false
}

// GetLoginQRCode 获取微信登录二维码
func (s *WechatService) GetLoginQRCode() (*QRInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return s.defaultClient.FetchLoginQRCode(ctx)
}

// CheckQRStatus 轮询检测二维码状态（支持传入自定义备注名创建独立账号）
func (s *WechatService) CheckQRStatus(qrcode, remarkName string) (*QRStatus, error) {
	qrcode = strings.TrimSpace(qrcode)
	if qrcode == "" {
		return nil, fmt.Errorf("qrcode 不能为空")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()

	res, err := s.defaultClient.PollQRStatus(ctx, qrcode)
	if err != nil {
		return nil, err
	}

	// 如果登录成功，自动添加/更新多账号配置并启动监听
	if res.Status == "confirmed" && res.BotToken != "" {
		accountID := res.IlinkBotID
		if accountID == "" {
			accountID = fmt.Sprintf("wechat-%d", time.Now().Unix())
		}
		remark := strings.TrimSpace(remarkName)
		if remark == "" {
			if res.IlinkUserID != "" {
				remark = fmt.Sprintf("微信助手 (%s)", res.IlinkUserID)
			} else {
				remark = fmt.Sprintf("微信机器人 (%s)", accountID)
			}
		}

		acc := settings.WechatAccount{
			ID:           accountID,
			RemarkName:   remark,
			BotToken:     res.BotToken,
			IlinkBotID:   res.IlinkBotID,
			IlinkUserID:  res.IlinkUserID,
			TargetUserID: res.IlinkUserID, // 默认发给自身/扫码者
			BaseURL:      res.BaseURL,
			CreatedAt:    time.Now().Format("2006-01-02 15:04:05"),
		}

		_ = s.store.UpsertWechatAccount(acc)

		// 启动该账号监听
		go func() {
			_ = s.StartAccountListener(accountID)
		}()
	}

	return res, nil
}

// RefreshAccountContextToken 手动拉取指定账号的 updates
func (s *WechatService) RefreshAccountContextToken(accountID string) (string, error) {
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return "", fmt.Errorf("请指定要刷新的账号 ID")
	}

	acc, ok := s.store.GetWechatAccountByID(accountID)
	if !ok || acc.BotToken == "" {
		return "", fmt.Errorf("账号凭据无效或未找到")
	}

	s.mu.Lock()
	l, exists := s.listeners[accountID]
	if !exists {
		client := s.getClientForAccount(acc.BaseURL)
		l = NewListener(accountID, client, s.store)
		s.listeners[accountID] = l
	}
	s.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	token, err := l.FetchUpdatesOnce(ctx, acc.BotToken)
	if err != nil {
		return "", err
	}
	if token == "" {
		return "", fmt.Errorf("未获取到 context_token，请确保手机微信已向机器人发送任意消息")
	}
	return token, nil
}

// SendTextMessage 发送文字消息（指定账号与目标）
func (s *WechatService) SendTextMessage(accountID, toUserID, text string) error {
	accountID = strings.TrimSpace(accountID)
	var acc settings.WechatAccount
	if accountID != "" {
		var ok bool
		acc, ok = s.store.GetWechatAccountByID(accountID)
		if !ok {
			return fmt.Errorf("未找到指定微信账号: %s", accountID)
		}
	} else {
		accounts := s.store.GetWechatAccounts()
		if len(accounts) > 0 {
			acc = accounts[0]
		}
	}

	if acc.BotToken == "" {
		return fmt.Errorf("未登录，请先扫码绑定微信账号")
	}
	if acc.ContextToken == "" {
		return fmt.Errorf("未获取到 context_token，请先在手机微信给机器人发送一条任意消息以建立会话")
	}

	toUserID = strings.TrimSpace(toUserID)
	if toUserID == "" {
		toUserID = acc.TargetUserID
	}
	if toUserID == "" {
		toUserID = acc.IlinkUserID
	}
	if toUserID == "" {
		return fmt.Errorf("请提供目标用户 ID (To User ID)")
	}

	text = strings.TrimSpace(text)
	if text == "" {
		return fmt.Errorf("消息内容不能为空")
	}

	client := s.getClientForAccount(acc.BaseURL)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	return client.SendTextMessage(ctx, acc.BotToken, acc.ContextToken, toUserID, text)
}

// SendImageMessage 发送图片消息（指定账号与目标）
func (s *WechatService) SendImageMessage(accountID, toUserID, filePath string) error {
	accountID = strings.TrimSpace(accountID)
	var acc settings.WechatAccount
	if accountID != "" {
		var ok bool
		acc, ok = s.store.GetWechatAccountByID(accountID)
		if !ok {
			return fmt.Errorf("未找到指定微信账号: %s", accountID)
		}
	} else {
		accounts := s.store.GetWechatAccounts()
		if len(accounts) > 0 {
			acc = accounts[0]
		}
	}

	if acc.BotToken == "" {
		return fmt.Errorf("未登录，请先扫码绑定微信账号")
	}
	if acc.ContextToken == "" {
		return fmt.Errorf("未获取到 context_token，请先在手机微信给机器人发送一条任意消息以建立会话")
	}

	toUserID = strings.TrimSpace(toUserID)
	if toUserID == "" {
		toUserID = acc.TargetUserID
	}
	if toUserID == "" {
		toUserID = acc.IlinkUserID
	}
	if toUserID == "" {
		return fmt.Errorf("请提供目标用户 ID (To User ID)")
	}

	filePath = strings.TrimSpace(filePath)
	if filePath == "" {
		return fmt.Errorf("图片文件路径不能为空")
	}

	client := s.getClientForAccount(acc.BaseURL)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	return client.SendImageMessage(ctx, acc.BotToken, acc.ContextToken, toUserID, filePath)
}

// SendFileMessage 发送文件消息（指定账号与目标）
func (s *WechatService) SendFileMessage(accountID, toUserID, filePath string) error {
	accountID = strings.TrimSpace(accountID)
	var acc settings.WechatAccount
	if accountID != "" {
		var ok bool
		acc, ok = s.store.GetWechatAccountByID(accountID)
		if !ok {
			return fmt.Errorf("未找到指定微信账号: %s", accountID)
		}
	} else {
		accounts := s.store.GetWechatAccounts()
		if len(accounts) > 0 {
			acc = accounts[0]
		}
	}

	if acc.BotToken == "" {
		return fmt.Errorf("未登录，请先扫码绑定微信账号")
	}
	if acc.ContextToken == "" {
		return fmt.Errorf("未获取到 context_token，请先在手机微信给机器人发送一条任意消息以建立会话")
	}

	toUserID = strings.TrimSpace(toUserID)
	if toUserID == "" {
		toUserID = acc.TargetUserID
	}
	if toUserID == "" {
		toUserID = acc.IlinkUserID
	}
	if toUserID == "" {
		return fmt.Errorf("请提供目标用户 ID (To User ID)")
	}

	filePath = strings.TrimSpace(filePath)
	if filePath == "" {
		return fmt.Errorf("文件路径不能为空")
	}

	client := s.getClientForAccount(acc.BaseURL)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	return client.SendFileMessage(ctx, acc.BotToken, acc.ContextToken, toUserID, filePath)
}

// PickImageDialog 打开系统原生文件选择对话框选择图片并返回真实绝对路径
func (s *WechatService) PickImageDialog() (string, error) {
	app := application.Get()
	if app == nil {
		return "", fmt.Errorf("application instance not available")
	}

	dialog := app.Dialog.OpenFile()
	dialog.SetTitle("选择要发送的图片")
	dialog.AddFilter("图片文件 (*.png;*.jpg;*.jpeg;*.gif;*.bmp;*.webp)", "*.png;*.jpg;*.jpeg;*.gif;*.bmp;*.webp")
	dialog.AddFilter("所有文件 (*.*)", "*.*")

	filePath, err := dialog.PromptForSingleSelection()
	if err != nil {
		return "", err
	}
	return filePath, nil
}

// PickFileDialog 打开系统原生文件选择对话框选择任意文件并返回真实绝对路径
func (s *WechatService) PickFileDialog() (string, error) {
	app := application.Get()
	if app == nil {
		return "", fmt.Errorf("application instance not available")
	}

	dialog := app.Dialog.OpenFile()
	dialog.SetTitle("选择要发送的文件")
	dialog.AddFilter("所有文件 (*.*)", "*.*")

	filePath, err := dialog.PromptForSingleSelection()
	if err != nil {
		return "", err
	}
	return filePath, nil
}

// StartListener 启动主账号后台实时监听（遗留兼容接口）
func (s *WechatService) StartListener() error {
	accounts := s.store.GetWechatAccounts()
	if len(accounts) == 0 {
		return fmt.Errorf("暂无微信账号，请先扫码绑定")
	}
	return s.StartAccountListener(accounts[0].ID)
}

// StopListener 停止主账号后台监听（遗留兼容接口）
func (s *WechatService) StopListener() bool {
	accounts := s.store.GetWechatAccounts()
	if len(accounts) == 0 {
		return false
	}
	return s.StopAccountListener(accounts[0].ID)
}

// RefreshContextToken 刷新主账号 Context Token（遗留兼容接口）
func (s *WechatService) RefreshContextToken() (string, error) {
	accounts := s.store.GetWechatAccounts()
	if len(accounts) == 0 {
		return "", fmt.Errorf("暂无微信账号，请先扫码绑定")
	}
	return s.RefreshAccountContextToken(accounts[0].ID)
}

// GetPendingMessages 取走指定账号后台积累的未读消息（消费后清空，供前端页面重新挂载时补取）
func (s *WechatService) GetPendingMessages(accountID string) []InboundMessage {
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return nil
	}
	s.mu.RLock()
	l, ok := s.listeners[accountID]
	s.mu.RUnlock()
	if !ok || l == nil {
		return nil
	}
	return l.DrainMsgBuf()
}
