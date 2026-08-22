package wechat

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

// WechatState 模块当前运行时状态
type WechatState struct {
	IsLoggedIn            bool   `json:"isLoggedIn"`
	BotToken              string `json:"botToken"`
	IlinkBotID            string `json:"ilinkBotId"`
	IlinkUserID           string `json:"ilinkUserId"`
	ContextToken          string `json:"contextToken"`
	ContextTokenUpdatedAt string `json:"contextTokenUpdatedAt"`
	TargetUserID          string `json:"targetUserId"`
	IsListening           bool   `json:"isListening"`
}

// SendMessageReq 发送消息请求
type SendMessageReq struct {
	ToUserID string `json:"toUserId"`
	Content  string `json:"content"`
	FilePath string `json:"filePath,omitempty"`
}

// InboundMessage 接收到的消息
type InboundMessage struct {
	From      string `json:"from"`
	Type      int    `json:"type"` // 1: Text, 2: Image, 3: Voice, 4: File, 5: Video
	Text      string `json:"text,omitempty"`
	MediaPath string `json:"mediaPath,omitempty"`
	FileName  string `json:"fileName,omitempty"`
	Time      string `json:"time"`
}
