package portscan

// PortStatus 端口开放状态枚举
type PortStatus string

const (
	PortOpen    PortStatus = "open"
	PortClosed  PortStatus = "closed"
	PortFiltered PortStatus = "filtered"
)

// ScanRequest 端口扫描请求
type ScanRequest struct {
	Target      string `json:"target"`      // 目标 IP 或 域名 (如 127.0.0.1, 192.168.1.1, baidu.com)
	PortRange   string `json:"portRange"`   // 端口表达式 (如 "80,443,8080-8090")
	ProxyURL    string `json:"proxyUrl"`    // 可选代理 (如 socks5://127.0.0.1:7890 或 http://127.0.0.1:7890)
	TimeoutMs   int    `json:"timeoutMs"`   // 单端口连接超时(ms)，默认 600
	Concurrency int    `json:"concurrency"` // 并发协程数，默认 30
	RateLimitMs int    `json:"rateLimitMs"` // 单 Worker 发包微延迟(ms)，温和防封模式推荐 5~15ms
	DeepDetect  bool   `json:"deepDetect"`  // 是否启用轻量指纹探测
}

// PortResult 单端口探测结果
type PortResult struct {
	Port        int        `json:"port"`
	Status      PortStatus `json:"status"`
	Service     string     `json:"service"`     // 识别出的服务名称 (如 http, ssh, mysql, redis)
	Banner      string     `json:"banner"`      // 抓取到的响应特征或标题 (如 nginx/1.24.0, OpenSSH 8.9)
	Fingerprint string     `json:"fingerprint"` // 原始指纹或证书等详情
	LatencyMs   int64      `json:"latencyMs"`   // 响应延迟 (ms)
}

// ScanProgress 实时扫描进度推送
type ScanProgress struct {
	TaskID      string      `json:"taskId"`
	Target      string      `json:"target"`
	Scanned     int         `json:"scanned"`
	Total       int         `json:"total"`
	Percent     float64     `json:"percent"`
	FoundOpen   int         `json:"foundOpen"`
	LatestPort  *PortResult `json:"latestPort,omitempty"` // 最新发现的开放端口
	IsFinished  bool        `json:"isFinished"`
	Error       string      `json:"error,omitempty"`
}

// ScanSummary 扫描任务最终汇总
type ScanSummary struct {
	TaskID     string       `json:"taskId"`
	Target     string       `json:"target"`
	TotalPorts int          `json:"totalPorts"`
	OpenPorts  []PortResult `json:"openPorts"`
	DurationMs int64        `json:"durationMs"`
}

// PresetGroup 常用预设端口分组
type PresetGroup struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Ports       string `json:"ports"`
}
