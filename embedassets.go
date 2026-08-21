// Package embedassets 承载前端编译产物（frontend/dist）的 embed。
//
// 说明：Go embed 规则不允许包含 ".." 的 pattern，因此无法在 cmd/hubkit 下
// 通过相对路径引用根目录的 frontend/dist；此包必须位于仓库根目录。
package embedassets

import "embed"

//go:embed all:frontend/dist
var FS embed.FS