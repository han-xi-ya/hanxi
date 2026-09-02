// Package nodeversion 实现 Node.js 官网 LTS 与 Current 版本发现。
package nodeversion

import (
	"hanxi/internal/modules/envcheck/detect"
	"hanxi/internal/modules/envcheck/remoteversion"
)

// Overview 组合本机 Node.js 探测结果与官网 LTS/Current 通道。
type Overview struct {
	Local     detect.ToolInfo         `json:"local"`
	Channels  []remoteversion.Channel `json:"channels"`
	IsStale   bool                    `json:"isStale"`
	FetchedAt string                  `json:"fetchedAt"`
}
