//go:build !windows

package instance

import "time"

// NewInstallerProbe 非 Windows 占位：VS Code 托管仅面向 Windows 形态。
func NewInstallerProbe() Probe { return noopProbe{} }

// NewPortableProbe 非 Windows 占位。
func NewPortableProbe(versionsDir string) Probe { return noopProbe{} }

type noopProbe struct{}

func (noopProbe) IsRunning() bool                 { return false }
func (noopProbe) WaitForReady(time.Duration) bool { return false }

// InstallerInstanceRunning 非 Windows 占位。
func InstallerInstanceRunning() bool { return false }
