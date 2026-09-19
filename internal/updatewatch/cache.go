// cache.go 是感知结果的 state 目录落盘：重启后首帧回灌（Restore）用的
// 轻量快照，不是第二份权威状态——Registry 仍是唯一权威源，本文件只让
// "上次确认的现态"熬过进程边界，避免重启后 health 短暂回落到 current
// 又在一轮感知后重新点亮的闪烁。
//
// 容忍口径：读失败/损坏一律视为"无缓存"（默认 current 起步，等下一轮
// 真实裁决），绝不让可选加速路径阻断装配；写失败仅告警。旧版本快照的
// Available 是 []string（只记"有更新"不记版本），读到该形状时按"无版本"
// 迁移一次继续用，不炸不丢信号。
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
	// Available 上轮判定为 update-available 的模块集合：moduleID → 上游新版本
	//（value 可为空串 = 旧格式迁移或感知时未拿到版本号，存在即 update-available）。
	// 仅存"有更新"一侧：current 是投影缺省值，无须占位。
	Available map[string]string `json:"available"`
}

// verdict 单模块本轮感知的成功结论：hasUpdate 决定条目进出，remote 是展示用
// 上游新版本（仅 hasUpdate=true 时有意义）。
type verdict struct {
	hasUpdate bool
	remote    string
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
// 新形状解析失败时先按旧 []string 形状迁移一次（历史版本落盘格式，版本记空），
// 仍失败才按无缓存起步。
func (c *cache) load() snapshot {
	if c.path == "" {
		return snapshot{}
	}
	var snap snapshot
	ok, err := jsonstore.Load(c.path, &snap)
	if err != nil {
		if legacy, lok := c.loadLegacy(); lok {
			return legacy
		}
		slog.Info("updatewatch: 更新感知缓存不可读，按无缓存起步", "err", err)
		return snapshot{}
	}
	if !ok {
		return snapshot{}
	}
	return snap
}

// loadLegacy 按旧落盘形状（Available []string，无版本号）读取并迁移为
// "存在即 update-available、版本留空"。返回 false 表示也不是旧形状。
func (c *cache) loadLegacy() (snapshot, bool) {
	var legacy struct {
		CheckedAt string   `json:"checkedAt"`
		Available []string `json:"available"`
	}
	ok, err := jsonstore.Load(c.path, &legacy)
	if err != nil || !ok {
		return snapshot{}, false
	}
	snap := snapshot{CheckedAt: legacy.CheckedAt, Available: make(map[string]string, len(legacy.Available))}
	for _, id := range legacy.Available {
		snap.Available[id] = ""
	}
	return snap, true
}

// AvailableSet 快照中的 update-available 模块集合（moduleID → 上游新版本，
// 版本可为空串）。返回副本供 merge 增删，不回写快照本体。
func (s snapshot) AvailableSet() map[string]string {
	out := make(map[string]string, len(s.Available))
	for id, remote := range s.Available {
		out[id] = remote
	}
	return out
}

// merge 用本轮成功判定（moduleID → verdict）覆盖快照对应条目后原子落盘；
// 本轮未覆盖到的模块（失败/超时跳过）保留其历史事实——失败不清信号。
func (c *cache) merge(result map[string]verdict) {
	if c.path == "" {
		return
	}
	snap := c.load()
	avail := snap.AvailableSet()
	for id, v := range result {
		if v.hasUpdate {
			avail[id] = v.remote
		} else {
			delete(avail, id)
		}
	}
	// 落盘定序：map 迭代随机序不配进文件 diff，按 key 排序重建后再写。
	ids := make([]string, 0, len(avail))
	for id := range avail {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	snap.Available = make(map[string]string, len(ids))
	for _, id := range ids {
		snap.Available[id] = avail[id]
	}
	snap.CheckedAt = time.Now().Format(time.RFC3339)
	if err := jsonstore.Save(c.path, &snap); err != nil {
		slog.Warn("updatewatch: 更新感知缓存落盘失败（不影响内存态）", "err", err)
	}
}
