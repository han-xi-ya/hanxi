package appicons

// 升切台账（N27 追加批 2026-09-26）：在册真图标全册按"源资产真实尺寸、64px 封顶"
// 重切后的**出货边长**单一登记表，供消费面（如轮盘 wheelIconBudget 的 srcPx）
// 判断"目标显示尺寸是否已到源天花板、继续放大只会糊"。
//
// 口径：
//   - exe 提取件（bcu…windterm 19 枚）：从本机已装托管 exe 的 PE 资源
//     RT_GROUP_ICON/RT_ICON 取**最大真实画幅**（多数源为 256px，quicklook 512、
//     keyviz 128、rufus 恰 64）再等比缩到 64，绝不上采样；
//   - 开源仓库件：按源最大真实尺寸取——bili23(icns 1024)/eartrumpet(包资产 256)
//     缩至 64；rustdesk(icon.ico 原生 64 BMP 画幅)/termora(icons/termora_64x64.png)
//     /nanazip(PackageAssets targetsize-64，CC BY-ND 零改作)直用；
//   - ddnsgo/frpc 官方 favicon.ico 最大画幅即 48px——**源天花板 48**，
//     不为凑 64 放大；
//   - 前端 assets/apps/generic.png（自绘通用徽标）不在真图标之列，维持 32。
//
// 防漂移：sources_test.go 逐枚解码内嵌 PNG 比对边长，且键集与内嵌件双向锁死——
// 任何人单侧换图/漏登记都会变红。
var sourcePx = map[string]int{
	"bcu":           64,
	"bili23":        64,
	"ccswitch":      64,
	"ddnsgo":        48, // 源天花板：官方 favicon.ico 最大画幅 48
	"douzy":         64,
	"eartrumpet":    64,
	"everything":    64,
	"flclash":       64,
	"frpc":          48, // 源天花板：官方 favicon.ico 最大画幅 48
	"guoheview":     64,
	"keyviz":        64,
	"litemonitor":   64,
	"mangodisk":     64,
	"markeron":      64,
	"nanazip":       64,
	"papertodo":     64,
	"paseo":         64,
	"piclite":       64,
	"quicklook":     64,
	"rufus":         64,
	"rustdesk":      64,
	"snipaste":      64,
	"subnetdesk":    64,
	"termora":       64,
	"translucenttb": 64,
	"windterm":      64,
}

// SourcePx 返回模块真图标入库件的边长（px）；未入库/无内嵌件恒 0。
// 消费面据此判定放大上限（0 = 无真图标位图，走通用徽标/文字回落轨）。
func SourcePx(moduleID string) int {
	return sourcePx[moduleID]
}
