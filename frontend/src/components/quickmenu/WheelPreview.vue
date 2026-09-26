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
//
// "预览与实盘同皮"是皮肤批起的新契约：盘面配色/描边色轨/图标像素预算与
// QuickMenuPopup 吃同一套 wheelSkin/wheelIconBudget 口径（父层喂 skin prop），
// 设置页拨皮肤即时见皮，不再"设置页一个样、挂出另一个样"。
import { computed, watch } from 'vue'
import type { MenuItem } from '../../../bindings/hanxi/internal/modules/quickmenu/models'
import AppIcon from '../ui/AppIcon.vue'
import { wheelIconOf } from './wheelIcons'
import {
  WHEEL, VIS, wedgePath, mainSectorAngles, mainAnchor,
  fullViewBox, trimViewBox, toTrimWindowPercent, PREVIEW_ICON,
} from './wheelGeometry'
import { isRasterWheelIcon, snapIconCssPx } from './wheelIconBudget'
import { useWheelDpr } from './useWheelDpr'
import { DEFAULT_WHEEL_SKIN, wheelSkinVars, wheelTintCss, type WheelSkin } from './wheelSkin'
import { wheelTypeTint } from './wheelSkinColors'
import { useWheelTints } from './useWheelTints'

const props = withDefaults(defineProps<{
  items: MenuItem[]
  /** 高亮的扇区下标（null=无；C3 双向定位时由父层喂入） */
  activeIndex?: number | null
  scale?: number
  size?: number
  /** 皮肤账（缺省 = 默认素瓷，与实盘未配置时同解） */
  skin?: WheelSkin
  /** v3 紧凑档裁切窗（缺省 false=全尺寸窗，既有消费位零扰动）：viewBox 收进
   * 盘面（含描边墨迹）+ 安全边，整盘满格落盒；槽位锚点随窗折算。 */
  trim?: boolean
}>(), { activeIndex: null, scale: 1, size: WHEEL.size, skin: () => DEFAULT_WHEEL_SKIN, trim: false })

// trim 窗数学全部由 wheelGeometry 共享件推导（墨迹半经 + 安全边 → 窗位），
// 与 v3 手量常量 [82, 430] 逐位相等，contract spec 锁死零观感漂移。
const viewBox = computed(() => (props.trim ? trimViewBox(props.size) : fullViewBox(props.size)))
/** 槽位锚点：几何仍走 mainAnchor 同源纯函数，trim 窗只换映射基准（全窗 % → 窗内 %） */
function slotStyle(i: number, n: number): { left: string; top: string } {
  const a = mainAnchor(i, n)
  if (!props.trim) return a
  return { left: toTrimWindowPercent(a.left, props.size), top: toTrimWindowPercent(a.top, props.size) }
}

const emit = defineEmits<{ pick: [index: number] }>()
const isGroup = (item: MenuItem): boolean => (item.children?.length ?? 0) > 0

const n = computed(() => props.items.length)
const wedge = (i: number) => {
  // 审查 #9：预览绘制喂 VIS 观感常量（花瓣缝/收窄环带）——与真盘 N40③
  // 皮批观感同源；命中语义常量 WHEEL 不参与（预览无命中，纪律同指针侧）。
  const a = mainSectorAngles(i, n.value, VIS.padDeg)
  return wedgePath(VIS.rSecIn, VIS.rSecOut, a.a0, a.a1)
}
const hubVars = computed(() => ({ '--wp-scale': String(props.scale), ...wheelSkinVars(props.skin) }))
const rootClass = computed(() => `skin-${props.skin.preset}`)

// 图标像素预算 + 模块色描边：与实盘同一套纯函数/取色钩子（同皮契约）
const dpr = useWheelDpr()
function iconPx(item: MenuItem): number {
  const base = PREVIEW_ICON
  return isRasterWheelIcon(wheelIconOf(item)) ? snapIconCssPx(base, dpr.value || 1) : base
}
const { tints, refresh: refreshTints } = useWheelTints(
  () => props.items.map(wheelIconOf),
  () => props.skin.followModuleColor,
)
watch([() => props.items, () => props.skin.followModuleColor], refreshTints)
function sectorTint(item: MenuItem): string | undefined {
  if (!props.skin.followModuleColor) return undefined
  const dom = tints.value[wheelIconOf(item)]
  return dom ? wheelTintCss(dom, props.skin.faceAlpha) : undefined
}
const typeClass = (item: MenuItem) => `t-${wheelTypeTint(item.type)}`
</script>

<template>
  <div class="wp" :class="[rootClass, { 'wp-trim': trim }]" :style="hubVars" role="img" aria-label="轮盘预览">
    <svg :viewBox="viewBox" class="wp-svg">
      <defs>
        <radialGradient id="wp-face-grad" cx="50%" cy="38%" r="80%">
          <stop offset="0%" class="wp-face-hi" />
          <stop offset="100%" class="wp-face-lo" />
        </radialGradient>
      </defs>
      <circle :cx="size / 2" :cy="size / 2" :r="WHEEL.rDisc" class="wp-face" />
      <circle :cx="size / 2" :cy="size / 2" :r="WHEEL.rDisc - 1.25" class="wp-edge" />
      <g v-if="n === 0">
        <circle :cx="size / 2" :cy="size / 2" :r="(WHEEL.rSecIn + WHEEL.rSecOut) / 2" class="wp-ghost" />
      </g>
      <path
        v-for="(item, i) in items"
        :key="`wp-${i}-${item.index}`"
        class="wp-sector"
        :class="[{ 'is-active': activeIndex === i }, typeClass(item)]"
        :style="sectorTint(item) ? { '--tint': sectorTint(item) } : {}"
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
      :style="slotStyle(i, n)"
      :title="item.label"
      :aria-label="`扇区 ${i + 1}：${item.label}`"
      tabindex="-1"
      @click="emit('pick', i)"
    >
      <span class="wp-ico"><AppIcon :name="wheelIconOf(item)" :size="iconPx(item)" /></span>
      <span class="wp-name">{{ item.label }}</span>
      <span v-if="isGroup(item)" class="wp-caret">▸</span>
    </button>
    <div v-if="n === 0" class="wp-empty">还没有条目</div>
  </div>
</template>

<style scoped>
/* 预览皮与真轮盘同族（玻璃底盘 + 发丝辐线 + primary 高亮），尺寸缩略；
   颜色全 token，明暗自适应。pointer-events 仅扇区/槽位层，盘底不拦。
   皮肤批：预设色轨/透明度/描边强度与 QuickMenuPopup 同一批 CSS 变量口径，
   配置页切皮肤即时反映（同皮契约）。 */
/* 320 是预览在设置页的显示宽（CSS px），与弹窗 512 DIP 窗宽无关——viewBox
   已按 WHEEL.size 定基准，这里只是把那张 512 基准的画布缩进侧栏，非同源量勿对账。 */
.wp { position: relative; width: calc(320px * var(--wp-scale, 1)); aspect-ratio: 1; }
/* 皮肤预设配方（与弹窗 .popup.skin-* 一一对应，只覆写呈现变量） */
.wp { --wf-hi: var(--surface-panel); --wf-lo: color-mix(in srgb, var(--color-text) 6%, var(--surface-panel)); }
.wp.skin-veil {
  --wf-hi: color-mix(in srgb, var(--color-primary) 8%, var(--surface-panel));
  --wf-lo: color-mix(in srgb, var(--color-primary) 16%, var(--surface-panel));
}
.wp.skin-ink {
  --wf-hi: color-mix(in srgb, var(--color-text) 10%, var(--surface-panel));
  --wf-lo: color-mix(in srgb, var(--color-text) 20%, var(--surface-panel));
}
.wp-svg { position: absolute; inset: 0; width: 100%; height: 100%; pointer-events: none; }
.wp-face { fill: url(#wp-face-grad); opacity: var(--wf-face-a, 1); }
.wp-face-hi { stop-color: var(--wf-hi); }
.wp-face-lo { stop-color: var(--wf-lo); }
.wp-edge { fill: none; stroke: var(--color-border-strong); stroke-width: 1.5; }
.wp-ghost { fill: none; stroke: var(--color-border-strong); stroke-width: 1.5; stroke-dasharray: 2 7; stroke-linecap: round; }
/* 描边色轨：--tint（模块色）> --type-c（类型 token）> border 兜底，混入比例吃 --wf-edge */
.wp-sector.t-exe { --type-c: var(--color-primary); }
.wp-sector.t-command { --type-c: var(--state-information); }
.wp-sector.t-route { --type-c: var(--state-positive); }
.wp-sector.t-group { --type-c: var(--state-warning); }
.wp-sector { pointer-events: auto; cursor: pointer; fill: transparent; stroke: color-mix(in srgb, var(--tint, var(--type-c, var(--color-border))) var(--wf-edge, 38%), var(--color-border)); stroke-width: 1.5; transition: fill var(--motion-fast) ease, stroke var(--motion-fast) ease; }
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
/* v3 trim 紧凑档：满格小盘下 9px×scale 的名字缩成噪点（机主 628 截图点名"盘面
   文字与卡缘打架"），标签退到 title/aria，盘面只留图标点选。 */
.wp-trim .wp-name, .wp-trim .wp-caret { display: none; }
.wp-empty {
  position: absolute; inset: 0; display: flex; align-items: center; justify-content: center;
  font-size: var(--text-sm); color: var(--color-text-subtle); pointer-events: none;
}
</style>
