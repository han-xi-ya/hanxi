package ocr

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ServiceState 前端唯一状态视图模型：引擎快照 + /api/status 探测缓存合并。
// 事件 ocr:service-state 与 GetStatus 共用同一结构。
type ServiceState struct {
	State         string `json:"state"`    // stopped / starting / running / external / failed
	Online        bool   `json:"online"`   // 最近一次契约探测成功
	Managed       bool   `json:"managed"`  // 我方 JobObject 托管实例（running）
	External      bool   `json:"external"` // 外部实例（只转发不管理）
	PID           uint32 `json:"pid"`
	ListenAddr    string `json:"listenAddr"` // 127.0.0.1:<port>
	ExePath       string `json:"exePath"`    // 生效的服务程序路径（发现结果或设定值）
	ExeAuto       bool   `json:"exeAuto"`    // 生效路径是否来自自动发现（false=用户显式指定）
	Version       string `json:"version"`    // 上游 status.version
	Engine        string `json:"engine"`     // 上游 status.engine（如 wxocr@8094 / wcdir@4.1.13.65）
	EngineRunning bool   `json:"engineRunning"`
	Hung          bool   `json:"hung"`      // 上游报告引擎 Call 挂死
	Error         string `json:"error"`     // 中文人话错误（failed 时非空）
	CheckedAt     string `json:"checkedAt"` // 最近探测时刻（格式化，状态新鲜度）
}

// OcrLine 单行识别结果与左上角坐标（4.0 引擎不提供宽高/置信度）。
type OcrLine struct {
	Text string `json:"text"`
	X    int    `json:"x"`
	Y    int    `json:"y"`
}

// OcrOutcome 识别操作结果（业务失败折进 Error 中文，error 通道留给程序性错误）。
type OcrOutcome struct {
	Ok        bool      `json:"ok"`
	Text      string    `json:"text"`
	Lines     []OcrLine `json:"lines"`
	ElapsedMs int64     `json:"elapsedMs"`
	Error     string    `json:"error"`
}

// ImageRef 统一"已选图"模型：对话框/拖拽/粘贴三通道汇流。
type ImageRef struct {
	Path       string `json:"path"`
	Name       string `json:"name"`
	Size       int64  `json:"size"`
	PreviewURL string `json:"previewUrl"` // dataURL；超限为空
	Temporary  bool   `json:"temporary"`  // 临时件（RuntimeDir/ocr），生命周期归本模块
}

// ControlOutcome 启停操作结果（仿 KillResult/QuitOutcome 口径）。
type ControlOutcome struct {
	Action   string `json:"action"` // started / already-running / starting / external / stopped / already-stopped / external-unmanaged
	External bool   `json:"external"`
	Message  string `json:"message"`
}

// resolveServiceExe 纯函数：hanxi-ocr.exe 发现顺序（exeDir 注入便于单测）。
//  1. 用户设定路径（存在即用；失效以 error 明示，不静默回退）
//  2. Hanxi 同级目录 ../hanxi-ocr/hanxi-ocr.exe（私发组件默认落位）
//  3. PATH 兜底
func resolveServiceExe(exeDir, stored string) (path string, fromStore bool, err error) {
	if p := strings.TrimSpace(stored); p != "" {
		if st, e := os.Stat(p); e == nil && !st.IsDir() {
			return p, true, nil
		}
		return "", true, fmt.Errorf("设置的服务路径已失效：%s，请在设置中重新指定", p)
	}
	cand := filepath.Join(filepath.Dir(exeDir), "hanxi-ocr", "hanxi-ocr.exe")
	if st, e := os.Stat(cand); e == nil && !st.IsDir() {
		return cand, false, nil
	}
	if p, e := exec.LookPath("hanxi-ocr.exe"); e == nil {
		return p, false, nil
	}
	return "", false, fmt.Errorf("未找到 hanxi-ocr.exe，默认预期位置 %s；可在设置中手动指定路径", cand)
}
