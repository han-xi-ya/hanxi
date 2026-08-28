//go:build !windows

package instance

func postCloseByPID(uint32) int { return 0 }
