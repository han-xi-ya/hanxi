// storage_usage.go 存储分区·数据根占用度量（W3/N11）：数据根每个一级子目录
// （= 每个便携软件/数据类目的磁盘占用）一屏总览。度量委托公共件
// packages/go/dirstats（预算截断、链接防御、坏条目计数），本文件只做
// 结果整形与节流缓存。独立成文件，遵循 storage_service.go 的装配分区纪律。
package app

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
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

// StorageSubUsageItem 一级子目录二次展开后的"按软件"聚合占用（W3-b）。
type StorageSubUsageItem struct {
	Name       string   `json:"name"`
	Bytes      int64    `json:"bytes"`
	Files      int64    `json:"files"`
	Partial    bool     `json:"partial"`
	ErrorCount int64    `json:"errorCount"`
	Entries    []string `json:"entries"` // 聚合进来的原始子目录名（versions 下=版本目录清单，前端 title 悬停呈现）
}

// subCache 子级度量缓存（按子目录名分桶，与 DataRootUsage 同一 TTL 纪律）。
type subCache struct {
	items []StorageSubUsageItem
	at    time.Time
	root  string // 产出时的数据根（换绑失效）
}

var subUsageCache = map[string]subCache{}

// DataRootSubUsage 展开一级子目录 sub 并按"软件"聚合占用（W3-b：versions 目录
// 的 `<模块>_<版本>` 子目录按首个下划线前缀聚合成每软件一行；无下划线者自成一
// 组，如实呈现不硬套模块分类）。sub 仅收安全单段名（拒绝分隔符与遍历，扫描面
// 锁死在数据根之内）；3 分钟缓存与 force 穿透语义同 DataRootUsage。
func (s *AppService) DataRootSubUsage(sub string, force bool) ([]StorageSubUsageItem, error) {
	usageMu.Lock()
	defer usageMu.Unlock()

	if sub == "" || sub == "." || sub == ".." || strings.ContainsAny(sub, `/\`) {
		return nil, fmt.Errorf("非法子目录名: %q", sub)
	}
	root := settings.GetPaths().DataDir()
	dir := filepath.Join(root, sub)
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return nil, fmt.Errorf("子目录不存在或不是目录（%s）", dir)
	}
	if c, ok := subUsageCache[sub]; ok && !force && c.root == root && time.Since(c.at) < storageUsageTTL {
		return c.items, nil
	}

	children, err := dirstats.MeasureChildren(dir, dirstats.Options{TimeBudget: storageUsageBudget})
	if err != nil {
		return nil, fmt.Errorf("扫描失败（%s）: %w", dir, err)
	}
	items := groupBySoftware(children)
	subUsageCache[sub] = subCache{items: items, at: time.Now(), root: root}
	return items, nil
}

// groupBySoftware 按目录名首个 "_" 前缀分组聚合，Bytes 降序（同值按名）。
func groupBySoftware(children []dirstats.Child) []StorageSubUsageItem {
	byName := map[string]*StorageSubUsageItem{}
	var order []string
	for _, c := range children {
		name := c.Name
		if c.IsDir {
			if i := strings.Index(name, "_"); i > 0 {
				name = name[:i]
			}
		}
		g, ok := byName[name]
		if !ok {
			g = &StorageSubUsageItem{Name: name}
			byName[name] = g
			order = append(order, name)
		}
		g.Bytes += c.Bytes
		g.Files += c.Files
		g.ErrorCount += c.ErrorCount
		g.Partial = g.Partial || c.Partial
		g.Entries = append(g.Entries, c.Name)
	}
	items := make([]StorageSubUsageItem, 0, len(order))
	for _, n := range order {
		items = append(items, *byName[n])
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Bytes != items[j].Bytes {
			return items[i].Bytes > items[j].Bytes
		}
		return items[i].Name < items[j].Name
	})
	return items
}
