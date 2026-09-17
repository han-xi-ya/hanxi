package app

import (
	"hanxi/internal/settings"
)

// 存储分区 RPC 面（F6 数据目录绑定）。绑定指针是 exe 同级 hanxi.bind，
// "绑定哪个用哪个"；换绑/解绑写入声明后需重启 Hanxi 才生效——路径快照由
// InitPaths 一次性解析，运行中不做热迁移（避免半路改家的数据撕裂风险）。
// 独立成文件，避免触碰 service.go 的装配共享 hunk（并行开发纪律）。

// BindDataDir 将数据根显式绑定到指定绝对目录（重启后生效）。
// 校验、目标预创建与指针落盘失败均返回明确中文错误，由设置页 toast 呈现。
func (s *AppService) BindDataDir(target string) error {
	return settings.BindDataDir(target)
}

// UnbindDataDir 清除数据根绑定，回到应用同级默认（重启后生效，幂等）。
func (s *AppService) UnbindDataDir() error {
	return settings.UnbindDataDir()
}
