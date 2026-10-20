package portscan

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"hanxi/internal/extapi"
	"hanxi/internal/notify"
)

// PortScanService 暴露给前端的端口扫描服务。
// cancelMap 按任务 ID 登记每轮扫描的 context.CancelFunc，供 StopScan 精准取消；任务结束须删除条目防泄露。
// current 记录最新一轮任务 ID（而非 CancelFunc）：任务收尾按 ID 比对后才摘除标记，
// 否则旧任务的清理路径会把新任务刚登记的 current 一并删掉，导致 StopScan 兜底失效。
// 业务 RPC 方法经 holder.Enter() 接入统一调用门（Wave 3）；StopScan 拆出未接门的
// 内部版供 OnDestroy 直调（停用流程门已关，生命周期收口不得依赖运行态）。
type PortScanService struct {
	scanner   *Scanner
	holder    *extapi.LeaseHolder
	cancelMap sync.Map // map[string]context.CancelFunc
	// currentMu 只护 current 一个字符串——登记与"比对后再摘除"必须原子完成
	currentMu sync.Mutex
	current   string
}

// setCurrent 把 current 指向本轮任务，返回被顶替的上一轮任务 ID（无则空串）。
func (s *PortScanService) setCurrent(id string) string {
	s.currentMu.Lock()
	defer s.currentMu.Unlock()
	old := s.current
	s.current = id
	return old
}

// currentID 取当前任务 ID（无在册任务则空串）。
func (s *PortScanService) currentID() string {
	s.currentMu.Lock()
	defer s.currentMu.Unlock()
	return s.current
}

// clearCurrent 任务收尾摘除标记：仅当 current 仍指向自己才清，
// 已被新一轮任务顶替时保持不动，避免把新任务的登记误删。
func (s *PortScanService) clearCurrent(id string) {
	s.currentMu.Lock()
	defer s.currentMu.Unlock()
	if s.current == id {
		s.current = ""
	}
}

// NewPortScanService 创建无状态服务实例。
func NewPortScanService(holder *extapi.LeaseHolder) *PortScanService {
	return &PortScanService{
		scanner: NewScanner(),
		holder:  holder,
	}
}

// GetPresets 返回常见预设端口组合。
// Wave 3 口径：单值绑定签名扩为 ([]PresetGroup, error)，门拒绝如实上抛，禁止回空表。
func (s *PortScanService) GetPresets() ([]PresetGroup, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return nil, gateErr
	}
	defer release()
	return GetPresets(), nil
}

// CheckEgressIP 探测当前扫描配置（直连或代理）下的实际发包出网 IP
func (s *PortScanService) CheckEgressIP(proxyURL string) (string, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return "", gateErr
	}
	defer release()
	ctx, cancel := context.WithTimeout(context.Background(), 3500*time.Millisecond)
	defer cancel()
	return s.scanner.QueryEgressIP(ctx, proxyURL, 3000*time.Millisecond)
}

// CheckProxy 测一条代理的连通性，同步返回结论。
//
// 刻意不做批量任务制（cancelMap / 进度事件 / 前端流式上屏）：单条判定才是这个工具
// 的用途，为它配一套任务生命周期只会长出没人走的路径。
//
// 外层 ctx 当保险丝用（3 倍预算）：单条自身在 netx 侧已有 conn deadline、client
// timeout 与响应体封顶三层封顶，这里防的是"哪天某层封顶被改动"导致整次调用挂死。
func (s *PortScanService) CheckProxy(req ProxyCheckRequest) (*ProxyCheckResult, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return nil, gateErr
	}
	defer release()

	per := time.Duration(req.effectiveTimeoutMs()) * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), per*3)
	defer cancel()

	return s.scanner.CheckProxy(ctx, req)
}

// CheckSSH 测一条 SSH 端点的连通性与版本横幅。
//
// **能力边界**（别读成"能登录"）：只做协议第一回合的版本交换，不做密钥交换与认证
// ——后者需要 golang.org/x/crypto/ssh，本仓不依赖。所以 OK 的含义是"连上了，且对方
// 确实是 SSH 服务、拿到了它的版本"，与凭据无关（这个 RPC 压根不收凭据）。
func (s *PortScanService) CheckSSH(req SSHCheckRequest) (*SSHCheckResult, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return nil, gateErr
	}
	defer release()

	per := time.Duration(req.TimeoutMs) * time.Millisecond
	if per <= 0 {
		per = SSHCheckDefaultTimeoutMs * time.Millisecond
	}
	// 外层只当保险丝：连接与读横幅各自受 conn deadline 约束。
	ctx, cancel := context.WithTimeout(context.Background(), per*2)
	defer cancel()

	return s.scanner.CheckSSH(ctx, req)
}

// CheckSSHAuth 用给定凭据试一次 SSH 密码认证。
//
// 与 CheckSSH（只读版本横幅）刻意分成两个 RPC 与两个按钮：**这一步会真的向服务端发起
// 一次登录尝试**，会在对端 sshd 日志里留下记录，也可能触发 fail2ban 之类的封禁。
// 它只能是用户显式按下的动作，不能藏在"看看端口通不通"后面顺手做掉。
// 密码只在本次调用内存里活着：不进结果、不进错误文本、不落盘。
func (s *PortScanService) CheckSSHAuth(req SSHAuthRequest) (*SSHAuthResult, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return nil, gateErr
	}
	defer release()

	per := time.Duration(req.TimeoutMs) * time.Millisecond
	if per <= 0 {
		per = SSHCheckDefaultTimeoutMs * time.Millisecond
	}
	// KEX 与认证两段共用一条 conn deadline，这里的外层 ctx 只当保险丝。
	ctx, cancel := context.WithTimeout(context.Background(), per*2)
	defer cancel()

	return s.scanner.CheckSSHAuth(ctx, req)
}

// StartScan 启动端口扫描任务（异步），通过 Wails 事件 "portscan:progress" 实时推送进度。
// operation lease 覆盖整个扫描过程：扫描在途时停用需等待 drain（或由 drain 超时强制收口）。
func (s *PortScanService) StartScan(req ScanRequest) (*ScanSummary, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return nil, gateErr
	}
	defer release()
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
	// 将 current 指向最新任务，并主动取消被顶掉的上一轮任务（其条目一并摘除，
	// 由新任务接管取消权；旧任务自身的 defer 清理按 ID 比对，不会再误伤本轮登记）
	if oldID := s.setCurrent(taskID); oldID != "" && oldID != taskID {
		if old, loaded := s.cancelMap.LoadAndDelete(oldID); loaded {
			if c, ok := old.(context.CancelFunc); ok {
				c()
			}
		}
	}

	defer func() {
		s.cancelMap.Delete(taskID)
		// 仅当 current 仍指向本轮任务时才摘除；已被新任务顶替则保持不动
		s.clearCurrent(taskID)
		cancel()
	}()

	timeout := time.Duration(req.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = DefaultScanTimeout
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

// StopScan 中止指定任务（前端 RPC 入口，经调用门）。
// Wave 3 口径：单值绑定签名扩为 (bool, error)，门拒绝如实上抛，禁止回 false 假象。
func (s *PortScanService) StopScan(taskID string) (bool, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return false, gateErr
	}
	defer release()
	return s.stopScan(taskID), nil
}

// stopScan 中止指定任务的生命周期内部版：不接调用门，供 OnDestroy 在"门已关闭"
// 的停用流程中直调（若接门则永远拒无中止，残留孤儿扫描）。
// 指定 ID 未在册（或为空）时回退中止 current 任务。
// 兜底路径按 current 记录的 ID 反查取消函数，两条路径都遵循"取消即摘除条目"。
func (s *PortScanService) stopScan(taskID string) bool {
	stopped := false
	taskID = strings.TrimSpace(taskID)

	if taskID != "" {
		if val, ok := s.cancelMap.LoadAndDelete(taskID); ok {
			if cancel, ok := val.(context.CancelFunc); ok {
				cancel()
				s.clearCurrent(taskID)
				stopped = true
			}
		}
	}

	// 指定 ID 没找到或者为空，尝试中止 current 任务
	if !stopped {
		if cur := s.currentID(); cur != "" {
			if val, ok := s.cancelMap.LoadAndDelete(cur); ok {
				if cancel, ok := val.(context.CancelFunc); ok {
					cancel()
					stopped = true
				}
			}
			s.clearCurrent(cur)
		}
	}

	return stopped
}
