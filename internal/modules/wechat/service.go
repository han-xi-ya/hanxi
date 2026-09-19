package wechat

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"hanxi/internal/extapi"
	"hanxi/internal/settings"
)

// 各外呼通道的超时（一处收口，与前端按钮态对齐；改值须同时确认腾讯侧长轮询上限）。
const (
	qrCodeFetchTimeout  = 15 * time.Second  // 取登录二维码
	qrStatusPollTimeout = 40 * time.Second  // 扫码状态长轮询（含用户掏手机的等待）
	contextTokenTimeout = 30 * time.Second  // 手动拉取一轮 updates
	sendTextTimeout     = 15 * time.Second  // 发送文字
	sendImageTimeout    = 60 * time.Second  // 发送图片（含上传 CDN）
	sendFileTimeout     = 120 * time.Second // 发送文件（含上传 CDN）
)

// WechatService 暴露给 Wails 前端的服务（支持多微信账号并发管理与收发路由）
type WechatService struct {
	defaultClient *Client
	// clients 按 baseURL 缓存自定义网关的 Client：每个 Client 自带连接池（http.Transport），
	// 每次现建会让 keep-alive 连接与 TLS 会话在每次调用后作废。
	// 用独立锁而非 s.mu：StartAccountListener/RefreshAccountContextToken 在持有 s.mu 时取用
	// 客户端，复用同一把锁会自死锁；s.mu 护监听器表、clientMu 护客户端表，不存在反向嵌套。
	clientMu    sync.Mutex
	clients     map[string]*Client
	listeners   map[string]*Listener
	attachments *attachmentStore
	mu          sync.RWMutex
	store       *settings.Store
	// holder 统一调用门持有器（Wave 3）：RPC 导出版经 Enter() 入账；
	// 监听回调链（Listener）与生命周期内部版（initOnDemand/destroy/
	// startAccountListener/stopAccountListener/listAccounts）不接门。
	holder *extapi.LeaseHolder
}

// NewWechatService 创建服务并按遗留单账号配置选定 baseURL；各账号的 Listener 懒创建（首次登录/启动监听时）。
// 构造无网络 IO。
func NewWechatService(store *settings.Store, holder *extapi.LeaseHolder) *WechatService {
	// 配置为空一律回落官方端点：设置页已不再预置默认值（清空即"用官方地址"），
	// 这里的兜底是唯一防线，绝不允许把空串拼进请求 URL。
	baseURL := store.GetWechatConfig().BaseURL
	if baseURL == "" {
		baseURL = defaultBaseURL
	}

	return &WechatService{
		defaultClient: NewClient(baseURL),
		clients:       make(map[string]*Client),
		listeners:     make(map[string]*Listener),
		attachments:   newAttachmentStore(),
		store:         store,
		holder:        holder,
	}
}

// getClientForAccount 取用特定 baseURL 的 Client（官方端点复用 defaultClient，
// 自定义网关按地址缓存）。空地址一律归一到默认端点。
func (s *WechatService) getClientForAccount(baseURL string) *Client {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" || baseURL == s.defaultClient.baseURL {
		return s.defaultClient
	}

	s.clientMu.Lock()
	defer s.clientMu.Unlock()
	if c, ok := s.clients[baseURL]; ok {
		return c
	}
	c := NewClient(baseURL)
	s.clients[baseURL] = c
	return c
}

// InitOnDemand 按需懒初始化（RPC 导出版：接统一调用门）。
func (s *WechatService) InitOnDemand() error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()

	s.initOnDemand()
	return nil
}

// initOnDemand 生命周期内部无门版：Module.OnInit 在 ensureActive 期间调用，
// 此刻门必然拒绝（initialized 未置真），必须直连。
func (s *WechatService) initOnDemand() {
	accounts := s.store.GetWechatAccounts()
	for _, acc := range accounts {
		if acc.BotToken != "" {
			_ = s.startAccountListener(acc.ID)
		}
	}
}

// Destroy 销毁服务并彻底停止所有后台监听（RPC 导出版：接统一调用门）。
func (s *WechatService) Destroy() error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()

	s.destroy()
	return nil
}

// destroy 停监听内部无门版：Module.OnDestroy 在 stopping 态必须无条件停尽
// goroutine，不能经门。
func (s *WechatService) destroy() {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, l := range s.listeners {
		l.Stop()
	}
	s.listeners = make(map[string]*Listener)
	s.attachments.clear()

	s.clientMu.Lock()
	s.clients = make(map[string]*Client)
	s.clientMu.Unlock()
}

// ListAccounts 获取所有账号及其运行时状态（RPC 导出版：接统一调用门）。
func (s *WechatService) ListAccounts() ([]WechatAccountState, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return nil, gateErr
	}
	defer release()

	return s.listAccounts(), nil
}

// listAccounts 账号清单内部无门版：供带门方法与 GetState 等同链复用。
func (s *WechatService) listAccounts() []WechatAccountState {
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

// GetState 获取全局/主账号运行时状态（兼容旧前端接口；RPC 导出版：接统一调用门）
func (s *WechatService) GetState() (WechatState, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return WechatState{}, gateErr
	}
	defer release()

	accounts := s.listAccounts()
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
	}, nil
}

// UpdateAccount 更新账号基本信息（备注名、目标用户 ID、BaseURL）
func (s *WechatService) UpdateAccount(id, remarkName, targetUserID, baseURL string) error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()

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
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()

	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("账号 ID 不能为空")
	}

	s.stopAccountListener(id)
	s.attachments.deleteAccount(id)
	return s.store.DeleteWechatAccount(id)
}

// StartAccountListener 启动指定账号的后台监听（RPC 导出版：接统一调用门）
func (s *WechatService) StartAccountListener(accountID string) error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()

	return s.startAccountListener(accountID)
}

// startAccountListener 启听内部无门版：initOnDemand（OnInit 期）与 CheckQRStatus
// 登录成功后的补启协程直调，不得依赖运行态门。
func (s *WechatService) startAccountListener(accountID string) error {
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
		l = NewListener(accountID, client, s.store, s.attachments)
		s.listeners[accountID] = l
	}
	s.mu.Unlock()

	return l.Start()
}

// StopAccountListener 停止指定账号的后台监听（RPC 导出版：接统一调用门）。
func (s *WechatService) StopAccountListener(accountID string) (bool, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return false, gateErr
	}
	defer release()

	return s.stopAccountListener(accountID), nil
}

// stopAccountListener 停听内部无门版：DeleteAccount 同链复用（避免二次入账）。
func (s *WechatService) stopAccountListener(accountID string) bool {
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
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return nil, gateErr
	}
	defer release()

	ctx, cancel := context.WithTimeout(context.Background(), qrCodeFetchTimeout)
	defer cancel()
	return s.defaultClient.FetchLoginQRCode(ctx)
}

// CheckQRStatus 轮询检测二维码状态（支持传入自定义备注名创建独立账号）
func (s *WechatService) CheckQRStatus(qrcode, remarkName string) (*QRStatus, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return nil, gateErr
	}
	defer release()

	qrcode = strings.TrimSpace(qrcode)
	if qrcode == "" {
		return nil, fmt.Errorf("qrcode 不能为空")
	}

	ctx, cancel := context.WithTimeout(context.Background(), qrStatusPollTimeout)
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

		// 启动该账号监听：协程在 CheckQRStatus 租约归还后才跑，走无门内部版
		go func() {
			_ = s.startAccountListener(accountID)
		}()
	}

	return res, nil
}

// RefreshAccountContextToken 手动拉取指定账号的 updates
func (s *WechatService) RefreshAccountContextToken(accountID string) (string, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return "", gateErr
	}
	defer release()

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
		l = NewListener(accountID, client, s.store, s.attachments)
		s.listeners[accountID] = l
	}
	s.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), contextTokenTimeout)
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
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()

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
	ctx, cancel := context.WithTimeout(context.Background(), sendTextTimeout)
	defer cancel()

	return client.SendTextMessage(ctx, acc.BotToken, acc.ContextToken, toUserID, text)
}

// SendImageMessage 发送图片消息（指定账号与目标）
func (s *WechatService) SendImageMessage(accountID, toUserID, filePath string) error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()

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
	ctx, cancel := context.WithTimeout(context.Background(), sendImageTimeout)
	defer cancel()

	return client.SendImageMessage(ctx, acc.BotToken, acc.ContextToken, toUserID, filePath)
}

// SendFileMessage 发送文件消息（指定账号与目标）
func (s *WechatService) SendFileMessage(accountID, toUserID, filePath string) error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()

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
	ctx, cancel := context.WithTimeout(context.Background(), sendFileTimeout)
	defer cancel()

	return client.SendFileMessage(ctx, acc.BotToken, acc.ContextToken, toUserID, filePath)
}

// SaveInboundFile 弹出另存为对话框并保存微信入站文件。
func (s *WechatService) SaveInboundFile(attachmentID string) (AttachmentActionResult, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return AttachmentActionResult{}, gateErr
	}
	defer release()

	attachment, ok := s.attachments.get(attachmentID)
	if !ok {
		return AttachmentActionResult{}, fmt.Errorf("附件不存在或已过期")
	}

	app := application.Get()
	if app == nil {
		return AttachmentActionResult{}, fmt.Errorf("application instance not available")
	}
	dialog := app.Dialog.SaveFileWithOptions(&application.SaveFileDialogOptions{
		Title:    "保存微信文件",
		Filename: attachment.FileName,
	})
	if downloads := defaultDownloadsDir(); downloads != "" {
		dialog.SetDirectory(downloads)
	}
	target, err := dialog.PromptForSingleSelection()
	if err != nil {
		return AttachmentActionResult{}, err
	}
	if strings.TrimSpace(target) == "" {
		return AttachmentActionResult{Canceled: true}, nil
	}

	ctx, cancel := attachmentTimeout()
	defer cancel()
	data, err := s.getClientForAttachment(attachment).DownloadInboundFile(ctx, attachment.Media, attachment.FileSize)
	if err != nil {
		return AttachmentActionResult{}, err
	}
	if err := writeFileAtomically(target, data); err != nil {
		return AttachmentActionResult{}, err
	}
	return AttachmentActionResult{Path: target}, nil
}

// OpenInboundFile 下载微信入站文件到临时目录，并用系统默认程序打开。
func (s *WechatService) OpenInboundFile(attachmentID string) (AttachmentActionResult, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return AttachmentActionResult{}, gateErr
	}
	defer release()

	attachment, ok := s.attachments.get(attachmentID)
	if !ok {
		return AttachmentActionResult{}, fmt.Errorf("附件不存在或已过期")
	}

	ctx, cancel := attachmentTimeout()
	defer cancel()
	data, err := s.getClientForAttachment(attachment).DownloadInboundFile(ctx, attachment.Media, attachment.FileSize)
	if err != nil {
		return AttachmentActionResult{}, err
	}

	dir, err := os.MkdirTemp("", "hanxi-wechat-")
	if err != nil {
		return AttachmentActionResult{}, fmt.Errorf("创建临时目录失败: %w", err)
	}
	target := filepath.Join(dir, attachment.FileName)
	if err := writeFileAtomically(target, data); err != nil {
		os.RemoveAll(dir)
		return AttachmentActionResult{}, err
	}
	if err := openAttachmentFile(target); err != nil {
		return AttachmentActionResult{Path: target}, fmt.Errorf("文件已下载到 %s，但打开失败: %w", target, err)
	}
	return AttachmentActionResult{Path: target}, nil
}

// InspectOutgoingAttachment 校验本地附件并生成发送前预览信息。
func (s *WechatService) InspectOutgoingAttachment(filePath string) (OutgoingAttachmentDraft, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return OutgoingAttachmentDraft{}, gateErr
	}
	defer release()

	return inspectOutgoingAttachment(filePath)
}

// RegisterClipboardAttachment 将窗口剪贴板中的附件字节安全落到受管临时文件，供现有发送链路复用。
func (s *WechatService) RegisterClipboardAttachment(fileName, dataURL string) (OutgoingAttachmentDraft, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return OutgoingAttachmentDraft{}, gateErr
	}
	defer release()

	data, mime, err := decodeClipboardDataURL(dataURL)
	if err != nil {
		return OutgoingAttachmentDraft{}, err
	}
	if int64(len(data)) > maxOutgoingAttachmentBytes {
		return OutgoingAttachmentDraft{}, fmt.Errorf("剪贴板附件超过 %d MB，当前加密上传方式不支持", maxOutgoingAttachmentBytes>>20)
	}

	name := sanitizeInboundFileName(fileName)
	ext := strings.ToLower(filepath.Ext(name))
	isImage := strings.HasPrefix(mime, "image/")
	if isImage {
		if int64(len(data)) > maxImagePreviewBytes {
			return OutgoingAttachmentDraft{}, fmt.Errorf("剪贴板图片超过 %d MB，无法发送前预览", maxImagePreviewBytes>>20)
		}
		detectedExt, ok := imageExtensionForMIME(mime)
		if !ok {
			return OutgoingAttachmentDraft{}, fmt.Errorf("剪贴板内容不是支持的图片格式 (%s)", mime)
		}
		ext = detectedExt
		base := strings.TrimSuffix(name, filepath.Ext(name))
		if base == "" || base == "微信文件" {
			base = fmt.Sprintf("微信粘贴图片_%s", time.Now().Format("20060102_150405"))
		}
		name = base + ext
	} else if ext == "" {
		ext = ".bin"
	}

	file, err := os.CreateTemp("", "hanxi-wechat-clipboard-*"+ext)
	if err != nil {
		return OutgoingAttachmentDraft{}, fmt.Errorf("创建剪贴板附件临时文件失败: %w", err)
	}
	path := file.Name()
	if _, err = file.Write(data); err != nil {
		file.Close()
		os.Remove(path)
		return OutgoingAttachmentDraft{}, fmt.Errorf("写入剪贴板附件失败: %w", err)
	}
	if err = file.Close(); err != nil {
		os.Remove(path)
		return OutgoingAttachmentDraft{}, fmt.Errorf("保存剪贴板附件失败: %w", err)
	}
	s.attachments.registerOutgoingTemp(path)

	draft := OutgoingAttachmentDraft{
		Path:      path,
		FileName:  name,
		FileSize:  int64(len(data)),
		IsImage:   isImage,
		Temporary: true,
	}
	if isImage {
		draft.PreviewURL, err = buildImageDataURL(data)
		if err != nil {
			s.attachments.releaseOutgoingTemp(path)
			return OutgoingAttachmentDraft{}, err
		}
	}
	return draft, nil
}

// ReleaseOutgoingAttachment 仅释放由 RegisterClipboardAttachment 创建的受管临时文件。
func (s *WechatService) ReleaseOutgoingAttachment(filePath string) (bool, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return false, gateErr
	}
	defer release()

	return s.attachments.releaseOutgoingTemp(strings.TrimSpace(filePath)), nil
}

// GetImagePreview 读取本地图片并以 Base64 Data URL 返回，供出站图片气泡内嵌缩略预览
// （WebView 无法直读 file:// 本地路径，预览字节必须走后端通道）。
func (s *WechatService) GetImagePreview(filePath string) (string, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return "", gateErr
	}
	defer release()

	return localImagePreview(filePath)
}

// OpenLocalImage 用系统默认查看器打开出站消息引用的本地图片。
// 扩展名白名单前置校验，杜绝该 RPC 被借道唤起任意本地关联程序。
func (s *WechatService) OpenLocalImage(filePath string) error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()

	if !isPreviewableImageName(strings.TrimSpace(filePath)) {
		return fmt.Errorf("仅允许打开图片文件")
	}
	return openAttachmentFile(filePath)
}

// RevealLocalFile 在资源管理器中定位本地文件（「打开目录」按钮：打开所在文件夹并选中）。
func (s *WechatService) RevealLocalFile(filePath string) error {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return gateErr
	}
	defer release()

	return revealInFolder(strings.TrimSpace(filePath))
}

// PreviewInboundImage 下载解密入站图片附件并以 Base64 Data URL 返回，供图片气泡内嵌缩略预览。
// 仅放行 kindImage 附件：预览通道不成为任意大文件的旁路下载器。
func (s *WechatService) PreviewInboundImage(attachmentID string) (string, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return "", gateErr
	}
	defer release()

	attachment, ok := s.attachments.get(attachmentID)
	if !ok {
		return "", fmt.Errorf("附件不存在或已过期")
	}
	if attachment.Kind != kindImage {
		return "", fmt.Errorf("该附件不是图片消息")
	}

	ctx, cancel := attachmentTimeout()
	defer cancel()
	data, err := s.getClientForAttachment(attachment).DownloadInboundFile(ctx, attachment.Media, attachment.FileSize)
	if err != nil {
		return "", err
	}
	if int64(len(data)) > maxImagePreviewBytes {
		return "", fmt.Errorf("图片超过 %d MB，已跳过预览", maxImagePreviewBytes>>20)
	}
	return buildImageDataURL(data)
}

func (s *WechatService) getClientForAttachment(attachment inboundAttachment) *Client {
	if acc, ok := s.store.GetWechatAccountByID(attachment.AccountID); ok {
		return s.getClientForAccount(acc.BaseURL)
	}
	return s.defaultClient
}

// PickAttachmentDialog 打开统一的系统附件选择对话框；图片真实性在发送前由后端按内容嗅探。
func (s *WechatService) PickAttachmentDialog() (string, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return "", gateErr
	}
	defer release()

	app := application.Get()
	if app == nil {
		return "", fmt.Errorf("application instance not available")
	}

	dialog := app.Dialog.OpenFile()
	dialog.SetTitle("选择要发送的附件")
	dialog.AddFilter("所有文件 (*.*)", "*.*")

	filePath, err := dialog.PromptForSingleSelection()
	if err != nil {
		return "", err
	}
	return filePath, nil
}

// GetPendingMessages 取走指定账号后台积累的未读消息（消费后清空，供前端页面重新挂载时补取）
func (s *WechatService) GetPendingMessages(accountID string) ([]InboundMessage, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return nil, gateErr
	}
	defer release()

	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return nil, nil
	}
	s.mu.RLock()
	l, ok := s.listeners[accountID]
	s.mu.RUnlock()
	if !ok || l == nil {
		return nil, nil
	}
	return l.DrainMsgBuf(), nil
}
