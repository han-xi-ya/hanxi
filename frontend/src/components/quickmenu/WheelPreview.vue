<script setup lang="ts">
// 轮盘实时预览（N6-C2）：把当前条目列表画成一张静态小盘，配置页内嵌——
// 勾选/排序即时反映到盘面，结清"改完看不到盘、全靠想象"。
//
// 与真轮盘（QuickMenuPopup）同源的是**几何**：半径、扇区角、锚点全部走
// wheelGeometry 纯函数——预览与实盘布局一致，不会"预览一个样、挂出另一个样"。
// 刻意剥离的是**交互与状态机**（dwell/迟滞/子环/键盘导航/派发）：预览是只读
// 示意图，不接管鼠标动作，也就不复制那套时序复杂度（两套几何同源、交互单份）。
//
// 纯 SVG + 定位浮层，零组件外依赖；scale 缩放整盘（viewBox 天然支持，配置页
// 用 0.62 缩进侧栏）。点击扇区经 emit('pick', i) 上抛供 C3「点上格定位/互换」，
// 但预览自身不做任何副作用。
import { computed } from 'vue'
import type { MenuItem } from '../../../bindings/hanxi/internal/modules/quickmenu/models'
import AppIcon from '../ui/AppIcon.vue'
import { appIconName } from '../../constants/appIcons'
import type { IconName, RenderableIcon } from '../../constants/icons'
import { WHEEL, wedgePath, mainSectorAngles, mainAnchor } from './wheelGeometry'

const props = withDefaults(defineProps<{
  items: MenuItem[]
  /** 高亮的扇区下标（null=无；C3 双向定位时由父层喂入） */
  activeIndex?: number | null
  scale?: number
  size?: number
}>(), { activeIndex: null, scale: 1, size: WHEEL.size })

const emit = defineEmits<{ pick: [index: number] }>()

const TYPE_ICON: Record<string, IconName> = { exe: 'box', command: 'terminal', route: 'layout', group: 'layers' }
// N27 批 B：app: 真图标与登记矢量名二轨；漂移回落类型图标（与轮盘弹窗同口径）
const iconOf = (item: MenuItem): RenderableIcon =>
  appIconName(item.icon) ?? TYPE_ICON[item.type] ?? 'box'
const isGroup = (item: MenuItem): boolean => (item.children?.length ?? 0) > 0

const n = computed(() => props.items.length)
const wedge = (i: number) => {
  const a = mainSectorAngles(i, n.value)
  return wedgePath(WHEEL.rSecIn, WHEEL.rSecOut, a.a0, a.a1)
}
const hubVars = computed(() => ({ '--wp-scale': String(props.scale) }))
</script>

<template>
  <div class="wp" :style="hubVars" role="img" aria-label="轮盘预览">
    <svg :viewBox="`0 0 ${size} ${size}`" class="wp-svg">
      <circle :cx="size / 2" :cy="size / 2" :r="WHEEL.rDisc" class="wp-face" />
      <circle :cx="size / 2" :cy="size / 2" :r="WHEEL.rDisc - 1.25" class="wp-edge" />
      <g v-if="n === 0">
        <circle :cx="size / 2" :cy="size / 2" :r="(WHEEL.rSecIn + WHEEL.rSecOut) / 2" class="wp-ghost" />
      </g>
      <path
        v-for="(item, i) in items"
        :key="`wp-${i}-${item.index}`"
        class="wp-sector"
        :class="{ 'is-active': activeIndex === i }"
        :d="wedge(i)"
        @click="emit('pick', i)"
      />
      <circle :cx="size / 2" :cy="size / 2" :r="WHEEL.rHub" class="wp-hub" />
    </svg>
    <button
      v-for="(item, i) in items"
      :key="`wpb-${i}-${item.index}`"
      type="button"
      class="wp-slot"
      :class="{ 'is-active': activeIndex === i }"
      :style="mainAnchor(i, n)"
      :aria-label="`扇区 ${i + 1}：${item.label}`"
      tabindex="-1"
      @click="emit('pick', i)"
    >
      <span class="wp-ico"><AppIcon :name="iconOf(item)" :size="16" /></span>
      <span class="wp-name">{{ item.label }}</span>
      <span v-if="isGroup(item)" class="wp-caret">▸</span>
    </button>
    <div v-if="n === 0" class="wp-empty">还没有条目</div>
  </div>
</template>

<style scoped>
/* 预览皮与真轮盘同族（玻璃底盘 + 发丝辐线 + primary 高亮），尺寸缩略；
   颜色全 token，明暗自适应。pointer-events 仅扇区/槽位层，盘底不拦。 */
.wp { position: relative; width: calc(320px * var(--wp-scale, 1)); aspect-ratio: 1; }
.wp-svg { position: absolute; inset: 0; width: 100%; height: 100%; pointer-events: none; }
.wp-face { fill: var(--surface-panel); }
.wp-edge { fill: none; stroke: var(--color-border-strong); stroke-width: 1.5; }
.wp-ghost { fill: none; stroke: var(--color-border-strong); stroke-width: 1.5; stroke-dasharray: 2 7; stroke-linecap: round; }
.wp-sector { pointer-events: auto; cursor: pointer; fill: transparent; stroke: var(--color-border); stroke-width: 1; transition: fill var(--motion-fast) ease, stroke var(--motion-fast) ease; }
.wp-sector:hover { fill: var(--surface-hover); }
.wp-sector.is-active { fill: var(--color-primary-soft); stroke: var(--color-primary); }
.wp-hub { fill: var(--surface-panel); stroke: var(--color-border-strong); stroke-width: 1; }
.wp-slot {
  position: absolute; transform: translate(-50%, -50%);
  width: calc(46px * var(--wp-scale, 1));
  display: flex; flex-direction: column; align-items: center; gap: 1px;
  border: none; background: transparent; padding: 0;
  color: var(--color-text-muted); cursor: pointer;
}
.wp-slot.is-active { color: var(--color-primary); }
.wp-ico { display: flex; align-items: center; justify-content: center; }
.wp-name {
  max-width: 100%; font-size: calc(9px * var(--wp-scale, 1)); font-weight: 600; line-height: 1.25;
  color: var(--color-text); text-align: center;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.wp-slot.is-active .wp-name { color: var(--color-primary); }
.wp-caret { position: absolute; top: -3px; right: 2px; font-size: 8px; color: var(--color-primary); }
.wp-empty {
  position: absolute; inset: 0; display: flex; align-items: center; justify-content: center;
  font-size: var(--text-sm); color: var(--color-text-subtle); pointer-events: none;
}
</style>
