package omp

import (
	"fmt"
	"os"
	"path/filepath"
)

// launcher.go：命令入口装配的领域编排（Windows 落地在 launcher_windows.go，
// 经 newWindowsLauncher 构造注入；单测以假实现驱动）。
//
// 设计=复刻官方 install.ps1 的"装完即可敲命令"体验，但入口归属 hanxi 版本树：
//   - 稳定入口目录 `<BaseDir>/bin/omp/omp.exe`（硬链接指向激活版本目录的资产，
//     零拷贝；跨卷回退复制）——用户 PATH 里永远只有这一个目录；
//   - 首次安装幂等写用户 PATH（HKCU\Environment 原文保留 + WM_SETTINGCHANGE
//     广播），此后升级/切换版本只换硬链接指向，注册表不再变动；
//   - 卸载入口=删我们建的那一个文件与我们的 PATH 条目；官方脚本装的
//     `%LOCALAPPDATA%\omp` 条目与目录**不属本模块，不碰**（诚实边界）。
//
// 不做的事：不改 `~/.omp` 里 omp 自己的配置（官方脚本会写 shellPath 探测 Git
// Bash——数据根是上游领地，hanxi 只读路径展示、不写内容，见包注释凭据边界）。

// launcher 是入口装配的接缝集（读/写用户 PATH、广播、链接落位）。
type launcher struct {
	readPath  func() (string, error)
	writePath func(string) error
	broadcast func()
	link      func(src, dst string) error
}

// LauncherOutcome 安装/卸载命令入口的结果如实回传。
type LauncherOutcome struct {
	BinDir      string `json:"binDir"`      // 稳定入口目录
	ExePath     string `json:"exePath"`     // 入口 exe 完整路径
	PathAdded   bool   `json:"pathAdded"`   // 本次是否真的写入了 PATH（安装时）
	PathRemoved bool   `json:"pathRemoved"` // 本次是否从 PATH 剔除了条目（卸载时）
	Broadcasted bool   `json:"broadcasted"` // 已广播环境变更（新终端即刻生效）
	Message     string `json:"message"`     // 如实备注（如"PATH 已含入口目录，未重复写入"）
}

// launcherExeName 入口 exe 定名 omp.exe（对齐官方脚本改名习惯与用户肌肉记忆）。
const launcherExeName = "omp.exe"

// install 把激活版本落位为稳定入口并确保 PATH 含入口目录；重复安装幂等
// （硬链接重建指向当前激活版；PATH 已有条目不重复写）。
func (l *launcher) install(binDir, activeExe string) (LauncherOutcome, error) {
	out := LauncherOutcome{BinDir: binDir, ExePath: filepath.Join(binDir, launcherExeName)}
	if _, err := os.Stat(activeExe); err != nil {
		return out, fmt.Errorf("激活版本 exe 不可用: %s", activeExe)
	}
	if err := l.link(activeExe, out.ExePath); err != nil {
		return out, fmt.Errorf("入口落位失败: %w", err)
	}

	raw, err := l.readPath()
	if err != nil {
		return out, err
	}
	parts, added := pathAdd(splitPathValue(raw), binDir)
	if added {
		if err := l.writePath(joinPathValue(parts)); err != nil {
			return out, err
		}
		l.broadcast()
		out.PathAdded = true
		out.Broadcasted = true
		out.Message = fmt.Sprintf("已把 %s 写入用户 PATH 并广播，新开终端即可直接敲 omp", binDir)
	} else {
		out.Message = "PATH 已含入口目录，未重复写入（幂等）"
	}
	return out, nil
}

// uninstall 删除入口文件（只删本模块建立的 launcherExeName）并幂等剔除 PATH 条目；
// 目录删空则收，非空保留（绝不连带删用户放进来别的东西）。
func (l *launcher) uninstall(binDir string) (LauncherOutcome, error) {
	out := LauncherOutcome{BinDir: binDir, ExePath: filepath.Join(binDir, launcherExeName)}
	if _, err := os.Stat(out.ExePath); err == nil {
		if err := os.Remove(out.ExePath); err != nil {
			return out, fmt.Errorf("删除入口文件失败: %w", err)
		}
	}
	raw, err := l.readPath()
	if err != nil {
		return out, err
	}
	parts, removed := pathRemove(splitPathValue(raw), binDir)
	if removed {
		if err := l.writePath(joinPathValue(parts)); err != nil {
			return out, err
		}
		l.broadcast()
		out.PathRemoved = true
		out.Broadcasted = true
	}
	if entries, err := os.ReadDir(binDir); err == nil && len(entries) == 0 {
		_ = os.Remove(binDir) // 空目录顺手收；非空绝不递归
	}
	if out.PathRemoved {
		out.Message = "入口文件与 PATH 条目已回收"
	} else {
		out.Message = "PATH 中无本模块条目，仅清理了入口文件（如有）"
	}
	return out, nil
}

// status 入口当前事实：文件在场与否 + 是否已在用户 PATH。
func (l *launcher) status(binDir string) (installed, onPath bool) {
	if fi, err := os.Stat(filepath.Join(binDir, launcherExeName)); err == nil && fi.Mode().IsRegular() && fi.Size() > 0 {
		installed = true
	}
	raw, err := l.readPath()
	if err != nil {
		return installed, false
	}
	return installed, pathContains(splitPathValue(raw), binDir)
}
