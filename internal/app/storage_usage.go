// storage_usage.go 存储分区·数据根占用度量（W3/N11）：数据根每个一级子目录
// （= 每个便携软件/数据类目的磁盘占用）一屏总览。度量委托公共件
// packages/go/dirstats（预算截断、链接防御、坏条目计数），本文件只做
// 结果整形与节流缓存。独立成文件，遵循 storage_service.go 的装配分区纪律。
package app

import (
	"fmt"
	"sync"
	"time"

	"hanxi/internal/settings"
	"hanxi/packages/go/dirstats"
)

// StorageUsageItem 一级子项占用（Partial=true 时 Bytes 为下限估算）。
type StorageUsageItem struct {
	Name       string `json:"name"`
	IsDir      bool   `json:"isDir"`
	Bytes      int64  `json:"bytes"`
	Files      int64  `json:"files"`
	Partial    bool   `json:"partial"`
	ErrorCount int64  `json:"errorCount"`
}

// 度量节流参数：15s 挂钟预算保证巨目录（Everything 索引/快照库）不拖死
// RPC；3 分钟结果缓存让"进页面即看"零成本，手动刷新可穿透（force）。
const (
	storageUsageBudget = 15 * time.Second
	storageUsageTTL    = 3 * time.Minute
)

var (
	usageMu      sync.Mutex
	usageCache   []StorageUsageItem
	usageCacheAt time.Time
	usageRoot    string
)

// DataRootUsage 度量数据根一级子项占用（按 Bytes 降序）。force=false 时
// 优先回 3 分钟内的缓存（数据根未变）；跨会话换绑自动失效（以根路径为键）。
// 部分子项超时截断不算错误：Partial 逐行透出，UI 负责"≥ 此值"话术。
func (s *AppService) DataRootUsage(force bool) ([]StorageUsageItem, error) {
	usageMu.Lock()
	defer usageMu.Unlock()

	root := settings.GetPaths().DataDir()
	if !force && usageCache != nil && root == usageRoot && time.Since(usageCacheAt) < storageUsageTTL {
		return usageCache, nil
	}

	children, err := dirstats.MeasureChildren(root, dirstats.Options{TimeBudget: storageUsageBudget})
	if err != nil {
		return nil, fmt.Errorf("扫描数据根失败（%s）: %w", root, err)
	}
	items := make([]StorageUsageItem, 0, len(children))
	for _, c := range children {
		items = append(items, StorageUsageItem{
			Name: c.Name, IsDir: c.IsDir, Bytes: c.Bytes, Files: c.Files,
			Partial: c.Partial, ErrorCount: c.ErrorCount,
		})
	}
	usageCache, usageCacheAt, usageRoot = items, time.Now(), root
	return items, nil
}
