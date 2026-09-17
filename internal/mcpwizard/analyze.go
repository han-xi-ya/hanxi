package mcpwizard

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// clientAnalysis 单客户端一次分析的完整中间态（内部结构，public() 出 DTO）。
type clientAnalysis struct {
	spec        clientSpec
	dir, path   string
	data        []byte
	dirExists   bool
	exists      bool
	state       string
	detail      string
	blocked     bool // 不可安全合并/不可达：一切写操作拒绝
	installedAt string
}

// analyze 读盘 + 指纹 + 四态判定。任何"看不懂"的文件都落 blocked，
// 把裁定 1 的 fail-closed 挡在状态层，后续 plan/confirm 无从绕过。
func (s *McpWizardService) analyze(spec clientSpec, r *receipt) clientAnalysis {
	a := clientAnalysis{spec: spec, state: stateNotInstalled}
	a.dir, a.path = spec.resolve(s.home, s.env)
	if a.path == "" {
		a.blocked = true
		a.detail = "无法解析用户主目录，客户端配置文件不可达"
		return a
	}
	a.dirExists = dirExists(a.dir)
	data, err := os.ReadFile(a.path)
	switch {
	case err == nil:
		a.exists = true
		a.data = data
	case os.IsNotExist(err):
		if !a.dirExists {
			a.blocked = true
			a.detail = fmt.Sprintf("未检测到该客户端（%s 不存在），不做凭空创建", a.dir)
			return a
		}
		// 目录在、文件缺：允许安装时凭空新建（claude 的"目录"即 home，恒在）
	default:
		a.blocked = true
		a.state = stateBlocked
		a.detail = fmt.Sprintf("读取 %s 失败: %v", a.path, err)
		return a
	}

	var curFp string
	if a.exists {
		var fpErr error
		switch spec.Format {
		case fmtTOML:
			if verr := validateToml(a.data); verr != nil {
				a.blocked = true
				a.state = stateBlocked
				a.detail = fmt.Sprintf("不可安全合并：%v", verr)
				return a
			}
			curFp, fpErr = tomlEntryFingerprint(a.data)
		default:
			curFp, fpErr = jsonEntryFingerprint(a.data)
		}
		if fpErr != nil {
			a.blocked = true
			a.state = stateBlocked
			a.detail = fmt.Sprintf("不可安全合并：%v", fpErr)
			return a
		}
	}

	rec, recOK := r.get(spec.ID)
	a.installedAt = rec.InstalledAt
	intended := s.intendedFingerprint(spec)
	switch {
	case recOK && curFp == rec.Fingerprint:
		a.state = stateInstalled
		a.detail = "已由 hanxi 安装"
	case recOK && curFp == "":
		a.state = stateNeedsRepair
		a.detail = "hanxi 登记过安装，但配置文件中的条目已不见（被删除或换机），可重新安装修复"
	case recOK:
		a.state = stateConflict
		a.detail = "hanxi 写入的条目被手工修改过——为避免覆盖你的改动，安装与卸载都被拒绝；如需变更请先在配置里删掉该条目再重装"
	case curFp == "":
		a.state = stateNotInstalled
	case curFp == intended:
		a.state = stateInstalled
		a.detail = "配置中已有与预期一致的 hanxi 条目（非本向导写入，无回执）"
	default:
		a.state = stateConflict
		a.detail = "已存在同名但内容不同的 hanxi 条目——绝不静默覆盖，请先手工处理该条目"
	}
	return a
}

func (a clientAnalysis) public() ClientState {
	return ClientState{
		ID:           a.spec.ID,
		Name:         a.spec.Name,
		Format:       a.spec.Format,
		ConfigPath:   a.path,
		DirExists:    a.dirExists,
		ConfigExists: a.exists,
		State:        a.state,
		Detail:       a.detail,
		CanInstall:   !a.blocked && a.state != stateConflict,
		CanUninstall: !a.blocked && (a.state == stateInstalled || a.state == stateNeedsRepair),
		InstalledAt:  a.installedAt,
	}
}

// intendedFingerprint 当前环境下将要写入的条目指纹（与 merge/append 产出的 fp 同算法）。
func (s *McpWizardService) intendedFingerprint(spec clientSpec) string {
	if spec.Format == fmtTOML {
		return fingerprint(normalizeTomlBody(tomlBlockBody(s.command, serverArgs)))
	}
	return fingerprint(canonicalOf(mustMarshal(map[string]any{"command": s.command, "args": serverArgs})))
}

// manualSnippet 手动配置片段（blocked 场景给用户自助；exe 不可解析时以占位符示形）。
func (s *McpWizardService) manualSnippet(spec clientSpec) string {
	cmd := s.command
	if cmd == "" {
		cmd = "<hanxi 可执行文件绝对路径>"
	}
	if spec.Format == fmtTOML {
		return fmt.Sprintf("将以下托管区块整段追加到 %s 末尾（hanxi 向导同款）：\n%s\n%s%s",
			filepath.Base(spec.fileName), tomlBegin+"\n", tomlBlockBody(cmd, serverArgs), tomlEnd)
	}
	entry := map[string]any{"mcpServers": map[string]any{entryName: map[string]any{"command": cmd, "args": serverArgs}}}
	raw, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return ""
	}
	return fmt.Sprintf("本文件无法自动合并（保留注释/原格式是二期外科手术编辑的范围）。请手工把 hanxi 条目合并进 %s 的 mcpServers：\n%s",
		filepath.Base(spec.fileName), string(raw))
}
