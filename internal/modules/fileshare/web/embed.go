// Package web 承载局域网快传站的自包含前端资产（单 HTML + 静态资源），
// 经 go:embed 打进二进制，由 fileshare.Server 在局域网监听上直接对外提供，不走主窗口。
package web

import "embed"

// DistFS 嵌入的站点文件树；index.html 为入口，assets 为其静态依赖。
//
//go:embed index.html assets
var DistFS embed.FS
