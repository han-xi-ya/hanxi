//go:build windows

package snapshot

// Windows 不支持以普通 os.File 句柄对目录调用 FlushFileBuffers；目录改名在同卷内
// 仍是原子发布，manifest 文件本身已先 fsync。此处保留统一步骤与故障注入 seam。
func syncDirectory(string) error { return nil }
