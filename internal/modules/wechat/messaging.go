package wechat

import (
	"context"
	"encoding/base64"
	"fmt"
	"math/rand"
	"path/filepath"
	"time"
)

type textItem struct {
	Text string `json:"text"`
}

type mediaObj struct {
	EncryptQueryParam string `json:"encrypt_query_param"`
	AESKey            string `json:"aes_key"`
	EncryptType       int    `json:"encrypt_type"`
}

type imageItem struct {
	Media   mediaObj `json:"media"`
	MidSize int      `json:"mid_size"`
}

type fileItem struct {
	Media    mediaObj `json:"media"`
	FileName string   `json:"file_name"`
	Len      string   `json:"len"`
}

type outboundItem struct {
	Type      int        `json:"type"` // 1: Text, 2: Image, 4: File
	TextItem  *textItem  `json:"text_item,omitempty"`
	ImageItem *imageItem `json:"image_item,omitempty"`
	FileItem  *fileItem  `json:"file_item,omitempty"`
}

type outboundMsg struct {
	FromUserID   string         `json:"from_user_id"`
	ToUserID     string         `json:"to_user_id"`
	ClientID     string         `json:"client_id"`
	MessageType  int            `json:"message_type"`
	MessageState int            `json:"message_state"`
	ItemList     []outboundItem `json:"item_list"`
}

type sendMessagePayload struct {
	BotToken     string      `json:"bot_token"`
	ContextToken string      `json:"context_token"`
	BaseInfo     BaseInfo    `json:"base_info"`
	Msg          outboundMsg `json:"msg"`
}

type sendMessageResp struct {
	Ret    int    `json:"ret"`
	ErrMsg string `json:"errmsg"`
}

func generateClientID() string {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	return fmt.Sprintf("hubkit-%d-%x", time.Now().Unix(), r.Uint32())
}

// SendTextMessage 发送文字消息
func (c *Client) SendTextMessage(ctx context.Context, botToken, contextToken, toUserID, text string) error {
	if botToken == "" {
		return fmt.Errorf("bot_token 不能为空")
	}
	if contextToken == "" {
		return fmt.Errorf("context_token 不能为空，请先在微信中给机器人发送一条任意消息以激活会话")
	}
	if toUserID == "" {
		return fmt.Errorf("目标用户 ID 不能为空")
	}

	payload := sendMessagePayload{
		BotToken:     botToken,
		ContextToken: contextToken,
		BaseInfo:     defaultBaseInfo(),
		Msg: outboundMsg{
			FromUserID:   "",
			ToUserID:     toUserID,
			ClientID:     generateClientID(),
			MessageType:  2,
			MessageState: 2,
			ItemList: []outboundItem{
				{
					Type: 1,
					TextItem: &textItem{
						Text: text,
					},
				},
			},
		},
	}

	var resp sendMessageResp
	err := c.post(ctx, "/ilink/bot/sendmessage", botToken, payload, &resp)
	if err != nil {
		return fmt.Errorf("send text message failed: %w", err)
	}
	if resp.Ret != 0 && resp.Ret != 200 {
		return fmt.Errorf("send text message server error: ret=%d, errmsg=%s", resp.Ret, resp.ErrMsg)
	}

	return nil
}

// SendImageMessage 发送图片消息
func (c *Client) SendImageMessage(ctx context.Context, botToken, contextToken, toUserID, filePath string) error {
	if botToken == "" {
		return fmt.Errorf("bot_token 不能为空")
	}
	if contextToken == "" {
		return fmt.Errorf("context_token 不能为空，请先在微信中给机器人发送一条任意消息以激活会话")
	}
	if toUserID == "" {
		return fmt.Errorf("目标用户 ID 不能为空")
	}

	// 1. 上传图片到 CDN 获取 x-encrypted-param
	uploadRes, err := c.UploadImageFile(ctx, botToken, toUserID, filePath)
	if err != nil {
		return fmt.Errorf("upload image failed: %w", err)
	}

	// 2. 注意微信协议核心细节：aes_key 必须是 32 位 Hex ASCII 字符串的 Base64 编码
	aesKeyBase64 := base64.StdEncoding.EncodeToString([]byte(uploadRes.AESKeyHex))

	payload := sendMessagePayload{
		BotToken:     botToken,
		ContextToken: contextToken,
		BaseInfo:     defaultBaseInfo(),
		Msg: outboundMsg{
			FromUserID:   "",
			ToUserID:     toUserID,
			ClientID:     generateClientID(),
			MessageType:  2,
			MessageState: 2,
			ItemList: []outboundItem{
				{
					Type: 2,
					ImageItem: &imageItem{
						Media: mediaObj{
							EncryptQueryParam: uploadRes.EncryptQueryParam,
							AESKey:            aesKeyBase64,
							EncryptType:       1,
						},
						MidSize: uploadRes.CipherSize,
					},
				},
			},
		},
	}

	var resp sendMessageResp
	err = c.post(ctx, "/ilink/bot/sendmessage", botToken, payload, &resp)
	if err != nil {
		return fmt.Errorf("send image message failed: %w", err)
	}
	if resp.Ret != 0 && resp.Ret != 200 {
		return fmt.Errorf("send image message server error: ret=%d, errmsg=%s", resp.Ret, resp.ErrMsg)
	}

	return nil
}

// SendFileMessage 发送文件附件消息
func (c *Client) SendFileMessage(ctx context.Context, botToken, contextToken, toUserID, filePath string) error {
	if botToken == "" {
		return fmt.Errorf("bot_token 不能为空")
	}
	if contextToken == "" {
		return fmt.Errorf("context_token 不能为空，请先在微信中给机器人发送一条任意消息以激活会话")
	}
	if toUserID == "" {
		return fmt.Errorf("目标用户 ID 不能为空")
	}

	// 1. 上传文件到 CDN (mediaType=3 for File)
	uploadRes, err := c.UploadMediaFile(ctx, botToken, toUserID, filePath, 3)
	if err != nil {
		return fmt.Errorf("upload file failed: %w", err)
	}

	// 2. aes_key 为 32 位 Hex ASCII 字符串的 Base64 编码
	aesKeyBase64 := base64.StdEncoding.EncodeToString([]byte(uploadRes.AESKeyHex))

	payload := sendMessagePayload{
		BotToken:     botToken,
		ContextToken: contextToken,
		BaseInfo:     defaultBaseInfo(),
		Msg: outboundMsg{
			FromUserID:   "",
			ToUserID:     toUserID,
			ClientID:     generateClientID(),
			MessageType:  2,
			MessageState: 2,
			ItemList: []outboundItem{
				{
					Type: 4,
					FileItem: &fileItem{
						Media: mediaObj{
							EncryptQueryParam: uploadRes.EncryptQueryParam,
							AESKey:            aesKeyBase64,
							EncryptType:       1,
						},
						FileName: filepath.Base(filePath),
						Len:      fmt.Sprintf("%d", uploadRes.CipherSize),
					},
				},
			},
		},
	}

	var resp sendMessageResp
	err = c.post(ctx, "/ilink/bot/sendmessage", botToken, payload, &resp)
	if err != nil {
		return fmt.Errorf("send file message failed: %w", err)
	}
	if resp.Ret != 0 && resp.Ret != 200 {
		return fmt.Errorf("send file message server error: ret=%d, errmsg=%s", resp.Ret, resp.ErrMsg)
	}

	return nil
}
