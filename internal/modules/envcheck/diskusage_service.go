// diskusage_service.go envcheck 空间家底 RPC 面（W3/N14）：
// "装了什么"（DetectAll）之外回答"各占多大、缓存肥不肥"——本体安装目录 +
// 依赖/缓存目录（Go 模块缓存/构建缓存、npm 缓存与全局包、pnpm 内容库、
// pip、Maven/Gradle、NuGet）逐一度量。推导与预算细节见 diskusage 子包。
//
// 独立成文件避免触碰 service.go 装配 hunk（并行开发纪律）；采集函数经字段
// 注入（单测替换），与既有版本源注入风格一致。
package envcheck

import (
	"context"
	"time"

	"hanxi/internal/modules/envcheck/detect"
	"hanxi/internal/modules/envcheck/diskusage"
)

// diskUsageScanBudget 整场扫描预算（RPC 级）：目录推导命令各有 5s 子超时，
// 度量阶段共享此预算，超时目录以 Partial"≥ 下限"呈现，绝不无限期挂住前端。
const diskUsageScanBudget = 30 * time.Second

// GetDiskUsage 重新探测本机工具链并按家底清单度量。
// 无耗时保护诉求的手动动作（前端按钮触发），不做服务端缓存；
// 结果按工具 → 目录逐行返回，Exists=false 的行由前端决定灰列或隐藏。
func (s *EnvCheckService) GetDiskUsage() ([]diskusage.ToolUsage, error) {
	release, gateErr := s.holder.Enter()
	if gateErr != nil {
		return nil, gateErr
	}
	defer release()

	ctx, cancel := context.WithTimeout(context.Background(), diskUsageScanBudget)
	defer cancel()
	usage := s.collectUsage(ctx, detect.RunAll(ctx))
	if usage == nil {
		return []diskusage.ToolUsage{}, nil // 空家底走空数组（前端"无可统计"文案），不携 nil 出绑定面
	}
	return usage, nil
}
