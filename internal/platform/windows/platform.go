//go:build windows

// Package windows 是 platform 抽象接口的 Windows 实现：iphlpapi/advapi32 等
// 系统 DLL 的直接调用（网卡、端口表、进程、Job Object、应用包、快捷方式、提权、
// DPAPI、托盘辅助能力）。仅在 windows 构建标签下编译，业务侧一律经 platform.Platform 获取。
package windows

import (
	"hanxi/internal/platform"
	"hanxi/internal/platform/apppackage"
)

// WindowsPlatform 聚合各子能力实现，是 platform.Platform 的 Windows 单例载体。
// 各子 API 均为无状态或自持锁实现，可在多 goroutine 并发调用。
type WindowsPlatform struct {
	network    platform.NetworkAPI
	port       platform.PortAPI
	process    platform.ProcessAPI
	job        platform.JobAPI
	appPackage apppackage.API
	keepAwake  platform.KeepAwakeAPI
}

// New 装配全部 Windows 子能力。当前各构造函数不会失败，error 恒为 nil，保留以对齐跨平台工厂签名。
func New() (platform.Platform, error) {
	return &WindowsPlatform{
		network:    NewNetworkAPI(),
		port:       NewPortAPI(),
		process:    NewProcessAPI(),
		job:        NewJobAPI(),
		appPackage: NewAppPackageAPI(),
		keepAwake:  NewKeepAwake(),
	}, nil
}

// 以下访问器返回 New 时创建的单例子 API 实例；DesktopDir/CreateDesktopShortcut/OpenURL 转发包级函数。
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

func (p *WindowsPlatform) AppPackage() apppackage.API {
	return p.appPackage
}

func (p *WindowsPlatform) KeepAwake() platform.KeepAwakeAPI {
	return p.keepAwake
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
