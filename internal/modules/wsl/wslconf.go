package wsl

// /etc/wsl.conf 图形化编辑：读→校验→备份→经管道写回。
// 安全模型（参考同类工具的三步防线，实现独立自研）：
//   - 语法闸门：自研 INI 校验器把关（节头/键值/注释），非法内容绝不落盘；
//   - 引用校验：[user] default 指向的用户必须真实存在（guest 内 id -u 求证），
//     写坏默认用户会把发行版变成登录黑洞；
//   - 写前备份：root cp 原文件到 /etc/wsl.conf.hanxi.bak（文件已存在才备份），
//     写回用 tee 整文件覆盖，CRLF 归一 LF；失败如实报因、不谎报成功。
// 版本门控采取"提示不拦路"：[boot]/[gpu]/[time] 等新 section 对 WSL 版本有要求，
// 但版本判据存在漂移风险，硬拦可能误伤新版本——交给前端黄条警示，用户自行定夺。

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// WslConfDoc /etc/wsl.conf 读取载荷。Missing=true 表示文件不存在（合法空态）。
type WslConfDoc struct {
	Name       string   `json:"name"`
	Text       string   `json:"text"`
	Missing    bool     `json:"missing"`
	WslVersion string   `json:"wslVersion"`
	Warnings   []string `json:"warnings"`
}

const (
	wslConfPath   = "/etc/wsl.conf"
	wslConfBackup = "/etc/wsl.conf.hanxi.bak"
	wslConfMaxLen = 64 * 1024
)

// versionGatedSections 需要较新 WSL 才完整支持的节（文案提示用，不做硬闸）。
var versionGatedSections = map[string]string{
	"boot": "systemd / command 等 [boot] 项要求 WSL 2.4.4+（老版本会静默忽略）",
	"gpu":  "[gpu] 页要求 WSL 2.2.0+",
	"time": "[time] 页要求 WSL 2.2.0+",
}

// iniSectionRe 节头：[name]，允许行尾注释。
var iniSectionRe = regexp.MustCompile(`^\[[A-Za-z0-9_.-]+\](\s*[#;].*)?$`)

// iniKVRe 键值：key=value / 裸 key（布尔开关形态）；key 限常规字符集。
var iniKVRe = regexp.MustCompile(`^[A-Za-z0-9_.-]+\s*(=.*)?$`)

// validateWslConf 逐行校验 wsl.conf/.wslconfig 共用的 INI 语法，返回版本警示；
// 非法即报错（含行号）。.wslconfig 的 [boot]/[gpu]/[time] 警示在这里虽基本不触发
// （那是 wsl.conf 的节），共用语法层不产生误拦，仅提示口径轻微冗余，可接受。
func validateWslConf(text string) ([]string, error) {
	if len(text) > wslConfMaxLen {
		return nil, fmt.Errorf("wsl.conf 内容超过 %d 字节上限", wslConfMaxLen)
	}
	var warnings []string
	seen := map[string]bool{}
	section := ""
	for i, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			if !iniSectionRe.MatchString(line) {
				return nil, fmt.Errorf("第 %d 行节头 %q 不合法（应为 [name] 形态）", i+1, line)
			}
			name := strings.TrimPrefix(line, "[")
			if j := strings.IndexByte(name, ']'); j >= 0 {
				name = name[:j]
			}
			section = strings.ToLower(strings.TrimSpace(name))
			if w, gated := versionGatedSections[section]; gated && !seen[section] {
				seen[section] = true
				warnings = append(warnings, fmt.Sprintf("[%s]：%s", section, w))
			}
			continue
		}
		if !iniKVRe.MatchString(line) {
			return nil, fmt.Errorf("第 %d 行 %q 无法解析（INI 仅支持节头、key=value 与 # 注释；当前节 [%s]）", i+1, line, section)
		}
	}
	return warnings, nil
}

// wslGuestReady 开机探针：先 `wsl -d X -- true` 把"进得去发行版"与
// "文件不存在"两态解耦（guest 工具的输出文案跨语言不可枚举，#39 教训）。
// 停止的发行版会在此被正常拉起——读/写 wsl.conf 本就以此为前提。
func (s *WslService) wslGuestReady(ctx context.Context, name string) error {
	out, err := s.runWsl(ctx, "-d", name, "--", "true")
	if err != nil {
		return fmt.Errorf("无法进入发行版（启动失败？内存/防病毒软件拦截？）: %w %s", err, strings.TrimSpace(out))
	}
	return nil
}

// GetWslConf 读取发行版的 /etc/wsl.conf。文件不存在返回 Missing 空态；
// 读取本身会经 wsl -d 拉起停止的发行版——这是显式点「配置」的预期动作。
// 存在性以 test -f 退出码为判据（与 Save 的备份决策同一口径），不猜文案。
func (s *WslService) GetWslConf(name string) (WslConfDoc, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return WslConfDoc{}, gateErr
	}
	defer release()

	name = strings.TrimSpace(name)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := s.distroAllowed(ctx, name); err != nil {
		return WslConfDoc{}, err
	}
	doc := WslConfDoc{Name: name, WslVersion: s.wslVersion(ctx)}
	if err := s.wslGuestReady(ctx, name); err != nil {
		return WslConfDoc{}, err
	}
	if _, terr := s.runWsl(ctx, "-d", name, "--", "test", "-f", wslConfPath); terr != nil {
		doc.Missing = true
		return doc, nil
	}
	out, err := s.runWsl(ctx, "-d", name, "--", "cat", wslConfPath)
	if err != nil {
		return WslConfDoc{}, fmt.Errorf("读取 %s 失败: %w %s", wslConfPath, err, strings.TrimSpace(out))
	}
	doc.Text = normalizeLF(out)
	if w, err := validateWslConf(doc.Text); err == nil {
		doc.Warnings = w
	} else {
		doc.Warnings = append(doc.Warnings, "现文件存在本工具无法解析的行（可能出自更新版本），直接保存会被拒绝——请手工调整后再保存: "+err.Error())
	}
	return doc, nil
}

// SaveWslConf 写回 /etc/wsl.conf：校验 → （原文件在则）root 备份 → tee 覆盖 → 复验。
func (s *WslService) SaveWslConf(name, text string) (DistroOpResult, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return DistroOpResult{}, gateErr
	}
	defer release()

	name = strings.TrimSpace(name)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if err := s.distroAllowed(ctx, name); err != nil {
		return DistroOpResult{}, err
	}
	finish, ok := s.tryBeginDistroOp(name, "wslconf")
	if !ok {
		return DistroOpResult{}, errDistroBusy
	}
	defer finish()

	// 开机探针先行：发行版进不去就在此拦下。若不探，停止实例会让
	// test -f 假报"文件不存在"→ 跳过备份 → tee 顺手开机并覆盖原文件——
	// "写前必备份"的红线就漏了（#39 同族：把启动失败错读成文件缺失）。
	if err := s.wslGuestReady(ctx, name); err != nil {
		return DistroOpResult{}, err
	}

	text = normalizeLF(strings.TrimSpace(text))
	if _, err := validateWslConf(text); err != nil {
		return DistroOpResult{}, err
	}

	// 引用校验：默认用户必须真实存在（写坏 default 会锁死登录）。
	if user := defaultUserOf(text); user != "" {
		out, err := s.runWsl(ctx, "-d", name, "-u", "root", "--", "id", "-u", user)
		trimmed := strings.TrimSpace(out)
		if err != nil || !wslconfUIDRe.MatchString(trimmed) {
			return DistroOpResult{}, fmt.Errorf("[user] default=%q 在该发行版中不存在（id -u 求证失败：%s），已拒绝写入", user, orFailDetail(trimmed, err))
		}
	}

	// 原文件存在才备份（不存在则首建，无需备份）。
	if _, err := s.runWsl(ctx, "-d", name, "-u", "root", "--", "test", "-f", wslConfPath); err == nil {
		if out, berr := s.runWsl(ctx, "-d", name, "-u", "root", "--", "cp", "-f", wslConfPath, wslConfBackup); berr != nil {
			return DistroOpResult{}, fmt.Errorf("备份原 wsl.conf 失败，写入中止: %w %s", berr, strings.TrimSpace(out))
		}
	}

	if out, werr := s.runWslIn(ctx, text+"\n", "-d", name, "-u", "root", "--", "tee", wslConfPath); werr != nil {
		return DistroOpResult{}, fmt.Errorf("写入 %s 失败: %w %s", wslConfPath, werr, strings.TrimSpace(out))
	}
	// 复验：读回比对（tee 成功退出码不代表内容一致）。
	out, err := s.runWsl(ctx, "-d", name, "--", "cat", wslConfPath)
	if err != nil {
		return DistroOpResult{}, fmt.Errorf("写入后复验读取失败（文件可能处于中间态，可从 %s 恢复）: %w", wslConfBackup, err)
	}
	if normalizeLF(out) != text {
		return DistroOpResult{}, fmt.Errorf("写入后复验不一致（可从 %s 恢复原文件后重试）", wslConfBackup)
	}
	return DistroOpResult{
		Success: true,
		Message: fmt.Sprintf("%s 已写入并复验一致；执行「⏹ 终止」%s 后新配置生效（wsl.conf 只在发行版启动时读取）", wslConfPath, name),
	}, nil
}

// wslconfUIDRe `id -u` 的输出形态（纯数字 UID）。
var wslconfUIDRe = regexp.MustCompile(`^\d+$`)

// orFailDetail 失败因由提纯：优先 guest 原文，其次错误，最后标注空输出。
func orFailDetail(out string, err error) string {
	if out != "" {
		return out
	}
	if err != nil {
		return err.Error()
	}
	return "空输出"
}

// defaultUserOf 提取 [user] 节的 default 值（行尾注释剔除；无则空串）。
func defaultUserOf(text string) string {
	inUser := false
	for raw := range strings.SplitSeq(text, "\n") {
		line := strings.TrimSpace(raw)
		switch {
		case strings.HasPrefix(line, "["):
			inUser = strings.EqualFold(strings.Trim(line, "[]"), "user")
		case inUser:
			if k, v, ok := strings.Cut(line, "="); ok && strings.EqualFold(strings.TrimSpace(k), "default") {
				v = strings.TrimSpace(v)
				if j := strings.IndexAny(v, "#;"); j >= 0 && (j == 0 || v[j-1] == ' ') {
					v = strings.TrimSpace(v[:j])
				}
				return v
			}
		}
	}
	return ""
}

// normalizeLF 行尾归一（textarea 会带 CRLF；guest 侧只认 LF）。
func normalizeLF(s string) string {
	return strings.ReplaceAll(s, "\r\n", "\n")
}
