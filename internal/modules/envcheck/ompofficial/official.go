// Package ompofficial 实现「Oh My Pi (omp)」的官方安装通道：一键安装/更新/卸载。
//
// 领域背景（自 internal/modules/omp 迁入，2026-11-04 机主拍板并入 envcheck
// 「版本与工具」）：omp 是纯 TUI 编码代理，机主要的安装形态是"点一下 = 跑官方
// install.ps1"——装到 %LOCALAPPDATA%\omp、PATH 与 Git Bash 接线全由官方脚本
// 自理，装完新开终端直接敲 omp。本包只编排与兜底，不复制脚本逻辑：
//   - 安装/更新：同一动作（脚本幂等取最新），瞬时网络失败自动重试（见下）；
//   - 卸载：删官方目录 + 从用户 PATH 剔除该条目 + 广播；`~/.omp` 数据根
//     （凭据/会话/记忆）**不属"卸载程序"职责，一律保留**并在回执里指路。
//
// 负裁决照旧：不接管进程运行（纯 TUI）、不自建版本树（追更感知归 envcheck 的
// ompversion 对照卡，两包分工见各自注释）。Windows 原语（注册表/广播/powershell
// 执行）在 official_windows.go 装配，单测走接缝假件，绝不碰真注册表。
//
// 两条通道的归属（2026-11-11 机主实报"点了更新不太成功"后重切）：
//   - 首装 → 官方 install.ps1 -Binary（目录里还没有 exe，脚本形态的 Git Bash 接线、
//     PATH 写入这些"装"的职责只有它全懂）；
//   - 更新/修复 → omp 自带 `omp update`（--force 为原地重装）。同一条 GitHub 通道实测
//     对比：脚本走**整包重下** 244MB / 189 秒，`omp update` 走**增量补丁** 35.7MB / 6 秒，
//     两者落位结果同为 18.18.8；而页内那张对照卡的指路文案本来就写着"官方脚本装用
//     omp update"，按钮却去跑整包脚本，是自我矛盾。换件后对新进程生效，在跑的会话仍是
//     旧镜像（自更新会明说 "Restart omp to use the new version"）。
package ompofficial

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// OpProgress 官方路线长动作的进度事件载荷（envcheck:omp-official-op）。
type OpProgress struct {
	Op      string `json:"op"`      // install/update/repair/uninstall
	Stage   string `json:"stage"`   // running/done/error
	Message string `json:"message"` // 如实备注（脚本输出摘要或错误）
}

// Status 官方装件的现场探测四事实 + 数据根指路（本包零持久化）。
type Status struct {
	OfficialDir     string `json:"officialDir"`     // 官方脚本安装目录 %LOCALAPPDATA%\omp
	OfficialPresent bool   `json:"officialPresent"` // 官方装件在场（omp.exe 存在）
	OfficialVersion string `json:"officialVersion"` // 官方装件实探版本（探测失败为空）
	OnPath          bool   `json:"onPath"`          // 用户 PATH 已含官方安装目录（命令行可直接敲 omp）
	DataDir         string `json:"dataDir"`         // omp 用户数据根（~/.omp：配置、凭据、会话、记忆库）
}

// Manager 官方安装通道编排器（接缝装配见 New / official_windows.go）。
type Manager struct {
	dir       string // %LOCALAPPDATA%\omp
	readPath  func() (string, error)
	writePath func(string) error
	broadcast func()
	runScript func(ctx context.Context) (string, error)                         // 官方 install.ps1 -Binary
	runUpdate func(ctx context.Context, exe string, force bool) (string, error) // omp 自带 `omp update`
	probeVer  func(exe string) string                                           // 实跑 exe --version，失败空串
}

// officialScript 官方一行命令的等价执行体（显式 -Binary：hanxi 用户不一定有 bun，
// 且单文件形态正是官方 install.ps1 的 binary 通道）。
const officialScriptCommand = `& ([scriptblock]::Create((irm https://omp.sh/install.ps1))) -Binary`

// officialDir 依官方脚本同口径定位安装目录（LOCALAPPDATA 优先，回退 home 拼法）。
func officialDir() string {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		base = filepath.Join(home, "AppData", "Local")
	}
	return filepath.Join(base, "omp")
}

// RepoURL 上游仓库地址（envcheck 页与本通道共用展示）。
func RepoURL() string { return "https://github.com/can1357/oh-my-pi" }

// exePath 官方装件路径。
func (m *Manager) exePath() string { return filepath.Join(m.dir, "omp.exe") }

// present 官方装件是否在场（常规文件且非空）。
func (m *Manager) present() bool {
	fi, err := os.Stat(m.exePath())
	return err == nil && fi.Mode().IsRegular() && fi.Size() > 0
}

// onPath 用户 PATH 是否已含官方目录（大小写折叠判同）。
func (m *Manager) onPath() bool {
	raw, err := m.readPath()
	if err != nil {
		return false
	}
	return pathContains(splitPathValue(raw), m.dir)
}

// Probe 现场探测状态四事实 + 数据根（无缓存无记账——文件在不在就是事实本身）。
func (m *Manager) Probe() Status {
	st := Status{OfficialDir: m.dir, OfficialPresent: m.present(), OnPath: m.onPath()}
	if st.OfficialPresent {
		st.OfficialVersion = m.probeVer(m.exePath())
	}
	if home, err := os.UserHomeDir(); err == nil {
		st.DataDir = filepath.Join(home, ".omp")
	}
	return st
}

// officialScriptAttempts 官方脚本瞬时失败的重试上限。依据：脚本第一步
// `irm https://omp.sh/install.ps1` 是单次拉取零重试，本机代理混连环境
// 下"基础连接已经关闭"类瞬时 reset 在册复发（TROUBLESHOOTING #35 同族）；
// 脚本整体幂等（重复跑=重取覆盖装），失败重跑无副作用，故整脚本级重试安全。
const officialScriptAttempts = 3

// retryBackoff 重试退避时长（包级变量：单测压缩、生产恢复，托管族
// closeGracePeriod 同规——否则重试用例真实吃满 4s+8s 拖慢门禁）。
var retryBackoff = func(attempt int) time.Duration {
	return time.Duration(attempt*4) * time.Second
}

// RunScript 执行官方安装/更新脚本（install 与 update 同一动作——脚本本身幂等
// 取最新；调用方以 op 标签区分语义）。瞬时失败自动重跑（退避递增、取消立停）；
// 返回脚本输出（截尾供展示）。
func (m *Manager) RunScript(ctx context.Context) (string, error) {
	var out string
	var err error
	for attempt := 1; attempt <= officialScriptAttempts; attempt++ {
		if cerr := ctx.Err(); cerr != nil {
			return out, cerr
		}
		out, err = m.runScript(ctx)
		if err == nil {
			// 脚本成功后机器态兜底核对：exe 必须真的在场（防"脚本说成功但文件没落"）。
			// 注意此项不重试——重试只治传输瞬时错，"跑完没落盘"是硬故障。
			if !m.present() {
				return out, fmt.Errorf("官方脚本执行完毕但未在 %s 发现 omp.exe，请查看输出", m.dir)
			}
			if attempt > 1 {
				out += fmt.Sprintf("\n（第 %d 次尝试成功，前 %d 次为瞬时网络失败已自动重试）", attempt, attempt-1)
			}
			return out, nil
		}
		if ctx.Err() != nil {
			return out, ctx.Err() // 取消优先于重试判断（模块停用即停，别硬吃退避）
		}
		if attempt < officialScriptAttempts {
			select {
			case <-time.After(retryBackoff(attempt)):
			case <-ctx.Done():
				return out, ctx.Err()
			}
		}
	}
	return out, fmt.Errorf("官方脚本连试 %d 次仍失败（多为网络瞬断，可稍后再点一次更新）: %w", officialScriptAttempts, err)
}

// selfUpdateAttempts 自更新瞬时失败的整命令级重试上限。与脚本同判据：失败点多为
// GitHub 传输瞬断（#35 同族），而 `omp update` 幂等——补丁下到临时件、校验 sha256
// 后才换件，重跑无副作用。
const selfUpdateAttempts = 3

// SelfUpdate 走 omp 自带更新通道（force=true → `omp update --force` 原地重装，
// 即页内"修复/重装"）。调用方须先确认装件在场（UpdateLatest 已代为分流）。
// 成功判据与 RunScript 同纪律：命令零退出 + 跑完 exe 仍在场；瞬时失败自动重跑。
func (m *Manager) SelfUpdate(ctx context.Context, force bool) (string, error) {
	exe := m.exePath()
	if !m.present() {
		return "", fmt.Errorf("%s 里没有 omp.exe，无从自更新（请先用官方脚本安装）", m.dir)
	}
	var out string
	var err error
	for attempt := 1; attempt <= selfUpdateAttempts; attempt++ {
		if cerr := ctx.Err(); cerr != nil {
			return out, cerr
		}
		out, err = m.runUpdate(ctx, exe, force)
		if err == nil {
			if !m.present() {
				return out, fmt.Errorf("omp update 执行完毕但 %s 不见了（换件半途失败？），请查看输出", exe)
			}
			if attempt > 1 {
				out += fmt.Sprintf("\n（第 %d 次尝试成功，前 %d 次为瞬时失败已自动重试）", attempt, attempt-1)
			}
			return out, nil
		}
		if ctx.Err() != nil {
			return out, ctx.Err() // 取消优先于重试判断（模块停用即停，别硬吃退避）
		}
		if attempt < selfUpdateAttempts {
			select {
			case <-time.After(retryBackoff(attempt)):
			case <-ctx.Done():
				return out, ctx.Err()
			}
		}
	}
	return out, fmt.Errorf("omp 自带更新连试 %d 次仍失败（多为网络瞬断，可稍后再点一次）: %w", selfUpdateAttempts, err)
}

// UpdateLatest 更新/修复重装的统一入口，按机器态分流（机主不必知道哪条通道）：
//   - 装件在场 → omp 自带自更新（增量补丁、能换动正在运行的 exe）；
//   - 装件不在场 → 官方脚本首装（此时无覆盖对象，脚本路线照常可用）。
//
// force 只影响自更新（true=已是最新也原地重装）。
func (m *Manager) UpdateLatest(ctx context.Context, force bool) (string, error) {
	if !m.present() {
		return m.RunScript(ctx)
	}
	return m.SelfUpdate(ctx, force)
}

// Uninstall 卸载官方装件：删目录 + 剔除 PATH 条目 + 广播。
// 目录不存在/PATH 不含时对应动作为 no-op，回执如实说明；`~/.omp` 数据根保留。
func (m *Manager) Uninstall() (string, error) {
	var acts []string
	if m.dir != "" {
		if err := os.RemoveAll(m.dir); err != nil {
			return "", fmt.Errorf("删除官方目录失败（若有终端正运行 omp，先退出再试）: %w", err)
		}
		acts = append(acts, fmt.Sprintf("已删除 %s", m.dir))
	}
	raw, err := m.readPath()
	if err != nil {
		return "", err
	}
	parts, removed := pathRemove(splitPathValue(raw), m.dir)
	if removed {
		if err := m.writePath(strings.Join(parts, ";")); err != nil {
			return "", err
		}
		m.broadcast()
		acts = append(acts, "已从用户 PATH 剔除该目录并广播")
	} else {
		acts = append(acts, "用户 PATH 本就不含该目录（无需剔除）")
	}
	acts = append(acts, "数据根 ~/.omp（凭据/会话/记忆）按官方口径保留")
	return strings.Join(acts, "；"), nil
}

// splitPathValue 注册表串 → 片段列表（空段由纯函数在写入时清理）。
func splitPathValue(v string) []string { return strings.Split(v, ";") }

// joinPathValue 片段列表 → 注册表串。
func joinPathValue(parts []string) string { return strings.Join(parts, ";") }
