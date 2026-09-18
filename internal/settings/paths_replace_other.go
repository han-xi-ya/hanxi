//go:build !windows

package settings

import "os"

func replaceFileOS(src, dst string) error {
	return os.Rename(src, dst)
}
