package sshkeys

import (
	"context"
	"fmt"
	"strings"
	"time"

	"hanxi/internal/extapi"
	"hanxi/internal/settings"
)

// SshKeysService 暴露给前端的 SSH 主机服务。
//
// 本结构体是 Wails 绑定面（每个导出方法前端可直调），故每个导出方法必须经
// holder.Enter() 接入统一调用门（Wave 3）——internal/app/rpc_gate_matrix_test.go
// 按 composition contract 逐模块 AST 穷举校验，漏接即测试红，不靠人记。
//
// prober 无状态，探测方法可直接并发调用；它自己那层刻意不接门（保持搬家前的
// 调用形状，见 prober.go 头注），门只在这一层上。
type SshKeysService struct {
	prober *prober
	holder *extapi.LeaseHolder
	// ledger / pins 都是"构造时定路径、首次访问才读盘"的存储：构造无 IO，
	// 装配期不因数据根抖动而失败（对齐 frpc 的 NewFrpcService 口径）。
	ledger *ledger
	pins   *pinsStore
}

// NewSshKeysService 创建服务实例。构造无 IO。
func NewSshKeysService(holder *extapi.LeaseHolder) *SshKeysService {
	stateDir := settings.GetPaths().StateDir()
	return &SshKeysService{
		prober: newProber(),
		holder: holder,
		ledger: newLedger(stateDir),
		pins:   newPinsStore(stateDir),
	}
}

// ---- 主机清单（账本）----

// ListHosts 返回按别名升序的清单副本。
func (s *SshKeysService) ListHosts() ([]HostEntry, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return nil, gateErr
	}
	defer release()
	return s.ledger.list()
}

// AddHost 新增一条；别名已存在则拒绝（重复别名会破坏渲染层的段数自证，见 ledger.go）。
// 返回新增后的整表：前端省一次刷新，也避免"保存成功但列表还是旧的"这种错觉。
func (s *SshKeysService) AddHost(h HostEntry) ([]HostEntry, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return nil, gateErr
	}
	defer release()
	if err := s.ledger.add(h); err != nil {
		return nil, err
	}
	return s.ledger.list()
}

// UpdateHost 整条覆盖已有别名（别名本身不可改，理由见 ledger.go）。
func (s *SshKeysService) UpdateHost(h HostEntry) ([]HostEntry, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return nil, gateErr
	}
	defer release()
	if err := s.ledger.update(h); err != nil {
		return nil, err
	}
	return s.ledger.list()
}

// RemoveHost 从账本移除一条。**不碰磁盘上的私钥、也不自动改产物**——
// 产物是否重生成由人决定（见 ExportConf）；删一条清单记录绝不该顺手清掉钥匙。
func (s *SshKeysService) RemoveHost(alias string) ([]HostEntry, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return nil, gateErr
	}
	defer release()
	if err := s.ledger.remove(alias); err != nil {
		return nil, err
	}
	return s.ledger.list()
}

// ---- 导出投递（ssh 真正会读的那份）----

// ExportConf 显式动作：渲染账本写 ~/.ssh/hanxi.conf，并保证 Include 接线在位。
//
// 刻意与"保存清单"分离：**账本改动不会自动落进用户主目录**（paths.go 那条
// "禁静默落数据"的纪律），必须人再点一次这个按钮。代价是多一步操作，换来的是
// "hanxi 什么时候碰了我的 ~/.ssh"这件事永远看得见、且可撤销。
func (s *SshKeysService) ExportConf() (ExportSummary, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return ExportSummary{}, gateErr
	}
	defer release()
	return s.ledger.exportConf()
}

// ExportStatus 只读对账：产物落后于账本吗、Include 接上了吗、有几条钥匙本机不存在。
func (s *SshKeysService) ExportStatus() (ExportSummary, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return ExportSummary{}, gateErr
	}
	defer release()
	return s.ledger.exportStatus()
}

// RemoveExport 撤销导出：删自己那份产物、摘自己那段 Include；用户自己的 config 不删。
func (s *SshKeysService) RemoveExport() (ExportSummary, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return ExportSummary{}, gateErr
	}
	defer release()
	return s.ledger.removeExport()
}

// ---- 主机密钥钉定（TOFU）----

// ListHostKeyPins 返回"本机认过指纹"的地址清单，供界面展示与逐条清除。
func (s *SshKeysService) ListHostKeyPins() ([]PinInfo, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return nil, gateErr
	}
	defer release()
	return s.listPins()
}

// listPins 未接门的内部版：供已持门的调用方复用，**不在其中再 Enter**——
// 租约嵌套计数虽然能对齐，但"停用时的 drain 要等几次归零"这件事一旦被嵌套就说不清。
func (s *SshKeysService) listPins() ([]PinInfo, error) {
	rows, err := s.pins.All()
	if err != nil {
		return nil, err
	}
	out := make([]PinInfo, 0, len(rows))
	for _, r := range rows {
		out = append(out, PinInfo{Addr: r.Addr, Fingerprint: r.Fingerprint})
	}
	return out, nil
}

// ResetHostKeyPin 清除某地址的钉定——重装服务器后指纹合法变化的**唯一**出口。
// 刻意不自动放行：自动放行等于 TOFU 只生效一次，之后与不校验没有区别。
func (s *SshKeysService) ResetHostKeyPin(addr, confirm string) ([]PinInfo, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return nil, gateErr
	}
	defer release()
	if strings.TrimSpace(confirm) != strings.TrimSpace(addr) {
		return nil, fmt.Errorf("清除钉定需要把地址原样回一遍（防手滑）：期望 %q", addr)
	}
	if err := s.pins.Reset(addr); err != nil {
		return nil, err
	}
	return s.listPins()
}

// CheckSSH 测一条 SSH 端点的连通性与版本横幅。
//
// **能力边界**（别读成"能登录"）：只做协议第一回合的版本交换，不做密钥交换与认证。
// 分层是刻意的——不是因为做不到（同包就在用 x/crypto），而是读横幅不碰凭据、不在
// 对端 sshd 日志留登录尝试记录，可以放心"看一眼"；发起认证尝试必须另走显式动作。
// 所以 OK 的含义是"连上了，且对方确实是 SSH 服务、拿到了它的版本"，与凭据无关
// （这个 RPC 压根不收凭据）。
func (s *SshKeysService) CheckSSH(req SSHCheckRequest) (*SSHCheckResult, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return nil, gateErr
	}
	defer release()

	per := sshTimeoutOf(req.TimeoutMs)
	// 外层只当保险丝：连接与读横幅各自受 conn deadline 约束。
	ctx, cancel := context.WithTimeout(context.Background(), per*2)
	defer cancel()

	return s.prober.CheckSSH(ctx, req)
}

// CheckSSHAuth 用给定凭据试一次 SSH 密码认证。
//
// 与 CheckSSH（只读版本横幅）刻意分成两个 RPC 与两个按钮：**这一步会真的向服务端发起
// 一次登录尝试**，会在对端 sshd 日志里留下记录，也可能触发 fail2ban 之类的封禁。
// 它只能是用户显式按下的动作，不能藏在"看看端口通不通"后面顺手做掉。
// 密码只在本次调用内存里活着：不进结果、不进错误文本（过 redactSecret）、不落盘。
func (s *SshKeysService) CheckSSHAuth(req SSHAuthRequest) (*SSHAuthResult, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return nil, gateErr
	}
	defer release()

	per := sshTimeoutOf(req.TimeoutMs)
	// KEX 与认证两段共用一条 conn deadline，这里的外层 ctx 只当保险丝。
	ctx, cancel := context.WithTimeout(context.Background(), per*2)
	defer cancel()

	return s.prober.CheckSSHAuth(ctx, req)
}

// sshTimeoutOf 归一单次探测预算：缺省与上限口径原先散在两个 RPC 里各写一遍，
// 现在两个 RPC 共用同一函数——否则日后调上限只改了一处，两个标签页行为就分叉。
func sshTimeoutOf(timeoutMs int) time.Duration {
	if timeoutMs <= 0 {
		return SSHCheckDefaultTimeoutMs * time.Millisecond
	}
	if timeoutMs > 30000 {
		return 30 * time.Second
	}
	return time.Duration(timeoutMs) * time.Millisecond
}
