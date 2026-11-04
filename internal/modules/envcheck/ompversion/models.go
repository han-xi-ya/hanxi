// Package ompversion 实现 Oh My Pi（omp）官方 GitHub Releases 版本发现与本机版本关系判断。
package ompversion

import (
	"hanxi/internal/modules/envcheck/detect"
	"hanxi/internal/modules/envcheck/remoteversion"
)

// Overview 组合本机 omp 探测结果与官网稳定版通道。
type Overview struct {
	Local     detect.ToolInfo         `json:"local"`
	Channels  []remoteversion.Channel `json:"channels"`
	IsStale   bool                    `json:"isStale"`
	FetchedAt string                  `json:"fetchedAt"`
}
