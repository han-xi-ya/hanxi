//go:build windows

package snapshot

import (
	"io/fs"

	"golang.org/x/sys/windows"
)

func unsafeLinkLike(path string, info fs.FileInfo) (bool, error) {
	if info.Mode()&fs.ModeSymlink != 0 {
		return true, nil
	}
	ptr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return false, err
	}
	attrs, err := windows.GetFileAttributes(ptr)
	if err != nil {
		return false, err
	}
	return attrs&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0, nil
}
