package npmtool

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"hanxi/internal/modules/envcheck/detect"
	"hanxi/internal/modules/envcheck/npmregistry"
	"hanxi/internal/modules/envcheck/remoteversion"
)

// Deps 为 overview 构建的注入缝，包内测可替身（沿用 envcheck 函数字段 seam 惯例）。
type Deps struct {
	Detect  func(context.Context, string) (detect.ToolInfo, error)
	Latest  func(string) (remoteversion.Release, bool, time.Time, error)
	GlobDir func(context.Context) (string, error)
}

func buildDeps() Deps {
	return Deps{Detect: detect.RunOne, Latest: npmregistry.Latest, GlobDir: npmGlobalBinDir}
}

// BuildOverview 生产入口：并发构建目录内各工具的本机×registry 视图。
// registry 失败逐工具降级不整体报错，故 error 恒为 nil（保留签名与前端契约一致）。
func BuildOverview(ctx context.Context) (Overview, error) {
	return buildOverview(ctx, buildDeps())
}

func buildOverview(ctx context.Context, deps Deps) (Overview, error) {
	catalog := Catalog()
	tools := make([]ToolOverview, len(catalog))
	var wg sync.WaitGroup
	for i, spec := range catalog {
		wg.Go(func() { tools[i] = toolOverview(ctx, spec, deps) })
	}
	wg.Wait()
	return Overview{Tools: tools, ActiveOperation: ActiveOperation()}, nil
}

func toolOverview(ctx context.Context, spec ToolSpec, deps Deps) ToolOverview {
	overview := ToolOverview{
		Tool: ToolBrief{Command: spec.Command, Display: spec.Display, Package: spec.Package},
	}
	if local, err := deps.Detect(ctx, spec.Command); err == nil {
		overview.Local = local
	}
	installed := overview.Local.Status == detect.StatusInstalled

	latest, stale, fetchedAt, remoteErr := deps.Latest(spec.Package)
	if remoteErr != nil {
		// registry 查询失败：逐工具降级，本机状态与操作按钮仍需渲染。
		overview.LatestError = remoteErr.Error()
		overview.Relation = remoteversion.RelationUnknown
	} else {
		overview.Latest = latest
		overview.IsStale = stale
		overview.FetchedAt = formatFetchedAt(fetchedAt)
		overview.Relation = remoteversion.RelationFor(installed, overview.Local.Version, latest.Version, npmregistry.Compare)
	}

	// PATH 命中目录与 npm 全局 prefix 不一致时加安全提醒（只改 detail，不改 status）：
	// 典型为本机 ~/.local/bin 优先于 %AppData%\npm 的双拷贝场景。
	if installed && overview.Local.Path != "" && overview.RelationDetail == "" {
		if prefix, err := deps.GlobDir(ctx); err == nil && prefix != "" {
			if !sameDir(filepath.Dir(overview.Local.Path), prefix) {
				overview.RelationDetail = fmt.Sprintf(
					"当前命中的是 npm 全局目录之外的拷贝（%s）；升级/卸载仅作用于 npm 全局安装，PATH 命中结果可能不变", overview.Local.Path)
			}
		}
	}
	// 未安装且 npm 不可用：提前告知无法一键安装（nvm/Volta 劫持 prefix 亦落此）。
	if !installed && deps.GlobDir != nil {
		if _, err := deps.GlobDir(ctx); err != nil {
			overview.RelationDetail = "未检测到可用的 npm，请先安装 Node.js 后再一键安装"
		}
	}
	return overview
}

func sameDir(a, b string) bool {
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}

func formatFetchedAt(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.Local().Format("2006-01-02 15:04:05")
}
