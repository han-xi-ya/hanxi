//go:build windows

package instance

import (
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	// vscodeMutexName 安装版运行实例持有的命名互斥体（product.win32MutexName="vscode"，
	// app.ts installMutex 实证：仅 Inno 安装版创建，便携版无此互斥体）。
	// Inno 安装器的 AppMutex 升级前拦截用的就是同一名字——存在即有安装版实例存活。
	vscodeMutexName = "vscode"

	codeExeName = "Code.exe"
)

// ---------- 安装版：命名互斥体探测 ----------

type mutexProbe struct{}

// NewInstallerProbe Windows 实现：OpenMutex("vscode") 存在性探测。
// 刻意只申请 SYNCHRONIZE 最小权限（ccswitch 同策略），避免 MUTEX_ALL_ACCESS
// 在特殊 ACL 场景下 access denied 误判。
func NewInstallerProbe() Probe { return &mutexProbe{} }

func (p *mutexProbe) IsRunning() bool {
	h, err := windows.OpenMutex(windows.SYNCHRONIZE, false, windows.StringToUTF16Ptr(vscodeMutexName))
	if err != nil {
		// ERROR_FILE_NOT_FOUND(2) 等任何失败均视为不存在
		return false
	}
	_ = windows.CloseHandle(h)
	return true
}

func (p *mutexProbe) WaitForReady(timeout time.Duration) bool { return waitProbeReady(p, timeout) }

// InstallerInstanceRunning 一次性"安装版实例存活"判定（OpenMutex("vscode")）。
// 供 service 层安装升级确认闸使用——与安装版引擎的周期探测相互独立。
func InstallerInstanceRunning() bool { return (&mutexProbe{}).IsRunning() }

// ---------- 便携版：托管目录内进程镜像路径探测 ----------

type pathProbe struct {
	versionsDir string // Hanxi 托管版本根目录（vscode_* 隔离目录的父目录）
}

// NewPortableProbe Windows 实现：便携版无互斥体可依赖，以
// "存在 Code.exe 进程且镜像路径位于托管目录内"判定存活。
// 用户在任意位置自行解压的 VS Code 不在托管半径内，不误报；
// 安装版 Code.exe 路径在注册表安装目录，不会命中 versions 前缀，两形态不串。
func NewPortableProbe(versionsDir string) Probe {
	return &pathProbe{versionsDir: filepath.Clean(versionsDir)}
}

func (p *pathProbe) IsRunning() bool {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return false
	}
	defer windows.CloseHandle(snap)

	var entry windows.ProcessEntry32
	entry.Size = uint32(unsafe.Sizeof(entry))
	for err = windows.Process32First(snap, &entry); err == nil; err = windows.Process32Next(snap, &entry) {
		if !strings.EqualFold(windows.UTF16ToString(entry.ExeFile[:]), codeExeName) {
			continue
		}
		h, oerr := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, entry.ProcessID)
		if oerr != nil {
			continue // 已退出或无权限的进程：跳过（权限不足的只会是提权实例，不属托管）
		}
		image := queryImageName(h)
		windows.CloseHandle(h)
		if image == "" {
			continue
		}
		if isUnderDir(image, p.versionsDir) {
			return true
		}
	}
	return false
}

func (p *pathProbe) WaitForReady(timeout time.Duration) bool { return waitProbeReady(p, timeout) }

var (
	modKernel32            = syscall.NewLazyDLL("kernel32.dll")
	procQueryFullImageName = modKernel32.NewProc("QueryFullProcessImageNameW")
)

// queryImageName QueryFullProcessImageNameW 取进程镜像完整路径（失败返回空串）。
func queryImageName(h windows.Handle) string {
	buf := make([]uint16, 4096)
	size := uint32(len(buf))
	r, _, _ := procQueryFullImageName.Call(
		uintptr(h), 0,
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&size)),
	)
	if r == 0 {
		return ""
	}
	return syscall.UTF16ToString(buf[:size])
}

// isUnderDir 路径不区分大小写的前缀包含判定（Windows 文件系统语义），
// 且必须是目录边界（前缀以分隔符结束），防 D:\vscode-evil 命中 D:\vscode。
func isUnderDir(path, dir string) bool {
	path = filepath.Clean(path)
	dir = strings.TrimSuffix(filepath.Clean(dir), string(filepath.Separator)) + string(filepath.Separator)
	return len(path) > len(dir) && strings.EqualFold(path[:len(dir)], dir)
}

func waitProbeReady(p Probe, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if p.IsRunning() {
			return true
		}
		if time.Now().After(deadline) {
			// 末位复探一次，覆盖探测与超时判断之间的边界竞态
			return p.IsRunning()
		}
		time.Sleep(100 * time.Millisecond)
	}
}
