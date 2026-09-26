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
// 约定 0°=12 点、顺时针；三层结构不变：SVG 画扇区视觉面（楔形绘制留 pad 缝），
// 命中/高亮不再吃 DOM hover 而是 pointermove 按 wheelGeometry.slotOf 名义角域
// 几何归属（N40①②,缝隙带零死区、圆心静区清态）；每个条目同时是落在质点上的
// 真实 <button>（键盘/ARIA 语义），中心 hub 为读数浮层（点击 = 收起）。
import { computed, nextTick, onBeforeUnmount, onMounted, ref, shallowRef, watch } from 'vue'
import * as QuickMenuAPI from '../../bindings/hanxi/internal/modules/quickmenu'
import type { MenuItem } from '../../bindings/hanxi/internal/modules/quickmenu/models'
import AppIcon from '../components/ui/AppIcon.vue'
import { useWailsEvent } from '../composables/useWailsEvent'
import { getErrorMessage } from '../utils/errors'
import { wheelIconOf } from '../components/quickmenu/wheelIcons'
import {
  WHEEL, VIS, polar, wedgePath, mainSectorAngles, capSpanDeg, capSectorAngles, capAnchorDeg, mainAnchor, slotOf,
} from '../components/quickmenu/wheelGeometry'
import { useWheelRingState } from '../composables/useWheelRingState'
import { isRasterWheelIcon, snapIconCssPx } from '../components/quickmenu/wheelIconBudget'
import { useWheelDpr } from '../components/quickmenu/useWheelDpr'
import {
  readWheelSkin, wheelSkinVars, wheelTintCss, WHEEL_SKIN_STORAGE_KEY, type WheelSkin,
} from '../components/quickmenu/wheelSkin'
import { wheelTypeTint } from '../components/quickmenu/wheelSkinColors'
import { useWheelTints } from '../components/quickmenu/useWheelTints'

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

// N6 F1 可读性 → N40③ 花瓣盘：图标常显，座径四档各 +2px；名称改单行省略，
// 全称读数完全交 hub 仪表核（F1"hub 干活"路线执行到底）。档位令牌经 CSS 变量
// 下发给样式层（pop 为径向外顶位移，密盘档位降回 5px 防与邻缝打架）。
// 密度分档：≤6 宽裕 / 7-9 标准 / 10-12 紧凑 / >12 极限（超密本就应分组收纳）。
const density = computed(() => {
  const n = items.value.length
  if (n <= 6) return { btnW: 64, well: 36, icon: 22, name: 12, pop: 8 }
  if (n <= 9) return { btnW: 58, well: 32, icon: 18, name: 12, pop: 8 }
  if (n <= 12) return { btnW: 52, well: 28, icon: 16, name: 11, pop: 5 }
  return { btnW: 48, well: 26, icon: 15, name: 11, pop: 5 }
})
const densityVars = computed(() => ({
  '--d-btn-w': `${density.value.btnW}px`,
  '--d-well': `${density.value.well}px`,
  '--d-icon': `${density.value.icon}px`,
  '--d-name': `${density.value.name}px`,
}))
const CAP_ICON = 15 // 帽带子扇区图标档（原模板裸值归口，便于像素预算消费）

// —— 皮肤账与图标像素预算（机主反馈 2026-09-26 两条：小图标糊 / 盘面单调）——
// 皮肤存 localStorage（纯视觉账，与 rail 展开态同族）；设置页保存后本窗经同源
// storage 事件即时跟皮，窗口隐藏期错过的改动由每次唤出 refresh() 重读兜底。
const dpr = useWheelDpr()
const skin = ref<WheelSkin>(readWheelSkin())
const skinVars = computed(() => wheelSkinVars(skin.value))
function onSkinStorage(ev: StorageEvent) {
  if (ev.key === null || ev.key === WHEEL_SKIN_STORAGE_KEY) skin.value = readWheelSkin()
}

// 模块色取色（"跟随模块色"勾选时启用）：app: 图标主色调喂扇区描边 --tint，
// 取不到色（灰图标/矢量轨/采样失败）如实回落类型 token。
const { tints, refresh: refreshTints } = useWheelTints(
  () => {
    const names = items.value.map(wheelIconOf)
    for (const ch of capItems.value) names.push(wheelIconOf(ch))
    return names
  },
  () => skin.value.followModuleColor,
)
watch([items, () => skin.value.followModuleColor], refreshTints)
watch(() => ring.openGroup.value, refreshTints) // 开环后帽带新面孔补采

/** 扇区描边 tint 色（皮肤样式经 inline var 下发；undefined = 不吃模块色） */
function sectorTint(item: MenuItem): string | undefined {
  if (!skin.value.followModuleColor) return undefined
  const dom = tints.value[wheelIconOf(item)]
  return dom ? wheelTintCss(dom, skin.value.faceAlpha) : undefined
}
const typeClass = (item: MenuItem) => `t-${wheelTypeTint(item.type)}`
/** 图标渲染边长：app: 位图轨吃 DPR 像素预算，i: 矢量轨原档直出（见 wheelIconBudget） */
function iconPx(item: MenuItem, base: number): number {
  return isRasterWheelIcon(wheelIconOf(item)) ? snapIconCssPx(base, dpr.value || 1) : base
}
const activeItem = computed(() => (active.value == null ? null : items.value[active.value] ?? null))
const ready = computed(() => !loading.value && !errorMsg.value)

// N6 F2 选中反馈 → N40③⑤：激活扇区沿自身中线径向外顶（宽裕档 8px，密盘档降 5，
// 见 density.pop），配主色辉光与全场让位构成"拿起这块"的层级叙事；
// 按钮层与扇区面吃同一个位移向量，两层严格同步。
function radialShift(i: number): { x: number; y: number } {
  const pop = density.value.pop
  const rad = ((mainCenterDeg(i) - 90) * Math.PI) / 180
  return { x: +(pop * Math.cos(rad)).toFixed(2), y: +(pop * Math.sin(rad)).toFixed(2) }
}

async function refresh() {
  loading.value = true
  errorMsg.value = ''
  active.value = null
  activeCap.value = null
  skin.value = readWheelSkin() // 设置页可能在窗口隐藏期间改皮：唤出即重读兜底
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
      // 父锚定（审查 #6）：帽带张角可越父扇区名义角域伸入邻区——指针从邻区楔形
      // 直穿帽带时 active 会停在邻扇区，造成"正开环的花瓣被让位降半档、邻瓣反而
      // 辉光顶出、hub 读帽带"三指打架。帽带在场则高亮恒归父组（opened 索引）。
      if (active.value !== opened) active.value = opened
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
  window.addEventListener('storage', onSkinStorage)
  refresh()
  rootEl.value?.focus()
})
onBeforeUnmount(() => {
  window.removeEventListener('keydown', onWindowKeydown)
  window.removeEventListener('storage', onSkinStorage)
})

// —— 渲染派生（主环） —— 绘制一律吃 VIS 观感常量（缝角/收窄环带），命中不动
const mainWedge = (i: number) => {
  const a = mainSectorAngles(i, items.value.length, VIS.padDeg)
  return wedgePath(VIS.rSecIn, VIS.rSecOut, a.a0, a.a1)
}
const capWedge = (j: number) => {
  const a = capSectorAngles(capItems.value.length, mainCenterDeg(ring.openGroup.value ?? 0), stepDeg(), j, VIS.capPadDeg)
  return a ? wedgePath(VIS.rCapIn, VIS.rCapOut, a.a0, a.a1) : ''
}
const capAnchor = (j: number) => {
  const a = capSectorAngles(capItems.value.length, mainCenterDeg(ring.openGroup.value ?? 0), stepDeg(), j)
  return a ? capAnchorDeg(a.a0, a.a1) : {}
}

/** 盘缘亮环带（①）：花瓣绘制外缘（含 ±3 膨胀）与盘缘描边之间的一段环带 */
const RIM_MID = (VIS.rSecOut + 3 + (R_DISC - 2)) / 2
const RIM_W = R_DISC - 2 - (VIS.rSecOut + 3)

/** hub 槽位刻度环（⑥，接替退役的 .ticks）：沿 r50→57 逐槽一枚，点亮当前悬停槽 */
const hubTicks = computed(() => {
  const n = items.value.length
  if (n < 2) return []
  return Array.from({ length: n }, (_, i) => {
    const a = (360 / n) * (i + 0.5)
    const p1 = polar(VIS.rTickIn, a)
    const p2 = polar(VIS.rTickOut, a)
    return { x1: p1.x, y1: p1.y, x2: p2.x, y2: p2.y, lit: active.value === i }
  })
})

/** 激活扇区的外缘高亮弧：盘缘亮环带上的一段主色弧线，pie menu 的直觉反馈 */
const activeRimArc = computed(() => {
  const i = active.value
  const n = items.value.length
  if (i == null || n < 2) return ''
  const step = 360 / n
  const a0 = step * i + 3
  const a1 = step * (i + 1) - 3
  const r = VIS.rSecOut + 5
  const p0 = polar(r, a0)
  const p1 = polar(r, a1)
  return `M ${p0.x.toFixed(2)} ${p0.y.toFixed(2)} A ${r} ${r} 0 ${a1 - a0 > 180 ? 1 : 0} 1 ${p1.x.toFixed(2)} ${p1.y.toFixed(2)}`
})

/** 子环外缘提示弧：展开分组在帽带外缘的主色弧段，级联归属一眼可辨 */
const capRimArc = computed(() => {
  const i = ring.openGroup.value
  const node = openNode.value
  if (i == null || !node || capItems.value.length === 0) return ''
  const span = capSpanDeg(capItems.value.length, stepDeg())
  const center = mainCenterDeg(i)
  const r = VIS.rCapOut + 2
  const p0 = polar(r, center - span / 2)
  const p1 = polar(r, center + span / 2)
  return `M ${p0.x.toFixed(2)} ${p0.y.toFixed(2)} A ${r} ${r} 0 ${span > 180 ? 1 : 0} 1 ${p1.x.toFixed(2)} ${p1.y.toFixed(2)}`
})

/** 帽带底环（②）：两端收圆帽的粗弧（帽带绘制带中线弧 + stroke-linecap round），
 *  primary 软底先于子扇区铺底，替代旧直角环段 */
const CAP_BAND_MID_R = (VIS.rCapIn + VIS.rCapOut) / 2
const CAP_BAND_W = VIS.rCapOut - VIS.rCapIn
const capBandArc = computed(() => {
  const i = ring.openGroup.value
  if (i == null || capItems.value.length === 0) return ''
  const span = capSpanDeg(capItems.value.length, stepDeg())
  const center = mainCenterDeg(i)
  const p0 = polar(CAP_BAND_MID_R, center - span / 2)
  const p1 = polar(CAP_BAND_MID_R, center + span / 2)
  return `M ${p0.x.toFixed(2)} ${p0.y.toFixed(2)} A ${CAP_BAND_MID_R} ${CAP_BAND_MID_R} 0 ${span > 180 ? 1 : 0} 1 ${p1.x.toFixed(2)} ${p1.y.toFixed(2)}`
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
    :class="[`skin-${skin.preset}`, { 'is-cancel': ring.cancelArmed.value }]"
    tabindex="0"
    role="menu"
    :style="[densityVars, skinVars]"
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
      <defs>
        <!-- 盘缘亮环渐变（①）：顶部最亮收到底部微光，接替退役的 .disc-glint -->
        <linearGradient id="qm-rim-grad" x1="0" y1="0" x2="0" y2="1">
          <stop offset="0%" class="rim-stop-hi" />
          <stop offset="100%" class="rim-stop-lo" />
        </linearGradient>
        <!-- 盘底纱面渐变（皮肤批）：偏上光源的径向明纱收进深底，替代一潭死水的平涂；
             两 stop 吃皮肤预设变量（--wf-hi/--wf-lo），盘面透明度由 --wf-face-a 统管 -->
        <radialGradient id="qm-face-grad" cx="50%" cy="38%" r="80%">
          <stop offset="0%" class="face-stop-hi" />
          <stop offset="100%" class="face-stop-lo" />
        </radialGradient>
      </defs>

      <!-- 盘底：真透明窗口上绘制不透明圆盘，边缘天然抗锯齿；缘环即盘缘 -->
      <circle :cx="C" :cy="C" :r="R_DISC" class="disc-face" />
      <circle :cx="C" :cy="C" :r="R_DISC - 1.25" class="disc-edge" />
      <!-- 盘缘亮环（N40③①）：花瓣收窄到 r158 后，162→176 段留给连续渐变亮环 -->
      <circle :cx="C" :cy="C" :r="RIM_MID" :stroke-width="RIM_W" class="disc-rim" />

      <g :key="entrySeq" class="sectors" :class="{ 'has-active': active != null }">
        <path
          v-for="(item, i) in items"
          :key="`${entrySeq}-${item.index}`"
          class="sector"
          :class="[{ 'is-active': active === i, 'is-pinned': ring.pinnedGroup.value === i }, typeClass(item)]"
          :d="mainWedge(i)"
          :style="[{ animationDelay: `${Math.min(i * 14, 84)}ms`, transform: active === i ? `translate(${radialShift(i).x}px, ${radialShift(i).y}px)` : '' }, sectorTint(item) ? { '--tint': sectorTint(item) } : {}]"
          role="presentation"
          @click="isGroup(item) ? onGroupActivate(i) : activate(item)"
        />
      </g>

      <!-- 外扩子环帽带：常驻主盘之外，角度随父扇区中线展开；
           底环为两端收圆帽的 primary 软底粗弧（N40③②），圆角花瓣子扇区叠其上 -->
      <g
        v-if="capItems.length > 0"
        :key="`cap-${ring.openGroup.value}`"
        class="cap"
        :class="{ 'has-active': activeCap != null }"
      >
        <path v-if="capBandArc" class="cap-band" :d="capBandArc" :stroke-width="CAP_BAND_W" />
        <path
          v-for="(ch, j) in capItems"
          :key="`cap-${j}-${ch.index}`"
          class="sector cap-sector"
          :class="[{ 'is-active': activeCap === j }, typeClass(ch)]"
          :d="capWedge(j)"
          :style="[{ animationDelay: `${Math.min(j * 14, 84)}ms` }, sectorTint(ch) ? { '--tint': sectorTint(ch) } : {}]"
          role="presentation"
          @click="launchCap(j)"
        />
        <path v-if="capRimArc" class="cap-rim-accent" :d="capRimArc" />
      </g>

      <!-- 激活扇区的外缘主色高亮弧 -->
      <path v-if="activeRimArc" class="rim-accent" :d="activeRimArc" />

      <!-- 无扇区可画时（加载/空态/错误）留一圈虚线：圆盘仍是"一个待用的轮盘" -->
      <circle
        v-if="!ready || items.length === 0"
        class="disc-ghost"
        :cx="C" :cy="C" :r="(R_SEC_IN + R_SEC_OUT) / 2"
      />

      <!-- hub 仪表核（N40③⑥）：磨砂面浮起于花瓣内圈缝隙之外，虚线环退役；
           槽位刻度环接替盘缘分界语言（点亮当前悬停槽）；
           hub-hit 是中心命中面——点击 hub = 收起轮盘（半径与语义不动） -->
      <circle class="hub-face" :cx="C" :cy="C" :r="VIS.rHubFace" />
      <g class="hub-ticks" aria-hidden="true">
        <line
          v-for="(t, i) in hubTicks"
          :key="i"
          :x1="t.x1" :y1="t.y1" :x2="t.x2" :y2="t.y2"
          :class="{ 'is-lit': t.lit }"
        />
      </g>
      <circle class="hub-hit" :cx="C" :cy="C" :r="R_HUB - 2" role="button" aria-label="收起轮盘" @click="dismiss" />
    </svg>

    <!-- 扇区图标/名称浮层：真实 button 提供键盘与可访问性语义；悬停高亮统一由
         pointermove 几何路由驱动（N40），DOM 层不再自持 hover -->
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
      :title="item.label"
      @focus="active = i"
      @click="isGroup(item) ? onGroupActivate(i) : activate(item)"
    >
      <span class="sector-icon"><AppIcon :name="wheelIconOf(item)" :size="iconPx(item, density.icon)" /></span>
      <span class="sector-name">{{ item.label }}</span>
      <!-- 分组计数徽标（N40③④）：▸N 文字角标退役，圆形徽标只报数 -->
      <span v-if="isGroup(item)" class="sector-caret" aria-hidden="true">{{ item.children?.length ?? 0 }}</span>
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
        :title="ch.label"
        @focus="activeCap = j"
        @click="launchCap(j)"
      >
        <span class="sector-icon cap-icon"><AppIcon :name="wheelIconOf(ch)" :size="iconPx(ch, CAP_ICON)" /></span>
        <span class="sector-name cap-name">{{ ch.label }}</span>
      </button>
    </template>

    <!-- 中心 hub：默认标题 / 悬停读数 / 子环面包屑与收起 / 状态与动作；点击盘面外语义：hub=收起 -->
    <!-- hub 浮层全程 pointer-events:none，点击穿到下层 SVG .hub-hit 圆收起
         （审查 #10：原 @click.self 为死绑定，已删） -->
    <div class="hub" aria-live="polite">
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
        <!-- 常驻收起提示（N40③⑥）：hub=收起的显性教学，此前只活在 aria-label 里；
             纯呈现不入命中（pointer-events:none，点击由下层 hub-hit 承接） -->
        <span class="hub-hint" aria-hidden="true">点按中心收起</span>
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
  /* N40③ 花瓣盘局部配方：花瓣/激活/钉住/帽带底色全部以不透明 panel 为基色
     混出（真透明窗上无退路可谈半透叠底），描边同色走膨胀圆角技巧，交叠不显脏；
     --ease-pop 与 .cap 入场同款弹簧尾（⑤） */
  --petal: color-mix(in srgb, var(--surface-hover) 70%, var(--surface-panel));
  --petal-on: color-mix(in srgb, var(--color-primary) 14%, var(--surface-panel));
  --petal-pin: color-mix(in srgb, var(--surface-selected) 85%, var(--surface-panel));
  --cap-petal: color-mix(in srgb, var(--surface-panel) 72%, var(--surface-hover));
  --ease-pop: cubic-bezier(0.16, 1, 0.3, 1);
  /* 皮肤默认配方 = frost 素瓷（frost 类即现状基线）：盘底纱面渐变的明暗两档。
     veil/ink 预设只覆写这几枚呈现变量，命中几何与 token 消费面零沾染。 */
  --wf-hi: var(--surface-panel);
  --wf-lo: color-mix(in srgb, var(--color-text) 6%, var(--surface-panel));
}
/* 雾青：盘面/花瓣染主色薄雾——全走 color-mix 派生，随色板轴（teal/sky/…）联动 */
.popup.skin-veil {
  --wf-hi: color-mix(in srgb, var(--color-primary) 8%, var(--surface-panel));
  --wf-lo: color-mix(in srgb, var(--color-primary) 16%, var(--surface-panel));
  --petal: color-mix(in srgb, var(--color-primary) 10%, var(--surface-panel));
  --petal-on: color-mix(in srgb, var(--color-primary) 22%, var(--surface-panel));
  --cap-petal: color-mix(in srgb, var(--color-primary) 7%, var(--surface-hover));
}
/* 玄影：墨纱盘——text token 混深，明暗主题皆玄，扇区浮起感由深度差扛起 */
.popup.skin-ink {
  --wf-hi: color-mix(in srgb, var(--color-text) 10%, var(--surface-panel));
  --wf-lo: color-mix(in srgb, var(--color-text) 20%, var(--surface-panel));
  --petal: color-mix(in srgb, var(--color-text) 14%, var(--surface-panel));
  --petal-on: color-mix(in srgb, var(--color-primary) 18%, var(--petal));
  --cap-petal: color-mix(in srgb, var(--surface-panel) 82%, var(--color-text));
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

.disc-face { fill: url(#qm-face-grad); opacity: var(--wf-face-a, 1); }
.face-stop-hi { stop-color: var(--wf-hi); }
.face-stop-lo { stop-color: var(--wf-lo); }
.disc-edge { fill: none; stroke: var(--color-border-strong); stroke-width: 1.5; }
/* 外甩取消态描边升级 danger 混色：取消语义从"整盘变淡"升级为"盘缘报警"，
   滑回盘面即复原（transition 在 .disc-edge 未挂，状态切换瞬时更利落） */
.popup.is-cancel .disc-edge { stroke: color-mix(in srgb, var(--state-danger) 55%, var(--color-border-strong)); }

/* N40③①⑤ 磨砂花瓣盘：连续玻璃面退役，每枚扇区是独立圆角花瓣——
   stroke-width 6 + linejoin round + paint-order:stroke 的 SVG 膨胀接合技巧做出
   ≈3px 圆角（零 path 布尔运算）；皮肤批起描边不再与 fill 同色，而是"模块/类型色轨"
   （缝内一圈身份色轮廓），膨胀技巧与圆角效果原样保留；大间隙（VIS.padDeg 4.2°）
   即分界语言，刻度线退役。激活 = primary 软底 + 辉光 + 8px 顶出；
   有激活者时其余花瓣 fill/stroke 半透让位（"选中块拿起、全场退后"）。 */
.sector {
  pointer-events: auto;
  cursor: pointer;
  fill: var(--petal);
  /* 描边即"模块色轨"：色源 = 皮肤取色的 --tint > 类型 token --type-c > muted 兜底，
     按皮肤描边强度 (--wf-edge) 混进瓣色——膨胀描边技巧不变，只是缝内轮廓有了身份 */
  stroke: color-mix(in srgb, var(--tint, var(--type-c, var(--color-text-muted))) var(--wf-edge, 38%), var(--petal));
  stroke-width: 6;
  stroke-linejoin: round;
  paint-order: stroke;
  transition:
    fill var(--motion-fast) var(--ease-pop),
    stroke var(--motion-fast) var(--ease-pop),
    transform var(--motion-fast) var(--ease-pop),
    fill-opacity var(--motion-fast) ease,
    stroke-opacity var(--motion-fast) ease,
    filter var(--motion-fast) ease;
  animation: sector-in 120ms ease-out backwards; /* 错峰淡入：delay 由模板按序下发 */
}
@keyframes sector-in {
  from { opacity: 0; }
  to { opacity: 1; }
}
/* 类型轨描边色（"跟随模块色"关/取不到色时的回落身份色，全走既有语义 token） */
.sector.t-exe { --type-c: var(--color-primary); }
.sector.t-command { --type-c: var(--state-information); }
.sector.t-route { --type-c: var(--state-positive); }
.sector.t-group { --type-c: var(--state-warning); }
.sector.is-active {
  fill: var(--petal-on);
  stroke: color-mix(in srgb, var(--color-primary) 35%, var(--petal-on));
  filter: drop-shadow(0 3px 6px var(--color-primary-glow));
}
.sector.is-pinned {
  fill: var(--petal-pin);
  stroke: var(--color-primary);
  stroke-width: 1.25;
  paint-order: normal;
}
/* 全场让位：主环/帽带有激活者时，未激活花瓣降半档 */
.sectors.has-active .sector:not(.is-active),
.cap.has-active .cap-sector:not(.is-active) {
  fill-opacity: 0.5;
  stroke-opacity: 0.5;
}

/* 子环帽带扇区：帽带底环之上的独立花瓣，弹性外扩入场 */
.cap {
  transform-origin: 50% 50%;
  animation: cap-in 150ms cubic-bezier(0.16, 1, 0.3, 1);
}
@keyframes cap-in {
  from { transform: scale(0.94); opacity: 0; }
  to { transform: scale(1); opacity: 1; }
}
/* 帽带花瓣：同吃描边色轨（--tint > --type-c > muted），混入基色换 cap-petal */
.cap-sector {
  fill: var(--cap-petal);
  stroke: color-mix(in srgb, var(--tint, var(--type-c, var(--color-text-muted))) var(--wf-edge, 38%), var(--cap-petal));
}
.cap-sector.is-active {
  fill: var(--petal-on);
  stroke: color-mix(in srgb, var(--color-primary) 35%, var(--petal-on));
}
.cap-rim-accent {
  fill: none;
  stroke: var(--color-primary);
  stroke-width: 1.5; /* N40③②：提示弧收细，级联归属让位给 primary 软底底环 */
  stroke-linecap: round;
  opacity: 0.85;
}

/* 帽带底环（②）：primary 软底的收圆帽粗弧——弧宽由模板按帽带绘制带厚度下发，
   级联归属自带主色血统（替代旧 surface-hover 直角环段） */
.cap-band {
  fill: none;
  stroke: color-mix(in srgb, var(--color-primary) 10%, var(--surface-panel));
  stroke-linecap: round;
}

/* 盘缘连续亮环（①）：花瓣外缘与盘缘之间的渐变环带，替头发丝反光 glint；
   皮肤批起顶档染主色三分——盘上第一圈反光是色板色不是灰，盘面不再单调 */
.rim-stop-hi { stop-color: color-mix(in srgb, var(--color-primary) 30%, var(--surface-selected)); }
.rim-stop-lo { stop-color: var(--surface-hover); stop-opacity: 0.35; }
.disc-rim {
  fill: none;
  stroke: url(#qm-rim-grad);
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

/* hub 仪表核（⑥）：磨砂面独立浮起（与花瓣内缘以 80-64=16px 缝隙分离），
   虚线环退役；槽位刻度沿 r50→57 一圈，指针式仪表语言点亮当前悬停槽 */
.hub-face {
  fill: color-mix(in srgb, var(--surface-panel) 85%, transparent);
  stroke: var(--color-border-strong);
  stroke-width: 1.25;
}
.hub-ticks line {
  stroke: var(--color-border-strong);
  stroke-width: 2;
  stroke-linecap: round;
  opacity: 0.5;
  transition: stroke var(--motion-fast) ease, opacity var(--motion-fast) ease;
}
.hub-ticks line.is-lit {
  stroke: var(--color-primary);
  opacity: 1;
}
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
  transition: color var(--motion-fast) ease, transform var(--motion-fast) var(--ease-pop);
}
.sector-btn.is-active {
  color: var(--color-primary);
  /* 径向外顶由模板内联 transform 下发（与扇区面同向量）；这里只留微放大基调 */
}
.sector-btn:focus-visible {
  outline: 2px solid var(--color-primary);
  outline-offset: 1px;
}
/* 图标座（③）：描边井升级为磨砂浮起座——明色 panel≈85% 半透 + 强描边 + 顶光/
   底影；暗色以 text 实值混出 frost 底 + 白档描边（旧"反向抬升一档面板"补丁废除，
   磨砂语言避免贴片感）。激活座挂 primary 描边 + 软底 + ⑤ 同款辉光。 */
.sector-icon {
  width: var(--d-well, 32px);
  height: var(--d-well, 32px);
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  background: color-mix(in srgb, var(--surface-panel) 85%, transparent);
  border: 1px solid var(--color-border-strong);
  box-shadow:
    inset 0 1px 0 color-mix(in srgb, var(--surface-soft) 55%, transparent),
    0 2px 4px color-mix(in srgb, var(--overlay-mask) 22%, transparent);
  transition: background var(--motion-fast) ease, border-color var(--motion-fast) ease, filter var(--motion-fast) ease;
}
[data-theme='dark'] .sector-icon {
  background: color-mix(in srgb, var(--color-text) 8%, transparent);
  border-color: color-mix(in srgb, var(--color-text) 16%, transparent);
}
.sector-btn.is-active .sector-icon {
  background: color-mix(in srgb, var(--color-primary) 16%, transparent);
  border-color: var(--color-primary);
  filter: drop-shadow(0 3px 6px var(--color-primary-glow));
}
.sector-name {
  max-width: 100%;
  font-size: var(--d-name, 11px);
  line-height: 1.3;
  font-weight: 600;
  color: var(--color-text);
  /* 单行省略（N40③④）：扇区重心归图标座，全称交 hub 大读数 + title 兜底；
     两行折行（N6 F1 v2）让位给间隙变大后的留白秩序 */
  display: block;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
/* 分组计数徽标（N6 F3 → N40③④）：▸N 文字改圆形徽标，座外上角只报数 */
.sector-caret {
  position: absolute;
  top: 1px;
  right: 0;
  min-width: 14px;
  height: 14px;
  padding: 0 3px;
  box-sizing: border-box;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border: 1px solid var(--color-primary);
  border-radius: var(--radius-pill);
  background: var(--surface-panel);
  font-size: 9px;
  font-weight: 700;
  line-height: 1;
  color: var(--color-primary);
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
  font-size: 14px; /* N40③⑥：hub 干活的读数字段——全称大字号（14/700） */
  font-weight: 700;
  line-height: 1.35;
  overflow-wrap: break-word;
}
.hub-state-error { color: var(--state-danger); }
.hub-kind {
  font-size: 10px;
  color: var(--color-primary);
  background: color-mix(in srgb, var(--color-primary) 12%, transparent);
  border-radius: var(--radius-pill);
  padding: 0 7px;
}
.hub-meta {
  font-size: 10px;
  color: var(--color-text-subtle);
}
.hub-esc { opacity: 0.75; }
/* 常驻"点按中心收起"小字：贴 hub 面底缘，pointer-events 由 .hub > * 统一归零，
   点击穿到下层 hub-hit，不入命中面 */
.hub-hint {
  position: absolute;
  left: 6px;
  right: 6px;
  bottom: 13px;
  font-size: 9px;
  line-height: 1;
  color: var(--color-text-subtle);
  opacity: 0.7;
  white-space: nowrap;
}
</style>
