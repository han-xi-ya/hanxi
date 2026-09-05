<script setup lang="ts">
// Everything 内嵌搜索结果区：结果计数行 + 列宽可拖拽结果表 + 三种空态提示，
// 自 EverythingView 随 DOM 逐字迁出。列宽拖拽/localStorage 记忆经 useEverythingColumns
// 内聚在本组件（存储键与钳位语义不变）；打开/定位/复制三种行操作回视图执行业务调用。
import type { Result } from '../../../bindings/hanxi/internal/modules/everything/search/models'
import { useEverythingColumns } from '../../composables/useEverythingColumns'
import { resultFullPath } from '../../composables/useEverythingSearch'
import { fmtSize } from '../../utils/format'

defineProps<{
  results: Result[]
  searched: string
  truncated: boolean
  searching: boolean
}>()

const emit = defineEmits<{
  'open': [r: Result]
  'reveal': [r: Result]
  'copy': [r: Result]
}>()

const { colWidths, totalColWidth, startResize } = useEverythingColumns()
</script>

<template>
  <div class="results-wrap">
    <div v-if="results.length" class="results-meta">
      共 {{ results.length }} 条结果（关键词「{{ searched }}」）
      <span v-if="truncated" class="warn-text">已达单次 300 条上限——请细化关键词</span>
      <span class="hint-dim">点击名称/路径单元格复制完整路径</span>
    </div>
    <div class="result-scroll" v-if="results.length">
      <table class="tbl result-tbl resizable" :style="{ minWidth: totalColWidth + 'px' }">
        <thead>
          <tr>
            <th :style="{ width: colWidths.name + 'px' }">名称<span class="col-resizer" @mousedown="startResize('name', $event)"></span></th>
            <th :style="{ width: colWidths.path + 'px' }">路径<span class="col-resizer" @mousedown="startResize('path', $event)"></span></th>
            <th :style="{ width: colWidths.size + 'px' }">大小<span class="col-resizer" @mousedown="startResize('size', $event)"></span></th>
            <th :style="{ width: colWidths.time + 'px' }">修改时间<span class="col-resizer" @mousedown="startResize('time', $event)"></span></th>
            <th :style="{ width: colWidths.action + 'px' }">操作<span class="col-resizer" @mousedown="startResize('action', $event)"></span></th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="(r, i) in results" :key="i">
            <!-- table-layout:fixed 的列宽以首行单元格为准；首行与表头同步绑定，保证列对齐 -->
            <td
              class="result-name copyable"
              :class="{ 'is-dir': r.isDir }"
              :style="i === 0 ? { width: colWidths.name + 'px' } : undefined"
              :title="`点击复制：${resultFullPath(r)}`"
              @click="emit('copy', r)"
            >
              {{ r.isDir ? '📁' : '📄' }} {{ r.name }}
            </td>
            <td
              class="mono result-path copyable"
              :style="i === 0 ? { width: colWidths.path + 'px' } : undefined"
              :title="`点击复制：${resultFullPath(r)}`"
              @click="emit('copy', r)"
            >{{ r.path }}</td>
            <td :style="i === 0 ? { width: colWidths.size + 'px' } : undefined">{{ r.isDir ? '—' : fmtSize(r.size) }}</td>
            <td class="result-time" :style="i === 0 ? { width: colWidths.time + 'px' } : undefined">{{ r.modified }}</td>
            <td :style="i === 0 ? { width: colWidths.action + 'px' } : undefined">
              <div class="row-actions">
                <button class="link-button" @click="emit('open', r)">打开</button>
                <button class="link-button" @click="emit('reveal', r)">定位</button>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
    <div v-else-if="searched && !searching" class="empty-hint">「{{ searched }}」无匹配结果</div>
    <div v-else-if="searching" class="empty-hint">搜索中…（首次搜索会自动启动后台实例）</div>
    <div v-else class="empty-hint idle-hint">输入即搜；结果可打开 / 在资源管理器中定位</div>
  </div>
</template>

<style scoped>
.results-wrap { display: flex; flex-direction: column; gap: 8px; }
.results-meta { font-size: 12px; color: var(--color-text-muted); display: flex; gap: 12px; align-items: center; }
.warn-text { color: var(--state-warning); }
.hint-dim { color: var(--color-text-subtle); }
.result-scroll { overflow-x: auto; }
.result-tbl { max-height: 428px; display: block; overflow-y: auto; }
.result-tbl thead, .result-tbl tbody { display: table; width: 100%; table-layout: fixed; }
.resizable th { position: relative; }
.col-resizer {
  position: absolute; top: 0; right: 0; width: 6px; height: 100%;
  cursor: col-resize; transition: background 0.1s ease;
}
.col-resizer:hover { background: color-mix(in srgb, var(--color-primary) 35%, transparent); }
.result-tbl td { font-size: 12px; }
.result-name { font-weight: 500; color: var(--color-text); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.result-name.is-dir { color: var(--color-primary); }
.result-path { font-size: 11px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; display: block; color: var(--color-text-subtle); }
.result-time { font-size: 11px; color: var(--color-text-subtle); white-space: nowrap; }
.row-actions { display: flex; gap: 4px; }
.copyable { cursor: pointer; }
.copyable:hover { background: var(--surface-hover); }
.empty-hint { text-align: center; padding: 20px; color: var(--color-text-subtle); font-size: 13px; background: var(--surface-panel); border-radius: 6px; border: 1px dashed var(--color-border); }
.idle-hint { color: var(--color-text-subtle); }
</style>
