// Package pythonversion 实现 Python 官方正式版本与受支持 minor 通道发现。
package pythonversion

import (
	"hanxi/internal/modules/envcheck/detect"
	"hanxi/internal/modules/envcheck/remoteversion"
)

// Overview 组合本机 Python 探测结果与 Python.org 正式版本通道。
type Overview struct {
	Local     detect.ToolInfo         `json:"local"`
	Channels  []remoteversion.Channel `json:"channels"`
	IsStale   bool                    `json:"isStale"`
	FetchedAt string                  `json:"fetchedAt"`
}

// Lifecycle 是 Python 官方开发指南声明的 minor 分支生命周期。
type Lifecycle struct {
	Minor     string `json:"minor"`
	Status    string `json:"status"`
	EndOfLife string `json:"endOfLife"`
}

// Catalog 是经严格校验、去重和排序后的官方发布集合。
type Catalog struct {
	Releases   []remoteversion.Release `json:"releases"`
	Lifecycles []Lifecycle             `json:"lifecycles"`
}
