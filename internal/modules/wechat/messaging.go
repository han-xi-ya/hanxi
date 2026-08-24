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
		return fmt.Errorf("发送文字消息网络请求失败: %w", err)
	}
	if resp.Ret != 0 && resp.Ret != 200 {
		return parseSendMessageError("文字", resp.Ret, resp.ErrMsg)
	}

	return nil
}

// parseSendMessageError 解析微信 iLink 服务端返回的错误码并输出对开发者极度友好的诊断提示
func parseSendMessageError(msgType string, ret int, errMsg string) error {
	var friendlyTip string
	switch ret {
	case -2:
		friendlyTip = "会话尚未建立或已失效 (prepare failed)。微信官方限制机器人无法主动发起新会话，请使用目标微信号先在微信中主动给机器人发送一条任意消息（如发送“你好”），待 HubKit 捕获最新会话凭据后再发送。"
	case -1:
		friendlyTip = "系统繁忙或参数异常，请稍后重试。"
	case 40001, 40014:
		friendlyTip = "Bot Token 无效或已过期，请重新扫码登录授权。"
	case 40003:
		friendlyTip = "目标微信用户 ID (To User ID) 无效或不存在。"
	default:
		friendlyTip = "微信服务器返回异常，请确认网络连接及会话状态。"
	}

	return fmt.Errorf("微信服务器拒绝发送%s (ret=%d, errmsg=%s): %s", msgType, ret, errMsg, friendlyTip)
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
		return fmt.Errorf("发送图片消息网络请求失败: %w", err)
	}
	if resp.Ret != 0 && resp.Ret != 200 {
		return parseSendMessageError("图片", resp.Ret, resp.ErrMsg)
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
		return fmt.Errorf("发送文件消息网络请求失败: %w", err)
	}
	if resp.Ret != 0 && resp.Ret != 200 {
		return parseSendMessageError("文件", resp.Ret, resp.ErrMsg)
	}

	return nil
}
