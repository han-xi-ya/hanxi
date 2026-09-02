// Package gitversion 实现 Git for Windows 稳定版本发现与本机版本关系判断。
package gitversion

import "hanxi/internal/modules/envcheck/detect"

// Relation 表示本机 Git 与官网最新 Git for Windows 稳定版的关系。
type Relation string

const (
	RelationUnknown         Relation = "unknown"
	RelationNotInstalled    Relation = "not-installed"
	RelationLatest          Relation = "latest"
	RelationUpdateAvailable Relation = "update-available"
	RelationAhead           Relation = "ahead"
)

// Release 表示一个 Git for Windows 官网稳定版本。
type Release struct {
	Version   string `json:"version"`
	Published string `json:"published"`
	Stale     bool   `json:"stale"`
}

// Overview 组合本机 Git 探测结果与官网近期稳定版本。
type Overview struct {
	Local    detect.ToolInfo `json:"local"`
	Releases []Release       `json:"releases"`
	Relation Relation        `json:"relation"`
	IsStale  bool            `json:"isStale"`
}
