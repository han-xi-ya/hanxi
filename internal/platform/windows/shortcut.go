//go:build windows

package windows

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"unsafe"

	"github.com/go-ole/go-ole"
	"github.com/go-ole/go-ole/oleutil"
)

// 桌面快捷方式：经 WScript.Shell COM（IDispatch）创建 .lnk——
// 免去手写 IShellLinkW 21 槽 vtable 的易错路径，且 .lnk 由系统正规生成。
// 便携版工具没有安装器，此能力补齐"一键放到桌面"的常规装机心智。

var (
	procSHGetKnownFolderPath = syscall.NewLazyDLL("shell32.dll").NewProc("SHGetKnownFolderPath")
)

// folderIDDesktop FOLDERID_Desktop 的 KNOWNFOLDERID（兼容 OneDrive 重定向桌面）
func folderIDDesktop() *syscall.GUID {
	return &syscall.GUID{
		Data1: 0xB4BFCC3A,
		Data2: 0xDB2C,
		Data3: 0x424C,
		Data4: [8]byte{0xB0, 0x29, 0x7F, 0xE9, 0x9A, 0x87, 0xC6, 0x41},
	}
}

// DesktopDir 返回当前用户桌面目录。
func DesktopDir() (string, error) {
	var p *uint16
	r, _, _ := procSHGetKnownFolderPath.Call(
		uintptr(unsafe.Pointer(folderIDDesktop())),
		0, 0,
		uintptr(unsafe.Pointer(&p)),
	)
	if r != 0 {
		return "", fmt.Errorf("SHGetKnownFolderPath: 0x%08x", r)
	}
	defer ole.CoTaskMemFree(uintptr(unsafe.Pointer(p)))
	return syscall.UTF16ToString((*[1 << 16]uint16)(unsafe.Pointer(p))[:]), nil
}

// CreateDesktopShortcut 在桌面创建指向 target 的快捷方式（同名已存在则覆盖）。
// workDir 为启动工作目录（版本隔离目录）；图标取 target exe 自带图标。
func CreateDesktopShortcut(name, target, workDir string) error {
	desktop, err := DesktopDir()
	if err != nil {
		return err
	}
	if _, err := os.Stat(target); err != nil {
		return fmt.Errorf("目标不存在: %s", target)
	}
	lnkPath := filepath.Join(desktop, name+".lnk")

	// COM 调用需 STA 线程；RPC 落在某个 goroutine 上，本线程自初始化
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	coInitErr := ole.CoInitializeEx(0, ole.COINIT_APARTMENTTHREADED)
	if coInitErr != nil {
		// RPC_E_CHANGED_MODE(0x80010106)：线程已被其他模式初始化（如 HubKit 主线程 MTA）——
		// 借用现有状态继续；其余错误直接返回
		if oe, ok := coInitErr.(*ole.OleError); !ok || oe.Code() != 0x80010106 {
			return fmt.Errorf("COM 初始化失败: %w", coInitErr)
		}
	} else {
		defer ole.CoUninitialize()
	}

	unknown, err := oleutil.CreateObject("WScript.Shell")
	if err != nil {
		return fmt.Errorf("创建 WScript.Shell 失败: %w", err)
	}
	defer unknown.Release()
	shell, err := unknown.IDispatch(ole.IID_IDispatch)
	if err != nil {
		return fmt.Errorf("WScript.Shell 转 IDispatch 失败: %w", err)
	}

	// CreateShortcut 返回的对象已是 IDispatch（返回值 VARIANT 内含）
	ret, err := shell.CallMethod("CreateShortcut", lnkPath)
	if err != nil {
		return fmt.Errorf("CreateShortcut 失败: %w", err)
	}
	v := ret.Value()
	sc, ok := v.(*ole.IDispatch)
	if !ok {
		// VARIANT 拿不到 IDispatch 时走 ToIDispatch 兜底
		sc = ret.ToIDispatch()
	}
	if sc == nil {
		return fmt.Errorf("CreateShortcut 未返回快捷方式对象")
	}
	defer sc.Release()

	for _, set := range [][2]string{
		{"TargetPath", target},
		{"WorkingDirectory", workDir},
		{"IconLocation", target + ", 0"},
		{"Description", name},
	} {
		if _, err := sc.PutProperty(set[0], set[1]); err != nil {
			return fmt.Errorf("设置 %s 失败: %w", set[0], err)
		}
	}
	if _, err := sc.CallMethod("Save"); err != nil {
		return fmt.Errorf("保存快捷方式失败: %w", err)
	}
	return nil
}

// OpenURL 用默认浏览器打开链接（rundll32 FileProtocolHandler 语义）。
func OpenURL(url string) error {
	return exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", url).Start()
}