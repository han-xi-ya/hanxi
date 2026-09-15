package wsl

// 后台长操作的统一取消通道。设计口径：
//   - 克隆（拷贝/挂载段）与 MSI 下载：随时可停——失败路径本就完备
//     （半成品自清、原件不碰），取消只是让人不必干等；
//   - 瘦身：只在"尚未碰数据盘"的阶段（备份/待停/等停）允许取消；
//     进入 Optimize-VHD 或注销重导入段后拒绝——半途而废会把用户
//     留在比"慢"更糟的中间态，宁可等它跑完或自己失败（都有备份兜底）。

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// cancelableCompactStages 瘦身可取消阶段表（表外一律拒绝）。
var cancelableCompactStages = map[string]bool{"backup": true, "trim": true, "waiting": true}

// compactStageText 阶段的中文称谓（取消拒绝文案用）。
var compactStageText = map[string]string{
	"backup":   "全量备份",
	"trim":     "fstrim/停机",
	"waiting":  "等待停止确认",
	"optimize": "Tier1 数据盘压缩",
	"reimport": "Tier2 注销重导入",
}

func (s *WslService) registerClone(src string, cancel context.CancelFunc) {
	s.mu.Lock()
	if s.cloneOps == nil {
		s.cloneOps = map[string]longOpHandle{}
	}
	s.cloneOps[strings.ToLower(src)] = longOpHandle{cancel: cancel, stage: "copying"}
	s.mu.Unlock()
}

func (s *WslService) setCloneStage(src, stage string) {
	k := strings.ToLower(src)
	s.mu.Lock()
	if h, ok := s.cloneOps[k]; ok {
		h.stage = stage
		s.cloneOps[k] = h
	}
	s.mu.Unlock()
}

func (s *WslService) finishClone(src string) {
	k := strings.ToLower(src)
	s.mu.Lock()
	delete(s.cloneOps, k)
	s.mu.Unlock()
}

// CancelClone 请求取消指定发行版进行中的克隆。
func (s *WslService) CancelClone(src string) (OperationOutcome, error) {
	src = strings.TrimSpace(src)
	k := strings.ToLower(src)
	s.mu.Lock()
	h, ok := s.cloneOps[k]
	s.mu.Unlock()
	if !ok {
		return OperationOutcome{}, fmt.Errorf("%s 当前没有进行中的克隆", src)
	}
	h.cancel()
	return OperationOutcome{Success: true, Message: fmt.Sprintf("已请求取消克隆（当前阶段：%s），等待其落终态回执", h.stage)}, nil
}

func (s *WslService) registerCompact(cancel context.CancelFunc) {
	s.mu.Lock()
	s.compactOp = &longOpHandle{cancel: cancel, stage: "backup"}
	s.mu.Unlock()
}

func (s *WslService) setCompactStage(stage string) {
	s.mu.Lock()
	if s.compactOp != nil {
		s.compactOp.stage = stage
	}
	s.mu.Unlock()
}

func (s *WslService) finishCompact() {
	s.mu.Lock()
	s.compactOp = nil
	s.mu.Unlock()
}

// CancelCompact 请求取消进行中的瘦身；已进入数据盘处理/重建段则拒绝。
func (s *WslService) CancelCompact() (OperationOutcome, error) {
	s.mu.Lock()
	h := s.compactOp
	s.mu.Unlock()
	if h == nil {
		return OperationOutcome{}, errors.New("当前没有进行中的磁盘瘦身")
	}
	if !cancelableCompactStages[h.stage] {
		zh := compactStageText[h.stage]
		if zh == "" {
			zh = h.stage
		}
		return OperationOutcome{}, fmt.Errorf("瘦身已进入「%s」段——此段取消会把数据盘留在中间态，拒绝；等它跑完或自行失败都有备份兜底", zh)
	}
	h.cancel()
	return OperationOutcome{Success: true, Message: "已请求取消瘦身（备份/停机前的安全段），等待终态回执"}, nil
}

func (s *WslService) registerDownload(cancel context.CancelFunc) bool {
	s.mu.Lock()
	if s.dlOp != nil {
		s.mu.Unlock()
		return false
	}
	s.dlOp = &longOpHandle{cancel: cancel, stage: "downloading"}
	s.mu.Unlock()
	return true
}

func (s *WslService) finishDownload() {
	s.mu.Lock()
	s.dlOp = nil
	s.mu.Unlock()
}

// CancelMsiDownload 取消进行中的 MSI 下载（.part 临时文件随协程清理，正式文件不受影响）。
func (s *WslService) CancelMsiDownload() (OperationOutcome, error) {
	s.mu.Lock()
	h := s.dlOp
	s.mu.Unlock()
	if h == nil {
		return OperationOutcome{}, errors.New("当前没有进行中的下载")
	}
	h.cancel()
	return OperationOutcome{Success: true, Message: "已请求取消下载"}, nil
}

// canceled 判断错误链里是否是取消语义（用户主动停 vs 真失败）。
func canceled(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
