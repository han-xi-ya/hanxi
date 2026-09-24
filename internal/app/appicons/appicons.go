// Package appicons 内嵌托管工具真图标的 **Go 侧副本**（N27 批 C，托盘菜单专用）。
//
// 为什么要第二份：托盘是原生菜单，位图字节必须在 Go 进程手里；前端那份走
// vite glob 打包进 dist 后文件名带哈希/小图内联 data-URL，dist 里根本没有
// 可按原名读取的 PNG（侦查 PLAN_N27_ICON 批 C 注记"双份 embed"即此意）。
// 两副本防漂移由 appicons_sync_test.go 逐字节对账钉死：前端 assets/apps 下
// 与本目录**同名**文件必须一致；新增图标必须两边同时落才不被托盘使用。
//
// 许可：入库件逐个过 docs/THIRD_PARTY_NOTICES.md「真图标」节；权利方异议时
// 删除两份副本即整批回落文字菜单（宁缺毋滥，功能无损）。
package appicons

import (
	"embed"
	"io/fs"
)

//go:embed *.png
var files embed.FS

// FS 暴露内嵌文件系统（对账测试用）。
func FS() fs.FS { return files }

// For 按模块 ID 取 PNG 字节（托盘 MenuItem.SetBitmap 直接消费）；
// 未内嵌的模块返回 nil——调用方维持无图标文字菜单，绝不回落占位图。
func For(moduleID string) []byte {
	if moduleID == "" {
		return nil
	}
	data, err := files.ReadFile(moduleID + ".png")
	if err != nil {
		return nil
	}
	return data
}
