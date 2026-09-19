// cache.go 是感知结果的 state 目录落盘：重启后首帧回灌（Restore）用的
// 轻量快照，不是第二份权威状态——Registry 仍是唯一权威源，本文件只让
// "上次确认的现态"熬过进程边界，避免重启后 health 短暂回落到 current
// 又在一轮感知后重新点亮的闪烁。
//
// 容忍口径：读失败/损坏一律视为"无缓存"（默认 current 起步，等下一轮
// 真实裁决），绝不让可选加速路径阻断装配；写失败仅告警。
package updatewatch

import (
	"log/slog"
	"path/filepath"
	"sort"
	"time"

	"hanxi/internal/jsonstore"
)

// snapshot state/updates.json 的落盘形状。
type snapshot struct {
	// CheckedAt 最后一次成功收口轮次的时刻（RFC3339，展示/排障用）。
	CheckedAt string `json:"checkedAt"`
	// Available 上轮判定为 update-available 的模块 ID 集合。
	// 仅存"有更新"一侧：current 是投影缺省值，无须占位。
	Available []string `json:"available"`
}

// cache 感知结果快照读写器。path 空串 = 禁用（单测/无状态进程）。
type cache struct {
	path string
}

func newCache(stateDir string) *cache {
	if stateDir == "" {
		return &cache{}
	}
	return &cache{path: filepath.Join(stateDir, "updates.json")}
}

// load 读取快照；文件缺失/损坏返回零值快照（ok=false 语义并入空集）。
func (c *cache) load() snapshot {
	if c.path == "" {
		return snapshot{}
	}
	var snap snapshot
	ok, err := jsonstore.Load(c.path, &snap)
	if err != nil || !ok {
		if err != nil {
			slog.Info("updatewatch: 更新感知缓存不可读，按无缓存起步", "err", err)
		}
		return snapshot{}
	}
	return snap
}

// AvailableSet 快照中的 update-available 模块集合。
func (s snapshot) AvailableSet() map[string]bool {
	out := make(map[string]bool, len(s.Available))
	for _, id := range s.Available {
		out[id] = true
	}
	return out
}

// merge 用本轮成功判定（moduleID → hasUpdate）覆盖快照对应条目后原子落盘；
// 本轮未覆盖到的模块（失败/超时跳过）保留其历史事实——失败不清信号。
func (c *cache) merge(result map[string]bool) {
	if c.path == "" {
		return
	}
	snap := c.load()
	avail := snap.AvailableSet()
	for id, hasUpdate := range result {
		if hasUpdate {
			avail[id] = true
		} else {
			delete(avail, id)
		}
	}
	snap.Available = snap.Available[:0]
	for id := range avail {
		snap.Available = append(snap.Available, id)
	}
	sort.Strings(snap.Available) // 落盘定序：map 迭代随机序不配进文件 diff
	snap.CheckedAt = time.Now().Format(time.RFC3339)
	if err := jsonstore.Save(c.path, &snap); err != nil {
		slog.Warn("updatewatch: 更新感知缓存落盘失败（不影响内存态）", "err", err)
	}
}
