package omp

// OmpStatus 模块整体状态快照（前端一次拉全）。
// ActiveVersion 为空表示尚未指定激活版本，冷启动回退"最新已装"。
type OmpStatus struct {
	ActiveVersion     string `json:"activeVersion"`     // 激活版本（vX.Y.Z 或 vimported-<hash12>；空=未指定回退最新）
	ExePath           string `json:"exePath"`           // 激活版本 exe 完整路径（无已装版本为空串）
	DataDir           string `json:"dataDir"`           // omp 用户数据根（~/.omp：配置、凭据、会话、记忆库）
	BinDir            string `json:"binDir"`            // 稳定命令入口目录 <BaseDir>\bin\omp
	LauncherInstalled bool   `json:"launcherInstalled"` // 入口 exe 文件在场（硬链接指向当前激活版）
	OnPath            bool   `json:"onPath"`            // 用户 PATH 已含入口目录（命令行可直接敲 omp）
}
