package wechat

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"hubkit/internal/settings"
)

// WechatService 暴露给 Wails 前端的服务
type WechatService struct {
	client   *Client
	listener *Listener
	store    *settings.Store
}

func NewWechatService(store *settings.Store) *WechatService {
	cfg := store.GetWechatConfig()
	client := NewClient(cfg.BaseURL)
	listener := NewListener(client, store)

	// 如果应用启动时已存在登录凭据，自动拉起长轮询监听
	if cfg.BotToken != "" {
		go func() {
			_ = listener.Start()
		}()
	}

	return &WechatService{
		client:   client,
		listener: listener,
		store:    store,
	}
}

// GetState 获取当前模块状态
func (s *WechatService) GetState() WechatState {
	cfg := s.store.GetWechatConfig()
	return WechatState{
		IsLoggedIn:            cfg.BotToken != "",
		BotToken:              cfg.BotToken,
		IlinkBotID:            cfg.IlinkBotID,
		IlinkUserID:           cfg.IlinkUserID,
		ContextToken:          cfg.ContextToken,
		ContextTokenUpdatedAt: cfg.ContextTokenUpdatedAt,
		TargetUserID:          cfg.TargetUserID,
		IsListening:           s.listener.IsRunning(),
	}
}

// SaveConfig 保存用户配置 (支持手动配置目标用户 ID 或自定义 BaseURL)
func (s *WechatService) SaveConfig(cfg settings.WechatConfig) error {
	return s.store.SetWechatConfig(cfg)
}

// ClearCredentials 清除已保存的登录凭据
func (s *WechatService) ClearCredentials() error {
	s.listener.Stop()
	return s.store.SetWechatConfig(settings.WechatConfig{
		BaseURL: "https://ilinkai.weixin.qq.com",
	})
}

// GetLoginQRCode 获取微信登录二维码
func (s *WechatService) GetLoginQRCode() (*QRInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return s.client.FetchLoginQRCode(ctx)
}

// CheckQRStatus 轮询检测二维码状态
func (s *WechatService) CheckQRStatus(qrcode string) (*QRStatus, error) {
	qrcode = strings.TrimSpace(qrcode)
	if qrcode == "" {
		return nil, fmt.Errorf("qrcode 不能为空")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()

	res, err := s.client.PollQRStatus(ctx, qrcode)
	if err != nil {
		return nil, err
	}

	// 如果登录成功，自动持久化凭据并自动启动后台长轮询监听
	if res.Status == "confirmed" && res.BotToken != "" {
		_ = s.store.Update(func(c *settings.AppSettings) {
			c.Wechat.BotToken = res.BotToken
			c.Wechat.IlinkBotID = res.IlinkBotID
			c.Wechat.IlinkUserID = res.IlinkUserID
			if res.BaseURL != "" {
				c.Wechat.BaseURL = res.BaseURL
			}
		})
		// 登录成功后自动拉起后台监听，手机一发消息立即自动获取 ContextToken，免去手动刷新
		go func() {
			_ = s.listener.Start()
		}()
	}

	return res, nil
}

// RefreshContextToken 手动拉取一次 updates，尝试获取最新的 context_token
func (s *WechatService) RefreshContextToken() (string, error) {
	cfg := s.store.GetWechatConfig()
	if cfg.BotToken == "" {
		return "", fmt.Errorf("请先完成扫码登录")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	token, err := s.listener.FetchUpdatesOnce(ctx, cfg.BotToken)
	if err != nil {
		return "", err
	}
	if token == "" {
		return "", fmt.Errorf("未获取到 context_token，请确保手机微信已向机器人发送任意消息")
	}
	return token, nil
}

// SendTextMessage 发送文字消息
func (s *WechatService) SendTextMessage(toUserID, text string) error {
	cfg := s.store.GetWechatConfig()
	if cfg.BotToken == "" {
		return fmt.Errorf("未登录，请先扫码登录")
	}
	if cfg.ContextToken == "" {
		return fmt.Errorf("未获取到 context_token，请先在手机微信给机器人发一条消息以建立会话")
	}

	toUserID = strings.TrimSpace(toUserID)
	if toUserID == "" {
		toUserID = cfg.TargetUserID
	}
	if toUserID == "" {
		toUserID = cfg.IlinkUserID
	}
	if toUserID == "" {
		return fmt.Errorf("请提供目标用户 ID (To User ID)")
	}

	text = strings.TrimSpace(text)
	if text == "" {
		return fmt.Errorf("消息内容不能为空")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	return s.client.SendTextMessage(ctx, cfg.BotToken, cfg.ContextToken, toUserID, text)
}

// SendImageMessage 发送图片消息
func (s *WechatService) SendImageMessage(toUserID, filePath string) error {
	cfg := s.store.GetWechatConfig()
	if cfg.BotToken == "" {
		return fmt.Errorf("未登录，请先扫码登录")
	}
	if cfg.ContextToken == "" {
		return fmt.Errorf("未获取到 context_token，请先在手机微信给机器人发一条消息以建立会话")
	}

	toUserID = strings.TrimSpace(toUserID)
	if toUserID == "" {
		toUserID = cfg.TargetUserID
	}
	if toUserID == "" {
		toUserID = cfg.IlinkUserID
	}
	if toUserID == "" {
		return fmt.Errorf("请提供目标用户 ID (To User ID)")
	}

	filePath = strings.TrimSpace(filePath)
	if filePath == "" {
		return fmt.Errorf("图片文件路径不能为空")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	return s.client.SendImageMessage(ctx, cfg.BotToken, cfg.ContextToken, toUserID, filePath)
}

// SendFileMessage 发送文件消息
func (s *WechatService) SendFileMessage(toUserID, filePath string) error {
	cfg := s.store.GetWechatConfig()
	if cfg.BotToken == "" {
		return fmt.Errorf("未登录，请先扫码登录")
	}
	if cfg.ContextToken == "" {
		return fmt.Errorf("未获取到 context_token，请先在手机微信给机器人发一条消息以建立会话")
	}

	toUserID = strings.TrimSpace(toUserID)
	if toUserID == "" {
		toUserID = cfg.TargetUserID
	}
	if toUserID == "" {
		toUserID = cfg.IlinkUserID
	}
	if toUserID == "" {
		return fmt.Errorf("请提供目标用户 ID (To User ID)")
	}

	filePath = strings.TrimSpace(filePath)
	if filePath == "" {
		return fmt.Errorf("文件路径不能为空")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	return s.client.SendFileMessage(ctx, cfg.BotToken, cfg.ContextToken, toUserID, filePath)
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

// StartListener 启动后台实时监听
func (s *WechatService) StartListener() error {
	return s.listener.Start()
}

// StopListener 停止后台监听
func (s *WechatService) StopListener() bool {
	s.listener.Stop()
	return true
}
