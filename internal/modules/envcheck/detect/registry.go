package detect

import (
	"sort"
	"sync"
)

// Detector 单个开发工具的版本探测器。
// 新增工具 = 新增一个 xxx.go（实现该接口 + init() 里 Register 一行），
// 注册表、探测流程、service 与前端均零改动。
type Detector interface {
	// Name 可执行文件名（exec.LookPath 用），兼作注册键：如 "node"、"pnpm"。
	Name() string
	// Display 前端展示名：如 "Node.js"、"Java (JRE/JDK)"。
	Display() string
	// VersionArgs 版本命令参数：如 []string{"--version"}。
	VersionArgs() []string
	// Parse 从版本命令输出中提取版本号；识别失败必须返回 ""（由流程落 error 状态）。
	Parse(output string) string
}

// DetailAware 可选能力：从同一次版本命令输出中提取结构化详情。
type DetailAware interface {
	ParseDetails(output string) *ToolDetails
}

// StubAware 可选能力：判定"假安装"（Microsoft Store 存根）。
// 仅在版本命令执行失败或输出为空时被 DetectOne 询问，命中则状态升级为 store-stub。
type StubAware interface {
	IsStoreStub(exePath string) bool
}

// MissingHintAware 可选能力：为未安装状态提供更准确的安装说明。
type MissingHintAware interface {
	MissingHint() string
}

// 包级注册表：各探测器在 init() 阶段自主注册，无中央清单。
// 注册全部发生在包初始化期，此后只读，读写锁仅是并发防御。
var (
	regMu    sync.RWMutex
	registry []Detector
)

// Register 注册一个探测器（供各工具文件的 init() 调用）。
func Register(d Detector) {
	regMu.Lock()
	defer regMu.Unlock()
	registry = append(registry, d)
}

// Detectors 返回全部已注册探测器的快照，按 Name 字典序稳定排序。
// RunAll 会在探测完成后按状态重新分组：已安装在前，未安装与异常环境在后。
func Detectors() []Detector {
	regMu.RLock()
	defer regMu.RUnlock()
	ds := append([]Detector(nil), registry...)
	sort.Slice(ds, func(i, j int) bool { return ds[i].Name() < ds[j].Name() })
	return ds
}
