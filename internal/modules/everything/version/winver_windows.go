//go:build windows

package version

import (
	"fmt"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modVersion = syscall.NewLazyDLL("version.dll")
	procGFVIS  = modVersion.NewProc("GetFileVersionInfoSizeW")
	procGFVI   = modVersion.NewProc("GetFileVersionInfoW")
	procVQV    = modVersion.NewProc("VerQueryValueW")
)

// exeFileVersion 读取 PE 文件的字符串 FileVersion（如 "1.5.0.1422b"）。
// 必须走 StringFileInfo 字符串表而非 VS_FIXEDFILEINFO 数值——
// alpha 修订后缀（1422b 的 b）只能由字符串表示，数值型只有 4×16bit 段。
func exeFileVersion(path string) (string, error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return "", err
	}

	s, _, e1 := procGFVIS.Call(uintptr(unsafe.Pointer(p)), 0)
	if s == 0 {
		return "", win32Err("GetFileVersionInfoSizeW", e1)
	}
	buf := make([]byte, s)
	r, _, e1 := procGFVI.Call(uintptr(unsafe.Pointer(p)), 0, s, uintptr(unsafe.Pointer(&buf[0])))
	if r == 0 {
		return "", win32Err("GetFileVersionInfoW", e1)
	}

	// 先取 Translation 表确定语言/代码页，再定位字符串子块
	transSub, err := syscall.UTF16PtrFromString(`\VarFileInfo\Translation`)
	if err != nil {
		return "", err
	}
	var transPtr unsafe.Pointer
	var transLen uint32
	r, _, e1 = procVQV.Call(
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(transSub)),
		uintptr(unsafe.Pointer(&transPtr)),
		uintptr(unsafe.Pointer(&transLen)),
	)
	if r == 0 || transLen < 4 {
		return "", win32Err("VerQueryValueW(Translation)", e1)
	}
	pairs := unsafe.Slice((*uint16)(transPtr), 4/2)
	subBlock, err := syscall.UTF16PtrFromString(fmt.Sprintf(`\StringFileInfo\%04X%04X\FileVersion`, pairs[0], pairs[1]))
	if err != nil {
		return "", err
	}

	var valPtr unsafe.Pointer
	var valLen uint32
	r, _, e1 = procVQV.Call(
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(subBlock)),
		uintptr(unsafe.Pointer(&valPtr)),
		uintptr(unsafe.Pointer(&valLen)),
	)
	if r == 0 || valPtr == nil {
		return "", win32Err("VerQueryValueW(FileVersion)", e1)
	}
	// 两个实测坑（Everything.exe 1.5.0.1422b）：
	//  1. VerQueryValueW 的 valLen 对此值返回【字符数】而非字节数——按“字节/2”切会截断成 “1.5.0.”；
	//  2. utf16.Decode 不因 NUL 停——裸解码会把 szValue 之后的下一个条目头字节吞进字符串。
	// 正确姿势：windows.UTF16ToString（首个 NUL 截断）+ 可用长度封顶（指针到缓冲末端的距离）。
	avail := uintptr(len(buf)) - (uintptr(valPtr) - uintptr(unsafe.Pointer(&buf[0])))
	maxUnits := avail / 2
	if maxUnits > 512 {
		maxUnits = 512 // 版本字符串不可能超长，防御性封顶
	}
	units := unsafe.Slice((*uint16)(valPtr), maxUnits)
	return windows.UTF16ToString(units), nil
}

func win32Err(api string, e1 error) error {
	errno, ok := e1.(syscall.Errno)
	if !ok {
		errno = 0
	}
	if errno != 0 {
		return fmt.Errorf("%s: %w", api, errno)
	}
	return fmt.Errorf("%s failed", api)
}