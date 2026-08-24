package fileshare

import "time"

// ShareConfig 局域网共享服务配置
type ShareConfig struct {
	Port            int    `json:"port"`            // 监听端口 (默认 80，0 为系统自动随机分配)
	SharePath       string `json:"sharePath"`       // 挂载共享的本地物理绝对路径
	AllowUpload     bool   `json:"allowUpload"`     // 是否允许访客上传文件
	AllowTextDrop   bool   `json:"allowTextDrop"`   // 是否允许文本/链接投递
	AutoSaveToMemo  bool   `json:"autoSaveToMemo"`  // 投递的文本是否自动存入 DevMemo 备忘录
	MaxUploadSizeMB int64  `json:"maxUploadSizeMB"` // 单文件最大上传限制 (MB，0 为不限制)
	AuthToken       string `json:"authToken"`       // 可选访问口令 (为空表示免密局域网共享)
}

// ServerStatus 服务运行时状态
type ServerStatus struct {
	IsRunning         bool     `json:"isRunning"`
	Port              int      `json:"port"`
	SharePath         string   `json:"sharePath"`
	AllowUpload       bool     `json:"allowUpload"`
	AllowTextDrop     bool     `json:"allowTextDrop"`
	AutoSaveToMemo    bool     `json:"autoSaveToMemo"`
	ActiveURLs        []string `json:"activeUrls"`        // 如 http://192.168.1.100:8080
	ActiveConnections int64    `json:"activeConnections"` // 活跃传输连接数
	UploadCount       int64    `json:"uploadCount"`       // 累计上传文件数
	DownloadCount     int64    `json:"downloadCount"`     // 累计下载次数
	UploadBytes       int64    `json:"uploadBytes"`       // 累计上传字节数
	DownloadBytes     int64    `json:"downloadBytes"`     // 累计下载字节数
	UploadRate        float64  `json:"uploadRate"`        // 当前上传速率 (B/s)
	DownloadRate      float64  `json:"downloadRate"`      // 当前下载速率 (B/s)
	StartedAt         string   `json:"startedAt"`         // 启动时间
}

// NetworkEndpoint 局域网接入点信息
type NetworkEndpoint struct {
	InterfaceName string `json:"interfaceName"` // 网卡名称 (如 "Wi-Fi", "以太网")
	IP            string `json:"ip"`            // IPv4 地址
	URL           string `json:"url"`           // 完整访问 URL
	IsDefault     bool   `json:"isDefault"`     // 是否具有网关 (默认出站网卡)
}

// FileEntry 局域网 Web 端展示的文件或目录条目
type FileEntry struct {
	Name      string    `json:"name"`
	Path      string    `json:"path"`      // 相对 URL 路径 (经 URL 编码)
	Size      int64     `json:"size"`      // 字节大小
	SizeHuman string    `json:"sizeHuman"` // 格式化大小 (如 "12.4 MB")
	IsDir     bool      `json:"isDir"`
	ModTime   time.Time `json:"modTime"`
	Ext       string    `json:"ext"` // 扩展名 (小写)
}

// DropItem 局域网访客投递的文本/链接
type DropItem struct {
	ID        string    `json:"id"`
	Content   string    `json:"content"`   // 文本内容或 URL
	SenderIP  string    `json:"senderIp"`  // 来源客户端 IP
	UserAgent string    `json:"userAgent"` // 客户端 UA
	CreatedAt time.Time `json:"createdAt"`
	IsURL     bool      `json:"isUrl"`
}

// TransferEvent 文件传输审计事件 (用于界面日志流)
type TransferEvent struct {
	Type      string    `json:"type"` // "upload" | "download" | "drop"
	Filename  string    `json:"filename"`
	Size      int64     `json:"size"`
	ClientIP  string    `json:"clientIp"`
	Timestamp time.Time `json:"timestamp"`
	Success   bool      `json:"success"`
	ErrorMsg  string    `json:"errorMsg,omitempty"`
}
