//go:build windows

package windows

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"

	"golang.org/x/sys/windows"
)

// ExecutionLevel 是 PE 内嵌 manifest 的 requestedExecutionLevel。
// Unknown 表示没有清单、清单损坏或清单声明无法确认；调用方必须保留
// 740 运行时兜底，不能把 Unknown 当成无需提权。
type ExecutionLevel string

const (
	ExecutionUnknown              ExecutionLevel = "unknown"
	ExecutionAsInvoker            ExecutionLevel = "asInvoker"
	ExecutionHighestAvailable     ExecutionLevel = "highestAvailable"
	ExecutionRequireAdministrator ExecutionLevel = "requireAdministrator"
)

// ReadExecutionLevel 以 LOAD_LIBRARY_AS_DATAFILE 读取 PE 的 RT_MANIFEST 资源，
// 不执行目标文件。它只做静态预判，不是运行时权限真相：资源缺失/格式未知时
// 返回 ExecutionUnknown,nil；文件打不开或资源读取失败才返回 error。
func ReadExecutionLevel(path string) (ExecutionLevel, error) {
	h, err := windows.LoadLibraryEx(path, 0, windows.LOAD_LIBRARY_AS_DATAFILE)
	if err != nil {
		return ExecutionUnknown, fmt.Errorf("读取 PE manifest 失败: %w", err)
	}
	defer windows.FreeLibrary(h)

	resource, err := windows.FindResource(h, windows.ResourceID(windows.CREATEPROCESS_MANIFEST_RESOURCE_ID), windows.RT_MANIFEST)
	if err != nil {
		// 大多数 asInvoker/旧程序没有 RT_MANIFEST；缺资源不是 IO 故障。
		return ExecutionUnknown, nil
	}
	data, err := windows.LoadResourceData(h, resource)
	if err != nil {
		return ExecutionUnknown, fmt.Errorf("读取 RT_MANIFEST 失败: %w", err)
	}
	return ParseExecutionLevel(data)
}

// ParseExecutionLevel 从 manifest XML 提取 requestedExecutionLevel。
// 独立成纯函数，测试不需要准备真实 PE；XML namespace 只看 Local 名，兼容
// Windows manifest 的多种 namespace 前缀与默认 namespace 写法。
func ParseExecutionLevel(data []byte) (ExecutionLevel, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	for {
		tok, err := dec.Token()
		if errors.Is(err, io.EOF) {
			return ExecutionUnknown, nil
		}
		if err != nil {
			return ExecutionUnknown, fmt.Errorf("解析 manifest XML 失败: %w", err)
		}
		start, ok := tok.(xml.StartElement)
		if !ok || start.Name.Local != "requestedExecutionLevel" {
			continue
		}
		for _, attr := range start.Attr {
			if attr.Name.Local != "level" {
				continue
			}
			switch strings.TrimSpace(attr.Value) {
			case string(ExecutionAsInvoker):
				return ExecutionAsInvoker, nil
			case string(ExecutionHighestAvailable):
				return ExecutionHighestAvailable, nil
			case string(ExecutionRequireAdministrator):
				return ExecutionRequireAdministrator, nil
			default:
				return ExecutionUnknown, nil
			}
		}
		return ExecutionUnknown, nil
	}
}
