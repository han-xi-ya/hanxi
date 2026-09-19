<script setup lang="ts">
// MarkerOn 控制台（Wave 5 · 批 0 共享契约迁移，#primary-action 槽首个实战件）：
// 业务投影全部收进 src/adapters/markeron（RPC/事件/六态矩阵/文案），
// ManagedConsoleShell 管页头页签骨架，ManagedControlBar 管状态头与启停钮区，
// 标注开关钮体经 #primary-action 槽自绘——钮序按 Shell 约定：
// 声明主钮（本模块空置）→ 槽内 toggle → 退出钮「⏻ 停止」恒居末位。
// 状态轮询/uptime/下载进度 map/busy 闩等编排由 store 单源；切换动作引发的
// 状态刷新由后端 instance-state 事件即时回推 + 2.5s 轮询等价承接。
import ManagedConsoleShell from '../components/managed/ManagedConsoleShell.vue'
import type { ManagedSnapshot } from '../components/managed/adapter'
import { annotateToggleView, createMarkerOnAdapter } from '../adapters/markeron'
import { useToast } from '../composables/useToast'
import { getErrorMessage } from '../utils/errors'

const adapter = createMarkerOnAdapter()

const { showToast } = useToast()

/** 快照业务扩展：drawing 经 ManagedSnapshot 可选字段读出（共享件不感知业务键）。 */
function drawingOf(snap: ManagedSnapshot | null): boolean {
  return !!(snap as { drawing?: boolean } | null)?.drawing
}

/** 标注开关点击：动词经 adapter.toggle 槽注入；失败 toast 保持裸串（原文案口径）。 */
async function onToggle(input: { state: string; drawing: boolean; busy: boolean; installedCount: number }) {
  if (annotateToggleView(input).disabled) return
  try {
    const res = await adapter.toggle?.run()
    if (res?.message !== undefined) showToast(res.message)
  } catch (e) {
    showToast(getErrorMessage(e))
  }
}
</script>

<template>
  <ManagedConsoleShell
    class="markeron-view"
    :adapter="adapter"
    title="MarkerOn 桌面标注"
    subtitle="一键进入桌面标注态；版本与运行状态统一管理。"
    console-tab-key="annotate"
    console-tab-label="✎ 标注开关"
  >
    <!-- 六态标注开关钮（#primary-action 槽注入位；矩阵算法在 adapters/markeron） -->
    <template #primary-action="{ state, busy, installedCount, snap }">
      <button
        class="btn btn-small annotate-toggle"
        :class="annotateToggleView({ state, drawing: drawingOf(snap), busy, installedCount }).variant"
        :disabled="annotateToggleView({ state, drawing: drawingOf(snap), busy, installedCount }).disabled"
        :title="annotateToggleView({ state, drawing: drawingOf(snap), busy, installedCount }).title"
        @click="onToggle({ state, drawing: drawingOf(snap), busy, installedCount })"
      >✎ {{ annotateToggleView({ state, drawing: drawingOf(snap), busy, installedCount }).label }}</button>
    </template>

    <!-- 控制台 Tab 主体：状态说明六行（含与 banner 并存的原形态）+ 快捷键说明卡 -->
    <template #default="{ state, snap }">
      <div class="control-detail">
        <span v-if="state === 'running' && !drawingOf(snap)">MarkerOn 正在后台待命，点击「开启标注」显示桌面覆盖层。</span>
        <span v-else-if="state === 'running' && drawingOf(snap)">桌面覆盖层已开启，可直接进行屏幕标注。</span>
        <span v-else-if="state === 'stopped'">点击「启动 MarkerOn」后台运行，随后可开启桌面标注。</span>
        <span v-else-if="state === 'starting'">正在拉起 MarkerOn 主实例（约 1~3 秒）…</span>
        <span v-else-if="state === 'failed'">请确认已安装 WebView2 Runtime 后重试。</span>
        <span v-else-if="state === 'external'">非 Hanxi 托管的 MarkerOn 实例正在运行。</span>
      </div>

      <details class="info-details">
        <summary class="info-summary">快捷键与使用说明</summary>
        <div class="info-body">
          <div class="kbd-row">
            <span class="kbd-chip">Ctrl+Shift+D</span> 切换标注
            <span class="kbd-chip">Ctrl+Shift+C</span> 清空涂鸦
            <span class="kbd-chip">Ctrl+Shift+X</span> 穿透点击
          </div>
          <p class="hint-dim">按钮与快捷键等效；若状态与桌面实际不符，重新切换一次即可同步。</p>
        </div>
      </details>
    </template>
  </ManagedConsoleShell>
</template>

<style scoped>
/* 页头/控制条/提示条/版本区/联动卡/页签与 flex 骨架全部由 managed 组件 +
   components.css 全局原子接管；本页仅余标注开关钮的五档皮与说明行私有形 */

/* ---------- 标注开关六态变体（#primary-action 槽钮体；全局原子不含本家族） ---------- */
.annotate-toggle { min-width: 108px; }
.btn-toggle-primary, .btn-toggle-active { background: var(--color-primary); border-color: var(--color-primary); color: var(--color-on-primary); }
.btn-toggle-primary:hover:not(:disabled), .btn-toggle-active:hover:not(:disabled) { background: var(--color-primary-hover); }
.btn-toggle-outline { background: var(--surface-panel); border-color: var(--color-primary); color: var(--color-primary); }
.btn-toggle-outline:hover:not(:disabled) { background: var(--surface-selected); }
.btn-toggle-warn { background: var(--state-warning-soft); border-color: var(--state-warning); color: var(--state-warning); }
.btn-toggle-danger { background: var(--surface-panel); border-color: var(--state-danger); color: var(--state-danger); }

/* ---------- 状态说明行（原 control-bar 内联位，现渲染于状态头之后） ---------- */
.control-detail { font-size: var(--text-sm); color: var(--color-text-subtle); }
/* info-* 折叠卡家族与 hint-dim 由 components.css 全局原子接管 */
.kbd-row { font-size: var(--text-sm); color: var(--color-text-muted); display: flex; align-items: center; gap: 6px; flex-wrap: wrap; }
.kbd-chip { font-family: var(--font-mono); font-size: var(--text-xs); background: var(--surface-panel); border: 1px solid var(--color-border); border-radius: 4px; padding: 2px 8px; color: var(--color-text); }
</style>
