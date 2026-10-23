// Package sshconf 把「SSH 主机清单」账本渲染成 OpenSSH 可读的配置文件文本
// （纯函数，无 IO、无副作用——落盘/读取归上层模块）。
//
// 设计动机与 scripts/cataloggen 同构：权威一份，产物幂等。账本权威存在
// hanxi 数据根，本包负责把它机械地展开成 ssh -F 能消费的文本；同一份账本
// 永远渲染出字节相同的产物（按别名排序落笔，杜绝遍历顺序漂移）。
//
// 安全前提（本包存在的主要理由）：别名字段是最现实的注入面。若别名允许
// 换行，用户填 "x\n    ProxyCommand 恶意命令" 就等于往配置文件里注入任意
// 本地指令——ssh 会照着执行。因此：
//   - ValidateAlias 把别名锁死在 [a-z0-9_-]{1,32}，且拒绝前导/尾随连字符
//     （前导 '-' 会被 ssh 命令行当作选项）；
//   - Validate 逐字段拒绝换行、控制字符、空白（单 token 值）、ssh:// 前缀
//     与内嵌端口——值里能混进换行，一切防御都不成立；
//   - Render 落笔后有最后一道自证（selfCheck）：把产物逐行切回，必须与
//     生成时的行序列逐行相等，且每一行都认得形态（段头 Host、固定四空格
//     的已知关键字、注释、空行）。防御性冗余——即便上游校验有漏，也不
//     产出可注入文本。
//
// 纪律：不生成 `Host *` 全局段（那是用户自己的地盘，我们不抢）；每个段
// 自带 IdentitiesOnly yes，杜绝 ssh 默认拿 agent 里所有密钥挨个试探导致的
// "Too many authentication failures"；IdentityFile 只存路径（可含 ~/ 前缀），
// 密钥内容永不进这个包。
package sshconf

import (
	"errors"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// GeneratedHeader 产物文件头的固定一行（Render 输出以它开头）。
// 写明由 hanxi 生成、手改会被覆盖、请改 hanxi 的 SSH 主机清单——
// "权威一份"的口径必须在产物里自述。
const GeneratedHeader = "# 此文件由 hanxi 自动生成，请勿手改：改动会被下一次渲染覆盖；" +
	"要增删主机，请编辑 hanxi 数据根里的 SSH 主机清单。"

// maxAliasLen 别名字符数上限：防超长别名撑爆展示与比对逻辑。
const maxAliasLen = 32

// indentWidth 关键字行固定缩进：4 空格。selfCheck 认死这一种形态，
// 缩进漂移（tab、2 空格）一律按"不认得的行"处理。
const indentWidth = 4

// knownKeywords 段内允许出现的关键字白名单（"    关键字 值"）。
// 表外的关键字——哪怕拼写是真实的 ssh 指令（ProxyCommand、LocalCommand
// 之类）——都绝不允许出现在产物里，这是自证的核心断言。
var knownKeywords = map[string]bool{
	"HostName":       true,
	"Port":           true,
	"User":           true,
	"IdentityFile":   true,
	"IdentitiesOnly": true,
}

// Host 一台机器的清单项（账本数据进渲染器的形态）。字段口径见 Validate。
type Host struct {
	Alias        string // ssh 别名，唯一键；口径见 ValidateAlias
	HostName     string // IP 或域名（IPv4/IPv6/hostname 皆可，见 Validate）
	Port         int    // 0 表示按 22 处理（不渲染 Port 行，走 ssh 缺省）
	User         string // 登录用户；空 = 不渲染 User 行（交给 ssh 默认）
	IdentityFile string // 只存路径（可含 ~/ 前缀），永不存密钥内容；空 = 不渲染该行
	Note         string // 渲染成注释行；允许为空；含 CR/LF/控制字符即非法
	Enabled      bool   // false 的项不出现在产物里（但 Render 仍校验其合法性）
}

// ValidateAlias 校验 ssh 别名。只允许 [a-z0-9_-]{1,32}：拒绝空串、大写、
// 点、斜杠、空格、引号、换行（含 \r\n）等一切越界字符，也拒绝前导/尾随
// 连字符（前导 '-' 会被 ssh 命令行当作选项）。错误信息不带包前缀，
// 由 Render 等出口统一补全上下文。
//
// **改这里必须同步前端**：`frontend/src/components/sshkeys/useHosts.ts` 的 `aliasIssue`
// 是**刻意的一份镜像**（为了在输入现场把机制讲清：换行=注入指令、空格=多开匹配模式、
// 大写=静默失效……），只镜像别名这一个字段，其余字段前端只做轻拦、以后端原文为准。
// 两侧各自有同一组样本的用例；规则变了而只改一侧，另一侧的用例会红。
func ValidateAlias(alias string) error {
	if alias == "" {
		return errors.New("别名不能为空")
	}
	if len(alias) > maxAliasLen {
		return fmt.Errorf("别名 %q 超过 %d 字符上限", alias, maxAliasLen)
	}
	if alias[0] == '-' || alias[len(alias)-1] == '-' {
		return fmt.Errorf("别名 %q 不能以连字符开头或结尾（前导连字符会被 ssh 当作命令行选项）", alias)
	}
	for i := 0; i < len(alias); i++ {
		c := alias[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_' || c == '-' {
			continue
		}
		return fmt.Errorf("别名 %q 含非法字符 %q（只允许小写字母、数字、下划线、连字符）", alias, string(rune(c)))
	}
	return nil
}

// Validate 汇总校验一个清单项，返回第一个问题。各字段口径（与注入面
// 清单逐项对应）：
//   - Alias：见 ValidateAlias；
//   - HostName：非空；拒绝空白与控制字符（换行=注入通道）；拒绝 ssh://
//     前缀（本清单走 HostName 指令，不走 URI）；拒绝内嵌端口——含 ':' 且
//     含 '.' 即判定为 "1.1.1.1:22" 形态（纯冒号无点放行，那是合法 IPv6），
//     方括号属端口地址形态也拒绝；
//   - Port：0（按 22 处理）或 1–65535，越界报错；
//   - User：允许空（走 ssh 默认）；拒绝一切空白与控制字符；
//   - IdentityFile：允许空；拒绝空白与控制字符。这里比"只拒前导空格"更
//     严：渲染时不加引号，行内空格会让 ssh 只认路径首个 token，与其产出
//     静默坏掉的配置，不如显式拒绝（含空格的路径请上层先规范化）；
//   - Note：允许空；拒绝 CR/LF 与控制字符（宁拒勿折叠改写——账本坏数据
//     应显式暴露，不许渲染层静默修数据）。
func Validate(h Host) error {
	if err := ValidateAlias(h.Alias); err != nil {
		return err
	}
	if err := validateHostName(h.HostName); err != nil {
		return fmt.Errorf("主机 %q: %w", h.Alias, err)
	}
	if h.Port < 0 || h.Port > 65535 {
		return fmt.Errorf("主机 %q: 端口 %d 越界（合法为 0 或 1–65535）", h.Alias, h.Port)
	}
	if h.User != "" {
		if err := rejectToken("User", h.User); err != nil {
			return fmt.Errorf("主机 %q: %w", h.Alias, err)
		}
	}
	if h.IdentityFile != "" {
		if err := rejectToken("IdentityFile", h.IdentityFile); err != nil {
			return fmt.Errorf("主机 %q: %w", h.Alias, err)
		}
	}
	if err := rejectNoControl("Note", h.Note); err != nil {
		return fmt.Errorf("主机 %q: %w", h.Alias, err)
	}
	return nil
}

func validateHostName(name string) error {
	if name == "" {
		return errors.New("HostName 不能为空")
	}
	if err := rejectToken("HostName", name); err != nil {
		return err
	}
	if strings.HasPrefix(strings.ToLower(name), "ssh://") {
		return fmt.Errorf("HostName %q 不得带 ssh:// 前缀（本清单只写 IP/域名）", name)
	}
	if strings.ContainsAny(name, "[]") {
		return fmt.Errorf("HostName %q 不得含方括号（端口地址属 ssh:// 形态）", name)
	}
	// **先问 net.ParseIP，再谈启发式**。旧写法是"含 ':' 且含 '.' 就判内嵌端口"，
	// 于是 IPv4 映射形态 `::ffff:192.168.1.1` 被当端口误拒——而它明明是合法 IPv6。
	// 能解析成 IP 的一律放行（含裸 IPv6，端口本来就走独立的 Port 字段，不需要猜）；
	// 只有解析不出来，才怀疑用户把 `1.2.3.4:22` 写进了地址栏。
	if net.ParseIP(name) != nil {
		return nil
	}
	if strings.Contains(name, ":") && strings.Contains(name, ".") {
		return fmt.Errorf("HostName %q 疑似内嵌端口（端口请填 Port 字段）", name)
	}
	return nil
}

// rejectToken 拒绝换行、控制字符与一切空白——单 token 值（HostName/User/
// IdentityFile）的通行口径：值里出现空白，ssh 侧解析就会错位。
func rejectToken(field, v string) error {
	if v == "" {
		return nil
	}
	for _, r := range v {
		if r == '\n' || r == '\r' {
			return fmt.Errorf("%s 不得含换行（换行可向配置文件注入新指令行）", field)
		}
		if unicode.IsControl(r) {
			return fmt.Errorf("%s 不得含控制字符 U+%04X", field, r)
		}
		if unicode.IsSpace(r) {
			return fmt.Errorf("%s 不得含空白字符（值必须是单 token）", field)
		}
	}
	return nil
}

// rejectNoControl 只拒绝 CR/LF 与控制字符、允许可读空白（Note 用：
// 注释行里的空格无害且常见）。
func rejectNoControl(field, v string) error {
	for _, r := range v {
		if r == '\n' || r == '\r' {
			return fmt.Errorf("%s 不得含换行（换行可向配置文件注入新指令行）", field)
		}
		if unicode.IsControl(r) {
			return fmt.Errorf("%s 不得含控制字符 U+%04X", field, r)
		}
	}
	return nil
}

// Render 把清单渲染成完整配置文件文本：
//   - 只渲染 Enabled 项；Enabled=false 的项不出现，也不影响其他项；
//   - 任意输入非法（含 Enabled=false 的坏数据、Enabled 项之间重复别名）
//     即整体报错且不出产物——账本有脏数据时宁可罢工，不产半本配置；
//   - 幂等：同一输入任意两次调用字节相同（按 Alias 字典序排序落笔）；
//   - 行尾统一 \n（Windows 下 OpenSSH 照常可读；要 CRLF 由上层落盘时转）；
//   - 落笔后对产物逐行自证（selfCheck），任何一行不认得即报错返回。
func Render(hosts []Host) (string, error) {
	for i, h := range hosts {
		if err := Validate(h); err != nil {
			return "", fmt.Errorf("sshconf: 输入非法（第 %d 项）: %w", i, err)
		}
	}
	return render(hosts)
}

// render 排序与落笔（刻意不做上游校验）。自证测试直接驱动这里，以证明
// selfCheck 独立于 Validate 有效——哪怕校验层整段漏网，产物仍会被拦下。
func render(hosts []Host) (string, error) {
	enabled := make([]Host, 0, len(hosts))
	for _, h := range hosts {
		if h.Enabled {
			enabled = append(enabled, h)
		}
	}
	sort.Slice(enabled, func(i, j int) bool { return enabled[i].Alias < enabled[j].Alias })
	for i := 1; i < len(enabled); i++ {
		if enabled[i].Alias == enabled[i-1].Alias {
			return "", fmt.Errorf("sshconf: 别名 %q 重复出现（别名为唯一键）", enabled[i].Alias)
		}
	}

	lines := []string{GeneratedHeader}
	for _, h := range enabled {
		lines = append(lines, "") // 段间空行
		if h.Note != "" {
			lines = append(lines, "# "+h.Note)
		}
		lines = append(lines, "Host "+h.Alias)
		lines = append(lines, indent("HostName "+h.HostName))
		if h.Port != 0 {
			lines = append(lines, indent(fmt.Sprintf("Port %d", h.Port)))
		}
		if h.User != "" {
			lines = append(lines, indent("User "+h.User))
		}
		if h.IdentityFile != "" {
			lines = append(lines, indent("IdentityFile "+h.IdentityFile))
		}
		lines = append(lines, indent("IdentitiesOnly yes"))
	}

	out := strings.Join(lines, "\n") + "\n"
	if err := selfCheck(out, lines, enabled); err != nil {
		return "", err
	}
	return out, nil
}

func indent(body string) string {
	return strings.Repeat(" ", indentWidth) + body
}

// selfCheck 是 Render 的最后一道自证（防御性冗余）。三重断言：
//  1. 还原性：产物按 \n 切回的行序列必须与生成时的行序列逐行相等、行数
//     一致——任何字段值里混入换行，切分即错位，必在此现形；
//  2. 首行必须一字不差是 GeneratedHeader；
//  3. 形态：每行只认四种——空行（段分隔）、注释（"# " 前缀）、段头
//     "Host <alias>"（alias 须过 ValidateAlias 且属于本次渲染，凭空多出
//     的 Host 行即注入行）、"四空格 + 已知关键字 + 单空格 + 单 token 值"。
//     段头总数还须恰等于渲染项数，堵死"注入行恰好复刻自家合法形态"。
func selfCheck(content string, composed []string, rendered []Host) error {
	if !strings.HasSuffix(content, "\n") {
		return errors.New("sshconf: 自证失败：产物未以换行收尾")
	}
	got := strings.Split(strings.TrimSuffix(content, "\n"), "\n")
	if len(got) != len(composed) {
		return fmt.Errorf("sshconf: 自证失败：产物行数 %d ≠ 生成行数 %d（字段值混入了换行）", len(got), len(composed))
	}
	for i := range got {
		if got[i] != composed[i] {
			return fmt.Errorf("sshconf: 自证失败：第 %d 行与生成内容不一致", i+1)
		}
	}
	if len(got) == 0 || got[0] != GeneratedHeader {
		return errors.New("sshconf: 自证失败：首行不是 GeneratedHeader")
	}
	hostLines := 0
	for i, line := range got {
		switch {
		case line == "": // 段分隔空行
		case strings.HasPrefix(line, "# "): // 注释（头部与 note；ssh 忽略，惰性文本）
		case strings.HasPrefix(line, "Host "):
			alias := strings.TrimPrefix(line, "Host ")
			if err := ValidateAlias(alias); err != nil {
				return fmt.Errorf("sshconf: 自证失败：第 %d 行段头别名不合法: %w", i+1, err)
			}
			found := false
			for _, h := range rendered {
				if h.Alias == alias {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("sshconf: 自证失败：第 %d 行段头 %q 不在本次渲染清单内", i+1, alias)
			}
			hostLines++
		case strings.HasPrefix(line, strings.Repeat(" ", indentWidth)):
			if err := checkKeywordLine(line); err != nil {
				return fmt.Errorf("sshconf: 自证失败：第 %d 行: %w", i+1, err)
			}
		default:
			return fmt.Errorf("sshconf: 自证失败：第 %d 行形态不认得 %q", i+1, line)
		}
	}
	if hostLines != len(rendered) {
		return fmt.Errorf("sshconf: 自证失败：段头行数 %d ≠ 渲染项数 %d", hostLines, len(rendered))
	}
	return nil
}

// checkKeywordLine 断言一行确为"四空格 + 已知关键字 + 单空格 + 单 token 值"。
func checkKeywordLine(line string) error {
	key, value, ok := strings.Cut(line[indentWidth:], " ")
	switch {
	case !ok || !knownKeywords[key]:
		return fmt.Errorf("不认得的关键字行 %q", line)
	case value == "" || strings.ContainsAny(value, " \t"):
		return fmt.Errorf("关键字 %s 的值形态异常（须单 token）%q", key, line)
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return fmt.Errorf("关键字 %s 的值含控制字符 %q", key, line)
		}
	}
	if key == "IdentitiesOnly" && value != "yes" {
		return fmt.Errorf("IdentitiesOnly 的值必须是 yes，实际 %q", value)
	}
	if key == "Port" {
		p, err := strconv.Atoi(value)
		if err != nil || p < 1 || p > 65535 || strconv.Itoa(p) != value {
			return fmt.Errorf("Port 的值不是规范端口号 %q", value)
		}
	}
	return nil
}

// ContainsAlias 判断配置文件文本里是否已有 `Host <alias>` 段头（供上层做
// 幂等/去重判断）。alias 不合法恒为 false；注释行、缩进行、"HostAlias"
// 这类前缀词都不算命中；段头允许多别名（"Host a b"），任一 token 相等
// 即命中。
func ContainsAlias(content, alias string) bool {
	if ValidateAlias(alias) != nil {
		return false
	}
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") || strings.HasPrefix(line, "#") {
			continue // 缩进行 / 注释行都不是段头
		}
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != "Host" {
			continue
		}
		for _, tok := range fields[1:] {
			if tok == alias {
				return true
			}
		}
	}
	return false
}
