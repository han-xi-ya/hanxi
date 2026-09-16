package wechat

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"hanxi/internal/notify"
	"hanxi/internal/settings"
)

type updatesReq struct {
	BotToken             string   `json:"bot_token"`
	LongPollingTimeoutMs int      `json:"longpolling_timeout_ms"`
	BaseInfo             BaseInfo `json:"base_info"`
	GetUpdatesBuf        string   `json:"get_updates_buf"`
}

type updatesResp struct {
	GetUpdatesBuf string          `json:"get_updates_buf"`
	Msgs          []InboundRawMsg `json:"msgs"`
	Ret           int             `json:"ret"`
	ErrMsg        string          `json:"errmsg"`
}

// msgBufMax 前端断连期间的消息留存上限，超出丢最旧。
const msgBufMax = 100

// 长轮询与失败退避的时间常量：手动单次拉取留足响应余量，后台轮询略长于腾讯侧上限，
// 网络抖动/协议失败后统一按 pollRetryDelay 退避重试。
const (
	manualPollTimeoutMs = 25000 // 给腾讯长轮询留出足够响应时间 (25s)
	loopPollTimeoutMs   = 35000
	pollRetryDelay      = 2 * time.Second
)

// Listener 负责长轮询获取微信消息并提取/刷新 ContextToken
type Listener struct {
	accountID   string
	client      *Client
	store       *settings.Store
	attachments *attachmentStore
	cancel      context.CancelFunc
	running     atomic.Bool
	mu          sync.Mutex
	updatesBuf  string
	msgBuf      []InboundMessage // 有界环形缓冲，供前端重新挂载时拉取
}

// NewListener 创建账号级监听器（不启动轮询；goroutine 由 Start 派生、Stop 经 cancel 终止）。
func NewListener(accountID string, client *Client, store *settings.Store, attachments *attachmentStore) *Listener {
	return &Listener{
		accountID:   accountID,
		client:      client,
		store:       store,
		attachments: attachments,
	}
}

// IsRunning 无锁读原子标志，仅表示轮询 goroutine 存活，不代表本轮已成功拉取。
func (l *Listener) IsRunning() bool {
	return l.running.Load()
}

// Start 启动后台长轮询监听
func (l *Listener) Start() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.running.Load() {
		return nil
	}

	acc, ok := l.store.GetWechatAccountByID(l.accountID)
	if !ok || acc.BotToken == "" {
		// 尝试回退从遗留配置读
		cfg := l.store.GetWechatConfig()
		if cfg.BotToken != "" {
			acc.BotToken = cfg.BotToken
		} else {
			return fmt.Errorf("请先完成微信扫码登录")
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	l.cancel = cancel
	l.running.Store(true)

	go l.pollLoop(ctx, acc.BotToken)
	return nil
}

// Stop 停止监听
func (l *Listener) Stop() {
	l.mu.Lock()
	defer l.mu.Unlock()

	if !l.running.Load() {
		return
	}

	if l.cancel != nil {
		l.cancel()
		l.cancel = nil
	}
	l.running.Store(false)
}

// FetchUpdatesOnce 单次拉取 updates (可用于手动一键更新 context_token)
func (l *Listener) FetchUpdatesOnce(ctx context.Context, botToken string) (string, error) {
	if botToken == "" {
		return "", fmt.Errorf("bot_token 不能为空")
	}

	l.mu.Lock()
	buf := l.updatesBuf
	l.mu.Unlock()

	req := updatesReq{
		BotToken:             botToken,
		LongPollingTimeoutMs: manualPollTimeoutMs,
		BaseInfo:             defaultBaseInfo(),
		GetUpdatesBuf:        buf,
	}

	var resp updatesResp
	err := l.client.post(ctx, "/ilink/bot/getupdates", botToken, req, &resp)
	if err != nil {
		return "", err
	}

	if resp.Ret != 0 && resp.Ret != 200 {
		return "", fmt.Errorf("getupdates failed: ret=%d, msg=%s", resp.Ret, resp.ErrMsg)
	}

	return l.handleUpdates(botToken, resp), nil
}

// handleUpdates 统一处理一轮 getupdates 回执：推进游标、逐条持久化 context_token
// 并广播事件（手动刷新与后台轮询共用同一份语义，避免两条路径各修一半漂移）。
// 返回本轮看到的最新 context_token（无则空串）。
func (l *Listener) handleUpdates(botToken string, resp updatesResp) string {
	if resp.GetUpdatesBuf != "" {
		l.mu.Lock()
		l.updatesBuf = resp.GetUpdatesBuf
		l.mu.Unlock()
	}

	var latestToken string
	nowStr := time.Now().Format("2006-01-02 15:04:05")

	for _, msg := range resp.Msgs {
		if msg.ContextToken != "" {
			latestToken = msg.ContextToken
			l.persistContextToken(botToken, msg, nowStr)

			// 广播 Wails 事件
			if app := application.Get(); app != nil && app.Event != nil {
				app.Event.Emit("wechat:context-token-updated", map[string]string{
					"accountId":    l.accountID,
					"contextToken": latestToken,
					"updatedAt":    nowStr,
					"fromUserId":   msg.FromUserID,
				})
			}
		}

		// 广播消息事件
		l.dispatchInboundMsg(msg, nowStr)
	}

	return latestToken
}

// persistContextToken 把消息携带的 context_token（及首次见到的对端 ID）落库：
// 账号表按 accountID 命中，遗留单账号配置按 botToken 命中，两者互不排斥。
func (l *Listener) persistContextToken(botToken string, msg InboundRawMsg, nowStr string) {
	token := msg.ContextToken
	if err := l.store.Update(func(c *settings.AppSettings) {
		for i, acc := range c.WechatAccounts {
			if acc.ID == l.accountID {
				c.WechatAccounts[i].ContextToken = token
				c.WechatAccounts[i].ContextTokenUpdatedAt = nowStr
				if msg.FromUserID != "" && c.WechatAccounts[i].TargetUserID == "" {
					c.WechatAccounts[i].TargetUserID = msg.FromUserID
				}
				break
			}
		}
		if c.Wechat.BotToken == botToken {
			c.Wechat.ContextToken = token
			c.Wechat.ContextTokenUpdatedAt = nowStr
			if msg.FromUserID != "" && c.Wechat.TargetUserID == "" {
				c.Wechat.TargetUserID = msg.FromUserID
			}
		}
	}); err != nil {
		slog.Warn("failed to persist updated wechat context_token", "err", err, "accountId", l.accountID)
	}
}

func (l *Listener) pollLoop(ctx context.Context, botToken string) {
	slog.Info("wechat listener started", "accountId", l.accountID)
	defer func() {
		l.running.Store(false)
		slog.Info("wechat listener stopped", "accountId", l.accountID)
	}()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		l.mu.Lock()
		buf := l.updatesBuf
		l.mu.Unlock()

		req := updatesReq{
			BotToken:             botToken,
			LongPollingTimeoutMs: loopPollTimeoutMs,
			BaseInfo:             defaultBaseInfo(),
			GetUpdatesBuf:        buf,
		}

		var resp updatesResp
		err := l.client.post(ctx, "/ilink/bot/getupdates", botToken, req, &resp)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			if !waitBackoff(ctx, pollRetryDelay) {
				return
			}
			continue
		}

		if resp.Ret != 0 && resp.Ret != 200 {
			if !waitBackoff(ctx, pollRetryDelay) {
				return
			}
			continue
		}

		l.handleUpdates(botToken, resp)
	}
}

// waitBackoff 可被 ctx 打断的退避等待：time.Sleep 硬等会让 Stop() 之后轮询 goroutine
// 还要空转到退避结束（并在此期间无视取消信号），故一律走 select + timer。
// 返回 false 表示退避期间被取消，调用方应立即退出循环。
func waitBackoff(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (l *Listener) dispatchInboundMsg(msg InboundRawMsg, nowStr string) {
	for _, item := range msg.ItemList {
		inMsg := InboundMessage{
			AccountID: l.accountID,
			From:      msg.FromUserID,
			Type:      item.Type,
			Time:      nowStr,
		}
		summary := "收到新的微信消息"
		if item.TextItem != nil {
			inMsg.Text = item.TextItem.Text
			summary = item.TextItem.Text
		}
		if item.ImageItem != nil {
			// 图片消息只带媒体凭据：注册为附件后即获得预览/打开/保存能力，
			// 文件名由后端按入站时刻合成（nowStr 同源），前端不感知注册细节。
			fileName := inboundImageFileName(time.Now())
			if l.attachments == nil {
				inMsg.AttachmentError = "附件服务未初始化"
			} else if attachmentID, err := l.attachments.registerImage(l.accountID, fileName, item.ImageItem.Media); err != nil {
				inMsg.AttachmentError = err.Error()
				slog.Warn("wechat inbound image is not downloadable",
					"accountId", l.accountID,
					"encryptType", item.ImageItem.Media.EncryptType,
					"queryLength", len(item.ImageItem.Media.EncryptQueryParam),
					"aesKeyLength", len(item.ImageItem.Media.AESKey),
					"err", err,
				)
			} else {
				inMsg.FileName = fileName
				inMsg.AttachmentID = attachmentID
				inMsg.Downloadable = true
			}
			summary = "[图片消息]"
		}
		if item.FileItem != nil {
			inMsg.FileName = sanitizeInboundFileName(item.FileItem.FileName)
			inMsg.FileSize = int64(item.FileItem.Len)
			summary = fmt.Sprintf("[文件] %s", inMsg.FileName)
			if l.attachments == nil {
				inMsg.AttachmentError = "附件服务未初始化"
			} else if attachmentID, err := l.attachments.register(l.accountID, *item.FileItem); err != nil {
				inMsg.AttachmentError = err.Error()
				slog.Warn("wechat inbound file is not downloadable",
					"accountId", l.accountID,
					"fileName", inMsg.FileName,
					"fileSize", inMsg.FileSize,
					"encryptType", item.FileItem.Media.EncryptType,
					"queryLength", len(item.FileItem.Media.EncryptQueryParam),
					"aesKeyLength", len(item.FileItem.Media.AESKey),
					"err", err,
				)
			} else {
				inMsg.AttachmentID = attachmentID
				inMsg.Downloadable = true
			}
		}

		if app := application.Get(); app != nil && app.Event != nil {
			app.Event.Emit("wechat:message-received", inMsg)
		}

		// 写入有界缓冲，供前端重新挂载时补取离线消息
		l.mu.Lock()
		l.msgBuf = append(l.msgBuf, inMsg)
		if len(l.msgBuf) > msgBufMax {
			l.msgBuf = l.msgBuf[len(l.msgBuf)-msgBufMax:]
		}
		l.mu.Unlock()

		// 优先获取账号备注名称，绝不回退为冗长的 TargetUserID/FromUserID/原始 Hash；
		// 无备注名即固定称号（原 else 分支再读一次遗留配置赋同一个值，属无操作死码，已删）
		displayName := "微信机器人"
		if acc, ok := l.store.GetWechatAccountByID(l.accountID); ok && acc.RemarkName != "" {
			displayName = acc.RemarkName
		}

		notify.Info("wechat", fmt.Sprintf("微信消息 (%s)", displayName), summary, "/ext/wechat")
	}
}

// DrainMsgBuf 取走缓冲区中所有消息（消费后清空）
func (l *Listener) DrainMsgBuf() []InboundMessage {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.msgBuf) == 0 {
		return nil
	}
	out := l.msgBuf
	l.msgBuf = nil
	return out
}
