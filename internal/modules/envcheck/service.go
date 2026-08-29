package envcheck

import (
	"context"

	"hanxi/internal/modules/envcheck/detect"
)

// EnvCheckService Wails 绑定服务：开发工具链版本探测。
// 无状态：无 store、无事件、无缓存，每次调用实时探测（全量并发 ≤5s）；
// 失败落在工具级 status 上，不整体报错。
type EnvCheckService struct{}

func NewEnvCheckService() *EnvCheckService {
	return &EnvCheckService{}
}

// DetectAll 并发探测全部已注册工具，同步返回完整列表（前端主入口）。
func (s *EnvCheckService) DetectAll() []detect.ToolInfo {
	return detect.RunAll(context.Background())
}

// Detect 按注册名探测单个工具（预留单卡刷新扩展），未知名返回错误。
func (s *EnvCheckService) Detect(name string) (detect.ToolInfo, error) {
	return detect.RunOne(context.Background(), name)
}
