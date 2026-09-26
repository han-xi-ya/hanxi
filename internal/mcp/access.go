package mcp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
)

// AccessFileName 授权文件名；完整路径 <DataDir>/mcp/access.json（PLAN_MCP §6 拍板文本，
// 与 GUI「AI 接入」分区（F4b）共用的唯一契约——字段与路径不得擅自扩展）。
const (
	AccessFileName    = "access.json"
	accessDirName     = "mcp"
	maxAccessFileSize = 16 << 10 // 16KB 上限：授权文件本应只有一小段 JSON，超限视为异常拒绝采信
	accessSchemaVer   = 1        // 当前唯一合法 version（升级须双读过渡，MCP 侧读到不认识的版本=全拒绝）
)

// accessFile 是 access.json 的落盘结构（§6 拍板文本逐字段实现；契约扩充批至
// 六键 N32/N34、AI 接入批加 portscan/lan 至八键——未知键整体拒读的 fail-closed
// 裁决只认 knownModuleIDs 集合，键集演进必须与写方 mcpwizard 同步）：
//
//	{"version":1,"tools":{"envcheck":true,"everything":false,"ocr":false,"memo":false,"sysinfo":false,"logs":false,"portscan":false,"lan":false}}
type accessFile struct {
	Version int             `json:"version"`
	Tools   map[string]bool `json:"tools"`
}

// Access 是 access.json 授权引擎：fail-closed + 每次调用重读。
//
// 判定口径（PLAN_MCP §3/§6，与 F4b 对账以此为准）：
//   - 文件缺失：合法状态（默认全关），全部工具拒绝；
//   - 文件损坏 / 体积超 16KB / version≠1 / 类型不符 / 出现未知顶层字段或未知 tools 键：
//     一律视为非法授权态，全部工具拒绝（宁可错杀，绝不带病放行）；
//   - tools 中缺某个键：该工具按默认 false 处理；
//   - 每次 Allowed 都重新读盘：GUI 撤权即时生效，MCP 会话内不养缓存。
type Access struct {
	path string

	// reason 记录最近一次判定的拒绝原因（仅诊断用途，日志可读；并发保护下同锁）。
	mu     sync.Mutex
	reason string
}

// NewAccess 指向 <DataDir>/mcp/access.json。构造无 IO。
func NewAccess(path string) *Access {
	return &Access{path: path}
}

// Path 返回授权文件绝对路径（日志与诊断用）。
func (a *Access) Path() string { return a.path }

// Allowed 报告 moduleID 对应工具是否已授权。任何异常都收敛为 false（fail-closed）。
func (a *Access) Allowed(moduleID string) bool {
	tools, err := a.load()
	if err != nil {
		a.setReason(err.Error())
		return false
	}
	allowed := tools[moduleID] // 缺键=零值 false（默认全关）
	if !allowed {
		a.setReason(fmt.Sprintf("tool %q not enabled in %s", moduleID, a.path))
	}
	return allowed
}

// LastReason 返回最近一次拒绝判定的原因快照（诊断/日志用，不参与授权判定）。
func (a *Access) LastReason() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.reason
}

func (a *Access) setReason(s string) {
	a.mu.Lock()
	a.reason = s
	a.mu.Unlock()
}

// load 读盘并严格解析。文件不存在返回空集合（默认全关，非错误）。
func (a *Access) load() (map[string]bool, error) {
	fi, err := os.Stat(a.path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]bool{}, nil
		}
		return nil, fmt.Errorf("access.json stat failed: %w", err)
	}
	if fi.IsDir() || fi.Size() > maxAccessFileSize {
		return nil, fmt.Errorf("access.json size/type invalid (%d bytes, limit %d)", fi.Size(), maxAccessFileSize)
	}
	data, err := os.ReadFile(a.path)
	if err != nil {
		return nil, fmt.Errorf("access.json read failed: %w", err)
	}
	var doc accessFile
	dec := json.NewDecoder(bytes.NewReader(data))
	// DisallowUnknownFields：授权文件出现任何契约外字段即整体拒绝——
	// F4b 按 PLAN §6 拍板文本写同一结构，这里不做静默宽容，防止授权语义漂移。
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("access.json parse failed: %w", err)
	}
	if _, err := dec.Token(); err != io.EOF {
		// 尾随垃圾（如两个 JSON 拼接的半截写坏形态）同样视为损坏。
		return nil, fmt.Errorf("access.json has trailing garbage")
	}
	if doc.Version != accessSchemaVer {
		return nil, fmt.Errorf("access.json version %d unsupported (want %d)", doc.Version, accessSchemaVer)
	}
	if doc.Tools == nil {
		return nil, fmt.Errorf("access.json missing \"tools\" object")
	}
	for key := range doc.Tools {
		if !knownModuleIDs[key] {
			return nil, fmt.Errorf("access.json unknown tool key %q", key)
		}
	}
	return doc.Tools, nil
}
