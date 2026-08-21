package portscan

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
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
	// 同时将 "current" 指向最新任务，确保即使 taskID 未传到前端也能随时停下
	s.cancelMap.Store("current", cancel)

	defer func() {
		s.cancelMap.Delete(taskID)
		s.cancelMap.Delete("current")
		cancel()
	}()

	timeout := time.Duration(req.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 800 * time.Millisecond
	}

	summary, err := s.scanner.ExecuteScan(
		ctx,
		taskID,
		target,
		ports,
		timeout,
		req.Concurrency,
		req.DeepDetect,
		func(p ScanProgress) {
			if app := application.Get(); app != nil && app.Event != nil {
				app.Event.Emit("portscan:progress", p)
			}
		},
	)

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
