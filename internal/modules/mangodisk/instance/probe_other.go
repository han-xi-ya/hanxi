//go:build !windows

package instance

import "time"

type noopProbe struct{}

func NewMangoDiskProbe() MangoDiskProbe              { return &noopProbe{} }
func (p *noopProbe) IsRunning() bool                 { return false }
func (p *noopProbe) WaitForReady(time.Duration) bool { return false }
func (p *noopProbe) SignalPID() (uint32, bool)       { return 0, false }
