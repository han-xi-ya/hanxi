// Package appicons 内嵌托管工具真图标的 **Go 侧副本**（N27 批 C，托盘菜单专用）。
//
// 为什么要第二份：托盘是原生菜单，位图字节必须在 Go 进程手里；前端那份走
// vite glob 打包进 dist 后文件名带哈希/小图内联 data-URL，dist 里根本没有
// 可按原名读取的 PNG（侦查 PLAN_N27_ICON 批 C 注记"双份 embed"即此意）。
// 两副本防漂移由 appicons_sync_test.go 逐字节对账钉死：前端 assets/apps 下
// 与本目录**同名**文件必须一致；新增图标必须两边同时落才不被托盘使用。
//
// 尺寸分档（升切批后续拆雷）：根目录 *.png 是升切后的 64px 展示档（与前端
// 逐字节同源）；menus/*.png 是托盘专用的 **16px 菜单变体档**——Wails beta.10
// 的 SetBitmap 通路（w32.SetMenuIcons→SetMenuItemBitmaps）把 PNG 按原生尺寸
// 透传给 Win32，不做 SM_CXSMICON 缩放，64px 直接挂菜单会撑爆行高。变体系
// LANCZOS 预生成件，运行期零计算；每登记件必有对偶由 sources_test.go 锁死。
//
// 许可：入库件逐个过 docs/THIRD_PARTY_NOTICES.md「真图标」节（16px 变体为
// 同源降采样派生件，许可随原件，不另立条目）；权利方异议时删除两份副本即
// 整批回落文字菜单（宁缺毋滥，功能无损）。
package appicons

import (
	"embed"
	"io/fs"
)

//go:embed *.png
var files embed.FS

//go:embed menus/*.png
var menuFiles embed.FS

// FS 暴露内嵌文件系统（根展示档，对账测试用）。
func FS() fs.FS { return files }

// MenuFS 暴露 16px 菜单变体内嵌文件系统（对账测试用）。
func MenuFS() fs.FS { return menuFiles }

// For 按模块 ID 取 64px 展示档 PNG 字节。**托盘菜单勿用本函数**（原生尺寸
// 透传会撑行高），菜单消费一律走 ForMenu；本函数保留给未来需要全分辨率
// 位图的其它原生场景（如自绘窗口图标）。未内嵌的模块返回 nil。
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

// ForMenu 按模块 ID 取 16px 菜单变体 PNG 字节（托盘 MenuItem.SetBitmap 消费）；
// 未登记/无变体的模块恒 nil——调用方维持无图标文字菜单，绝不回落占位图，
// 也绝不回退 64px 展示档（宁缺毋滥，防撑行高回潮）。
func ForMenu(moduleID string) []byte {
	if moduleID == "" {
		return nil
	}
	data, err := menuFiles.ReadFile("menus/" + moduleID + ".png")
	if err != nil {
		return nil
	}
	return data
}
