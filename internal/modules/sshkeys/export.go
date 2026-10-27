package sshkeys

// 导出投递：把账本渲染成 ssh 真正会去读的那份文件。
//
// **这里有一处架构张力，写在代码里而不是藏在讨论里**：本仓的数据纪律是
// "同级即唯一存在方式、不往用户目录静默落东西"（internal/settings/paths.go），
// 而 ssh 客户端读配置的位置是**它编译时写死的** ~/.ssh/config——不是偏好，是硬约束，
// 第三方程序没法被教去读一个新路径。
//
// 取舍形态（四条，缺一条就不该把这个按钮放出来）：
//  1. **只在用户显式点「同步给 ssh」时写**，绝不在账本变更时自动落盘——禁的是"静默"；
//  2. **只写自己那个文件** `~/.ssh/hanxi.conf`，顶层 `~/.ssh/config` 只幂等加一段
//     带标记的 Include，撤销时也只切自己那段，**永不删除用户的 config**；
//  3. 覆盖前留 `.hanxi.bak`，写完**读回复核**（照 internal/modules/wsl/hostconf.go
//     的 SaveWslHostConf 四步：闸门 → 备份 → 原子写 → 复核）；
//  4. 落盘路径**原样回显给前端**，让人知道哪儿多了个文件。

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"hanxi/packages/go/sshconf"
)

const (
	// confFileName 是产物文件名。刻意带 hanxi 前缀，避免与用户自己的文件撞名。
	confFileName = "hanxi.conf"
	// include 标记对：撤销时按这两个标记之间切，不靠行数或位置猜。
	// 标记串里带模块显示名，改名时要一并改；**改它的真正代价不是文案**：
	// 已经写过的 config 里留的是旧标记，stripInclude 会找不到段落而选择"不动"，
	// 留下一行失效的 Include。所以标记串一旦对外发过就视为冻结，宁可文案略旧也不改。
	includeMarkStart = "# ↓ hanxi「SSH 密钥管理」：主机清单产物，勿手改（改到 hanxi 里）"
	includeMarkEnd   = "# ↑ hanxi include 结束"
	includeLine      = "Include hanxi.conf"
	backupSuffix     = ".hanxi.bak"
)

// ExportConf 渲染账本并落盘到 ~/.ssh/hanxi.conf，同时保证 Include 接线在位。
func (l *ledger) exportConf() (ExportSummary, error) {
	items, err := l.list()
	if err != nil {
		return ExportSummary{}, err
	}
	summary, content, err := renderForExport(items)
	if err != nil {
		return summary, err
	}

	dir, err := sshHomeDir()
	if err != nil {
		return summary, err
	}
	confPath := filepath.Join(dir, confFileName)
	summary.ConfPath = confPath
	// 产物内容与盘上一致就什么都不做（幂等：反复点按钮不产生新备份、不改 mtime）。
	if existing, rerr := os.ReadFile(confPath); rerr == nil && string(existing) == content {
		summary.Written = false
		summary.Drift = false
		summary.Included = includeInPlace(filepath.Join(dir, "config"))
		summary.Message = "产物已是最新，未写盘。"
		if !summary.Included {
			summary.Message = "产物是最新的，但顶层 config 还缺 Include 行。"
		}
		return summary, nil
	}

	backup, err := writeConfAtomic(confPath, content, false)
	if err != nil {
		return summary, err
	}
	summary.Written = true
	summary.Drift = false
	summary.BackupPath = backup
	summary.Included, err = ensureInclude(filepath.Join(dir, "config"))
	if err != nil {
		// 产物已经写成功、Include 没接上，这是**半态**，必须如实说清——
		// 报成一个 error 会让人以为整件事没发生，转而重复点按钮。
		summary.Message = fmt.Sprintf("产物已写入 %s，但接线失败：%v", confPath, err)
		return summary, errors.New(summary.Message)
	}
	summary.Message = fmt.Sprintf("已写入并复验一致：%s", confPath)
	if summary.KeyMissing > 0 {
		summary.Message += fmt.Sprintf("；其中 %d 条的私钥文件在本机不存在，ssh 会连不上", summary.KeyMissing)
	}
	return summary, nil
}

// ExportStatus 只读对账：账本比盘上产物新吗、接线在不在、有没有缺钥匙。
func (l *ledger) exportStatus() (ExportSummary, error) {
	items, err := l.list()
	if err != nil {
		return ExportSummary{}, err
	}
	summary, content, err := renderForExport(items)
	if err != nil {
		return summary, err
	}
	dir, err := sshHomeDir()
	if err != nil {
		return summary, err
	}
	confPath := filepath.Join(dir, confFileName)
	summary.ConfPath = confPath
	existing, rerr := os.ReadFile(confPath)
	switch {
	case errors.Is(rerr, os.ErrNotExist):
		summary.Drift = len(content) > 0 && summary.Rendered > 0
		summary.Message = "还没导出过：ssh 目前读不到 hanxi 的清单。"
	case rerr != nil:
		return summary, fmt.Errorf("读取 %s 失败: %w", confPath, rerr)
	default:
		summary.Drift = string(existing) != content
		if summary.Drift {
			summary.Message = "账本已改动，产物还是旧的。"
		} else {
			summary.Message = "产物与账本一致。"
		}
	}
	summary.Included = includeInPlace(filepath.Join(dir, "config"))
	return summary, nil
}

// removeExport 撤销导出：回到"hanxi 没碰过 ~/.ssh"的状态。
//
// 三条判据，都是 2026-10-23 机主实测"撤销后 ~/.ssh 里还留着一堆 .bak"之后定的：
//
// 一、**产物删掉就是删掉，不备份**。`hanxi.conf` 是账本的渲染结果，可从账本随时重生成，
// 给它做 .bak 只会在用户目录里多留一份清单副本（别名/地址/用户/密钥路径都是可看的信息）。
// 顺带清掉历史遗留的 `hanxi.conf.hanxi.bak`——那是旧版本留下的脏物。
//
// 二、**用户的 config 只摘我们那段，且摘完为空时要分清是谁的**。能删的唯一条件是：
// 摘掉我们的段之后文件为空，**且备份里除了我们那段也没有别的内容**——这说明这个 config
// 本来就是「同步给 ssh」时我们创建的，删掉才是"回到没碰过"。有任何一行是别人的，
// 就原样留着（哪怕空一行不剩也别赌）。
//
// 三、`.bak` 这个名字**只用于"改动用户文件前的快照"**。它是快照，不是"你的原件"——
// 用户原本可能压根没有这个文件。文案要照实说，别让文件名骗人。
func (l *ledger) removeExport() (ExportSummary, error) {
	dir, err := sshHomeDir()
	if err != nil {
		return ExportSummary{}, err
	}
	confPath := filepath.Join(dir, confFileName)
	summary := ExportSummary{ConfPath: confPath}

	if _, statErr := os.Stat(confPath); statErr == nil {
		if rErr := os.Remove(confPath); rErr != nil {
			return summary, fmt.Errorf("删除产物失败: %w", rErr)
		}
		summary.Written = true
	}
	// 旧版撤销会先给产物留一份 .bak；一并清掉，撤销的终态就是"这里没有 hanxi 的东西"。
	_ = os.Remove(confPath + backupSuffix)

	configPath := filepath.Join(dir, "config")
	changed, err := stripInclude(configPath)
	if err != nil {
		return summary, err
	}
	summary.Message = "已撤销：产物 hanxi.conf 已删除"
	if changed {
		summary.Message += "，顶层 config 里那段 Include 已摘掉"
	}

	// 摘完若只剩空白，且备份里除了我们那段也没有别的内容 → 这文件是我们建的，删掉。
	if rest, rErr := os.ReadFile(configPath); rErr == nil && strings.TrimSpace(string(rest)) == "" {
		if ownFileWeCreated(configPath + backupSuffix) {
			if rmErr := os.Remove(configPath); rmErr == nil {
				_ = os.Remove(configPath + backupSuffix)
				summary.Message += "（该 config 由此前导出时创建，已一并移除）"
			}
		} else if summary.BackupPath == "" {
			summary.BackupPath = configPath + backupSuffix
		}
	} else if changed {
		summary.BackupPath = configPath + backupSuffix
	}
	return summary, nil
}

// ownFileWeCreated 判"摘掉我们那段之后是否本来就没有内容"——即备份里除了我们那段
// 只剩空白。真拿不准时一律返回 false：宁可留一个空文件，也不能删掉别人的东西。
func ownFileWeCreated(backupPath string) bool {
	raw, err := os.ReadFile(backupPath)
	if err != nil {
		return false
	}
	i := strings.Index(string(raw), includeMarkStart)
	j := strings.Index(string(raw), includeMarkEnd)
	if i < 0 || j < 0 {
		return false // 没有完整标记段：不猜
	}
	rest := string(raw)[:i] + string(raw[j+len(includeMarkEnd):])
	return strings.TrimSpace(rest) == ""
}

// renderForExport 走一遍校验与渲染，并顺手统计"本机有没有这把钥匙"。
// 渲染失败（含 sshconf 的最后一道自证）时**不写盘**——宁可不写，不写坏。
func renderForExport(items []HostEntry) (ExportSummary, string, error) {
	hosts := toConfHosts(items)
	content, err := sshconf.Render(hosts)
	if err != nil {
		return ExportSummary{}, "", fmt.Errorf("清单渲染失败，未写盘: %w", err)
	}
	summary := ExportSummary{Rendered: 0, MissingKeyAliases: []string{}}
	for _, h := range hosts {
		if !h.Enabled {
			continue
		}
		summary.Rendered++
		p := strings.TrimSpace(h.IdentityFile)
		if p == "" {
			continue // 没指定钥匙 ≠ 钥匙缺失：前者是"走 agent/默认身份"，语义不同，不该扣帽子
		}
		if _, statErr := os.Stat(expandHome(p)); statErr != nil {
			summary.KeyMissing++
			// 名单与总数同批给出：前端只渲染事实，不再自己拿台账基名去猜（猜错=误删导火索）。
			summary.MissingKeyAliases = append(summary.MissingKeyAliases, h.Alias)
		}
	}
	return summary, content, nil
}

// writeConfAtomic 原子写：（按 keepBackup 决定是否备份）→ 临时文件 → 改名 → 读回复核。
// 返回实际留下的备份路径（未备份或原文件不存在则空串）。
func writeConfAtomic(path, content string, keepBackup bool) (string, error) {
	if dirErr := os.MkdirAll(filepath.Dir(path), 0o700); dirErr != nil {
		return "", fmt.Errorf("无法准备 %s 所在目录: %w", filepath.Dir(path), dirErr)
	}
	// keepBackup 的分界是**这份文件是不是只有 hanxi 自己造得出**：
	//   - 产物 hanxi.conf 传 false —— 它是账本的渲染结果，随时可重生成。给它留 .bak 只是在
	//     用户的 ~/.ssh 里多放一份清单副本（别名/地址/用户/密钥路径都在里面），既不是退路、
	//     又是可能外泄的东西。机主 2026-10-24 实测到"撤销后还留着 .bak"正是这条的产物。
	//   - 用户的 ~/.ssh/config 传 true —— 里面有别人手写的段，改砸了必须能还原。
	backup := ""
	if keepBackup {
		if old, rerr := os.ReadFile(path); rerr == nil {
			backup = path + backupSuffix
			if werr := os.WriteFile(backup, old, 0o600); werr != nil {
				return "", fmt.Errorf("备份原文件失败，写入中止: %w", werr)
			}
		}
	} else if _, statErr := os.Stat(path + backupSuffix); statErr == nil {
		// 旧版本会给产物留 .bak；这次写顺带清掉，否则升级到新版后那份垃圾永远躺在那儿。
		_ = os.Remove(path + backupSuffix)
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), confFileName+".tmp-*")
	if err != nil {
		return "", fmt.Errorf("创建临时文件失败: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.WriteString(content); err != nil {
		_ = tmp.Close()
		return "", fmt.Errorf("写临时文件失败: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("关闭临时文件失败: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return backup, fmt.Errorf("落盘改名失败（原文件已备份在 %s）: %w", backup, err)
	}
	back, rerr := os.ReadFile(path)
	if rerr != nil || string(back) != content {
		return backup, fmt.Errorf("写入后复验不一致——可从 %s 恢复原文件重试", backup)
	}
	return backup, nil
}

// ensureInclude 在顶层 config 里保证那段带标记的 Include 存在（幂等）。
// config 不存在时创建它——这是"接线"必需的最小动作，且内容只有我们自己那一小段。
func ensureInclude(configPath string) (bool, error) {
	raw, err := os.ReadFile(configPath)
	if errors.Is(err, os.ErrNotExist) {
		_, wErr := writeConfAtomic(configPath, includeBlock(), true)
		return wErr == nil, wErr
	}
	if err != nil {
		return false, fmt.Errorf("读取 %s 失败: %w", configPath, err)
	}
	if strings.Contains(string(raw), includeMarkStart) {
		return true, nil
	}
	next := strings.TrimRight(string(raw), "\n") + "\n\n" + includeBlock()
	_, wErr := writeConfAtomic(configPath, next, true)
	return wErr == nil, wErr
}

// includeInPlace 只查不写，供状态对账用。
func includeInPlace(configPath string) bool {
	raw, err := os.ReadFile(configPath)
	if err != nil {
		return false
	}
	return strings.Contains(string(raw), includeMarkStart)
}

// stripInclude 切掉自己那段；文件里没有标记就什么都不做。
func stripInclude(configPath string) (bool, error) {
	raw, err := os.ReadFile(configPath)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("读取 %s 失败: %w", configPath, err)
	}
	text := string(raw)
	i := strings.Index(text, includeMarkStart)
	if i < 0 {
		return false, nil
	}
	j := strings.Index(text[i:], includeMarkEnd)
	if j < 0 {
		// 起始标记在、结束标记没了——文件被手改过。此刻**宁可不动**：
		// 按"找到起始就删到结尾"处理会连带吞掉用户自己的配置。
		return false, fmt.Errorf("%s 里 hanxi 的 Include 段缺结束标记，可能被手工编辑过；为不误删你的配置，撤销中止", configPath)
	}
	next := text[:i] + text[i+j+len(includeMarkEnd):]
	next = strings.Trim(next, "\n")
	if next != "" {
		next += "\n"
	}
	_, wErr := writeConfAtomic(configPath, next, true)
	return wErr == nil, wErr
}

func includeBlock() string {
	return includeMarkStart + "\n" + includeLine + "\n" + includeMarkEnd + "\n"
}

// expandHome 只展开前导 `~/`——账本里的 KeyPath 允许用户写 ~ 形态（ssh 也这么认），
// 判"钥匙在不在本机"时必须先展开，否则永远判成缺失。
func expandHome(p string) string {
	if p == "" || (p[0] != '~') {
		return p
	}
	home, err := userHomeDir() // 与台账扫描同一个接缝，见 keyring.go 的 userHomeDir 注释
	if err != nil {
		return p
	}
	if len(p) == 1 || p[1] == '/' || p[1] == '\\' {
		return filepath.Join(home, strings.TrimLeft(p[1:], "/\\"))
	}
	return p
}
