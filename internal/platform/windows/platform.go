//go:build windows

package windows

import (
	"hubkit/internal/platform"
)

type WindowsPlatform struct {
	network platform.NetworkAPI
	port    platform.PortAPI
	process platform.ProcessAPI
	job     platform.JobAPI
}

func New() (platform.Platform, error) {
	return &WindowsPlatform{
		network: NewNetworkAPI(),
		port:    NewPortAPI(),
		process: NewProcessAPI(),
		job:     NewJobAPI(),
	}, nil
}

func (p *WindowsPlatform) Network() platform.NetworkAPI {
	return p.network
}

func (p *WindowsPlatform) Port() platform.PortAPI {
	return p.port
}

func (p *WindowsPlatform) Process() platform.ProcessAPI {
	return p.process
}

func (p *WindowsPlatform) Job() platform.JobAPI {
	return p.job
}

func (p *WindowsPlatform) DesktopDir() (string, error) {
	return DesktopDir()
}

func (p *WindowsPlatform) CreateDesktopShortcut(name, target, workDir string) error {
	return CreateDesktopShortcut(name, target, workDir)
}

func (p *WindowsPlatform) OpenURL(url string) error {
	return OpenURL(url)
}
