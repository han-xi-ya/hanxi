// Package product 集中定义 Hanxi 的稳定产品身份。
package product

const (
	Name           = "Hanxi"
	Tagline        = "开源工具工作台"
	Description    = "集中安装、管理与运行常用开源软件"
	ExecutableName = "hanxi"
	Identifier     = "io.hanxi.desktop"
	Publisher      = "Hanxi"
	DataDirName    = "Hanxi"
)

// Version 是仓库内兜底版本号：正式发布经构建脚本
// -ldflags "-X hanxi/internal/product.Version=x.y.z" 注入，避免双源漂移；
// 因此必须保持 var（const 无法被 -X 覆盖）。
var Version = "0.3.0"

// UserAgent 派生自产品身份，各模块网络请求统一引用，避免手写版本串漂移。
// 构建脚本经 -ldflags -X 注入 Version 后即自动跟随真实发布版本。
func UserAgent() string { return Name + "/" + Version }
