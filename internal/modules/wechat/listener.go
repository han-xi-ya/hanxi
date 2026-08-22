package wechat

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"hubkit/internal/settings"
)

type updatesReq struct {
	BotToken             string   `json:"bot_token"`
	LongPollingTimeoutMs int      `json:"longpolling_timeout_ms"`
	BaseInfo             BaseInfo `json:"base_info"`
	GetUpdatesBuf        string   `json:"get_updates_buf"`
}

type inboundRawItem struct {
	Type     int `json:"type"`
	TextItem *struct {
		Text string `json:"text"`
	} `json:"text_item,omitempty"`
	ImageItem *struct {
		Media struct {
			EncryptQueryParam string `json:"encrypt_query_param"`
			AESKey            string `json:"aes_key"`
		} `json:"media"`
	} `json:"image_item,omitempty"`
	FileItem *struct {
		FileName string `json:"file_name"`
	} `json:"file_item,omitempty"`
}

type inboundRawMsg struct {
	FromUserID   string           `json:"from_user_id"`
	ToUserID     string           `json:"to_user_id"`
	ContextToken string           `json:"context_token"`
	ItemList     []inboundRawItem `json:"item_list"`
}

type updatesResp struct {
	GetUpdatesBuf string          `json:"get_updates_buf"`
	Msgs          []inboundRawMsg `json:"msgs"`
	Ret           int             `json:"ret"`
	ErrMsg        string          `json:"errmsg"`
}

// Listener 负责长轮询获取微信消息并提取/刷新 ContextToken
type Listener struct {
	client     *Client
	store      *settings.Store
	cancel     context.CancelFunc
	running    atomic.Bool
	mu         sync.Mutex
	updatesBuf string
}

func NewListener(client *Client, store *settings.Store) *Listener {
	return &Listener{
		client: client,
		store:  store,
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

	cfg := l.store.GetWechatConfig()
	if cfg.BotToken == "" {
		return fmt.Errorf("请先完成微信扫码登录")
	}

	ctx, cancel := context.WithCancel(context.Background())
	l.cancel = cancel
	l.running.Store(true)

	go l.pollLoop(ctx, cfg.BotToken)
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

	req := updatesReq{
		BotToken:             botToken,
		LongPollingTimeoutMs: 25000, // 给腾讯长轮询留出足够响应时间 (25s)
		BaseInfo:             defaultBaseInfo(),
		GetUpdatesBuf:        l.updatesBuf,
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
		l.updatesBuf = resp.GetUpdatesBuf
	}

	var latestToken string
	nowStr := time.Now().Format("2006-01-02 15:04:05")

	for _, msg := range resp.Msgs {
		if msg.ContextToken != "" {
			latestToken = msg.ContextToken
			// 自动持久化
			_ = l.store.Update(func(c *settings.AppSettings) {
				c.Wechat.ContextToken = latestToken
				c.Wechat.ContextTokenUpdatedAt = nowStr
				if msg.FromUserID != "" && c.Wechat.TargetUserID == "" {
					c.Wechat.TargetUserID = msg.FromUserID
				}
			})

			// 广播 Wails 事件
			if app := application.Get(); app != nil && app.Event != nil {
				app.Event.Emit("wechat:context-token-updated", map[string]string{
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
	slog.Info("wechat listener started")
	defer func() {
		l.running.Store(false)
		slog.Info("wechat listener stopped")
	}()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		req := updatesReq{
			BotToken:             botToken,
			LongPollingTimeoutMs: 35000,
			BaseInfo:             defaultBaseInfo(),
			GetUpdatesBuf:        l.updatesBuf,
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
			l.updatesBuf = resp.GetUpdatesBuf
		}

		nowStr := time.Now().Format("2006-01-02 15:04:05")
		for _, msg := range resp.Msgs {
			if msg.ContextToken != "" {
				_ = l.store.Update(func(c *settings.AppSettings) {
					c.Wechat.ContextToken = msg.ContextToken
					c.Wechat.ContextTokenUpdatedAt = nowStr
					if msg.FromUserID != "" && c.Wechat.TargetUserID == "" {
						c.Wechat.TargetUserID = msg.FromUserID
					}
				})

				if app := application.Get(); app != nil && app.Event != nil {
					app.Event.Emit("wechat:context-token-updated", map[string]string{
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

func (l *Listener) dispatchInboundMsg(msg inboundRawMsg, nowStr string) {
	for _, item := range msg.ItemList {
		inMsg := InboundMessage{
			From: msg.FromUserID,
			Type: item.Type,
			Time: nowStr,
		}
		if item.TextItem != nil {
			inMsg.Text = item.TextItem.Text
		}
		if item.FileItem != nil {
			inMsg.FileName = item.FileItem.FileName
		}

		if app := application.Get(); app != nil && app.Event != nil {
			app.Event.Emit("wechat:message-received", inMsg)
		}
	}
}
