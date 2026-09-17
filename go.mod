module hanxi

go 1.26.0

require (
	github.com/BurntSushi/toml v1.6.0
	github.com/go-ole/go-ole v1.3.0
	github.com/mark3labs/mcp-go v0.41.1
	github.com/wailsapp/wails/v3 v3.0.0-beta.10
	golang.org/x/net v0.58.0
	golang.org/x/sys v0.47.0
)

require (
	github.com/adrg/xdg v0.5.3 // indirect
	github.com/bahlo/generic-list-go v0.2.0 // indirect
	github.com/buger/jsonparser v1.1.1 // indirect
	github.com/coder/websocket v1.8.14 // indirect
	github.com/godbus/dbus/v5 v5.2.2 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/invopop/jsonschema v0.13.0 // indirect
	github.com/jchv/go-winloader v0.0.0-20250406163304-c1995be93bd1 // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/mailru/easyjson v0.9.0 // indirect
	github.com/mattn/go-colorable v0.1.14 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	// cast/easyjson：本地 GOMODCACHE 只存有 mcp-go 所需 API 的旧/新版 zip
	// （cast v1.10.0 与 easyjson v0.7.7 缺 zip/mod），离线构建期钉到可解析形态：
	// cast 被 wails 图抬高到 v1.10.0 故用 replace 落回 v1.7.1（mcp-go 自身要求版本，
	// wails 无 import 不受影响）；easyjson 升到 v0.9.0。恢复网络后跑
	// `go mod tidy` 验证并酌情撤除 replace（详见 docs/TROUBLESHOOTING.md #61）。
	github.com/spf13/cast v1.10.0 // indirect
	github.com/wk8/go-ordered-map/v2 v2.1.8 // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace github.com/spf13/cast => github.com/spf13/cast v1.7.1
