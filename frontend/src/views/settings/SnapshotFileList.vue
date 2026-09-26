<script setup lang="ts">
// 历史版本·左栏受保文件清单（N33 批 B 自 SnapshotSection 拆出，批 C 补徽标与窄屏）：
// 按 memo/config/state 三分组渲染 ListFiles 结果（组序信任后端），每行
// 中文名（前端映射表优先→后端 Display→文件名回落，见 snapshotLabels）、
// 已删除徽标、恢复生效语义常驻徽标（§0.3-C：memo=即时、config/state=重启）
// 与"最近 N 次变化"窗内口径整句；点选向父级抛 select。
// DOM 刻意保持「组标签 + 行按钮」同层平铺（无分组包裹盒）：≤640px 纯 CSS
// 即可把同一容器折成横向滚动 chip 条，恢复入口保持一步可达。
import { computed } from 'vue'
import type { TrackedFile } from '../../../bindings/hanxi/internal/snapshot/models'
import { fileCountNote, fileCountTitle, fileDisplay, groupLabels, restoreScopeFor, restoreScopeLabels } from '../../constants/snapshotLabels'

const countTitle = fileCountTitle()

/** 行级恢复生效徽标数据（白名单外路径 null → 不出徽标，不猜语义）。 */
function scopeBadge(f: TrackedFile) {
  const scope = restoreScopeFor(f.path)
  return scope ? restoreScopeLabels[scope] : null
}

const props = defineProps<{
  files: TrackedFile[]
  selectedPath: string
}>()

const emit = defineEmits<{
  (e: 'select', path: string): void
}>()

const grouped = computed(() => {
  const out: { group: string; label: string; items: TrackedFile[] }[] = []
  for (const f of props.files) {
    let g = out.find((o) => o.group === f.group)
    if (!g) {
      g = { group: f.group, label: groupLabels[f.group] ?? f.group, items: [] }
      out.push(g)
    }
    g.items.push(f)
  }
  return out
})
</script>

<template>
  <div class="fa-list">
    <template v-for="g in grouped" :key="g.group">
      <div class="fa-group">{{ g.label }}</div>
      <button
        v-for="f in g.items"
        :key="f.path"
        class="fa-file"
        :class="{ active: f.path === selectedPath }"
        @click="emit('select', f.path)"
      >
        <span class="fa-name" :title="f.path">{{ fileDisplay(f) }}</span>
        <span v-if="!f.alive" class="chip chip-warning fa-dead">已删除</span>
        <span v-if="scopeBadge(f)" class="chip fa-scope" :class="scopeBadge(f)!.chip">{{ scopeBadge(f)!.text }}</span>
        <span class="fa-count" :title="countTitle">{{ fileCountNote(f.revisions) }}</span>
      </button>
    </template>
  </div>
</template>

<style scoped>
/* 皮与 SnapshotSection 批 A 语系逐字同构（拆分不动相）；批 C：行宽按整句+双徽标
   重排，≤640px 同一容器折成横向滚动 chip 条（平铺 DOM 是前提，勿加包裹盒） */
.fa-list {
  flex: none; width: 300px; max-height: 380px; overflow: auto;
  display: flex; flex-direction: column; gap: 2px;
  border-right: 1px solid var(--color-border); padding-right: 10px;
}
.fa-group {
  font-size: var(--text-xs); color: var(--color-text-subtle);
  padding: 8px 8px 2px; letter-spacing: 0.04em;
}
.fa-file {
  display: flex; align-items: center; gap: 6px; width: 100%;
  padding: 5px 8px; border: 1px solid transparent; border-radius: var(--radius-control);
  background: transparent; color: var(--color-text); font-size: var(--text-sm);
  text-align: left; cursor: pointer; font-family: inherit;
}
.fa-file:hover { background: var(--surface-hover); }
.fa-file.active { background: var(--surface-hover); border-color: var(--color-border); }
.fa-name { flex: 1; min-width: 0; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.fa-dead { flex: none; font-size: var(--text-xs); padding: 0 5px; }
/* 恢复生效常驻徽标：micro chip（.fa-st 语系的迷你档），色走全局 chip token */
.fa-scope { flex: none; font-size: var(--text-xs); padding: 0 5px; }
.fa-count { flex: none; font-size: var(--text-xs); color: var(--color-text-subtle); white-space: nowrap; }
@media (max-width: 720px) {
  .fa-list {
    width: auto; max-height: 220px; border-right: none; padding-right: 0;
    border-bottom: 1px solid var(--color-border); padding-bottom: 6px;
  }
}
/* 批 C 窄屏档：清单收成横向滚动 chip 条——恢复入口保持"点一下 chip 即出时间线"，
   不折叠进 details（折叠会把入口埋回两步深，违背重做初衷） */
@media (max-width: 640px) {
  .fa-list {
    flex-direction: row; align-items: center; gap: 6px;
    width: auto; max-height: none; overflow-x: auto; overflow-y: hidden;
    padding-bottom: 8px;
  }
  .fa-group { flex: none; align-self: center; padding: 0 2px 0 6px; white-space: nowrap; }
  .fa-file {
    flex: none; width: auto; padding: 4px 10px;
    border-color: var(--color-border); border-radius: var(--radius-pill);
  }
  .fa-file.active { border-color: var(--color-primary); }
  .fa-name { flex: none; max-width: 34vw; }
}
</style>
