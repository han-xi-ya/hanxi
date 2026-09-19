package ocr

// ---------- 前端 API：双引擎注册表（计划 §5.4） ----------
//
// 单端口、单活引擎语义下的引擎管理面：GetEngines 供前端渲染引擎列表，
// SetActiveEngine 负责切换（停当前托管实例 → 起新件，复用既有生命周期代码，
// 无新增启停路径）。external 状态不越权，一律先让用户手动退场。

import (
	"fmt"
	"strings"

	"hanxi/internal/modules/ocr/instance"
)

// GetEngines 返回引擎注册表全量视图（含未安装引擎——前端据此渲染"导入"入口）。
// Installed 判定即时解析（登记件在位或自动发现锚点命中）；活跃引擎在线时
// Version 以 /api/status 实测值优先（探测顺带回写注册表，见 probeStatus）。
func (s *OcrService) GetEngines() ([]EngineInfo, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return nil, gateErr
	}
	defer release()
	active := s.store.GetActiveEngine()
	s.mu.Lock()
	liveVersion := s.probe.version
	online := s.probe.online
	s.mu.Unlock()

	out := make([]EngineInfo, 0, len(engineOrder))
	for _, id := range engineOrder {
		info := EngineInfo{
			ID:      id,
			Label:   engineLabel(id),
			Active:  id == active,
			Version: s.store.GetEngineVersion(id),
		}
		if exe, fromStore, err := s.resolveEngineExe(id); err == nil {
			info.Installed = true
			info.Path = exe
			info.Auto = !fromStore
		} else {
			info.Error = err.Error() // 未安装指引 / 登记路径失效，中文原因透传
		}
		if info.Active && online && liveVersion != "" {
			info.Version = liveVersion // 在线实测版本覆盖登记版本
		}
		out = append(out, info)
	}
	return out, nil
}

// SetActiveEngine 切换活跃引擎并落盘（id：wechat / paddle）。
//   - 未知 ID / 目标未安装（登记件失效或锚点无组件）→ error 通道给中文指引；
//   - 当前托管实例在跑 → 先按 StopService 语义优雅停、再按 StartService 语义起新件
//     （Detached 联动仍随 followOnExit 开关）；active 在停旧后即持久化，起新件失败
//     不回滚（注册表如实反映用户意图，失败原因折进 Message，引擎侧已广播 + notify）；
//   - 外部实例在服务（external）→ 不越权：只提示先手动停止，不做任何切换；
//   - 服务原本未运行 → 仅切换登记，不擅自拉起。
//
// 幂等：切到当前活跃引擎直接返回 already-active。
func (s *OcrService) SetActiveEngine(id string) (ControlOutcome, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return ControlOutcome{}, gateErr
	}
	defer release()
	id = strings.ToLower(strings.TrimSpace(id))
	if !isKnownEngine(id) {
		return ControlOutcome{}, fmt.Errorf("未知 OCR 引擎：%s（可选 wechat / paddle）", id)
	}
	label := engineLabel(id)

	if s.store.GetActiveEngine() == id {
		return ControlOutcome{Action: "already-active", Message: label + "已是当前引擎"}, nil
	}
	// 先验目标可用：未安装/登记件被删都拒绝在切换之前（error 携带中文指引）
	if _, _, err := s.resolveEngineExe(id); err != nil {
		return ControlOutcome{}, fmt.Errorf("无法切换到 %s：%w", label, err)
	}

	snap := s.engine.Snapshot()
	if snap.State == instance.StateExternal {
		return ControlOutcome{Action: "external-unmanaged", External: true,
			Message: "外部 hanxi-ocr 实例正在服务，Hanxi 不接管其启停；请先在其运行环境中退出该实例，再切换引擎"}, nil
	}
	wasRunning := snap.State == instance.StateRunning || snap.State == instance.StateStarting

	if wasRunning {
		if _, err := s.StopService(); err != nil {
			return ControlOutcome{}, fmt.Errorf("切换 %s 失败（停止当前服务出错）: %w", label, err)
		}
	}
	if err := s.store.SetActiveEngine(id); err != nil {
		return ControlOutcome{}, err
	}

	if !wasRunning {
		s.refresh() // 让前端立即看到新活跃引擎的解析结果（ExePath/EngineID 随动）
		return ControlOutcome{Action: "switched",
			Message: "已切换为 " + label + "（服务未启动，启动或截屏识别时自动拉起）"}, nil
	}

	start, err := s.StartService()
	if err != nil {
		return ControlOutcome{Action: "switched-start-failed",
			Message: fmt.Sprintf("已切换为 %s，但新引擎启动失败，请到文字识别页查看", label)}, nil
	}
	if start.External {
		start.Message = "已切换为 " + label + "：" + start.Message
		return start, nil
	}
	return ControlOutcome{Action: "switched",
		Message: fmt.Sprintf("已切换为 %s，新引擎已启动（%s）", label, s.addr())}, nil
}
