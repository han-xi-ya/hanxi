package wechat

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
)

// QRInfo 微信登录二维码信息
type QRInfo struct {
	QRCode    string `json:"qrcode"`
	QRCodeURL string `json:"qrcodeUrl"`
}

// QRStatus 登录状态响应
type QRStatus struct {
	Status      string `json:"status"` // wait | scaned | confirmed | expired | error
	BotToken    string `json:"botToken,omitempty"`
	IlinkBotID  string `json:"ilinkBotId,omitempty"`
	IlinkUserID string `json:"ilinkUserId,omitempty"`
	BaseURL     string `json:"baseUrl,omitempty"`
	Message     string `json:"message,omitempty"`
}

// WechatAccountState 微信单账号运行时状态模型
type WechatAccountState struct {
	ID                    string `json:"id"`
	RemarkName            string `json:"remarkName"`
	BotToken              string `json:"botToken"`
	IlinkBotID            string `json:"ilinkBotId"`
	IlinkUserID           string `json:"ilinkUserId"`
	ContextToken          string `json:"contextToken"`
	ContextTokenUpdatedAt string `json:"contextTokenUpdatedAt"`
	TargetUserID          string `json:"targetUserId"`
	BaseURL               string `json:"baseUrl"`
	CreatedAt             string `json:"createdAt"`
	IsListening           bool   `json:"isListening"`
}

// WechatState 模块当前运行时状态（兼容旧版接口）
type WechatState struct {
	IsLoggedIn            bool                 `json:"isLoggedIn"`
	BotToken              string               `json:"botToken"`
	IlinkBotID            string               `json:"ilinkBotId"`
	IlinkUserID           string               `json:"ilinkUserId"`
	ContextToken          string               `json:"contextToken"`
	ContextTokenUpdatedAt string               `json:"contextTokenUpdatedAt"`
	TargetUserID          string               `json:"targetUserId"`
	IsListening           bool                 `json:"isListening"`
	Accounts              []WechatAccountState `json:"accounts"`
}

// SendMessageReq 发送消息请求
type SendMessageReq struct {
	AccountID string `json:"accountId,omitempty"`
	ToUserID  string `json:"toUserId"`
	Content   string `json:"content"`
	FilePath  string `json:"filePath,omitempty"`
}

type AttachmentActionResult struct {
	Path     string `json:"path,omitempty"`
	Canceled bool   `json:"canceled,omitempty"`
}

// InboundMessage 接收到的消息（业务实体）
type InboundMessage struct {
	AccountID       string `json:"accountId"`
	From            string `json:"from"`
	Type            int    `json:"type"` // 1: Text, 2: Image, 3: Voice, 4: File, 5: Video
	Text            string `json:"text,omitempty"`
	MediaPath       string `json:"mediaPath,omitempty"`
	FileName        string `json:"fileName,omitempty"`
	FileSize        int64  `json:"fileSize,omitempty"`
	AttachmentID    string `json:"attachmentId,omitempty"`
	Downloadable    bool   `json:"downloadable,omitempty"`
	AttachmentError string `json:"attachmentError,omitempty"`
	Time            string `json:"time"`
}

// InboundTextPayload 文本消息载荷
type InboundTextPayload struct {
	Text string `json:"text"`
}

// InboundMediaPayload 媒体资源载荷
type InboundMediaPayload struct {
	Media InboundMedia `json:"media"`
}

// InboundMedia 微信媒体下载凭据（仅后端使用，不透传前端）
type InboundMedia struct {
	EncryptQueryParam string `json:"encrypt_query_param"`
	AESKey            string `json:"aes_key"`
	EncryptType       int    `json:"encrypt_type"`
}

// InboundFileSize 兼容微信将文件长度编码为 JSON 字符串或整数。
type InboundFileSize int64

func (s *InboundFileSize) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		*s = 0
		return nil
	}

	var text string
	if data[0] == '"' {
		if err := json.Unmarshal(data, &text); err != nil {
			return fmt.Errorf("parse file size string: %w", err)
		}
	} else {
		text = string(data)
	}

	value, err := strconv.ParseInt(text, 10, 64)
	if err != nil || value < 0 {
		return fmt.Errorf("invalid file size %q", text)
	}
	*s = InboundFileSize(value)
	return nil
}

// InboundFilePayload 文件消息载荷
type InboundFilePayload struct {
	Media    InboundMedia    `json:"media"`
	FileName string          `json:"file_name"`
	Len      InboundFileSize `json:"len"`
}

// InboundRawItem 原始单条消息元素
type InboundRawItem struct {
	Type      int                  `json:"type"`
	TextItem  *InboundTextPayload  `json:"text_item,omitempty"`
	ImageItem *InboundMediaPayload `json:"image_item,omitempty"`
	FileItem  *InboundFilePayload  `json:"file_item,omitempty"`
}

// InboundRawMsg 原始消息外层结构
type InboundRawMsg struct {
	FromUserID   string           `json:"from_user_id"`
	ToUserID     string           `json:"to_user_id"`
	ContextToken string           `json:"context_token"`
	ItemList     []InboundRawItem `json:"item_list"`
}
