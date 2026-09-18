//go:build !windows

package snapshot

import (
	"io/fs"
)

func unsafeLinkLike(_ string, info fs.FileInfo) (bool, error) {
	return info.Mode()&fs.ModeSymlink != 0, nil
}
