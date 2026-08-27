package wechat

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"hubkit/internal/notify"
	"hubkit/internal/settings"
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

const msgBufMax = 100

// Listener 负责长轮询获取微信消息并提取/刷新 ContextToken
type Listener struct {
	accountID  string
	client     *Client
	store      *settings.Store
	cancel     context.CancelFunc
	running    atomic.Bool
	mu         sync.Mutex
	updatesBuf string
	msgBuf     []InboundMessage // 有界环形缓冲，供前端重新挂载时拉取
}

func NewListener(accountID string, client *Client, store *settings.Store) *Listener {
	return &Listener{
		accountID: accountID,
		client:    client,
		store:     store,
	}
}

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
		LongPollingTimeoutMs: 25000, // 给腾讯长轮询留出足够响应时间 (25s)
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
			// 自动持久化指定账号
			if err := l.store.Update(func(c *settings.AppSettings) {
				for i, acc := range c.WechatAccounts {
					if acc.ID == l.accountID {
						c.WechatAccounts[i].ContextToken = latestToken
						c.WechatAccounts[i].ContextTokenUpdatedAt = nowStr
						if msg.FromUserID != "" && c.WechatAccounts[i].TargetUserID == "" {
							c.WechatAccounts[i].TargetUserID = msg.FromUserID
						}
						break
					}
				}
				if c.Wechat.BotToken == botToken {
					c.Wechat.ContextToken = latestToken
					c.Wechat.ContextTokenUpdatedAt = nowStr
					if msg.FromUserID != "" && c.Wechat.TargetUserID == "" {
						c.Wechat.TargetUserID = msg.FromUserID
					}
				}
			}); err != nil {
				slog.Warn("failed to persist updated wechat context_token", "err", err, "accountId", l.accountID)
			}

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

	return latestToken, nil
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
			LongPollingTimeoutMs: 35000,
			BaseInfo:             defaultBaseInfo(),
			GetUpdatesBuf:        buf,
		}

		var resp updatesResp
		err := l.client.post(ctx, "/ilink/bot/getupdates", botToken, req, &resp)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			time.Sleep(2 * time.Second)
			continue
		}

		if resp.Ret != 0 && resp.Ret != 200 {
			time.Sleep(2 * time.Second)
			continue
		}

		if resp.GetUpdatesBuf != "" {
			l.mu.Lock()
			l.updatesBuf = resp.GetUpdatesBuf
			l.mu.Unlock()
		}

		nowStr := time.Now().Format("2006-01-02 15:04:05")
		for _, msg := range resp.Msgs {
			if msg.ContextToken != "" {
				if err := l.store.Update(func(c *settings.AppSettings) {
					for i, acc := range c.WechatAccounts {
						if acc.ID == l.accountID {
							c.WechatAccounts[i].ContextToken = msg.ContextToken
							c.WechatAccounts[i].ContextTokenUpdatedAt = nowStr
							if msg.FromUserID != "" && c.WechatAccounts[i].TargetUserID == "" {
								c.WechatAccounts[i].TargetUserID = msg.FromUserID
							}
							break
						}
					}
					if c.Wechat.BotToken == botToken {
						c.Wechat.ContextToken = msg.ContextToken
						c.Wechat.ContextTokenUpdatedAt = nowStr
						if msg.FromUserID != "" && c.Wechat.TargetUserID == "" {
							c.Wechat.TargetUserID = msg.FromUserID
						}
					}
				}); err != nil {
					slog.Warn("failed to persist updated wechat context_token", "err", err, "accountId", l.accountID)
				}

				if app := application.Get(); app != nil && app.Event != nil {
					app.Event.Emit("wechat:context-token-updated", map[string]string{
						"accountId":    l.accountID,
						"contextToken": msg.ContextToken,
						"updatedAt":    nowStr,
						"fromUserId":   msg.FromUserID,
					})
				}
			}

			l.dispatchInboundMsg(msg, nowStr)
		}
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
		if item.FileItem != nil {
			inMsg.FileName = item.FileItem.FileName
			summary = fmt.Sprintf("[文件] %s", item.FileItem.FileName)
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

		// 优先获取账号备注名称，绝不回退为冗长的 TargetUserID/FromUserID/原始 Hash
		displayName := "微信机器人"
		if acc, ok := l.store.GetWechatAccountByID(l.accountID); ok && acc.RemarkName != "" {
			displayName = acc.RemarkName
		} else {
			cfg := l.store.GetWechatConfig()
			if cfg.IlinkBotID != "" {
				displayName = "微信机器人"
			}
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
