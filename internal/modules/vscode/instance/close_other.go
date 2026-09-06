//go:build !windows

package instance

// postCloseByPID 非 Windows 占位（引擎仅测试路径可达；生产托管仅 Windows）。
func postCloseByPID(pid uint32) {}
