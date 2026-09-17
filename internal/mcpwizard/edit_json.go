package mcpwizard

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
)

// errUnsafeJSON 统一表达"该 JSON 文件不可安全合并"（fail-closed 判定核心），
// 调用方据此拒绝写入并给手动片段；错误文本直接面向用户，保持中文完整句。
var errUnsafeJSON = errors.New("unsafe JSON")

// stripBOM 检测 UTF-8 BOM。带 BOM 的 JSON 严格解析必失败，且"顺手去 BOM 重写"
// 会改动 hanxi 条目之外的字节，违背最小干预——按裁定 1 归入不可安全合并。
func hasBOM(data []byte) bool {
	return len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF
}

// hasJSONComments 扫描字符串字面量之外的 // 与 /* 注释起始（JSONC 特征判定）。
// 真 JSON 不可能含注释，故"解析失败 + 命中此扫描"即可判定为注释文件而非纯损坏。
func hasJSONComments(data []byte) bool {
	var (
		inStr bool
		esc   bool
	)
	for i := 0; i < len(data); i++ {
		ch := data[i]
		if inStr {
			switch {
			case esc:
				esc = false
			case ch == '\\':
				esc = true
			case ch == '"':
				inStr = false
			}
			continue
		}
		switch ch {
		case '"':
			inStr = true
		case '/':
			if i+1 < len(data) && (data[i+1] == '/' || data[i+1] == '*') {
				return true
			}
		}
	}
	return false
}

// detectIndent 探测顶层对象第一个缩进层级（空格数或制表符），探测不到回落两空格。
func detectIndent(data []byte) string {
	indented := false
	for i := 0; i < len(data); i++ {
		ch := data[i]
		if ch == '\n' {
			indented = false
			continue
		}
		if ch == ' ' || ch == '\t' {
			indented = true
			continue
		}
		if indented {
			// 首个缩进行的前导空白即缩进单元（Tab 或 N 空格，N=连续空格数）
			if ch == ',' || ch == '"' || ch == '}' || ch == ']' || ch == ':' {
				start := i
				for start > 0 && (data[start-1] == ' ' || data[start-1] == '\t') {
					start--
				}
				return string(data[start:i])
			}
		}
	}
	return "  "
}

// parseJSONObject 严格解析顶层为字符串键对象；非对象/损坏/注释/BOM 均归 errUnsafeJSON。
func parseJSONObject(data []byte) (map[string]json.RawMessage, error) {
	if hasBOM(data) {
		return nil, fmt.Errorf("%w: 文件带 UTF-8 BOM，无法在不改动其余字节的前提下安全合并", errUnsafeJSON)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		if hasJSONComments(data) {
			return nil, fmt.Errorf("%w: 文件含注释（JSONC），自动合并会丢注释", errUnsafeJSON)
		}
		return nil, fmt.Errorf("%w: %v", errUnsafeJSON, err)
	}
	return top, nil
}

// parseServers 取顶层 mcpServers 子对象（不存在时返回 nil, false）；存在但非对象判形状异常。
func parseServers(top map[string]json.RawMessage) (map[string]json.RawMessage, bool, error) {
	raw, ok := top["mcpServers"]
	if !ok {
		return nil, false, nil
	}
	var servers map[string]json.RawMessage
	if err := json.Unmarshal(raw, &servers); err != nil {
		return nil, false, fmt.Errorf("%w: mcpServers 不是对象，形状异常", errUnsafeJSON)
	}
	return servers, true, nil
}

// renderJSON 把顶层 map 序列化回文本：RawMessage 子树经 json.Marshal 会被压成
// 单行，故先紧凑产出再整篇 json.Indent 重排，保证嵌套缩进一致（键序为 Go 排序，
// 语义等价且稳定）。
func renderJSON(top map[string]json.RawMessage, indent string) ([]byte, error) {
	compact, err := json.Marshal(top)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, compact, "", indent); err != nil {
		return nil, err
	}
	buf.WriteByte('\n')
	return buf.Bytes(), nil
}

// canonicalJSON 将任意 JSON 值规范化为键排序的紧凑文本，指纹比对专用
// （Go map 序列化天然按键排序，同语义两份文本必然得到同一指纹）。
func canonicalJSON(raw json.RawMessage) (string, error) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return "", err
	}
	out, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func fingerprint(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

// jsonEntryFingerprint 提取配置文件中 hanxi 条目的指纹；文件不可解析原样透传
// errUnsafeJSON，条目不存在返回 ("", nil)。
func jsonEntryFingerprint(data []byte) (string, error) {
	top, err := parseJSONObject(data)
	if err != nil {
		return "", err
	}
	servers, _, err := parseServers(top)
	if err != nil {
		return "", err
	}
	raw, ok := servers[entryName]
	if !ok {
		return "", nil
	}
	canon, err := canonicalJSON(raw)
	if err != nil {
		return "", err
	}
	return fingerprint(canon), nil
}

// mergeJSONEntry 在保留顶层键的前提下写 mcpServers.hanxi；条目已按语义一致时
// 返回原字节（幂等：重复安装零 diff、零重写）。
func mergeJSONEntry(data []byte, command string, args []string) (newData []byte, changed bool, fp string, err error) {
	entry := map[string]any{"command": command, "args": args}
	entryRaw, err := json.Marshal(entry)
	if err != nil {
		return nil, false, "", err
	}
	fp, err = canonicalAndFingerprint(entryRaw)
	if err != nil {
		return nil, false, "", err
	}
	if len(data) == 0 {
		top := map[string]json.RawMessage{}
		servers := map[string]json.RawMessage{entryName: entryRaw}
		top["mcpServers"] = json.RawMessage(mustMarshal(servers))
		out, err := renderJSON(top, "  ")
		return out, true, fp, err
	}
	top, err := parseJSONObject(data)
	if err != nil {
		return nil, false, "", err
	}
	servers, present, err := parseServers(top)
	if err != nil {
		return nil, false, "", err
	}
	if !present {
		servers = map[string]json.RawMessage{}
	}
	if old, ok := servers[entryName]; ok {
		if oldCanon, cerr := canonicalJSON(old); cerr == nil && oldCanon == canonicalOf(entryRaw) && len(data) > 0 {
			// 语义一致：原字节原样返回，一个字都不动
			return data, false, fp, nil
		}
	}
	servers[entryName] = entryRaw
	top["mcpServers"] = json.RawMessage(mustMarshal(servers))
	out, err := renderJSON(top, detectIndent(data))
	if err != nil {
		return nil, false, "", err
	}
	return out, true, fp, nil
}

// removeJSONEntry 精确摘除 mcpServers.hanxi；条目本就不存在返回原字节。
// mcpServers 摘空后保留空对象（顶层键不动，最小干预）。
func removeJSONEntry(data []byte) (newData []byte, changed bool, err error) {
	if len(data) == 0 {
		return data, false, nil
	}
	top, err := parseJSONObject(data)
	if err != nil {
		return nil, false, err
	}
	servers, present, err := parseServers(top)
	if err != nil {
		return nil, false, err
	}
	if !present {
		return data, false, nil
	}
	if _, ok := servers[entryName]; !ok {
		return data, false, nil
	}
	delete(servers, entryName)
	top["mcpServers"] = json.RawMessage(mustMarshal(servers))
	out, err := renderJSON(top, detectIndent(data))
	if err != nil {
		return nil, false, err
	}
	return out, true, nil
}

func mustMarshal(v any) []byte {
	out, err := json.Marshal(v)
	if err != nil {
		panic("mcpwizard: 内部序列化失败（不应发生）: " + err.Error())
	}
	return out
}

func canonicalOf(raw []byte) string {
	s, _ := canonicalJSON(raw)
	return s
}

func canonicalAndFingerprint(raw []byte) (string, error) {
	s, err := canonicalJSON(raw)
	if err != nil {
		return "", err
	}
	return fingerprint(s), nil
}
