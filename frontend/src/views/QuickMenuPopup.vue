<script setup lang="ts">
// 快捷菜单光标轮盘：独立 frameless 真透明顶层窗口的全部内容（main.ts 按
// #quickmenu hash 分流挂载）。窗口正方形 376 = 盘径 340 + 四周 18 透明边距
// （容纳投影）；四角经 GDI 区域裁剪从命中测试中剪掉（windows.ClipWindowEllipse），
// 圆盘视觉边缘全部由本页抗锯齿绘制，窗口本体透明、不存在窗底白边。
//
// 三层结构：SVG 画几何与扇区命中面（悬停/点击），每个条目同时是一个落在扇区质点
// 上的真实 <button>（键盘 Tab/Enter/focus-visible 语义），中心 hub 为读数浮层。
// 条目与托盘右键菜单共用一份配置（后端 launcher 分发），每次唤出事件重拉，改配置免重启。
import { computed, nextTick, onBeforeUnmount, onMounted, ref, shallowRef } from 'vue'
import * as QuickMenuAPI from '../../bindings/hanxi/internal/modules/quickmenu'
import type { MenuItem } from '../../bindings/hanxi/internal/modules/quickmenu/models'
import AppIcon from '../components/ui/AppIcon.vue'
import { useWailsEvent } from '../composables/useWailsEvent'
import { getErrorMessage } from '../utils/errors'
import { ICON_NAMES, type IconName } from '../constants/icons'

// —— 轮盘几何（SVG 用户单位 = 窗口 DIP，1:1）——
const SIZE = 376 // 窗口边长：盘径 340 + 四周 18 透明投影边距
const C = SIZE / 2 // 圆心 = 光标锚点（后端将盘心对准光标）
const R_DISC = 170 // 盘面半径：缘环与填充画到这里，GDI 裁剪圈（r=C）在其外的全透明区
const R_HUB = 62 // 中心 hub 半径
const R_SEC_IN = 74 // 扇区内缘
const R_SEC_OUT = 162 // 扇区外缘（162~169 为刻度/缘环/高亮弧的边带）

/** 极坐标 → 直角坐标：0° 取 12 点方向，顺时针增长（轮盘的直觉序） */
function polar(r: number, deg: number) {
  const rad = ((deg - 90) * Math.PI) / 180
  return { x: C + r * Math.cos(rad), y: C + r * Math.sin(rad) }
}

/** 环形扇区 path；padDeg 为每侧角度留白，用细缝分层而非描边噪声 */
function wedgePath(i: number, n: number, padDeg = 0.9): string {
  const step = 360 / n
  const a0 = step * i + padDeg
  const a1 = step * (i + 1) - padDeg
  const p0 = polar(R_SEC_IN, a0)
  const p1 = polar(R_SEC_OUT, a0)
  const p2 = polar(R_SEC_OUT, a1)
  const p3 = polar(R_SEC_IN, a1)
  const large = a1 - a0 > 180 ? 1 : 0
  return [
    `M ${p0.x.toFixed(2)} ${p0.y.toFixed(2)}`,
    `L ${p1.x.toFixed(2)} ${p1.y.toFixed(2)}`,
    `A ${R_SEC_OUT} ${R_SEC_OUT} 0 ${large} 1 ${p2.x.toFixed(2)} ${p2.y.toFixed(2)}`,
    `L ${p3.x.toFixed(2)} ${p3.y.toFixed(2)}`,
    `A ${R_SEC_IN} ${R_SEC_IN} 0 ${large} 0 ${p0.x.toFixed(2)} ${p0.y.toFixed(2)}`,
    'Z',
  ].join(' ')
}

/** 条目 <button> 锚点：扇区质点半径处的百分比坐标（浮层容器与窗口同为正方形） */
function anchorOf(i: number, n: number) {
  const p = polar((R_SEC_IN + R_SEC_OUT) / 2, (360 / n) * (i + 0.5))
  return { left: `${(p.x / SIZE) * 100}%`, top: `${(p.y / SIZE) * 100}%` }
}

/** 扇区分界刻度（r 落在扇区外缘与盘缘的细缝带，指针式表盘的方位感） */
const tickMarks = computed(() => {
  const n = items.value.length
  if (n < 3) return []
  return Array.from({ length: n }, (_, i) => {
    const a = (360 / n) * i
    const p1 = polar(R_SEC_OUT + 1.5, a)
    const p2 = polar(R_DISC - 2.5, a)
    return { x1: p1.x, y1: p1.y, x2: p2.x, y2: p2.y }
  })
})

/** 激活扇区的外缘高亮弧：盘缘上的一段主色弧线，pie menu 的直觉反馈 */
const activeRimArc = computed(() => {
  const i = active.value
  const n = items.value.length
  if (i == null || n < 2) return ''
  const step = 360 / n
  const a0 = step * i + 3
  const a1 = step * (i + 1) - 3
  const p0 = polar(R_DISC - 1.25, a0)
  const p1 = polar(R_DISC - 1.25, a1)
  return `M ${p0.x.toFixed(2)} ${p0.y.toFixed(2)} A ${R_DISC - 1.25} ${R_DISC - 1.25} 0 ${a1 - a0 > 180 ? 1 : 0} 1 ${p1.x.toFixed(2)} ${p1.y.toFixed(2)}`
})

const items = shallowRef<MenuItem[]>([])
const loading = ref(true)
const errorMsg = ref('')
// 当前高亮条目（扇区悬停、按钮悬停或键盘聚焦），null 时 hub 回到标题态
const active = ref<number | null>(null)
// 弹出动画重放键：每次唤出重挂圆盘层，常驻隐藏复用的窗口也有"弹出来"的观感
const entrySeq = ref(0)
const rootEl = ref<HTMLElement | null>(null)

const TYPE_LABEL: Record<string, string> = {
  exe: '程序',
  command: '命令',
  route: '页面',
}
const typeLabel = (t: string) => TYPE_LABEL[t] ?? '条目'
const TYPE_ICON: Record<string, IconName> = {
  exe: 'box',
  command: 'terminal',
  route: 'layout',
}
// 后端下发的图标名与注册表漂移（未登记/空/非 i: 形态）时回退类型图标，渲染永不因图标断链
const iconOf = (item: MenuItem): IconName =>
  (ICON_NAMES as readonly string[]).includes(item.icon) ? (item.icon as IconName) : TYPE_ICON[item.type] ?? 'box'

// ≤8 项扇区够宽，图标下方带短名；更密时纯图标，全名交给 hub 读数
const showLabels = computed(() => items.value.length <= 8)
const activeItem = computed(() => (active.value == null ? null : items.value[active.value] ?? null))
const ready = computed(() => !loading.value && !errorMsg.value)

async function refresh() {
  loading.value = true
  errorMsg.value = ''
  active.value = null
  try {
    items.value = (await QuickMenuAPI.QuickMenuService.ListItems()) ?? []
  } catch (err) {
    errorMsg.value = getErrorMessage(err)
  } finally {
    loading.value = false
  }
}

async function launch(item: MenuItem) {
  // 后端先收起弹窗再异步派发，失败经统一通知 Hub 反馈，此处只兜绑定层异常。
  try {
    await QuickMenuAPI.QuickMenuService.Launch(item.index)
  } catch (err) {
    errorMsg.value = getErrorMessage(err)
  }
}

function dismiss() {
  QuickMenuAPI.QuickMenuService.Dismiss().catch(() => { /* 窗口已在收起路径上 */ })
}

function openSettings() {
  QuickMenuAPI.QuickMenuService.OpenSettings().catch((err: unknown) => {
    errorMsg.value = getErrorMessage(err)
  })
}

// 扇区指针离开即读数归位；仍在悬停旧扇区时才清除，防 enter/leave 交错闪烁
function leaveSector(i: number) {
  if (active.value === i) active.value = null
}

// 方向键循环步进扇区并把 DOM 焦点交给对应按钮（Enter/Space 由原生 button 承担）。
function moveFocus(dir: number) {
  const n = items.value.length
  if (n === 0) return
  const matched = /^qm-sector-(\d+)$/.exec(document.activeElement?.id ?? '')
  const cur = matched ? Number(matched[1]) : dir > 0 ? -1 : 0
  const next = (cur + dir + n) % n
  document.getElementById(`qm-sector-${next}`)?.focus()
}

// 键盘导航挂在根容器：窗口唤起时根容器已获焦，方向键/Esc 直达；
// Esc 另挂 window 兜底（焦点被 WebView2 内部吞走时仍可收起，与旧列表版同策略）。
function onKeydown(ev: KeyboardEvent) {
  if (ev.key === 'Escape') {
    ev.preventDefault()
    dismiss()
  } else if (ev.key === 'ArrowRight' || ev.key === 'ArrowDown') {
    ev.preventDefault()
    moveFocus(1)
  } else if (ev.key === 'ArrowLeft' || ev.key === 'ArrowUp') {
    ev.preventDefault()
    moveFocus(-1)
  }
}

function onWindowKeydown(ev: KeyboardEvent) {
  if (ev.key === 'Escape') dismiss()
}

// 每次后端唤出弹窗：重拉条目（设置页可能刚改过共用配置）、重放弹出动画、归还键盘焦点。
useWailsEvent('quickmenu:opening', () => {
  entrySeq.value += 1
  active.value = null
  refresh()
  nextTick(() => rootEl.value?.focus())
})

onMounted(() => {
  window.addEventListener('keydown', onWindowKeydown)
  refresh()
  rootEl.value?.focus()
})
onBeforeUnmount(() => {
  window.removeEventListener('keydown', onWindowKeydown)
})
</script>

<template>
  <!-- 禁掉 WebView2 默认右键菜单：轮盘本身就是"右键"的产物 -->
  <div
    ref="rootEl"
    class="popup"
    tabindex="0"
    role="menu"
    aria-label="快捷启动轮盘"
    :aria-activedescendant="active != null ? `qm-sector-${active}` : undefined"
    @keydown="onKeydown"
    @contextmenu.prevent
  >
    <svg
      class="disc"
      :viewBox="`0 0 ${SIZE} ${SIZE}`"
    >
      <!-- 盘底：真透明窗口上绘制不透明圆盘，边缘天然抗锯齿；缘环即盘缘 -->
      <circle :cx="C" :cy="C" :r="R_DISC" class="disc-face" />
      <circle :cx="C" :cy="C" :r="R_DISC - 1.25" class="disc-edge" />

      <g :key="entrySeq" class="sectors">
        <path
          v-for="(item, i) in items"
          :key="item.index"
          class="sector"
          :class="{ 'is-active': active === i }"
          :d="wedgePath(i, items.length)"
          :style="{ animationDelay: `${Math.min(i * 14, 84)}ms` }"
          role="presentation"
          @mouseenter="active = i"
          @mouseleave="leaveSector(i)"
          @click="launch(item)"
        />
      </g>

      <!-- 分界刻度：扇区外缘与盘缘之间的细缝带，指针式表盘的方位刻度 -->
      <g v-if="tickMarks.length" class="ticks" aria-hidden="true">
        <line
          v-for="(t, i) in tickMarks"
          :key="i"
          :x1="t.x1" :y1="t.y1" :x2="t.x2" :y2="t.y2"
        />
      </g>

      <!-- 激活扇区的外缘主色高亮弧 -->
      <path v-if="activeRimArc" class="rim-accent" :d="activeRimArc" />

      <!-- 无扇区可画时（加载/空态/错误）留一圈虚线：圆盘仍是"一个待用的轮盘" -->
      <circle
        v-if="!ready || items.length === 0"
        class="disc-ghost"
        :cx="C" :cy="C" :r="(R_SEC_IN + R_SEC_OUT) / 2"
      />

      <!-- hub 轮廓：读数浮层的容器，画在 SVG 里保证与扇区同轴 -->
      <circle class="hub-ring" :cx="C" :cy="C" :r="R_HUB + 6" />
      <circle class="hub-face" :cx="C" :cy="C" :r="R_HUB" />
    </svg>

    <!-- 扇区图标/名称浮层：真实 button 提供键盘与可访问性语义，悬停高亮仍归扇区面 -->
    <button
      v-for="(item, i) in items"
      :id="`qm-sector-${i}`"
      :key="`${entrySeq}-${item.index}`"
      type="button"
      class="sector-btn"
      :class="{ 'is-active': active === i }"
      :style="anchorOf(i, items.length)"
      role="menuitem"
      :aria-label="`${item.label}（${typeLabel(item.type)}）`"
      :title="item.hint || item.label"
      @focus="active = i"
      @click="launch(item)"
    >
      <span class="sector-icon"><AppIcon :name="iconOf(item)" :size="18" /></span>
      <span v-if="showLabels" class="sector-name">{{ item.label }}</span>
    </button>

    <!-- 中心 hub：默认标题 / 悬停读数 / 状态与动作 -->
    <div class="hub" aria-live="polite">
      <template v-if="ready && items.length > 0">
        <template v-if="activeItem">
          <span class="hub-label">{{ activeItem.label }}</span>
          <span class="hub-kind">{{ typeLabel(activeItem.type) }}</span>
          <span class="hub-meta">点击或 Enter 执行</span>
        </template>
        <template v-else>
          <span class="hub-title">快捷菜单</span>
          <span class="hub-meta">{{ items.length }} 项 · 方向键选择</span>
          <span class="hub-meta hub-esc">Esc 收起</span>
        </template>
      </template>
      <span v-else-if="loading" class="hub-meta">正在加载…</span>
      <template v-else-if="errorMsg">
        <span class="hub-label hub-state-error">加载失败</span>
        <button type="button" class="hub-action" @click="refresh">重试</button>
      </template>
      <template v-else>
        <span class="hub-label">暂无条目</span>
        <span class="hub-meta">与托盘菜单共用配置</span>
        <button type="button" class="hub-action" @click="openSettings">配置条目</button>
      </template>
    </div>
  </div>
</template>

<style scoped>
/* 窗口本体真透明：根节点不带任何背景（圆盘由 SVG 绘出），投影落在四周
   透明边距内。颜色全部走 token，明暗主题随 data-theme 自动切换。 */
.popup {
  position: relative;
  width: 100vw;
  height: 100vh;
  overflow: hidden;
  background: transparent;
  color: var(--color-text);
  font-family: var(--font-text);
}
.popup:focus {
  outline: none; /* 方形焦点环与圆窗不兼容；键盘选中态由扇区描边表达 */
}

.disc {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  pointer-events: none; /* 盘面不拦截，扇区 path 逐枚放开 */
  /* 双层低噪投影：贴地接触影 + 环境扩散影，淡出全部落在 GDI 裁剪圈内 */
  filter: drop-shadow(0 1px 3px rgba(10, 20, 24, 0.20)) drop-shadow(0 12px 26px rgba(10, 20, 24, 0.24));
  animation: disc-in 140ms ease-out; /* 弹出动感：常驻窗口复用重挂时重放 */
}
@keyframes disc-in {
  from { transform: scale(0.965); opacity: 0.55; }
  to { transform: scale(1); opacity: 1; }
}

.disc-face { fill: var(--surface-panel); }
.disc-edge { fill: none; stroke: var(--color-border-strong); stroke-width: 1.5; }

.sector {
  pointer-events: auto;
  cursor: pointer;
  fill: var(--surface-soft);
  stroke: var(--color-border);
  stroke-width: 1;
  transition: fill var(--motion-fast) ease, stroke var(--motion-fast) ease;
  animation: sector-in 120ms ease-out backwards; /* 错峰淡入：delay 由模板按序下发 */
}
@keyframes sector-in {
  from { opacity: 0; }
  to { opacity: 1; }
}
.sector.is-active {
  fill: var(--surface-selected);
  stroke: var(--color-primary);
}

.ticks line {
  stroke: var(--color-border-strong);
  stroke-width: 1;
  opacity: 0.55;
}

.rim-accent {
  fill: none;
  stroke: var(--color-primary);
  stroke-width: 2.5;
  stroke-linecap: round;
}

.disc-ghost {
  fill: none;
  stroke: var(--color-border-strong);
  stroke-width: 1.5;
  stroke-dasharray: 2 7;
  stroke-linecap: round;
}

.hub-face { fill: var(--surface-panel); stroke: var(--color-border-strong); stroke-width: 1; }
.hub-ring { fill: none; stroke: var(--color-border); stroke-width: 1; stroke-dasharray: 1 5; stroke-linecap: round; }

/* —— 扇区图标按钮：悬停态视觉由扇区面驱动，这里只管图标配色与聚焦环 —— */
.sector-btn {
  position: absolute;
  transform: translate(-50%, -50%);
  width: 56px;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 3px;
  padding: 5px 2px;
  border: none;
  border-radius: var(--radius-control);
  background: transparent;
  color: var(--color-text-muted);
  font: inherit;
  cursor: pointer;
  transition: color var(--motion-fast) ease, transform var(--motion-fast) ease;
}
.sector-btn.is-active {
  color: var(--color-primary);
  transform: translate(-50%, -50%) scale(1.07);
}
.sector-btn:focus-visible {
  outline: 2px solid var(--color-primary);
  outline-offset: 1px;
}
/* 图标井：浅一档底 + 细描边（设计系统 surface 公式的"嵌套控件"层），激活时主色软化 */
.sector-icon {
  width: 30px;
  height: 30px;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--surface-panel);
  border: 1px solid var(--color-border);
  transition: background var(--motion-fast) ease, border-color var(--motion-fast) ease;
}
.sector-btn.is-active .sector-icon {
  background: var(--color-primary-soft);
  border-color: var(--color-primary);
}
.sector-name {
  max-width: 100%;
  font-size: 10px;
  line-height: 1.25;
  font-weight: 600;
  color: var(--color-text);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.sector-btn.is-active .sector-name {
  color: var(--color-primary);
}

/* —— 中心读数区：纯展示不拦截指针，仅状态动作按钮例外 —— */
.hub {
  position: absolute;
  left: 50%;
  top: 50%;
  transform: translate(-50%, -50%);
  width: 116px;
  height: 116px;
  border-radius: 50%;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 4px;
  text-align: center;
  pointer-events: none;
}
.hub-action {
  pointer-events: auto;
  min-height: 24px;
  padding: 2px 10px;
  border: 1px solid var(--color-border-strong);
  border-radius: var(--radius-pill);
  background: var(--surface-soft);
  color: var(--color-text);
  font: inherit;
  font-size: 11px;
  cursor: pointer;
  transition: background var(--motion-fast) ease;
}
.hub-action:hover { background: var(--surface-hover); }
.hub-action:focus-visible { outline: 2px solid var(--color-primary); outline-offset: 1px; }

.hub-title {
  font-size: 13px;
  font-weight: 600;
  letter-spacing: 0.02em;
}
.hub-label {
  max-width: 104px;
  font-size: 12px;
  font-weight: 600;
  line-height: 1.35;
  overflow-wrap: break-word;
}
.hub-state-error { color: var(--state-danger); }
.hub-kind {
  font-size: 10px;
  color: var(--color-text-muted);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-pill);
  padding: 0 7px;
}
.hub-meta {
  font-size: 10px;
  color: var(--color-text-subtle);
}
.hub-esc { opacity: 0.75; }

/* 暗色下亮度差收敛，扇区分层更多依赖描边；图标井反向抬升（面板比扇区更亮一档，
   避免在暗盘上凹成"黑洞"） */
[data-theme='dark'] .sector { stroke: var(--color-border-strong); }
[data-theme='dark'] .sector-icon { background: var(--surface-hover); }
</style>
