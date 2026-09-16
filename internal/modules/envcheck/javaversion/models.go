// Package javaversion 实现 Eclipse Temurin JDK 官网版本发现与本机版本关系判断。
package javaversion

import (
	"hanxi/internal/modules/envcheck/detect"
	"hanxi/internal/modules/envcheck/remoteversion"
)

// TemurinVendor 与 detect.javaVendor 的输出字符串保持一致：仅当本机发行版命中该值才做补丁级比较。
const TemurinVendor = "Eclipse Temurin"

// Overview 组合本机 Java 探测结果与 Temurin GA 版本通道。
type Overview struct {
	Local     detect.ToolInfo         `json:"local"`
	Channels  []remoteversion.Channel `json:"channels"`
	IsStale   bool                    `json:"isStale"`
	FetchedAt string                  `json:"fetchedAt"`
}

// RelationFor 仅在本机发行版为 Temurin 时比较补丁版本；其他厂商只能可靠比较 feature 线。
func RelationFor(local detect.ToolInfo, latest string) remoteversion.Relation {
	if local.Status != detect.StatusInstalled {
		return remoteversion.RelationNotInstalled
	}
	localVersion, okLocal := parseVersion(local.Version)
	latestVersion, okLatest := parseVersion(latest)
	if !okLocal || !okLatest {
		return remoteversion.RelationUnknown
	}
	if localVersion.feature != latestVersion.feature {
		return relationFromCompare(localVersion.feature, latestVersion.feature)
	}
	if local.Details == nil || local.Details.Java == nil || local.Details.Java.Vendor != TemurinVendor {
		return remoteversion.RelationUnknown
	}
	return remoteversion.RelationFor(true, local.Version, latest, Compare)
}

func relationFromCompare(local, latest uint64) remoteversion.Relation {
	switch {
	case local < latest:
		return remoteversion.RelationUpdateAvailable
	case local > latest:
		return remoteversion.RelationAhead
	default:
		return remoteversion.RelationUnknown
	}
}
