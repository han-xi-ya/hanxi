package portscan

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"hanxi/internal/notify"
)

// PortScanService 暴露给前端的端口扫描服务
type PortScanService struct {
	scanner   *Scanner
	cancelMap sync.Map // map[string]context.CancelFunc
}

func NewPortScanService() *PortScanService {
	return &PortScanService{
		scanner: NewScanner(),
	}
}

// GetPresets 返回常见预设端口组合
func (s *PortScanService) GetPresets() []PresetGroup {
	return GetPresets()
}

// CheckEgressIP 探测当前扫描配置（直连或代理）下的实际发包出网 IP
func (s *PortScanService) CheckEgressIP(proxyURL string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3500*time.Millisecond)
	defer cancel()
	return s.scanner.QueryEgressIP(ctx, proxyURL, 3000*time.Millisecond)
}

// StartScan 启动端口扫描任务（异步），通过 Wails 事件 "portscan:progress" 实时推送进度
func (s *PortScanService) StartScan(req ScanRequest) (*ScanSummary, error) {
	target := strings.TrimSpace(req.Target)
	if target == "" {
		return nil, fmt.Errorf("目标地址不能为空")
	}

	ports, err := ParsePortRange(req.PortRange)
	if err != nil {
		return nil, err
	}

	taskID := fmt.Sprintf("scan_%d", time.Now().UnixNano())
	ctx, cancel := context.WithCancel(context.Background())
	s.cancelMap.Store(taskID, cancel)
	// 同时将 "current" 指向最新任务，若存在上一个未结束的任务则主动触发取消
	if oldCancel, loaded := s.cancelMap.Swap("current", cancel); loaded && oldCancel != nil {
		if c, ok := oldCancel.(context.CancelFunc); ok {
			c()
		}
	}

	defer func() {
		s.cancelMap.Delete(taskID)
		s.cancelMap.Delete("current")
		cancel()
	}()

	timeout := time.Duration(req.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 600 * time.Millisecond
	}

	summary, err := s.scanner.ExecuteScan(
		ctx,
		taskID,
		target,
		ports,
		req.ProxyURL,
		timeout,
		req.Concurrency,
		req.RateLimitMs,
		req.DeepDetect,
		func(p ScanProgress) {
			if app := application.Get(); app != nil && app.Event != nil {
				app.Event.Emit("portscan:progress", p)
			}
		},
	)

	if err == nil && summary != nil {
		notify.Success("portscan", "端口扫描完成", fmt.Sprintf("目标 %s 扫描完成，共开放 %d 个端口（耗时 %dms）", target, len(summary.OpenPorts), summary.DurationMs), "/ext/portscan")
	}

	return summary, err
}

// StopScan 中止指定任务
func (s *PortScanService) StopScan(taskID string) bool {
	stopped := false
	taskID = strings.TrimSpace(taskID)

	if taskID != "" {
		if val, ok := s.cancelMap.Load(taskID); ok {
			if cancel, ok := val.(context.CancelFunc); ok {
				cancel()
				s.cancelMap.Delete(taskID)
				stopped = true
			}
		}
	}

	// 如果指定 ID 没找到或者为空，尝试中止 current 任务
	if val, ok := s.cancelMap.Load("current"); ok {
		if cancel, ok := val.(context.CancelFunc); ok {
			cancel()
			s.cancelMap.Delete("current")
			stopped = true
		}
	}

	return stopped
}
