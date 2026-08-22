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

// InboundMessage 接收到的消息（业务实体）
type InboundMessage struct {
	From      string `json:"from"`
	Type      int    `json:"type"` // 1: Text, 2: Image, 3: Voice, 4: File, 5: Video
	Text      string `json:"text,omitempty"`
	MediaPath string `json:"mediaPath,omitempty"`
	FileName  string `json:"fileName,omitempty"`
	Time      string `json:"time"`
}

// InboundTextPayload 文本消息载荷
type InboundTextPayload struct {
	Text string `json:"text"`
}

// InboundMediaPayload 媒体资源载荷
type InboundMediaPayload struct {
	Media struct {
		EncryptQueryParam string `json:"encrypt_query_param"`
		AESKey            string `json:"aes_key"`
	} `json:"media"`
}

// InboundFilePayload 文件消息载荷
type InboundFilePayload struct {
	FileName string `json:"file_name"`
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

