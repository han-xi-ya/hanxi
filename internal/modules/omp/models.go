package omp

// OmpStatus 模块整体状态快照（前端一次拉全）。纯官方通道形态：无自建版本树、
// 无激活记账——全部状态现场探测，本模块零持久化。
type OmpStatus struct {
	DataDir         string `json:"dataDir"`         // omp 用户数据根（~/.omp：配置、凭据、会话、记忆库）
	OfficialDir     string `json:"officialDir"`     // 官方脚本安装目录 %LOCALAPPDATA%\omp
	OfficialPresent bool   `json:"officialPresent"` // 官方装件在场（omp.exe 存在）
	OfficialVersion string `json:"officialVersion"` // 官方装件实探版本（探测失败为空）
	OnPath          bool   `json:"onPath"`          // 用户 PATH 已含官方安装目录（命令行可直接敲 omp）
}
