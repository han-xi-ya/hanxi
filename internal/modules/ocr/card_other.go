//go:build !windows

package ocr

import "github.com/wailsapp/wails/v3/pkg/application"

// forceCardForeground 非 Windows 无对应原语（截屏识别主链路本就 Windows-only）。
func forceCardForeground(*application.WebviewWindow) {}
