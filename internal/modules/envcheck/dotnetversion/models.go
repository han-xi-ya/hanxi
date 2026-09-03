// Package dotnetversion 只读查询 .NET 官方 release-metadata 支持线版本。
// 数据源为微软官方 CDN 的 releases-index.json（单一官方口径，无 vendor 歧义），
// 版本关系以 latest-runtime（运行时编号，如 9.0.19）为准，与 latest-sdk 的
// SDK 编号（9.0.317）分属两套体系，不可直接互比。
package dotnetversion

import (
	"hanxi/internal/modules/envcheck/detect"
	"hanxi/internal/modules/envcheck/remoteversion"
)

// Overview 是 GetDotNetOverview 返回给前端的组合视图。
type Overview struct {
	Local     detect.ToolInfo         `json:"local"`
	Channels  []remoteversion.Channel `json:"channels"`
	IsStale   bool                    `json:"isStale"`
	FetchedAt string                  `json:"fetchedAt"`
}

// SelectChannels 将按版本线降序排列的官方支持线压缩为至多两个通道：
// 本机运行时所在支持线置顶，其后跟最新支持线；同线时只保留一条。
// 第二个返回值表示本机版本线是否仍在官方支持范围内（本机版本未知时为 false）。
func SelectChannels(lines []remoteversion.Channel, localRuntime string) ([]remoteversion.Channel, bool) {
	if len(lines) == 0 {
		return nil, false
	}
	line := VersionLine(localRuntime)
	if line == "" {
		return []remoteversion.Channel{lines[0]}, false
	}
	for i := range lines {
		if len(lines[i].Releases) == 0 || VersionLine(lines[i].Releases[0].Version) != line {
			continue
		}
		if i == 0 {
			return []remoteversion.Channel{lines[0]}, true
		}
		return []remoteversion.Channel{lines[i], lines[0]}, true
	}
	return []remoteversion.Channel{lines[0]}, false
}
