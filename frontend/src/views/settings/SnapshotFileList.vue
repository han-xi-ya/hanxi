<script setup lang="ts">
// 历史版本·左栏受保文件清单（N33 批 B 自 SnapshotSection 拆出，行为零回退）：
// 按 memo/config/state 三分组渲染 ListFiles 结果（组序信任后端），每行
// 中文名（前端映射表优先→后端 Display→文件名回落，见 snapshotLabels）、
// 已删除徽标与观察窗内版本数；点选向父级抛 select。
import { computed } from 'vue'
import type { TrackedFile } from '../../../bindings/hanxi/internal/snapshot/models'
import { fileDisplay, groupLabels } from '../../constants/snapshotLabels'

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
        <span class="fa-count mono">{{ f.revisions || '' }}</span>
      </button>
    </template>
  </div>
</template>

<style scoped>
/* 皮与 SnapshotSection 批 A 语系逐字同构（拆分不动相） */
.fa-list {
  flex: none; width: 236px; max-height: 380px; overflow: auto;
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
.fa-count { flex: none; color: var(--color-text-subtle); }
.mono { font-family: var(--font-mono); font-size: var(--text-xs); }
@media (max-width: 720px) {
  .fa-list {
    width: auto; max-height: 220px; border-right: none; padding-right: 0;
    border-bottom: 1px solid var(--color-border); padding-bottom: 6px;
  }
}
</style>
