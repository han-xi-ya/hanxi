package mcpwizard

// access_write.go 是 access.json 的写引擎（R6：「设置 → AI 接入」分区从只读
// 呈现升级为真正的四工具开关）。读方契约权威是 internal/mcp/access.go（F4a，
// 领地只读）——本文件镜像同一套严格判定（DisallowedUnknownFields、version==1、
// 四键之外拒读、尾随垃圾、16 KiB 上限），用来裁定"能不能安全改、读者此刻看到
// 什么"，但生产代码不 import internal/mcp（包注释的编译期零耦合边界维持不变）；
// 写出的每个字节由对拍测试 access_readmatch_test.go 喂给真读方逐键验收，不许 mock。
//
// 落盘契约（PLAN_MCP §6 拍板文本，"恰好"二字的执行）：
//
//	{"version":1,"tools":{"envcheck":bool,"everything":bool,"ocr":bool,"memo":bool}}
//
// 形态纪律：固定四键齐全（json.MarshalIndent 2 空格缩进、键序恒定）、无 BOM、
// 目录不存在时由写方创建（读方永不建目录是 F4a 的零落盘承诺，写方补位）、
// tmp+rename 原子替换（本包 atomicWrite）。缺文件是合法态（视同全关），首次
// 开关即凭空建档；损坏/超纲文件拒绝盲写，覆盖修复必须走显式 ResetAccess。

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

const (
	accessSchemaVer   = 1
	maxAccessFileSize = 16 << 10 // 与读方 mcp/access.go 同额：超限视为异常，拒读也拒写
)

// accessToolKeys 授权四键的规范集合（键序=PLAN §6 文本序=工具面展示序）。
// 集合外键 reader 整体拒读，写侧同样既不产出也不盲改。
var accessToolKeys = []string{"envcheck", "everything", "ocr", "memo"}

func knownAccessTool(key string) bool {
	for _, k := range accessToolKeys {
		if k == key {
			return true
		}
	}
	return false
}

// accessStrict 是"读方视角"的严格加载结果：legal=true 当且仅当 reader 会采信此文件
// （missing 也 legal——合法的全关默认态）。reason 为中文诊断，直接进前端呈现。
type accessStrict struct {
	legal   bool
	missing bool // 文件不存在：合法态，四键视同 false
	exists  bool // 文件在位（无论可否采信）
	tools   map[string]bool
	reason  string
}

// strictLoadAccess 镜像 internal/mcp/access.go load() 的全部拒读条件，一条不多一条不少：
// 目录形态/超 16 KiB/解析失败/未知顶层字段/尾随垃圾/version≠1/缺 tools/未知 tools 键。
// 与读方的唯一允许差异是 reason 措辞（读方英文进日志，这里中文进 UI）；语义漂移由
// access_readmatch_test.go 的真读方对拍矩阵把关。
func strictLoadAccess(path string) accessStrict {
	st := accessStrict{tools: map[string]bool{}}
	fi, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			st.missing = true
			st.legal = true
			return st
		}
		st.reason = fmt.Sprintf("授权文件 stat 失败: %v", err)
		return st
	}
	st.exists = true
	if fi.IsDir() || fi.Size() > maxAccessFileSize {
		st.reason = fmt.Sprintf("授权文件形态异常（目录或超过 %d 字节上限）", maxAccessFileSize)
		return st
	}
	data, err := os.ReadFile(path)
	if err != nil {
		st.reason = fmt.Sprintf("授权文件读取失败: %v", err)
		return st
	}
	var doc struct {
		Version int             `json:"version"`
		Tools   map[string]bool `json:"tools"`
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields() // 与读方同款：契约外字段即拒
	if err := dec.Decode(&doc); err != nil {
		st.reason = fmt.Sprintf("授权文件 JSON 损坏或含契约外字段: %v", err)
		return st
	}
	if _, err := dec.Token(); err != io.EOF {
		st.reason = "授权文件存在尾随垃圾（疑似半截写坏或多档拼接）"
		return st
	}
	if doc.Version != accessSchemaVer {
		st.reason = fmt.Sprintf("授权文件版本为 %d，当前仅认版本 %d", doc.Version, accessSchemaVer)
		return st
	}
	if doc.Tools == nil {
		st.reason = `授权文件缺 "tools" 对象`
		return st
	}
	for key := range doc.Tools {
		if !knownAccessTool(key) {
			st.reason = fmt.Sprintf("授权文件含未知工具键 %q（读方为防授权语义漂移整体拒读）", key)
			return st
		}
	}
	st.tools = doc.Tools // 缺某键按 false（与读方 Allowed 的零值口径一致）
	st.legal = true
	return st
}

// accessDocForWrite 落盘专用结构：固定四键 + 固定键序，杜绝 map 序列化漂移。
type accessDocForWrite struct {
	Version int            `json:"version"`
	Tools   accessToolsDoc `json:"tools"`
}

type accessToolsDoc struct {
	Envcheck   bool `json:"envcheck"`
	Everything bool `json:"everything"`
	Ocr        bool `json:"ocr"`
	Memo       bool `json:"memo"`
}

func accessDocOf(tools map[string]bool) accessDocForWrite {
	return accessDocForWrite{
		Version: accessSchemaVer,
		Tools: accessToolsDoc{
			Envcheck:   tools["envcheck"],
			Everything: tools["everything"],
			Ocr:        tools["ocr"],
			Memo:       tools["memo"],
		},
	}
}

// accessWriteMu 串行化本进程内的授权写操作（读-改-写序列不可交叉；
// 跨进程无竞态——MCP 子进程对 access.json 严格只读）。
var accessWriteMu sync.Mutex

// writeAccessCanonical 整档原子落盘 + 回读复验（按读方同款严格规则重解析，
// 确认盘上字节的四键语义与写入意图逐键一致才算成功）。目录缺失由本方创建——
// 这是写方职责（读方永不建，F4a 零落盘承诺）。
func (s *McpWizardService) writeAccessCanonical(tools map[string]bool) error {
	data, err := json.MarshalIndent(accessDocOf(tools), "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.accessPath), 0o755); err != nil {
		return fmt.Errorf("创建 mcp 目录失败: %w", err)
	}
	if err := s.writeFn(s.accessPath, data); err != nil {
		return err
	}
	back := strictLoadAccess(s.accessPath)
	if !back.legal {
		return fmt.Errorf("写入后复验不通过（回读仍被严格规则拒绝）: %s", back.reason)
	}
	for _, key := range accessToolKeys {
		if back.tools[key] != tools[key] {
			return fmt.Errorf("写入后复验不通过：%s 键回读值与意图不符", key)
		}
	}
	return nil
}

// accessCorruptErr 拒写损坏档时的中文指引（覆盖修复必须走显式 ResetAccess 二次确认）。
func accessCorruptErr(reason string) error {
	return fmt.Errorf("授权文件当前读方不采信（%s）；为避免盲写碾掉你的既有意图，本次改动已拒绝。"+
		"请先用「修复（覆盖重置）」确认重写标准全关档后，再逐项重新授权", reason)
}

// SetToolAccess 开关单个工具的授权：读现档 → 改一键 → 整档原子回写（恰好四键）。
// 文件缺失是合法起点（凭空建档）；文件存在但读方不采信（损坏/超纲/未知键/版本≠1）
// 时拒绝盲写并报中文指引——覆盖修复归 ResetAccess 显式确认，不提供静默台阶。
// 成功返回写后呈现；保存即生效（读方每次调用重读盘，无需重启 hanxi mcp）。
func (s *McpWizardService) SetToolAccess(tool string, enabled bool) (AccessInfo, error) {
	if !knownAccessTool(tool) {
		return s.accessInfo(), fmt.Errorf("未知授权键 %q——合法键为 envcheck / everything / ocr / memo 四者之一", tool)
	}
	accessWriteMu.Lock()
	defer accessWriteMu.Unlock()
	st := strictLoadAccess(s.accessPath)
	if !st.legal {
		return s.accessInfo(), accessCorruptErr(st.reason)
	}
	tools := make(map[string]bool, len(accessToolKeys))
	for _, key := range accessToolKeys {
		tools[key] = st.tools[key] // 缺键按 false（读方同款），回写时归一为四键齐全
	}
	tools[tool] = enabled
	if err := s.writeAccessCanonical(tools); err != nil {
		return s.accessInfo(), fmt.Errorf("授权文件写入失败: %w", err)
	}
	return s.accessInfo(), nil
}

// ResetAccess 覆盖修复的唯一出口（前端二次确认后调用）：旧档存在则先另存
// .hanxi-bak-<时间戳>（损坏内容也要保全体证），再写标准全关档。备份失败即中止，
// 目标文件不动——绝不无退路覆盖。
func (s *McpWizardService) ResetAccess() (OpResult, error) {
	accessWriteMu.Lock()
	defer accessWriteMu.Unlock()
	bak := ""
	if old, err := os.ReadFile(s.accessPath); err == nil {
		bak = s.accessPath + ".hanxi-bak-" + s.now().Format("20060102-150405")
		if werr := os.WriteFile(bak, old, 0o600); werr != nil {
			return OpResult{Message: fmt.Sprintf("旧授权文件备份失败，重置中止（目标文件未动）: %v", werr)}, nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return OpResult{Message: fmt.Sprintf("旧授权文件无法读取，无从备份，重置中止: %v", err)}, nil
	}
	if err := s.writeAccessCanonical(map[string]bool{}); err != nil {
		return OpResult{BackupPath: bak, Message: fmt.Sprintf("授权文件重置失败: %v", err)}, nil
	}
	msg := "授权文件已重置为默认全关"
	if bak != "" {
		msg += fmt.Sprintf("，旧档已另存 %s", bak)
	} else {
		msg += "（此前无文件，已凭空建档）"
	}
	return OpResult{Success: true, BackupPath: bak, Message: msg}, nil
}
