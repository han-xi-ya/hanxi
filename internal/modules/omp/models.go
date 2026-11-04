package omp

// OmpStatus 模块整体状态快照（前端一次拉全）。
// ActiveVersion 为空表示尚未指定激活版本，冷启动回退"最新已装"。
type OmpStatus struct {
	ActiveVersion   string `json:"activeVersion"`   // 激活版本（vX.Y.Z 或 vimported-<hash12>；空=未指定回退最新）
	ExePath         string `json:"exePath"`         // 激活版本 exe 完整路径（无已装版本为空串）
	DataDir         string `json:"dataDir"`         // omp 用户数据根（~/.omp：配置、凭据、会话、记忆库）
	OfficialDir     string `json:"officialDir"`     // 官方脚本安装目录 %LOCALAPPDATA%\omp
	OfficialPresent bool   `json:"officialPresent"` // 官方装件在场（omp.exe 存在）
	OfficialVersion string `json:"officialVersion"` // 官方装件实探版本（探测失败为空）
	OnPath          bool   `json:"onPath"`          // 用户 PATH 已含官方安装目录（命令行可直接敲 omp）
}
