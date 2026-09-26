<script setup lang="ts">
// 托盘/轮盘条目编辑面（N5-C1 抽自设置页 TraySection；轮盘独立批按 scope 分账）：
// 候选条目勾选 / 外部程序添加 / 二级分组编辑 / 排序与移除 / 点上格互换交互全保留。
//
// —— 双账（①lane 冻结契约）：scope prop 切换两本独立账——'tray'（默认，设置页
//   TraySection 一字不改也行为如旧）打 Get/SetTrayMenu；'wheel'（QuickMenuView 宿主
//   接线属第四 lane）打 Get/SetWheelMenu。Get/Set 一律经 fetchMenu/pushMenu 成对切换，
//   禁止串账。契约镜像纪律：bindings 尚未随 Go 侧新 RPC 再生（再生归主会话统一收口），
//   再生前 GetWheelMenu/SetWheelMenu 成员不存在——循 WSLView/translucenttb 先例以本地
//   交叉型补函数面直调；spec 侧 vi.mock 全顶替；运行期 wheel 面在第四 lane 接线时
//   绑定必已再生，提前误用只会落失败 toast，护栏自洽。
//
// —— 实时保存（机主 2026-09-26：候选条目只要改变就实时保存）：保存钮与脏标记整退。
//   结构性操作（勾选候选 / 移除 / 排序 / 互换 / 分组增减 / 外部程序添加）saveNow() 即时落盘；
//   条目/分组/子条目名称文本走 800ms 防抖 saveDebounced() + blur 即时（saveNow 清防抖头）。
//   在途写合流：一笔写未回时再改动 → 尾随标记，本轮成功后重新取全量快照再写，绝不丢最后一次；
//   每笔成功落盘各上抛一次 saved（宿主每次刷的都是更新的真相）。保存失败语义保住：toast 中文错误 + 重拉服务端真相（本地草稿
//   与尾随合流作废，绝不本地假成功）。成功静默不 toast——每次改动都鸣笛是噪音不是反馈。
//   草稿门：分组名为空或分组零子条目是后端 Set 校验硬拒项，若照发则每次失败回滚重拉会
//   绞杀正在编辑的分组草稿——saveNow 先做本地同规则体检，草稿不全时暂缓落盘（不发送必拒
//   的账），页脚状态条如实说明原因，补全后的下一次操作自然重试。
//   账目代次 epoch：重载/换账递增，写回程后旧账的尾随合流与 saved 上抛作废，防串账。
//
// —— 候选目录收口（机主同批）：候选区只供 command 族（模块 TrayCommands「启动/打开 XX」，
//   含 webapp 动态暴露的 webapp/open:<id> 网页应用）；route 族（跳模块 hanxi 界面的页面
//   导航）整族退出。分类判据实读后端 elevate_tray.go：option.type='command' ⇔
//   registry.ListTrayCommands（ref "moduleId/commandId"）、'route' ⇔ extapi.GetEnabledNavs
//   （ref 前端路由）——按 kind 字段判，绝不按 label 字面猜。存量 route 条目照常渲染与管理
//   （行上标「不再供选」），不自动删除不报错。后端源收口更干净但撞 ①lane，本轮前端做。
//
// —— 勾选形制（机主同批「多选框不好看」）：候选勾选由原生裸形改自绘方框，hover/checked/
//   focus-visible 三态 + 勾 pop 动效（--motion-fast）；勾前景走 --color-on-accent 单档
//   契约（N37：彩底前景禁借 on-primary/text-inverse）；卡片整体选中反馈循 WslUsbPanel
//   .pick-option:has() 先例。勾选即落盘：checked 先随本地账渲染、写账在途失败时重拉翻转回来。
//
// 已知病灶处置在账：isOptionAdded/toggleOption 仍只判顶层——组内判重要求候选勾选具备
// 「该选项存在于哪个组」的交互语义（取消勾选从哪删？），属交互重设计非小补丁，本轮缓做；
// tray/wheel 两账独立后，判重各自账内顶层语义仍成立、未因分账恶化。
// 按钮家族不保留 scoped 副本，直接落回 components.css 全局 :where 标准形。
import { ref, computed, watch, onMounted, onBeforeUnmount, nextTick } from 'vue'
import * as AppAPI from '../../../bindings/hanxi/internal/app'
import type { TrayMenuOption } from '../../../bindings/hanxi/internal/app/models'
import type { TrayMenuItem } from '../../../bindings/hanxi/internal/settings/models'
import { getErrorMessage } from '../../utils/errors'
import { useToast } from '../../composables/useToast'
import AppIcon from '../ui/AppIcon.vue'

const props = withDefaults(defineProps<{
  /** 编辑账目：'tray'=托盘右键菜单（默认，设置页宿主）；'wheel'=轮盘独立账。 */
  scope?: 'tray' | 'wheel'
}>(), { scope: 'tray' })

const emit = defineEmits<{
  (e: 'saved'): void
}>()

// 契约镜像交叉型：GetWheelMenu/SetWheelMenu 与 TrayMenu 面同形；绑定再生后本组
// 声明可删、直调即天然合法（字段与 settings.TrayMenuItem 逐一对齐）。
type WheelMenuAPI = {
  GetWheelMenu(): Promise<TrayMenuItem[]>
  SetWheelMenu(items: TrayMenuItem[]): Promise<void>
}
const svc = AppAPI.AppService as typeof AppAPI.AppService & WheelMenuAPI

const isWheel = computed(() => props.scope === 'wheel')
// 文案词头：'托盘'/'轮盘'（空态与加载态沿用旧字面「托盘条目」等，宿主 spec 在册依赖）
const scopeWord = computed(() => (isWheel.value ? '轮盘' : '托盘'))
const menuName = computed(() => `${scopeWord.value}菜单`)

function fetchMenu(): Promise<TrayMenuItem[]> {
  return isWheel.value ? svc.GetWheelMenu() : svc.GetTrayMenu()
}
function pushMenu(items: TrayMenuItem[]): Promise<void> {
  return isWheel.value ? svc.SetWheelMenu(items) : svc.SetTrayMenu(items)
}

// —— N5-C3 点上格互换（读侧：宿主把预览选中扇区的身份喂进来定位行；写侧：行内互换钮）——
// 身份匹配（type+hint / 组按 label）而非下标换算：编辑列里可能存在被后端 wheelView
// 滤掉的停用模块行，相对序会错位；扁平拍平形态下组子条目占多扇区，也一律归位到
// 组行。映射逻辑收在本组件（唯一持有 trayItems 的人），宿主只转发"用户点了哪个扇区"。
const selectedRow = ref<number | null>(null)

const rowRefs = ref<Record<number, HTMLElement | undefined>>({})
function setRowRef(i: number, el: unknown) {
  rowRefs.value[i] = el as HTMLElement | undefined
}

const normHint = (s: string) => s.trim().toLowerCase().replace(/\//g, '\\')
function leafMatches(r: TrayMenuItem, type: string, hint: string): boolean {
  return r.type === type && normHint(r.type === 'exe' ? r.path : r.ref) === normHint(hint)
}
function rowIndexOfSector(type: string, hint: string, label: string): number | null {
  for (let i = 0; i < trayItems.value.length; i++) {
    const r = trayItems.value[i]
    if (r.type === 'group') {
      if (type === 'group' && r.label === label) return i
      if ((r.children ?? []).some((ch) => leafMatches(ch, type, hint))) return i
      continue
    }
    if (leafMatches(r, type, hint)) return i
  }
  return null
}

/** 宿主预览点击入口：定位并高亮对应编辑行（滚到可见）；命中返回 true。 */
function locate(sector: { type: string; hint: string; label: string }): boolean {
  const i = rowIndexOfSector(sector.type, sector.hint ?? '', sector.label ?? '')
  if (i == null) return false
  selectedRow.value = i
  nextTick(() => rowRefs.value[i]?.scrollIntoView({ block: 'nearest', behavior: 'smooth' }))
  return true
}

/** 清除选中（宿主在条目列表刷新后调用，防错位选中残留）。 */
function clearSelection() {
  selectedRow.value = null
}
defineExpose({ locate, clearSelection })

const { showToast } = useToast()

// —— 实时保存状态机（防抖/合流/回滚）——
const TEXT_DEBOUNCE_MS = 800

const trayLoading = ref(true)
const trayOptions = ref<TrayMenuOption[]>([])
const trayItems = ref<TrayMenuItem[]>([])
const autosaving = ref(false)
/** 草稿不完整挂起原因（null=账目可写）：空名/零子条目分组，后端 Set 必拒项。 */
const saveBlockHint = ref<string | null>(null)
const pickingExe = ref(false)
const exeForm = ref({ path: '', args: '', label: '' })

let debounceTimer: ReturnType<typeof setTimeout> | null = null
let inFlight: Promise<void> | null = null
let trailing = false
/** 账目代次：重载/换账递增；写回程后旧账的尾随与 saved 上抛作废（防跨账串写）。 */
let epoch = 0

function clearTextTimer() {
  if (debounceTimer) {
    clearTimeout(debounceTimer)
    debounceTimer = null
  }
}

// 全量快照：落盘载荷在调用时刻冻结（响应式数组直接交运会让在途写的 mock/序列化
// 观察到后续改动，尾随合流的每轮也须拿各自时刻的快照）。
function snapshotPayload(): TrayMenuItem[] {
  return JSON.parse(JSON.stringify(trayItems.value)) as TrayMenuItem[]
}

/** 本地体检 = 后端 SetTrayMenu 对 group 的硬校验镜像（elevate_tray.go）：草稿不全不发必拒账。 */
function draftBlocker(): string | null {
  for (const it of trayItems.value) {
    if (it.type !== 'group') continue
    if (!it.label?.trim()) return '分组名为空'
    if (!it.children?.length) return '分组还没有子条目'
  }
  return null
}

/** 结构性改动入口：即时落盘（草稿门内挂起等补全；在途则尾随合流）。 */
function saveNow() {
  clearTextTimer()
  saveBlockHint.value = draftBlocker()
  if (saveBlockHint.value) return
  if (inFlight) {
    trailing = true
    return
  }
  autosaving.value = true
  const myEpoch = epoch
  inFlight = writeLoop(myEpoch).finally(() => { inFlight = null })
}

/** 文本改动入口：800ms 防抖落盘；blur 经 saveNow 清定时器即时抢跑。 */
function saveDebounced() {
  clearTextTimer()
  debounceTimer = setTimeout(() => {
    debounceTimer = null
    saveNow()
  }, TEXT_DEBOUNCE_MS)
}

async function writeLoop(myEpoch: number) {
  try {
    for (;;) {
      trailing = false
      try {
        await pushMenu(snapshotPayload())
      } catch (e: unknown) {
        showToast(`保存${menuName.value}失败: ${getErrorMessage(e)}`)
        await loadMenu() // 回滚重拉服务端真相：本地草稿作废、尾随合流随之作废
        return
      }
      if (myEpoch !== epoch) return // 往返期间已换账/重载：旧账结果不上抛、不再尾随
      saveBlockHint.value = draftBlocker() // 在途期间的改动可能又造出不全草稿
      emit('saved') // 本轮写已真实落盘：上抛供宿主刷预览（如轮盘页 ListItems）
      if (saveBlockHint.value || !trailing) return // 草稿门挂起尾随：补全草稿的下一次操作自然续写，不丢改动
    }
  } finally {
    autosaving.value = false
  }
}

async function loadMenu() {
  epoch++
  clearTextTimer()
  trailing = false
  saveBlockHint.value = null
  selectedRow.value = null
  openGroup.value = -1
  trayLoading.value = true
  try {
    const [opts, items] = await Promise.all([svc.ListTrayMenuOptions(), fetchMenu()])
    trayOptions.value = opts ?? []
    trayItems.value = (items ?? []).map(i => ({ ...i }))
  } catch (e: unknown) {
    showToast(`获取${scopeWord.value}配置失败: ${getErrorMessage(e)}`)
  } finally {
    trayLoading.value = false
  }
}

onMounted(loadMenu)
onBeforeUnmount(clearTextTimer)
watch(() => props.scope, () => { void loadMenu() })

// 候选目录收口：route（跳 hanxi 界面的页面导航）整族退出候选，只供 command 族
//（「启动/打开 XX」模块命令 + webapp/open:<id> 网页应用）。判据见文件头实证注释。
const candidateOptions = computed(() => trayOptions.value.filter(o => o.type !== 'route'))

function swapRows(i: number) {
  const j = selectedRow.value
  if (j == null || j < 0 || j >= trayItems.value.length || j === i) return
  const arr = trayItems.value
  ;[arr[i], arr[j]] = [arr[j], arr[i]]
  selectedRow.value = i // 选中跟着"扇区内容"走：互换后被点扇区的内容落在被点击的行 i
  openGroup.value = -1 // 与 moveItem 同纪律：下标错位防护
  saveNow()
}

// —— 二级分组编辑（group 条目：轮盘外扩子环，托盘呈现原生子菜单）——
const openGroup = ref(-1) // 展开编辑中的分组下标（-1 无；增删/移动行后复位防错位）
const childPick = ref<Record<number, string>>({}) // 各组"从候选添加"下拉的暂存键

function ensureKids(g: TrayMenuItem): TrayMenuItem[] {
  if (!g.children) g.children = []
  return g.children
}

function addGroup() {
  trayItems.value.push({ type: 'group', ref: '', path: '', args: '', label: '', enabled: true, children: [] })
  openGroup.value = trayItems.value.length - 1
  saveNow() // 空分组草稿：draftBlocker 挂起落盘并给出等待补全提示
}

function addChildFromOption(g: TrayMenuItem, gi: number) {
  const opt = candidateOptions.value.find(o => `${o.type}|${o.ref}` === childPick.value[gi])
  if (!opt) return
  ensureKids(g).push({ type: opt.type, ref: opt.ref, path: '', args: '', label: '', enabled: true })
  childPick.value[gi] = ''
  saveNow()
}

async function addChildExe(g: TrayMenuItem) {
  pickingExe.value = true
  try {
    const p = await AppAPI.AppService.PickExeFile()
    if (p) {
      ensureKids(g).push({ type: 'exe', ref: '', path: p, args: '', label: '', enabled: true }) // 取消时为空串
      saveNow()
    }
  } catch (e: unknown) {
    showToast(`打开文件选择框失败: ${getErrorMessage(e)}`)
  } finally {
    pickingExe.value = false
  }
}

function moveChild(g: TrayMenuItem, j: number, delta: number) {
  const kids = g.children ?? []
  const k = j + delta
  if (k < 0 || k >= kids.length) return
  ;[kids[j], kids[k]] = [kids[k], kids[j]]
  saveNow()
}

function removeChild(g: TrayMenuItem, j: number) {
  g.children?.splice(j, 1)
  saveNow() // 删空最后一子 → 草稿门挂起，等补子条目或整组移除
}

// 候选项默认标签索引：type|ref → label（行内自定义名为空时回退展示）。
// 用全量 trayOptions 而非收口后的候选目录：存量 route 条目回退默认名仍需在案。
const optionLabels = computed(() => {
  const map = new Map<string, string>()
  for (const opt of trayOptions.value) map.set(`${opt.type}|${opt.ref}`, opt.label)
  return map
})

function exeBaseName(p: string): string {
  const base = p.split(/[\\/]/).pop() || p
  return base.replace(/\.[^.\\/]+$/, '')
}

function defaultLabelFor(item: TrayMenuItem): string {
  if (item.type === 'exe') return exeBaseName(item.path) || '外部程序'
  return optionLabels.value.get(`${item.type}|${item.ref}`) || item.ref
}

function typeLabel(type: string): string {
  if (type === 'command') return '工具命令'
  if (type === 'route') return '页面'
  if (type === 'group') return '分组'
  return '外部程序'
}

function isOptionAdded(opt: TrayMenuOption): boolean {
  return trayItems.value.some(i => i.type === opt.type && i.ref === opt.ref)
}

function toggleOption(opt: TrayMenuOption) {
  const idx = trayItems.value.findIndex(i => i.type === opt.type && i.ref === opt.ref)
  if (idx >= 0) {
    trayItems.value.splice(idx, 1)
  } else {
    trayItems.value.push({ type: opt.type, ref: opt.ref, path: '', args: '', label: '', enabled: true })
  }
  saveNow()
}

function moveItem(i: number, delta: number) {
  const j = i + delta
  const arr = trayItems.value
  if (j < 0 || j >= arr.length) return
  ;[arr[i], arr[j]] = [arr[j], arr[i]]
  openGroup.value = -1 // 下标错位防护：移动后不再认定原展开组
  selectedRow.value = null // C3 选中同样按位置索引持有，移动后不作废即错指
  saveNow()
}

function removeItem(i: number) {
  trayItems.value.splice(i, 1)
  openGroup.value = -1
  selectedRow.value = null
  saveNow()
}

async function browseExe() {
  pickingExe.value = true
  try {
    const p = await AppAPI.AppService.PickExeFile()
    if (p) exeForm.value.path = p // 取消时为空串，保持现值
  } catch (e: unknown) {
    showToast(`打开文件选择框失败: ${getErrorMessage(e)}`)
  } finally {
    pickingExe.value = false
  }
}

function addExe() {
  const path = exeForm.value.path.trim()
  if (!path) {
    showToast('请先填写或选择程序路径')
    return
  }
  trayItems.value.push({
    type: 'exe',
    ref: '',
    path,
    args: exeForm.value.args.trim(),
    label: exeForm.value.label.trim(),
    enabled: true,
  })
  exeForm.value = { path: '', args: '', label: '' }
  saveNow()
}

</script>

<template>
  <div class="tray-editor">
    <div v-if="trayLoading" class="state-box">正在加载{{ scopeWord }}配置…</div>

    <template v-else>
      <!-- 当前菜单条目（顺序即右键菜单/轮盘显示顺序；group 行可展开子条目编辑） -->
      <div v-if="trayItems.length > 0" class="tray-list">
        <template v-for="(item, i) in trayItems" :key="`${item.type}|${item.ref}|${item.path}|${i}`">
          <!-- 分组行：轮盘二级扇区 / 托盘子菜单 -->
          <div v-if="item.type === 'group'" class="tray-row tray-row-group" :class="{ 'tray-selected': selectedRow === i }" :ref="el => setRowRef(i, el)">
            <div class="tray-row-main">
              <span class="tray-tag tray-tag-group">分组</span>
              <input
                class="tray-input tray-name"
                v-model="item.label"
                placeholder="分组名（必填）"
                maxlength="30"
                title="分组显示名：轮盘外扩子环标题与托盘子菜单标题"
                @blur="saveNow"
                @input="saveDebounced"
              />
              <code class="tray-ref">{{ (item.children?.length ?? 0) }} 个子条目 · 轮盘外扩子环 / 托盘子菜单</code>
            </div>
            <div class="setting-actions">
              <button class="btn btn-secondary btn-small" :class="{ 'tray-active': openGroup === i }" @click="openGroup = openGroup === i ? -1 : i">
                {{ openGroup === i ? '收起子条目' : '子条目' }}
              </button>
              <button v-if="selectedRow != null && selectedRow !== i" class="btn btn-secondary btn-small" @click="swapRows(i)" :title="`与选中的第 ${selectedRow + 1} 行互换位置（预览盘点对应扇区即选中）`">⇄ 互换</button>
              <button class="btn btn-secondary btn-small" :disabled="i === 0" @click="moveItem(i, -1)" title="上移" aria-label="上移"><AppIcon name="chevron-up" :size="14" /></button>
              <button class="btn btn-secondary btn-small" :disabled="i === trayItems.length - 1" @click="moveItem(i, 1)" title="下移" aria-label="下移"><AppIcon name="chevron-down" :size="14" /></button>
              <button class="btn btn-secondary btn-small tray-remove" @click="removeItem(i)" title="删除分组及其子条目"><AppIcon name="x" :size="14" /> 移除</button>
            </div>
          </div>
          <div v-if="item.type === 'group' && openGroup === i" class="tray-children">
            <div
              v-for="(ch, j) in item.children ?? []"
              :key="`child-${ch.type}|${ch.ref}|${ch.path}|${j}`"
              class="tray-child-row"
            >
              <span class="tray-tag" :class="`tray-tag-${ch.type}`">{{ typeLabel(ch.type) }}</span>
              <span v-if="ch.type === 'route'" class="tray-tag tray-tag-retired" title="页面跳转类条目已退出候选目录，历史配置仍可正常使用与管理">不再供选</span>
              <input
                class="tray-input tray-name"
                v-model="ch.label"
                :placeholder="defaultLabelFor(ch)"
                maxlength="30"
                title="自定义显示名，留空使用默认名称"
                @blur="saveNow"
                @input="saveDebounced"
              />
              <code class="tray-ref" :title="ch.type === 'exe' ? ch.path : ch.ref">
                {{ ch.type === 'exe' ? ch.path : ch.ref }}<template v-if="ch.type === 'exe' && ch.args"> {{ ch.args }}</template>
              </code>
              <div class="setting-actions">
                <button class="btn btn-secondary btn-small" :disabled="j === 0" @click="moveChild(item, j, -1)" title="子条目上移" aria-label="子条目上移"><AppIcon name="chevron-up" :size="14" /></button>
                <button class="btn btn-secondary btn-small" :disabled="j >= (item.children?.length ?? 0) - 1" @click="moveChild(item, j, 1)" title="子条目下移" aria-label="子条目下移"><AppIcon name="chevron-down" :size="14" /></button>
                <button class="btn btn-secondary btn-small tray-remove" @click="removeChild(item, j)" title="移除子条目" aria-label="移除子条目"><AppIcon name="x" :size="14" /></button>
              </div>
            </div>
            <div v-if="!(item.children?.length)" class="tray-child-empty">分组还没有子条目（补全前自动保存暂缓，空分组直接保存也会被拒绝）：从下方添加。</div>
            <div class="tray-child-add">
              <select class="tray-input tray-child-select" v-model="childPick[i]">
                <option value="" disabled>从候选条目选择…</option>
                <option v-for="opt in candidateOptions" :key="`${opt.type}|${opt.ref}`" :value="`${opt.type}|${opt.ref}`">
                  {{ opt.label }}{{ opt.moduleName ? `（${opt.moduleName}）` : '' }}
                </option>
              </select>
              <button class="btn btn-secondary btn-small" :disabled="!childPick[i]" @click="addChildFromOption(item, i)"><AppIcon name="plus" :size="14" /> 子条目</button>
              <button class="btn btn-secondary btn-small" :disabled="pickingExe" @click="addChildExe(item)"><AppIcon name="folder" :size="14" /> 外部程序…</button>
            </div>
          </div>
          <!-- 普通叶子行（勿用 v-else：与分组行之间隔着子条目编辑面板，会断链误渲染） -->
          <div v-if="item.type !== 'group'" class="tray-row" :class="{ 'tray-selected': selectedRow === i }" :ref="el => setRowRef(i, el)">
            <div class="tray-row-main">
              <span class="tray-tag" :class="`tray-tag-${item.type}`">{{ typeLabel(item.type) }}</span>
              <span v-if="item.type === 'route'" class="tray-tag tray-tag-retired" title="页面跳转类条目已退出候选目录，历史配置仍可正常使用与管理">不再供选</span>
              <input
                class="tray-input tray-name"
                v-model="item.label"
                :placeholder="defaultLabelFor(item)"
                maxlength="30"
                title="自定义菜单显示名，留空使用默认名称"
                @blur="saveNow"
                @input="saveDebounced"
              />
              <code class="tray-ref" :title="item.type === 'exe' ? item.path : item.ref">
                {{ item.type === 'exe' ? item.path : item.ref }}<template v-if="item.type === 'exe' && item.args"> {{ item.args }}</template>
              </code>
            </div>
            <div class="setting-actions">
              <button v-if="selectedRow != null && selectedRow !== i" class="btn btn-secondary btn-small" @click="swapRows(i)" :title="`与选中的第 ${selectedRow + 1} 行互换位置（预览盘点对应扇区即选中）`">⇄ 互换</button>
              <button class="btn btn-secondary btn-small" :disabled="i === 0" @click="moveItem(i, -1)" title="上移" aria-label="上移"><AppIcon name="chevron-up" :size="14" /></button>
              <button class="btn btn-secondary btn-small" :disabled="i === trayItems.length - 1" @click="moveItem(i, 1)" title="下移" aria-label="下移"><AppIcon name="chevron-down" :size="14" /></button>
              <button class="btn btn-secondary btn-small tray-remove" @click="removeItem(i)" :title="`从${menuName}移除`"><AppIcon name="x" :size="14" /> 移除</button>
            </div>
          </div>
        </template>
      </div>
      <div v-else class="state-box">尚未配置{{ scopeWord }}条目：在下方「可选条目」中勾选、添加外部程序，或新建分组。</div>

      <div class="tray-tools">
        <button class="btn btn-secondary btn-small" @click="addGroup"><AppIcon name="plus" :size="14" /> 新建分组（轮盘二级扇区）</button>
        <span class="hint">分组在轮盘中悬停展开外扩子环（点击可钉住；可在快捷菜单页关闭二级、改为拍平），在托盘中呈现子菜单。子条目建议 ≤4，超过 8 轮盘仅显示前 8 项。</span>
      </div>

      <!-- 外部程序添加 -->
      <div class="exe-form">
        <span class="exe-form-title">添加外部程序</span>
        <div class="exe-form-row">
          <input class="tray-input tray-exe-path" v-model="exeForm.path" placeholder="程序绝对路径，如 C:\Tools\app.exe 或桌面快捷方式 .lnk" />
          <button class="btn btn-secondary btn-small" :disabled="pickingExe" @click="browseExe"><AppIcon name="folder" :size="14" /> 浏览…</button>
        </div>
        <div class="exe-form-row">
          <input class="tray-input tray-exe-args" v-model="exeForm.args" placeholder="启动参数（可选，空格分隔，引号可包空格）" />
          <input class="tray-input tray-exe-label" v-model="exeForm.label" placeholder="菜单名称（可选）" maxlength="30" />
          <button class="btn btn-secondary btn-small" @click="addExe"><AppIcon name="plus" :size="14" /> 添加</button>
        </div>
      </div>

      <!-- 候选条目目录（route 页面导航族已退出供选，见 script 头实证注释） -->
      <div class="option-block">
        <span class="exe-form-title">可选条目</span>
        <div class="option-grid">
          <label v-for="opt in candidateOptions" :key="`${opt.type}|${opt.ref}`" class="option-item" :title="opt.moduleName ? `来自模块：${opt.moduleName}` : opt.ref">
            <input type="checkbox" class="switch" :checked="isOptionAdded(opt)" @change="toggleOption(opt)" />
            <span class="option-label">{{ opt.label }}</span>
          </label>
        </div>
        <div v-if="candidateOptions.length === 0" class="state-box">暂无候选条目（托管工具命令与网页应用均为空）。</div>
      </div>

      <div class="tray-footer">
        <span class="hint">改动实时自动保存并即时生效；条目对应模块被禁用时，点击菜单项会收到错误提示。</span>
        <span class="autosave-state" :class="{ 'autosave-pending': !!saveBlockHint }">
          <template v-if="saveBlockHint">自动保存等待补全：{{ saveBlockHint }}</template>
          <template v-else-if="autosaving">自动保存中…</template>
          <template v-else>实时自动保存已开启</template>
        </span>
      </div>
    </template>
  </div>
</template>

<style scoped>
.tray-editor { display: flex; flex-direction: column; gap: 12px; }

.tray-list { display: flex; flex-direction: column; gap: 8px; }
.tray-row {
  background: var(--surface-page); border: 1px solid var(--color-border); border-radius: var(--radius-control);
  padding: 8px 12px; display: flex; justify-content: space-between; align-items: center; gap: 12px; flex-wrap: wrap;
}
.tray-row-group { border-color: var(--color-border-strong); background: var(--surface-panel); }
/* N5-C3：预览盘选中扇区对应行的双向定位高亮 */
.tray-selected { border-color: var(--color-primary); box-shadow: inset 3px 0 0 var(--color-primary); }
.tray-row-main { display: flex; align-items: center; gap: 8px; min-width: 0; flex: 1; }
.tray-tag {
  font-size: var(--text-micro); color: var(--color-text-muted); background: var(--surface-hover);
  padding: 1px 6px; border-radius: 4px; white-space: nowrap; border: 1px solid var(--color-border);
}
.tray-tag-command { color: var(--color-primary); border-color: var(--color-primary); }
.tray-tag-group { color: var(--color-primary); border-color: var(--color-primary); }
/* 存量 route 条目的"不再供选"如实小标：虚线中性形制，不引入新色、不与危险/主色态混淆 */
.tray-tag-retired { border-style: dashed; color: var(--color-text-subtle); }
.tray-active { color: var(--color-primary); border-color: var(--color-primary); }
.tray-ref {
  font-family: var(--font-mono); font-size: var(--text-xs); color: var(--color-text-subtle);
  white-space: nowrap; overflow: hidden; text-overflow: ellipsis; min-width: 0;
}
.tray-input {
  padding: 5px 8px; border: 1px solid var(--color-border); border-radius: 6px;
  background: var(--surface-panel); color: var(--color-text); font-size: var(--text-sm);
}
.tray-name { width: 170px; flex-shrink: 0; }
.tray-remove:hover:not(:disabled) { border-color: var(--state-danger); color: var(--state-danger); }

/* 分组子条目编辑面板 */
.tray-children {
  margin: -4px 0 4px 22px; padding: 8px 10px; display: flex; flex-direction: column; gap: 6px;
  border: 1px dashed var(--color-border-strong); border-radius: var(--radius-control); background: var(--surface-page);
}
.tray-child-row { display: flex; align-items: center; gap: 8px; min-width: 0; flex-wrap: wrap; }
.tray-child-empty { font-size: var(--text-xs); color: var(--state-danger); }
.tray-child-add { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.tray-child-select { min-width: 200px; }

.tray-tools { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }

.exe-form {
  background: var(--surface-page); border: 1px dashed var(--color-border); border-radius: var(--radius-control);
  padding: 10px 12px; display: flex; flex-direction: column; gap: 8px;
}
.exe-form-title { font-size: var(--text-sm); font-weight: 600; color: var(--color-text); }
.exe-form-row { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; }
.tray-exe-path { flex: 1; min-width: 0; }
.tray-exe-args { flex: 1; min-width: 0; }
.tray-exe-label { width: 170px; flex-shrink: 0; }

.option-block { display: flex; flex-direction: column; gap: 8px; }
.option-grid {
  display: grid; grid-template-columns: repeat(auto-fill, minmax(260px, 1fr));
  gap: 6px 12px;
}
.option-item {
  background: var(--surface-page); border: 1px solid var(--color-border); border-radius: var(--radius-control);
  padding: 7px 10px; display: flex; align-items: center; gap: 8px; cursor: pointer; min-width: 0;
  transition: border-color var(--motion-base), background-color var(--motion-base);
}
.option-item:hover { background: var(--surface-hover); }
/* 卡片级选中反馈：循 WslUsbPanel .pick-option:has(input:checked) 先例，primary 描边即身份 */
.option-item:has(.switch:checked) { border-color: var(--color-primary); background: var(--surface-hover); }
.option-label { font-size: var(--text-sm); color: var(--color-text); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }

/* 候选勾选自绘方框：三态齐整 + 勾 pop；勾前景 --color-on-accent 彩底单档契约（禁借 on-primary）；
   18px 沿用全站勾选尺寸档（QuickMenuView"沿用本页原 18px 档"注释同族） */
.switch {
  appearance: none;
  width: 18px;
  height: 18px;
  flex: none;
  cursor: pointer;
  display: grid;
  place-items: center;
  border: 1px solid var(--color-border);
  border-radius: 4px;
  background: var(--surface-panel);
  transition: border-color var(--motion-base), background-color var(--motion-base);
}
.option-item:hover .switch:not(:checked) { border-color: var(--color-border-strong); }
.switch:checked { border-color: var(--color-primary); background-color: var(--color-primary); }
.switch:checked::after {
  content: '';
  width: 9px;
  height: 4px;
  border-left: 2px solid var(--color-on-accent);
  border-bottom: 2px solid var(--color-on-accent);
  transform: rotate(-45deg) translate(0.5px, -1px);
  animation: option-check-pop var(--motion-fast) ease-out;
}
.switch:focus-visible { outline: 2px solid var(--color-primary); outline-offset: 2px; }
@keyframes option-check-pop {
  from { transform: rotate(-45deg) translate(0.5px, -1px) scale(0.4); opacity: 0; }
}

.tray-footer { display: flex; justify-content: space-between; align-items: center; gap: 16px; flex-wrap: wrap; }
.hint { font-size: var(--text-sm); color: var(--color-text-muted); }
/* 实时保存状态条：常态中性、写中提示、草稿挂起警示（warning 即有色档，语义如实） */
.autosave-state { font-size: var(--text-xs); color: var(--color-text-subtle); white-space: nowrap; }
.autosave-pending { color: var(--state-warning); }
</style>
