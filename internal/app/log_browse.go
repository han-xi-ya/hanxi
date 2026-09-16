package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"hanxi/internal/settings"
)

// defaultLogMaxLines ReadLogContent 未指定行数时的默认上限：限制单次 RPC 载荷，
// 避免超大日志整文件灌进前端 WebView。
const defaultLogMaxLines = 500

// LogFileInfo 日志文件基本元数据
type LogFileInfo struct {
	Name    string `json:"name"`
	Size    int64  `json:"size"`
	ModTime string `json:"modTime"`
}

// ListLogFiles 获取日志目录下的所有日志文件列表（按时间倒序排列）
func (s *AppService) ListLogFiles() ([]LogFileInfo, error) {
	logsDir := settings.GetPaths().LogsDir()
	entries, err := os.ReadDir(logsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []LogFileInfo{}, nil
		}
		return nil, fmt.Errorf("读取日志目录失败: %w", err)
	}

	var list []LogFileInfo
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".log") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		list = append(list, LogFileInfo{
			Name:    entry.Name(),
			Size:    info.Size(),
			ModTime: info.ModTime().Format("2006-01-02 15:04:05"),
		})
	}

	// 按修改时间倒序
	sort.Slice(list, func(i, j int) bool {
		return list[i].ModTime > list[j].ModTime
	})

	return list, nil
}

// ReadLogContent 读取指定日志文件的内容（限制最大行数避免内存溢出，默认倒序取最新行）
func (s *AppService) ReadLogContent(fileName string, maxLines int) (string, error) {
	fileName = filepath.Base(strings.TrimSpace(fileName))
	if fileName == "" || fileName == "." || fileName == "/" || fileName == "\\" {
		return "", fmt.Errorf("无效的日志文件名")
	}

	logsDir := settings.GetPaths().LogsDir()
	targetPath := filepath.Join(logsDir, fileName)

	contentBytes, err := os.ReadFile(targetPath)
	if err != nil {
		return "", fmt.Errorf("读取日志文件失败: %w", err)
	}

	raw := string(contentBytes)
	if maxLines <= 0 {
		maxLines = defaultLogMaxLines
	}

	lines := strings.Split(raw, "\n")
	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}

	return strings.Join(lines, "\n"), nil
}

// ClearLogs 清除所有历史日志（保留当天的）。
// 逐文件删除失败不再静默吞掉：聚合后一次性返回（如当天之前有文件被杀毒软件占用），
// 前端可如实提示；目录不存在视为无日志可清，仍按成功处理。
func (s *AppService) ClearLogs() error {
	logsDir := settings.GetPaths().LogsDir()
	entries, err := os.ReadDir(logsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("读取日志目录失败: %w", err)
	}
	today := time.Now().Format("2006-01-02")
	todayLog := "app-" + today + ".log"

	var errs []error
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".log") && entry.Name() != todayLog {
			if err := os.Remove(filepath.Join(logsDir, entry.Name())); err != nil {
				errs = append(errs, fmt.Errorf("删除 %s 失败: %w", entry.Name(), err))
			}
		}
	}
	return errors.Join(errs...)
}
