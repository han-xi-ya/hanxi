package omp

// OmpStatus 模块整体状态快照（前端一次拉全）。
// ActiveVersion 为空表示尚未指定激活版本，冷启动回退"最新已装"。
type OmpStatus struct {
	ActiveVersion string `json:"activeVersion"` // 激活版本（vX.Y.Z 或 vimported-<hash12>；空=未指定回退最新）
	ExePath       string `json:"exePath"`       // 激活版本 exe 完整路径（无已装版本为空串）
	DataDir       string `json:"dataDir"`       // omp 用户数据根（~/.omp：配置、凭据、会话、记忆库）
}
