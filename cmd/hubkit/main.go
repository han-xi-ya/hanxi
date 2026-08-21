// hubkit 是 HubKit 的可执行入口。
// 遵循架构约定：本文件只做两件事——按 internal/app 做装配并运行。
package main

import (
	"log"

	"github.com/wailsapp/wails/v3/pkg/application"

	embedassets "hubkit" // 根目录包：承载 //go:embed all:frontend/dist
	"hubkit/internal/app"
)

func main() {
	app.RegisterEvents()

	a, cleanup := app.New(application.AssetOptions{
		Handler: application.AssetFileServerFS(embedassets.FS),
	})
	defer cleanup()

	if err := a.Run(); err != nil {
		log.Fatal(err)
	}
}
