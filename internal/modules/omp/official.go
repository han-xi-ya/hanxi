package omp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// official.go：官方安装方式的探测/安装/更新/卸载编排（Windows 原语在
// official_windows.go，全部经接缝注入——单测用假实现，绝不碰真注册表、
// 不跑真脚本、不弹窗）。
//
// 产品口径（机主 2026-11-04 拍板）：页面上的「安装/更新/卸载」= **官方脚本路线**，
// 安装目录 `%LOCALAPPDATA%\omp`、PATH 归该目录——与官方脚本产物完全互通，
// 用户用惯的入口只有一个。hanxi 版本树（下载校验/导入/激活）是并存的
// "备用零件库"，两本账在页面上分开如实展示。
//   - 安装/更新：跑官方 install.ps1 -Binary（下载最新单文件 + 官方 sha256 自校
//     + 落位 + PATH 幂等写 + Git Bash 接线，全套官方行为）；重复跑=升到最新=更新。
//   - 卸载：删除官方目录 + 从用户 PATH 剔除该目录条目 + 广播。`~/.omp` 数据根
//     （凭据/会话/记忆）**不属"卸载程序"职责，一律保留**并在回执里指路。

// OfficialOpProgress 官方路线长动作的进度事件（'omp:official-op'）。
type OfficialOpProgress struct {
	Op      string `json:"op"`      // install/update/uninstall
	Stage   string `json:"stage"`   // running/done/error
	Message string `json:"message"` // 如实备注（脚本输出摘要或错误）
}

// officialOps 官方安装目录与其用户态集成（PATH）的操作集，接缝化。
type officialOps struct {
	dir       string // %LOCALAPPDATA%\omp
	readPath  func() (string, error)
	writePath func(string) error
	broadcast func()
	runScript func(ctx context.Context) (string, error) // 官方 install.ps1 -Binary
	probeVer  func(exe string) string                   // 实跑 exe --version，失败空串
}

// officialScript 官方一行命令的等价执行体（显式 -Binary：hanxi 用户不一定有 bun，
// 且单文件形态正是版本树同款）。
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

// exePath 官方装件路径。
func (o *officialOps) exePath() string { return filepath.Join(o.dir, "omp.exe") }

// present 官方装件是否在场（常规文件且非空）。
func (o *officialOps) present() bool {
	fi, err := os.Stat(o.exePath())
	return err == nil && fi.Mode().IsRegular() && fi.Size() > 0
}

// onPath 用户 PATH 是否已含官方目录（大小写折叠判同）。
func (o *officialOps) onPath() bool {
	raw, err := o.readPath()
	if err != nil {
		return false
	}
	return pathContains(splitPathValue(raw), o.dir)
}

// probe GetStatus 用的四元事实：目录 / 装件在场 / 实探版本 / PATH 收录。
func (o *officialOps) probe() (dir string, present bool, version string, onPath bool) {
	dir = o.dir
	present = o.present()
	if present {
		version = o.probeVer(o.exePath())
	}
	return dir, present, version, o.onPath()
}

// runOfficialScript 执行官方安装/更新脚本（install 与 update 同一动作——脚本
// 本身幂等取最新；调用方以 op 标签区分语义）。返回脚本输出（截尾供展示）。
func (o *officialOps) runOfficialScript(ctx context.Context) (string, error) {
	out, err := o.runScript(ctx)
	if err != nil {
		return out, err
	}
	// 脚本成功后机器态兜底核对：exe 必须真的在场（防"脚本说成功但文件没落"）
	if !o.present() {
		return out, fmt.Errorf("官方脚本执行完毕但未在 %s 发现 omp.exe，请查看输出", o.dir)
	}
	return out, nil
}

// uninstall 卸载官方装件：删目录 + 剔除 PATH 条目 + 广播。
// 目录不存在/PATH 不含时对应动作为 no-op，回执如实说明；`~/.omp` 数据根保留。
func (o *officialOps) uninstall() (string, error) {
	var acts []string
	if o.dir != "" {
		if err := os.RemoveAll(o.dir); err != nil {
			return "", fmt.Errorf("删除官方目录失败（若有终端正运行 omp，先退出再试）: %w", err)
		}
		acts = append(acts, fmt.Sprintf("已删除 %s", o.dir))
	}
	raw, err := o.readPath()
	if err != nil {
		return "", err
	}
	parts, removed := pathRemove(splitPathValue(raw), o.dir)
	if removed {
		if err := o.writePath(strings.Join(parts, ";")); err != nil {
			return "", err
		}
		o.broadcast()
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
