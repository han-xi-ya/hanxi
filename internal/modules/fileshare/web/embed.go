package web

import "embed"

//go:embed index.html assets
var DistFS embed.FS
