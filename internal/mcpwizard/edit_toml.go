package mcpwizard

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

// Codex 托管区块成对哨兵（PLAN §2.5"绝不重排用户文件"，只整块追加/删除）。
const (
	tomlBegin = "# >>> hanxi mcp >>>"
	tomlEnd   = "# <<< hanxi mcp <<<"
)

// errTomlSentinels 哨兵计数异常（0 对或 ≥2 对不成形），卸载/覆盖据此拒绝。
var errTomlSentinels = errors.New("hanxi 托管区块哨兵异常")

// tomlBlockBody 生成哨兵之间的托管内容（不含哨兵行本身）。
func tomlBlockBody(command string, args []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[mcp_servers.%s]\n", entryName)
	b.WriteString("command = " + strconv.Quote(command) + "\n")
	if len(args) > 0 {
		parts := make([]string, 0, len(args))
		for _, a := range args {
			parts = append(parts, strconv.Quote(a))
		}
		b.WriteString("args = [" + strings.Join(parts, ", ") + "]\n")
	}
	return b.String()
}

// normalizeTomlBody 指纹归一：CRLF→LF、去首尾空白与空行差异（行间缩进不敏感化）。
func normalizeTomlBody(body string) string {
	lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	var kept []string
	for _, l := range lines {
		if t := strings.TrimSpace(l); t != "" {
			kept = append(kept, t)
		}
	}
	return strings.Join(kept, "\n")
}

// extractTomlBlock 提取哨兵对包裹的区块体。返回 (body, found)；
// 哨兵 begin/end 各恰好一次且 begin 在 end 之前才视为合法单块，否则 errTomlSentinels。
func extractTomlBlock(data []byte) (string, bool, error) {
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	nBegin := strings.Count(text, tomlBegin)
	nEnd := strings.Count(text, tomlEnd)
	if nBegin == 0 && nEnd == 0 {
		return "", false, nil
	}
	if nBegin != 1 || nEnd != 1 {
		return "", false, fmt.Errorf("%w: 开始标记 %d 处、结束标记 %d 处（预期各 1）——可能被重排或复制，拒绝自动处理", errTomlSentinels, nBegin, nEnd)
	}
	i := strings.Index(text, tomlBegin)
	j := strings.Index(text, tomlEnd)
	if i > j {
		return "", false, fmt.Errorf("%w: 结束标记先于开始标记，区块不成对", errTomlSentinels)
	}
	bodyStart := i + len(tomlBegin)
	// 去掉 begin 行尾换行
	if bodyStart < len(text) && text[bodyStart] == '\n' {
		bodyStart++
	}
	return text[bodyStart:j], true, nil
}

// tomlEntryFingerprint 计算文件里托管区块的指纹；无区块返回 ("", nil)。
func tomlEntryFingerprint(data []byte) (string, error) {
	body, found, err := extractTomlBlock(data)
	if err != nil {
		return "", err
	}
	if !found {
		return "", nil
	}
	return fingerprint(normalizeTomlBody(body)), nil
}

// appendTomlBlock 末尾追加托管区块（唯一允许的修改方式：不动用户已有内容）。
// 已存在语义一致的区块时零改动返回（幂等）；已存在但内容不同由调用方按冲突拦截，
// 这里遇到"存在且不同"直接拒绝——追加会出现两个区块，破坏哨兵唯一性。
func appendTomlBlock(data []byte, command string, args []string) (newData []byte, changed bool, fp string, err error) {
	body := tomlBlockBody(command, args)
	fp = fingerprint(normalizeTomlBody(body))
	existing, found, err := extractTomlBlock(data)
	if err != nil {
		return nil, false, "", err
	}
	if found {
		if normalizeTomlBody(existing) == normalizeTomlBody(body) {
			return data, false, fp, nil
		}
		return nil, false, "", fmt.Errorf("%w: 文件内已存在内容不同的 hanxi 区块", errTomlSentinels)
	}
	var b strings.Builder
	b.Write(data)
	if len(data) > 0 {
		s := strings.ReplaceAll(string(data), "\r\n", "\n")
		if !strings.HasSuffix(s, "\n") {
			b.WriteByte('\n')
		}
		if !strings.HasSuffix(s, "\n\n") {
			b.WriteByte('\n')
		}
	}
	b.WriteString(tomlBegin + "\n")
	b.WriteString(body)
	b.WriteString(tomlEnd + "\n")
	out := []byte(b.String())
	if verr := validateToml(out); verr != nil {
		return nil, false, "", verr
	}
	return out, true, fp, nil
}

// removeTomlBlock 按哨兵精确删除整块（含哨兵行与其前空行分隔）；无区块零改动。
func removeTomlBlock(data []byte) (newData []byte, changed bool, err error) {
	_, found, err := extractTomlBlock(data)
	if err != nil {
		return nil, false, err
	}
	if !found {
		return data, false, nil
	}
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	i := strings.Index(text, tomlBegin)
	j := strings.Index(text, tomlEnd)
	end := j + len(tomlEnd)
	if end < len(text) && text[end] == '\n' {
		end++
	}
	// begin 前的换行/空行是我们追加时补的分隔，一并回收（TOML 空白不敏感，
	// 用户若有自有空行也会被收拢一行，语义无损）；前缀非空时保留单收尾换行。
	prefix := strings.TrimRight(text[:i], "\n")
	if prefix != "" {
		prefix += "\n"
	}
	out := []byte(prefix + text[end:])
	if verr := validateToml(out); verr != nil {
		return nil, false, verr
	}
	return out, true, nil
}

// validateToml 用 BurntSushi/toml 复验文本可解析（写前闸门 + 写后复验共用）。
func validateToml(data []byte) error {
	var probe map[string]any
	if _, err := toml.Decode(string(data), &probe); err != nil {
		return fmt.Errorf("%w: TOML 无法解析（%v），拒绝自动修改", errUnsafeJSON, err)
	}
	return nil
}
