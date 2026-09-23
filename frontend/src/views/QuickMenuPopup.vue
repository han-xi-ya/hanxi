<script setup lang="ts">
// 快捷菜单光标轮盘：独立 frameless 真透明顶层窗口的全部内容（main.ts 按
// #quickmenu hash 分流挂载）。窗口正方形 512 = 外扩子环帽带外缘 236×2 + 四周
// 20 透明边距（容纳投影与外甩取消判定环）；四角经 GDI 区域裁剪从命中测试中
// 剪掉（windows.ClipWindowEllipse），圆盘视觉边缘全部由本页抗锯齿绘制。
//
// 双环形态（StarPie 式扇区级联外扩，取代旧"同窗口换层"二级轮盘）：主盘常驻
// 不换层；悬停分组扇区驻留 120ms，其子条目以帽带（r174→236）在主盘外圈展开，
// 角度以父扇区中线对称、跨角 clamp(子数×24°, 父步长, 180°)；离开保持区 180ms
// 迟滞收起（useWheelRingState 纯时序状态机）；点击分组 = 钉住（键盘/触屏通道），
// 再点解除；指针越过 rCancel=244 进入半透明外甩取消态。子环呈现上限 8 项，
// 超出截断并在 hub 提示（数据层不限——托盘原生子菜单无数量压力）。
// 拍平开关（twoTier=off）由后端裁决：前端只见到单层数据，子环代码路径整体休眠。
// 条目与托盘右键菜单共用一份配置（后端 launcher 分发），每次唤出事件重拉。
//
// 几何单一来源在 components/quickmenu/wheelGeometry.ts（纯函数 + 常量），角度
// 约定 0°=12 点、顺时针；三层结构不变：SVG 画扇区命中面，每个条目同时是落在
// 质点上的真实 <button>（键盘/ARIA 语义），中心 hub 为读数浮层（点击 = 收起）。
import { computed, nextTick, onBeforeUnmount, onMounted, ref, shallowRef, watch } from 'vue'
import * as QuickMenuAPI from '../../bindings/hanxi/internal/modules/quickmenu'
import type { MenuItem } from '../../bindings/hanxi/internal/modules/quickmenu/models'
import AppIcon from '../components/ui/AppIcon.vue'
import { useWailsEvent } from '../composables/useWailsEvent'
import { getErrorMessage } from '../utils/errors'
import { ICON_NAMES, type IconName } from '../constants/icons'
import {
  WHEEL, polar, wedgePath, mainSectorAngles, capSpanDeg, capSectorAngles, capAnchorDeg, mainAnchor, slotOf,
} from '../components/quickmenu/wheelGeometry'
import { useWheelRingState } from '../composables/useWheelRingState'

// 子环呈现硬上限：帽带角度撑满 180° 时每枚子扇区仍 ≥22°，再密即失能
const CAP_MAX = 8

const items = shallowRef<MenuItem[]>([])
const loading = ref(true)
const errorMsg = ref('')
// 主环悬停读数索引（null = 标题态）；子环读数独立一路，两态在 hub 汇流
const active = ref<number | null>(null)
const activeCap = ref<number | null>(null)
// 弹出动画重放键：每次唤出重挂圆盘层（换层时代的层切换已退役）
const entrySeq = ref(0)
const rootEl = ref<HTMLElement | null>(null)

// 外扩子环时序状态机（dwell/迟滞/防抖/钉住/取消态全部集中于此，组件只喂几何事件）
const ring = useWheelRingState()

const SIZE = WHEEL.size
const C = WHEEL.c
const R_DISC = WHEEL.rDisc
const R_HUB = WHEEL.rHub
const R_SEC_IN = WHEEL.rSecIn
const R_SEC_OUT = WHEEL.rSecOut

const isGroup = (item: MenuItem): boolean => (item.children?.length ?? 0) > 0

/** 当前展开子环的分组节点（数据被热改后索引可能失配，全部经此归零） */
const openNode = computed<MenuItem | null>(() => {
  const i = ring.openGroup.value
  const node = i == null ? null : items.value[i]
  return node && isGroup(node) ? node : null
})
/** 子环呈现条目（截断至 CAP_MAX）；truncated 驱动 hub 提示 */
const capItems = computed<MenuItem[]>(() => (openNode.value?.children ?? []).slice(0, CAP_MAX))
const capTruncated = computed(() => (openNode.value?.children?.length ?? 0) > CAP_MAX)

const TYPE_LABEL: Record<string, string> = {
  exe: '程序',
  command: '命令',
  route: '页面',
  group: '分组',
}
const typeLabel = (t: string) => TYPE_LABEL[t] ?? '条目'
const TYPE_ICON: Record<string, IconName> = {
  exe: 'box',
  command: 'terminal',
  route: 'layout',
  group: 'layers',
}
// 后端下发的图标名与注册表漂移（未登记/空/非 i: 形态）时回退类型图标，渲染永不因图标断链
const iconOf = (item: MenuItem): IconName =>
  (ICON_NAMES as readonly string[]).includes(item.icon) ? (item.icon as IconName) : TYPE_ICON[item.type] ?? 'box'

// N6 F1 可读性：图标常显（名字再密也留视觉锚点，尺寸随条目数分档收缩），
// 名称两行折行不再单行省略；档位令牌经 CSS 变量下发给样式层。
// 密度分档：≤6 宽裕 / 7-9 标准 / 10-12 紧凑 / >12 极限（超密本就应分组收纳）。
const density = computed(() => {
  const n = items.value.length
  if (n <= 6) return { btnW: 64, well: 34, icon: 22, name: 12 }
  if (n <= 9) return { btnW: 58, well: 30, icon: 18, name: 11 }
  if (n <= 12) return { btnW: 52, well: 26, icon: 16, name: 10 }
  return { btnW: 48, well: 24, icon: 15, name: 9.5 }
})
const densityVars = computed(() => ({
  '--d-btn-w': `${density.value.btnW}px`,
  '--d-well': `${density.value.well}px`,
  '--d-icon': `${density.value.icon}px`,
  '--d-name': `${density.value.name}px`,
}))
const activeItem = computed(() => (active.value == null ? null : items.value[active.value] ?? null))
const ready = computed(() => !loading.value && !errorMsg.value)

// N6 F2 选中反馈：激活扇区沿自身中线径向外顶 4px（pie menu 的"拿起"感），
// 按钮层与扇区面吃同一个位移向量，两层严格同步。
const POP_OUT = 4
function radialShift(i: number): { x: number; y: number } {
  const rad = ((mainCenterDeg(i) - 90) * Math.PI) / 180
  return { x: +(POP_OUT * Math.cos(rad)).toFixed(2), y: +(POP_OUT * Math.sin(rad)).toFixed(2) }
}

async function refresh() {
  loading.value = true
  errorMsg.value = ''
  active.value = null
  activeCap.value = null
  ring.reset() // 每次唤出回主环：双环可预测的前提是"层级不跨会话残留"
  try {
    items.value = (await QuickMenuAPI.QuickMenuService.ListItems()) ?? []
  } catch (err) {
    errorMsg.value = getErrorMessage(err)
  } finally {
    loading.value = false
  }
}

// —— 保持区几何：enter/leave 的唯一驱动源是 pointermove 的区域归属 ——
// （状态机告诫：帽带↔楔形切换必须视为同一保持区，绝不误报 leave+enter）

const wrap180 = (deg: number) => ((deg + 540) % 360) - 180
const stepDeg = () => 360 / items.value.length
/** 第 i 枚主扇区的名义中线角（与 mainAnchor 同口径，不含缝隙 pad） */
const mainCenterDeg = (i: number) => stepDeg() * (i + 0.5)

/** 分组 i 的保持半跨：帽带张开时取帽带、未张开取父楔形 */
function keepHalfDeg(i: number): number {
  const node = items.value[i]
  const n = node?.children?.length ?? 0
  if (!node || n === 0) return stepDeg() / 2
  return Math.max(stepDeg(), capSpanDeg(Math.min(n, CAP_MAX), stepDeg())) / 2
}

/** 半径带边界迟滞（DIP）：楔形↔帽带↔圆心静区交界 ±4 防抖 */
const HIT_HYST = 4

/**
 * 悬停高亮的几何单点权威（N40①②）：由 pointer 极坐标解析出当前应高亮的主环
 * 槽位或子环帽带位,写回 active/activeCap——彻底弃用 SVG `<path>` 的 DOM
 * mouseenter/leave。旧法两大病灶一并根治:①楔形只覆盖绘制扇形(pad 缝隙、盘缘
 * 缝带、hub 缓冲环全是死区);②楔形随外顶动效缩放 + 叠放的 `<button>` 抢命中,
 * 交界 mouseenter/leave 交错致闪烁。此处按名义角域 `slotOf` 归属——牌面任意
 * 落点(含缝隙空白)都有唯一槽位点亮;`r < R_SEC_IN-迟滞` 为圆心静区清全部悬停。
 */
function resolveHover(r: number, ang: number, opened: number | null) {
  const capN = capItems.value.length
  // 1. 子环帽带：仅在开环分组的角度张角内认领（帽带可越过邻扇区边界）
  if (opened != null && capN > 0 && r >= WHEEL.rCapIn - HIT_HYST && r <= WHEEL.rCapOut + HIT_HYST) {
    const span = capSpanDeg(capN, stepDeg())
    const rel = wrap180(ang - mainCenterDeg(opened))
    if (Math.abs(rel) <= span / 2) {
      const j = Math.min(capN - 1, Math.max(0, Math.floor((rel + span / 2) / (span / capN))))
      if (activeCap.value !== j) activeCap.value = j
      return
    }
  }
  // 2. 主环节面:无开环时延伸认领到盘缘外沿(含扇区缝带死区);开环时上界止于帽带
  //    内缘,指针穿越楔形→帽带全程无缝(缝隙带归父扇区,不断高亮)
  const outer = opened != null ? WHEEL.rCapIn - HIT_HYST : R_DISC + HIT_HYST
  if (r >= R_SEC_IN - HIT_HYST && r <= outer) {
    const idx = slotOf(ang, items.value.length)
    if (active.value !== idx) active.value = idx
    if (activeCap.value !== null) activeCap.value = null
    return
  }
  // 3. 圆心静区 / 外虚空 → 清除一切选中悬停态
  if (active.value !== null) active.value = null
  if (activeCap.value !== null) activeCap.value = null
}

function onPointerMove(ev: PointerEvent) {
  const n = items.value.length
  if (n === 0) return
  const el = ev.currentTarget as HTMLElement
  const box = el.getBoundingClientRect()
  const x = ev.clientX - box.left - C
  const y = ev.clientY - box.top - C
  const r = Math.hypot(x, y)
  const ang = (Math.atan2(x, -y) * 180) / Math.PI // 0°=12 点、顺时针，[-180,180]

  ring.setCancel(r > WHEEL.rCancel)

  const opened = ring.openGroup.value
  // 高亮先由几何解析定夺（与开收时序解耦:高亮即时、开收走 dwell）
  resolveHover(r, ang, opened)

  // 已开环的保持区优先（帽带角度可越过邻扇区边界）
  if (opened != null && r >= R_SEC_IN - HIT_HYST && r <= WHEEL.rCapOut + HIT_HYST
    && Math.abs(wrap180(ang - mainCenterDeg(opened))) <= keepHalfDeg(opened)) {
    ring.enterGroup(opened)
    return
  }
  // 主环节面保持区：按名义角槽位归属
  if (r >= R_SEC_IN - HIT_HYST && r <= R_SEC_OUT + HIT_HYST) {
    const idx = slotOf(ang, n)
    const node = items.value[idx]
    if (node && isGroup(node) && Math.abs(wrap180(ang - mainCenterDeg(idx))) <= keepHalfDeg(idx)) {
      ring.enterGroup(idx)
      return
    }
  }
  ring.leaveGroup()
}

function onRootMouseleave() {
  ring.leaveGroup()
  // 指针离盘：几何路由不再有机会解析,读数一并归位（键盘焦点态由 :focus 独立持有）
  active.value = null
  activeCap.value = null
}

// hub 读数：子环读数 > 主环读数 > 开环面包屑 > 标题态
const hubNode = computed(() => (activeCap.value == null ? null : capItems.value[activeCap.value] ?? null))
const capLabel = computed(() => (openNode.value ? `${openNode.value.label} · 子环` : ''))

/** 扇区/子环激活分派：分组钉住开环，叶子（含子环条目）执行 */
function activate(item: MenuItem) {
  if (isGroup(item)) ring.clickGroup(items.value.indexOf(item))
  else launch(item, [item.index])
}

async function launch(item: MenuItem, path: number[]) {
  // 后端先收起弹窗再异步派发，失败经统一通知 Hub 反馈，此处只兜绑定层异常。
  try {
    await QuickMenuAPI.QuickMenuService.Launch(path)
  } catch (err) {
    errorMsg.value = getErrorMessage(err)
  }
}

/** 子环第 j 枚执行：路径 = [父组展示序索引, 子项索引]（与后端 wheelView 同序） */
function launchCap(j: number) {
  const parent = openNode.value
  const child = capItems.value[j]
  if (!parent || !child) return
  launch(child, [parent.index, child.index])
}

function dismiss() {
  ring.reset()
  QuickMenuAPI.QuickMenuService.Dismiss().catch(() => { /* 窗口已在收起路径上 */ })
}

function openSettings() {
  QuickMenuAPI.QuickMenuService.OpenSettings().catch((err: unknown) => {
    errorMsg.value = getErrorMessage(err)
  })
}

// 主环分组索引变化时子环读数复位（换组即换语境，不残留旧子项高亮）
const watchedOpen = shallowRef<number | null>(null)
watch(() => ring.openGroup.value, (v) => {
  if (v !== watchedOpen.value) {
    watchedOpen.value = v
    activeCap.value = null
  }
})

// 方向键循环并把 DOM 焦点交给对应按钮。焦点在子环内 → 在子环中循环；
// 否则主环循环（分组按钮 Enter 走 activate 钉住开环并进入子环首项）。
function moveFocus(dir: number) {
  const capFocused = /^qm-cap-(\d+)$/.test(document.activeElement?.id ?? '')
  if (capFocused && capItems.value.length > 0) {
    const cur = Number(/(\d+)$/.exec(document.activeElement!.id)![1])
    const next = (cur + dir + capItems.value.length) % capItems.value.length
    document.getElementById(`qm-cap-${next}`)?.focus()
    return
  }
  const n = items.value.length
  if (n === 0) return
  const matched = /^qm-sector-(\d+)$/.exec(document.activeElement?.id ?? '')
  const cur = matched ? Number(matched[1]) : dir > 0 ? -1 : 0
  const next = (cur + dir + n) % n
  document.getElementById(`qm-sector-${next}`)?.focus()
}

// 键盘导航挂在根容器：窗口唤起时根容器已获焦，方向键/Esc 直达；Esc 分级——
// 子环展开先收子环（collapse 不关窗）、主盘才收起。另挂 window 兜底
// （焦点被 WebView2 内部吞走时仍可收起，与旧列表版同策略）。
function onKeydown(ev: KeyboardEvent) {
  if (ev.key === 'Escape') {
    ev.preventDefault()
    if (ring.openGroup.value != null) ring.collapse()
    else dismiss()
  } else if (ev.key === 'ArrowRight' || ev.key === 'ArrowDown') {
    ev.preventDefault()
    moveFocus(1)
  } else if (ev.key === 'ArrowLeft' || ev.key === 'ArrowUp') {
    ev.preventDefault()
    moveFocus(-1)
  }
}

function onWindowKeydown(ev: KeyboardEvent) {
  if (ev.key !== 'Escape') return
  if (ring.openGroup.value != null) ring.collapse() // 与根容器处理器同分级
  else dismiss()
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

// —— 渲染派生（主环） ——
const mainWedge = (i: number) => {
  const a = mainSectorAngles(i, items.value.length)
  return wedgePath(R_SEC_IN, R_SEC_OUT, a.a0, a.a1)
}
const capWedge = (j: number) => {
  const a = capSectorAngles(capItems.value.length, mainCenterDeg(ring.openGroup.value ?? 0), stepDeg(), j)
  return a ? wedgePath(WHEEL.rCapIn, WHEEL.rCapOut, a.a0, a.a1) : ''
}
const capAnchor = (j: number) => {
  const a = capSectorAngles(capItems.value.length, mainCenterDeg(ring.openGroup.value ?? 0), stepDeg(), j)
  return a ? capAnchorDeg(a.a0, a.a1) : {}
}

/** 分界刻度：扇区外缘与盘缘细缝带上逐父扇区一枚；开环时在帽带外缘追加子缝刻度 */
const tickMarks = computed(() => {
  const n = items.value.length
  if (n < 3) return []
  const marks = Array.from({ length: n }, (_, i) => {
    const a = (360 / n) * i
    const p1 = polar(R_SEC_OUT + 1.5, a)
    const p2 = polar(R_DISC - 2.5, a)
    return { x1: p1.x, y1: p1.y, x2: p2.x, y2: p2.y }
  })
  return marks
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

/** 子环外缘提示弧：展开分组在帽带外缘的主色弧段，级联归属一眼可辨 */
const capRimArc = computed(() => {
  const i = ring.openGroup.value
  const node = openNode.value
  if (i == null || !node || capItems.value.length === 0) return ''
  const span = capSpanDeg(capItems.value.length, stepDeg())
  const center = mainCenterDeg(i)
  const p0 = polar(WHEEL.rCapOut + 2, center - span / 2)
  const p1 = polar(WHEEL.rCapOut + 2, center + span / 2)
  return `M ${p0.x.toFixed(2)} ${p0.y.toFixed(2)} A ${WHEEL.rCapOut + 2} ${WHEEL.rCapOut + 2} 0 ${span > 180 ? 1 : 0} 1 ${p1.x.toFixed(2)} ${p1.y.toFixed(2)}`
})

/** 帽带底环路径：开环分组的中线两侧展开 capSpanDeg，连续环带先于扇区铺底 */
const capBandPath = computed(() => {
  const i = ring.openGroup.value
  if (i == null || capItems.value.length === 0) return ''
  const span = capSpanDeg(capItems.value.length, stepDeg())
  const center = mainCenterDeg(i)
  return wedgePath(WHEEL.rCapIn, WHEEL.rCapOut, center - span / 2, center + span / 2)
})

// 键盘 Enter 钉住分组后把焦点送进子环首项（clickGroup 的同步语义在模板事件里）
function onGroupActivate(i: number) {
  const wasOpen = ring.openGroup.value === i
  ring.clickGroup(i)
  if (!wasOpen) nextTick(() => document.getElementById('qm-cap-0')?.focus())
}
</script>

<template>
  <!-- 禁掉 WebView2 默认右键菜单：轮盘本身就是"右键"的产物 -->
  <div
    ref="rootEl"
    class="popup"
    :class="{ 'is-cancel': ring.cancelArmed.value }"
    tabindex="0"
    role="menu"
    :style="densityVars"
    aria-label="快捷启动轮盘"
    :aria-activedescendant="active != null ? `qm-sector-${active}` : (activeCap != null ? `qm-cap-${activeCap}` : undefined)"
    @keydown="onKeydown"
    @contextmenu.prevent
    @pointermove="onPointerMove"
    @mouseleave="onRootMouseleave"
  >
    <svg
      class="disc"
      :viewBox="`0 0 ${SIZE} ${SIZE}`"
    >
      <!-- 盘底：真透明窗口上绘制不透明圆盘，边缘天然抗锯齿；缘环即盘缘 -->
      <circle :cx="C" :cy="C" :r="R_DISC" class="disc-face" />
      <circle :cx="C" :cy="C" :r="R_DISC - 1.25" class="disc-edge" />
      <!-- 内缘高光环：玻璃盘口的一线反光（明暗主题各一档低透明描边） -->
      <circle :cx="C" :cy="C" :r="R_DISC - 3" class="disc-glint" />

      <g :key="entrySeq" class="sectors">
        <path
          v-for="(item, i) in items"
          :key="`${entrySeq}-${item.index}`"
          class="sector"
          :class="{ 'is-active': active === i, 'is-pinned': ring.pinnedGroup.value === i }"
          :d="mainWedge(i)"
          :style="{ animationDelay: `${Math.min(i * 14, 84)}ms`, transform: active === i ? `translate(${radialShift(i).x}px, ${radialShift(i).y}px)` : '' }"
          role="presentation"
          @click="isGroup(item) ? onGroupActivate(i) : activate(item)"
        />
      </g>

      <!-- 外扩子环帽带：常驻主盘之外，角度随父扇区中线展开；
           先铺一条连续帽带底环（玻璃盘的"环中环"），扇区透明层叠其上 -->
      <g v-if="capItems.length > 0" :key="`cap-${ring.openGroup.value}`" class="cap">
        <path v-if="capBandPath" class="cap-band" :d="capBandPath" />
        <path
          v-for="(ch, j) in capItems"
          :key="`cap-${j}-${ch.index}`"
          class="sector cap-sector"
          :class="{ 'is-active': activeCap === j }"
          :d="capWedge(j)"
          :style="{ animationDelay: `${Math.min(j * 14, 84)}ms` }"
          role="presentation"
          @click="launchCap(j)"
        />
        <path v-if="capRimArc" class="cap-rim-accent" :d="capRimArc" />
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

      <!-- hub 轮廓：读数浮层的容器，画在 SVG 里保证与扇区同轴；
           hub-hit 是中心命中面——点击 hub = 收起轮盘（StarPie 中心核动作极简版） -->
      <circle class="hub-ring" :cx="C" :cy="C" :r="R_HUB + 6" />
      <circle class="hub-face" :cx="C" :cy="C" :r="R_HUB" />
      <circle class="hub-hit" :cx="C" :cy="C" :r="R_HUB - 2" role="button" aria-label="收起轮盘" @click="dismiss" />
    </svg>

    <!-- 扇区图标/名称浮层：真实 button 提供键盘与可访问性语义，悬停高亮仍归扇区面 -->
    <button
      v-for="(item, i) in items"
      :id="`qm-sector-${i}`"
      :key="`${entrySeq}-${item.index}`"
      type="button"
      class="sector-btn"
      :class="{ 'is-active': active === i }"
      :style="[mainAnchor(i, items.length), active === i ? { transform: `translate(calc(-50% + ${radialShift(i).x}px), calc(-50% + ${radialShift(i).y}px))` } : {}]"
      role="menuitem"
      :aria-haspopup="isGroup(item) ? 'true' : undefined"
      :aria-expanded="isGroup(item) ? ring.openGroup.value === i : undefined"
      :aria-label="isGroup(item) ? `${item.label}（分组，展开子环）` : `${item.label}（${typeLabel(item.type)}）`"
      :title="item.hint || item.label"
      @focus="active = i"
      @click="isGroup(item) ? onGroupActivate(i) : activate(item)"
    >
      <span class="sector-icon"><AppIcon :name="iconOf(item)" :size="density.icon" /></span>
      <span class="sector-name">{{ item.label }}</span>
      <span v-if="isGroup(item)" class="sector-caret" aria-hidden="true">▸ {{ item.children?.length ?? 0 }}</span>
    </button>

    <!-- 子环帽带按钮层：与帽带扇区同锚点，真实 button 语义与主环一致 -->
    <template v-if="capItems.length > 0">
      <button
        v-for="(ch, j) in capItems"
        :id="`qm-cap-${j}`"
        :key="`capbtn-${ring.openGroup.value}-${ch.index}`"
        type="button"
        class="sector-btn cap-btn"
        :class="{ 'is-active': activeCap === j }"
        :style="capAnchor(j)"
        role="menuitem"
        :aria-label="`${ch.label}（${typeLabel(ch.type)}，属于${openNode?.label}）`"
        :title="ch.hint || ch.label"
        @focus="activeCap = j"
        @click="launchCap(j)"
      >
        <span class="sector-icon cap-icon"><AppIcon :name="iconOf(ch)" :size="15" /></span>
        <span class="sector-name cap-name">{{ ch.label }}</span>
      </button>
    </template>

    <!-- 中心 hub：默认标题 / 悬停读数 / 子环面包屑与收起 / 状态与动作；点击盘面外语义：hub=收起 -->
    <div class="hub" aria-live="polite" @click.self="dismiss">
      <template v-if="ready && items.length > 0">
        <template v-if="hubNode">
          <span class="hub-label">{{ hubNode.label }}</span>
          <span class="hub-kind">{{ typeLabel(hubNode.type) }}</span>
          <span class="hub-meta">点击或 Enter 执行</span>
        </template>
        <template v-else-if="activeItem && isGroup(activeItem)">
          <span class="hub-label">{{ activeItem.label }}</span>
          <span class="hub-kind">分组</span>
          <span class="hub-meta">{{ (activeItem.children?.length ?? 0) > CAP_MAX ? `${activeItem.children?.length} 项 · 显示前 ${CAP_MAX}` : `${activeItem.children?.length ?? 0} 项 · 悬停展开` }}</span>
        </template>
        <template v-else-if="activeItem">
          <span class="hub-label">{{ activeItem.label }}</span>
          <span class="hub-kind">{{ typeLabel(activeItem.type) }}</span>
          <span class="hub-meta">点击或 Enter 执行</span>
        </template>
        <template v-else-if="openNode">
          <span class="hub-title hub-crumb">{{ capLabel }}</span>
          <span class="hub-meta">{{ capItems.length }} 项{{ capTruncated ? ` · 共 ${openNode.children?.length} 仅显示前 ${CAP_MAX}` : '' }}</span>
          <button type="button" class="hub-action hub-back" @click="ring.collapse()">
            <AppIcon name="chevrons-left" :size="14" /> 收起子环
          </button>
        </template>
        <template v-else>
          <span class="hub-title">快捷菜单</span>
          <span class="hub-meta">{{ items.length }} 项 · 方向键选择</span>
          <span class="hub-meta hub-esc">Esc 收起 · 外甩取消</span>
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
  filter: drop-shadow(0 1px 3px rgba(10, 20, 24, 0.20)) drop-shadow(0 14px 28px rgba(10, 20, 24, 0.24));
  animation: disc-in 140ms ease-out; /* 弹出动感：常驻窗口复用重挂时重放 */
  transition: opacity var(--motion-base) ease;
}
@keyframes disc-in {
  from { transform: scale(0.965); opacity: 0.55; }
  to { transform: scale(1); opacity: 1; }
}

/* 外甩取消态：越过 rCancel 全盘半透明，收回缓冲带即复原（transition 已在 .disc） */
.popup.is-cancel .disc { opacity: 0.5; }

.disc-face { fill: var(--surface-panel); }
.disc-edge { fill: none; stroke: var(--color-border-strong); stroke-width: 1.5; }

/* N6 F4 玻璃底盘：扇区不再是"一块块拼图"——面本身透明，连续盘底
   （disc-face）一镜到底，扇区间以发丝辐线分界；悬停/激活整片浮起
   primary-soft 光晕 + 径向外顶（F2），"拿起这块"的直觉反馈。 */
.sector {
  pointer-events: auto;
  cursor: pointer;
  fill: transparent;
  stroke: var(--color-border);
  stroke-width: 1;
  transition: fill var(--motion-fast) ease, stroke var(--motion-fast) ease, transform var(--motion-fast) ease;
  animation: sector-in 120ms ease-out backwards; /* 错峰淡入：delay 由模板按序下发 */
}
@keyframes sector-in {
  from { opacity: 0; }
  to { opacity: 1; }
}
.sector.is-active {
  fill: var(--color-primary-soft);
  stroke: var(--color-primary);
}
.sector.is-pinned { fill: var(--surface-selected); stroke: var(--color-primary); stroke-width: 1.6; }

/* 子环帽带扇区：比主环高一档表面（嵌套控件层），弹性外扩入场 */
.cap {
  transform-origin: 50% 50%;
  animation: cap-in 150ms cubic-bezier(0.16, 1, 0.3, 1);
}
@keyframes cap-in {
  from { transform: scale(0.94); opacity: 0; }
  to { transform: scale(1); opacity: 1; }
}
.cap-sector { fill: transparent; stroke: var(--color-border); }
.cap-sector.is-active { fill: var(--color-primary-soft); }
.cap-rim-accent {
  fill: none;
  stroke: var(--color-primary);
  stroke-width: 2;
  stroke-linecap: round;
  opacity: 0.85;
}

.cap-band {
  fill: var(--surface-hover);
  stroke: var(--color-border);
  stroke-width: 1;
  opacity: 0.9;
}
.disc-glint {
  fill: none;
  stroke: var(--surface-hover);
  stroke-width: 2;
  opacity: 0.6;
}
[data-theme='dark'] .disc-glint { opacity: 0.35; }

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
.hub-hit { pointer-events: all; cursor: pointer; fill: transparent; }

/* —— 扇区图标按钮：悬停态视觉由扇区面驱动，这里只管图标配色与聚焦环 —— */
.sector-btn {
  position: absolute;
  transform: translate(-50%, -50%);
  width: var(--d-btn-w, 56px);
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
  /* 径向外顶由模板内联 transform 下发（与扇区面同向量）；这里只留微放大基调 */
}
.sector-btn:focus-visible {
  outline: 2px solid var(--color-primary);
  outline-offset: 1px;
}
/* 图标井：浅一档底 + 细描边（设计系统 surface 公式的"嵌套控件"层），激活时主色软化 */
.sector-icon {
  width: var(--d-well, 30px);
  height: var(--d-well, 30px);
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
  font-size: var(--d-name, 10px);
  line-height: 1.3;
  font-weight: 600;
  color: var(--color-text);
  /* 两行折行截断（N6 F1）：单行省略号"看不清全称"的正解是给它两行 */
  display: -webkit-box;
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 2;
  overflow: hidden;
  overflow-wrap: break-word;
  white-space: normal;
}
/* 分组常驻角标（N6 F3）：▸+子数——"这个格子还能展开"从意外变成预告 */
.sector-caret {
  position: absolute;
  top: 1px;
  right: 2px;
  font-size: 9px;
  font-weight: 700;
  line-height: 1;
  color: var(--color-primary);
  opacity: 0.9;
}
.sector-btn.is-active .sector-name {
  color: var(--color-primary);
}

/* 子环帽带按钮：帽带厚度 62 DIP，图标井与字号收一档给名称留行 */
.cap-btn { width: 50px; }
.cap-icon { width: 26px; height: 26px; }
.cap-name { font-size: 9.5px; }

/* —— 中心读数区：纯展示不拦截指针，点击 hub 本体 = 收起轮盘，动作按钮例外 —— */
.hub {
  position: absolute;
  left: 50%;
  top: 50%;
  transform: translate(-50%, -50%);
  width: 124px;
  height: 124px;
  border-radius: 50%;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 4px;
  text-align: center;
  pointer-events: none;
  cursor: default;
}
.hub > * { pointer-events: none; }
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
/* 子环收起按钮：图标+文字横排，落在 hub 中央动作位 */
.hub-back {
  display: inline-flex;
  align-items: center;
  gap: 4px;
}
.hub-crumb {
  max-width: 104px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--color-primary);
}

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
[data-theme='dark'] .cap-sector { fill: var(--surface-selected); }
</style>
