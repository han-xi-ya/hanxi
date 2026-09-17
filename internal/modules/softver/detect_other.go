//go:build !windows

package softver

import "errors"

// 本模块的重头（注册表 Uninstall、PE 版本资源、两代微信目录布局）全是
// Windows 专属能力，非 Windows 构建保留编译与官方通道，本机探测如实报错。
func attachPlatformDefaults(s *SoftverService) {
	s.probeLocal = func() (localData, error) {
		return localData{}, errors.New("软件版本检测的本机探测目前仅支持 Windows")
	}
}
