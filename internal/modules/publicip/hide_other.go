//go:build !windows

package publicip

import "os/exec"

func hideWindow(cmd *exec.Cmd) {}
