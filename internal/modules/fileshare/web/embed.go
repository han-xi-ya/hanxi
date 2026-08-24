package web

import "embed"

//go:embed index.html
var DistFS embed.FS
