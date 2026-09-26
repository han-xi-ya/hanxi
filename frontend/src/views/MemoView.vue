<script setup lang="ts">
// 随手记 v2「摊开的笔记本」（2026-09-26 推倒重做，替换同日午间的"速记条+分组卡片群岛+双浮层"版）。
//
// 病根复盘（旧版定量诊断）：1200×780 窗内内容区实得约 834×701，旧骨架的页头副标题(~105px)
// +速记条面板(~110px)+过滤条面板(~100px)+三道 14px 间距合计 357px——首屏 51% 还没见到一条便签；
// 网格卡每行两张、卡高约 220px（其中摘要文本至多 93px，56% 是卡框装饰），一屏只容 4 条；
// 六档时间分组把 20 条的短库切成 5 段、光段头吃 150px；书写面有三个（速记条/编辑模态/悬浮卡）
// 加阅读浮层一个，"到底在哪写字"自成谜题。留言板 v4 同款病：页面长得像表单合集。
//
// v2 概念——一本摊开的笔记本，列表即主体：
//   顶栏一行轻工具条（搜索/标签/置顶/溢出菜单；无 panel 容器、无统计字、无页头四连钮）；
//   左索引栏＝笔记本侧边速记标：贴线行式条目（色彩封边 + 标题 + 首行摘要 + 时间/标签丸），
//     时间分组语义六档原样保留，形态从"切段卡片群岛"降为吸顶小字档；栏底折叠收纳
//     「悬浮速记卡」热键配置（低频件不再占独立大面板）；
//   右页面板＝正在书写/阅读的那一页：顶部一行克制的速记入口（"想到即存一次回车"语义原样），
//     阅读态（MD 自动渲染/纯代码等宽原文/遮罩圆点三分支照旧）与书写态（书写/预览分段照旧）
//     合并为同一页的两面——详情浮层退役，消灭"点开→再点编辑→再弹窗"的三级跳转与双壳复读；
//   左右分栏选型论证：内联展开会推挤后续条目、把编辑器塞进 ≤410px 的列宽，长代码/MD 预览
//     需要稳定版心；分栏让"列表+正在编辑的一条"在 834px 内容宽里各占全高、互不打断；
//     2560 下整页夹 --container-workbench 档、正文版心再封顶 72ch，笔记本不被拉散。
//
// 数据契约零改动：List/GetStats/Create/Update/Delete/TogglePin/ToggleMask/ClearAll +
// 速记卡三件（GetQuickSheetState/SetQuickSheetHotkey/ShowQuickSheet）共十一方法面照旧；
// memo:changed 重拉、搜索 350ms 防抖 + 序号守卫、卸载作废在飞请求等纪律逐字保留；
// 一键全删/导出全库（919c526 能力）原样，入口退工具条右端溢出菜单，danger 强确认链一字不动。
// 敏感遮罩纪律零回退：masked 条目索引行/页面板/预览三处不落明文（与旧版同口径：标题仍示人，
// "标题也是明文面"的从严收编属 MemoCard 线，接线时一并裁决）；masked 无复制/导出钮；
// 全库导出默认剔除敏感条目且确认框明说排除条数。
// 组件库 M4（components/memo/MemoCard·MemoToolbar·memoMetrics，f61060c）已奠基：本视图行式索引
// 与轻工具条即按该契约的观感先行，接线收口（删本文件内 dayBucket/groupedMemos 等重复纯函数）
// 另路执行，本轮不 import 在途重排中的组件件。
// 命名口径：本视图一律自称「随手记」。
import { computed, defineComponent, h, nextTick, onMounted, onUnmounted, ref, shallowRef, watch } from 'vue'
import * as MemoAPI from '../../bindings/hanxi/internal/modules/memo'
import type {
  MemoItem,
  MemoFilter,
  MemoStats,
  QuickSheetState,
} from '../../bindings/hanxi/internal/modules/memo/models'
import { getErrorMessage } from '../utils/errors'
import { fmtDate } from '../utils/format'
import { looksLikeMarkdown, renderMarkdown } from '../utils/markdown'
import {
  buildLibraryDigest,
  buildMemoFile,
  downloadTextFile,
  safeExportFileName,
  safeLibraryFileName,
} from '../utils/memoexport'
import { useToast } from '../composables/useToast'
import { useWailsEvent } from '../composables/useWailsEvent'
import { useClipboard } from '../composables/useClipboard'
import { useConfirm } from '../composables/useConfirm'
import UiClipboardField from '../components/ui/UiClipboardField.vue'
import UiButton from '../components/ui/UiButton.vue'
import PageHeader from '../components/ui/PageHeader.vue'

const { showToast } = useToast()
const { copyWithToast } = useClipboard()
const { confirm } = useConfirm()

// ---- 页内私有矢量图标（描边件与 AppIcon 注册表同血统；注册表缺 pin/eye/search/copy 等名，
// 全局层本轮禁碰，私有件先行——M4/图标线收编后可整块替换为注册表件）----
const GLYPH_PATHS: Record<string, readonly string[]> = {
  search: ['M10.5 3.5a7 7 0 1 0 0 14 7 7 0 0 0 0-14z', 'M20 20l-4.7-4.7'],
  pin: ['M8 2.5h8', 'M9.5 2.5v5L6 12.5h12L14.5 7.5v-5', 'M12 12.5v6'],
  eye: ['M2.5 12s3.7-6.5 9.5-6.5 9.5 6.5 9.5 6.5-3.7 6.5-9.5 6.5S2.5 12 2.5 12z', 'M12 9.6a2.4 2.4 0 1 0 0 4.8 2.4 2.4 0 0 0 0-4.8z'],
  'eye-off': ['M4 4l16 16', 'M9.9 5.8a9.8 9.8 0 0 1 2.1-.3c5.8 0 9.5 6.5 9.5 6.5a19 19 0 0 1-3 3.7', 'M6.4 7.6A19 19 0 0 0 2.5 12s3.7 6.5 9.5 6.5c1.2 0 2.3-.2 3.3-.6', 'M9.8 9.9a2.4 2.4 0 0 0 3.4 3.4'],
  copy: ['M11 9h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2h-8a2 2 0 0 1-2-2v-8a2 2 0 0 1 2-2z', 'M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1'],
  download: ['M12 3.5v11', 'M7 10l5 5 5-5', 'M4 20.5h16'],
  trash: ['M3.5 6h17', 'M8.5 6V3.5h7V6', 'M5.5 6l1.2 14h10.6L18.5 6', 'M10 10v6.5', 'M14 10v6.5'],
  pen: ['M16.5 3.5a2.6 2.6 0 0 1 3.7 3.7L7.5 19.9 2.8 21.2l1.3-4.7z'],
  more: ['M6 12h.01', 'M12 12h.01', 'M18 12h.01'],
  close: ['M6 6l12 12', 'M18 6L6 18'],
  sheet: ['M3.5 5.5h13v10h-13z', 'M8.5 19.5h12v-10h-4'],
  chevron: ['M6.5 9.5l5.5 5.5 5.5-5.5'],
}
const MemoGlyph = defineComponent({
  name: 'MemoGlyph',
  props: { name: { type: String, required: true }, size: { type: [Number, String], default: 15 } },
  setup(props) {
    return () =>
      h(
        'svg',
        {
          class: 'memo-glyph',
          viewBox: '0 0 24 24',
          style: { width: `${props.size}px`, height: `${props.size}px` },
          'aria-hidden': 'true',
        },
        (GLYPH_PATHS[props.name] ?? []).map((d) => h('path', { d })),
      )
  },
})

// ---- 数据与过滤状态 ----
const memos = shallowRef<MemoItem[]>([])
const stats = ref<MemoStats>({ totalCount: 0, pinnedCount: 0, tagCloud: {} })

const searchKw = ref('')
const selectedTag = ref('')
const filterPinned = ref<boolean | null>(null)
const loading = ref(false)
const errorMsg = ref('')

// ---- 速记入口（页面板顶部一行：想到即存的原语义原样保留）----
const quickText = ref('')
const quickTagsInput = ref('')
const quickSaving = ref(false)

// ---- 悬浮速记卡（N16 B 批：全局热键配置与唤出；配置收索引栏底折叠区）----
// QuickSheetState 用绑定官方型别（生成件已含该 typedef，旧版本地同构声明退役）。
const sheetState = ref<QuickSheetState | null>(null)
const sheetHotkeyDraft = ref('')
const sheetSaving = ref(false)
const sheetOpen = ref(false)

async function refreshQuickSheet() {
  try {
    const st = await MemoAPI.MemoService.GetQuickSheetState()
    sheetState.value = st
    sheetHotkeyDraft.value = st?.hotkey ?? ''
  } catch {
    sheetState.value = null // 拉不到实况就收整段，不给假配置可编
  }
}

// 速记热键未在位（开机被抢注等）：如实横幅提示改键或走菜单唤出（msgboard 先例语义）。
const sheetHotkeyMissing = computed(
  () => !!sheetState.value && sheetState.value.hotkey !== '' && !sheetState.value.hotkeyActive,
)

async function saveQuickHotkey() {
  if (sheetSaving.value || !sheetState.value) return
  sheetSaving.value = true
  try {
    // 后端事务自带冲突回滚（旧键全程活着、配置不动），失败原文上浮中文指引
    await MemoAPI.MemoService.SetQuickSheetHotkey(sheetHotkeyDraft.value.trim())
    await refreshQuickSheet()
    showToast('速记热键已更新')
  } catch (err: unknown) {
    const msg = getErrorMessage(err)
    showToast(`热键设置失败: ${msg}`)
    sheetHotkeyDraft.value = sheetState.value.hotkey // 回滚后草稿对齐生效值
    await refreshQuickSheet()
  } finally {
    sheetSaving.value = false
  }
}

async function openQuickSheet() {
  try {
    await MemoAPI.MemoService.ShowQuickSheet()
  } catch (err: unknown) {
    showToast(`唤出速记卡失败: ${getErrorMessage(err)}`)
  }
}

// ---- 页面板：阅读/书写同页合并（旧"详情浮层+编辑模态"双壳就此归一）----
const currentId = ref('') // 正在看/写的一条（空=闲页/新建）
const showEditor = ref(false)
const isEditing = ref(false)
const editMode = ref<'write' | 'preview'>('write')
const editForm = ref({
  id: '',
  title: '',
  content: '',
  tagInput: '',
  tags: [] as string[],
  colorTag: 'blue',
})

// 页面板内容声明式跟随列表账本：条目被别端改/删，computed 自动换身或收空，
// 不留陈旧明文（与旧版 loadMemos 里 imperative 同步 detailItem 同语义）。
const currentItem = computed<MemoItem | null>(
  () => memos.value.find((m) => m.id === currentId.value) ?? null,
)

const COLOR_OPTIONS = [
  { name: '蓝色', val: 'blue', hex: '#3b82f6' },
  { name: '翡翠绿', val: 'emerald', hex: '#10b981' },
  { name: '琥珀橙', val: 'amber', hex: '#f59e0b' },
  { name: '玫瑰红', val: 'rose', hex: '#f43f5e' },
  { name: '紫罗兰', val: 'purple', hex: '#8b5cf6' },
]

// 溢出菜单：导出全库/一键全删/浮窗速记退居次级入口（破坏性动作离主动线远一格，
// 确认链与禁用律一字不动）。开合纪律同 MemoToolbar 契约：点外/按 Esc/选中项即收。
const menuOpen = ref(false)
const overflowEl = ref<HTMLElement | null>(null)
function onDocMouseDown(e: MouseEvent) {
  if (!menuOpen.value) return
  if (overflowEl.value && !overflowEl.value.contains(e.target as Node)) menuOpen.value = false
}
watch(menuOpen, (open) => {
  if (open) document.addEventListener('mousedown', onDocMouseDown)
  else document.removeEventListener('mousedown', onDocMouseDown)
})

// 搜索/过滤拉取带序号守卫（模式同 useEverythingSearch 的 searchSeq）：
// 新查询发起即失效旧请求，List+GetStats 的慢响应不得覆盖新状态（loading 亦只由最新请求收口）。
let loadSeq = 0
let searchTimer: ReturnType<typeof setTimeout> | null = null

async function loadMemos() {
  if (searchTimer) {
    clearTimeout(searchTimer)
    searchTimer = null
  }
  const seq = ++loadSeq
  loading.value = true
  errorMsg.value = '' // 横幅只反映最近一次尝试：成功即收，失败由 catch 重写
  try {
    const filter: MemoFilter = {
      keyword: searchKw.value,
      tag: selectedTag.value,
      pinned: filterPinned.value,
      sortBy: 'updated',
      sortDesc: true,
    }

    const [items, curStats] = await Promise.all([
      MemoAPI.MemoService.List(filter),
      MemoAPI.MemoService.GetStats(),
    ])

    if (seq !== loadSeq) return
    memos.value = items ?? []
    stats.value = curStats
  } catch (err: unknown) {
    if (seq !== loadSeq) return
    errorMsg.value = `加载备忘录失败: ${getErrorMessage(err)}`
  } finally {
    if (seq === loadSeq) loading.value = false
  }
}

// 搜索框输入：350ms 防抖；输入瞬间即失效在飞查询，仅最后一次停顿后的请求允许写回。
function onSearchInput() {
  ++loadSeq
  if (searchTimer) clearTimeout(searchTimer)
  searchTimer = setTimeout(() => {
    searchTimer = null
    void loadMemos()
  }, 350)
}

function handleSelectTag(tag: string) {
  selectedTag.value = selectedTag.value === tag ? '' : tag
  loadMemos()
}

function clearFilters() {
  searchKw.value = ''
  selectedTag.value = ''
  filterPinned.value = null
  loadMemos()
}

const hasFilter = computed(
  () => searchKw.value !== '' || selectedTag.value !== '' || filterPinned.value !== null,
)

// ---- 时间分组（置顶由后端排序保证在前，这里按 updatedAt 再切自然日档；
// 六档口径逐字保留，接线时换 memoMetrics.groupMemoItems 单源）----
interface MemoGroup { key: string; label: string; items: MemoItem[] }

function dayBucket(iso: string): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return '更早'
  const now = new Date()
  const midnight = (x: Date) => new Date(x.getFullYear(), x.getMonth(), x.getDate()).getTime()
  const days = Math.round((midnight(now) - midnight(d)) / 86400000)
  if (days <= 0) return '今天'
  if (days === 1) return '昨天'
  if (days <= 7) return '7 天内'
  if (days <= 30) return '30 天内'
  return '更早'
}

const groupedMemos = computed<MemoGroup[]>(() => {
  const order = ['置顶', '今天', '昨天', '7 天内', '30 天内', '更早']
  const map = new Map<string, MemoItem[]>()
  for (const m of memos.value) {
    const key = m.isPinned ? '置顶' : dayBucket(m.updatedAt)
    if (!map.has(key)) map.set(key, [])
    map.get(key)!.push(m)
  }
  return order
    .filter((k) => map.has(k))
    .map((k) => ({ key: k, label: k, items: map.get(k)! }))
})

function fmtAgo(iso: string): string {
  const t = new Date(iso).getTime()
  if (Number.isNaN(t)) return '—'
  const min = Math.floor((Date.now() - t) / 60000)
  if (min < 1) return '刚刚'
  if (min < 60) return `${min} 分钟前`
  if (min < 24 * 60) return `${Math.floor(min / 60)} 小时前`
  return fmtDate(iso)
}

// 索引行摘要：首个非空行一行收（贴线密度优先；全文排版在右侧页面板看）。
// MD 徽标随之退役——能否渲染由页面板自动分流，不再用徽标向每一行剧透。
function rowExcerpt(content: string): string {
  const line = content.split('\n').find((l) => l.trim() !== '')
  return line?.trim() ?? ''
}

// ---- 索引选择 ----
function selectRow(item: MemoItem) {
  currentId.value = item.id
  showEditor.value = false // 换看下一条时，书写草稿按旧模态关窗口径丢弃（Esc 同权）
}

// ---- 速记 ----
function parseQuickTags(): string[] {
  const raw = quickTagsInput.value.split(/[\s,，、;；]+/).filter(Boolean)
  const set = new Set<string>()
  for (const t of raw) set.add(t.startsWith('#') ? t : `#${t}`)
  if (selectedTag.value) set.add(selectedTag.value)
  return [...set]
}

function onQuickEnter(e: KeyboardEvent) {
  // 中文输入法组字中的回车只结束Candidates，不该触发保存
  if (e.isComposing) return
  void commitQuick()
}

async function commitQuick() {
  const text = quickText.value.trim()
  if (!text || quickSaving.value) return
  quickSaving.value = true
  try {
    const created = await MemoAPI.MemoService.Create('', text, parseQuickTags(), 'blue')
    quickText.value = ''
    quickTagsInput.value = ''
    showToast('已记下')
    await loadMemos()
    // 新条翻给右页看（当前视图过滤不含它则不抢闲页）
    if (created?.id && memos.value.some((m) => m.id === created.id)) currentId.value = created.id
  } catch (err: unknown) {
    showToast(`记录失败: ${getErrorMessage(err)}`)
  } finally {
    quickSaving.value = false
  }
}

// ---- 页面板书写态 ----
function openCreateModal() {
  isEditing.value = false
  editMode.value = 'write'
  currentId.value = ''
  editForm.value = {
    id: '',
    title: '',
    content: '',
    tagInput: '',
    tags: selectedTag.value ? [selectedTag.value] : [],
    colorTag: 'blue',
  }
  showEditor.value = true
}

function openEditModal(item: MemoItem) {
  isEditing.value = true
  editMode.value = 'write'
  currentId.value = item.id
  editForm.value = {
    id: item.id,
    title: item.title,
    content: item.content,
    tagInput: '',
    tags: item.tags ? [...item.tags] : [],
    colorTag: item.colorTag || 'blue',
  }
  showEditor.value = true
}

function cancelEdit() {
  showEditor.value = false
}

function addTagFromInput() {
  const val = editForm.value.tagInput.trim()
  if (!val) return
  const formatted = val.startsWith('#') ? val : '#' + val
  if (!editForm.value.tags.includes(formatted)) {
    editForm.value.tags.push(formatted)
  }
  editForm.value.tagInput = ''
}

function removeTag(tag: string) {
  editForm.value.tags = editForm.value.tags.filter((t) => t !== tag)
}

async function handleSaveMemo() {
  addTagFromInput()
  if (!editForm.value.title.trim() && !editForm.value.content.trim()) {
    showToast('标题或内容至少填写一项')
    return
  }

  try {
    if (isEditing.value) {
      await MemoAPI.MemoService.Update(
        editForm.value.id,
        editForm.value.title,
        editForm.value.content,
        editForm.value.tags,
        editForm.value.colorTag,
      )
      showToast('备忘录已更新')
      showEditor.value = false
    } else {
      const created = await MemoAPI.MemoService.Create(
        editForm.value.title,
        editForm.value.content,
        editForm.value.tags,
        editForm.value.colorTag,
      )
      showToast('已新建备忘录')
      showEditor.value = false
      if (created?.id) currentId.value = created.id
    }
    await loadMemos()
  } catch (err: unknown) {
    showToast(`保存失败: ${getErrorMessage(err)}`)
  }
}

// 书写态预览：所见即阅读态所得（同一渲染器同源）
const editorPreviewHtml = computed(() => renderMarkdown(editForm.value.content))

// ---- 页面板阅读态 ----
function maskFromPane() {
  if (currentItem.value) void handleToggleMask(currentItem.value)
}
function editFromPane() {
  const cur = currentItem.value
  if (!cur) return
  openEditModal(cur)
}
const paneHtml = computed(() =>
  currentItem.value && !currentItem.value.isMasked && looksLikeMarkdown(currentItem.value.content)
    ? renderMarkdown(currentItem.value.content)
    : '',
)

// ---- 单条导出（N16 C 批：与文件库同格式的 frontmatter 文档，可回灌）----
// 通道为浏览器原生下载，不可用时降级"复制原文到剪贴板"（同一份文档，不另做取舍）。
function exportFromPane() {
  const cur = currentItem.value
  if (!cur || cur.isMasked) return // 遮罩语义：明文不落导出文件，同"复制全文"纪律
  const doc = buildMemoFile(cur)
  const fileName = safeExportFileName(cur)
  if (downloadTextFile(fileName, doc)) {
    showToast(`已导出 ${fileName}`)
  } else {
    void copyWithToast(doc, '保存通道不可用，原文已复制到剪贴板')
  }
}

// 全库：永远按无过滤全集重拉（当前检索/标签视图不得缩小导出面）；IsMasked
// 条目默认剔除且确认框明说条数——排除动作发生在取数后、进汇总器之前，
// 敏感明文根本不进导出字符串。
const exporting = ref(false)

async function handleExportLibrary() {
  if (exporting.value) return
  exporting.value = true
  try {
    const all =
      (await MemoAPI.MemoService.List({
        keyword: '',
        tag: '',
        pinned: null,
        sortBy: 'updated',
        sortDesc: true,
      })) ?? []
    const kept = all.filter((m) => !m.isMasked)
    const maskedCount = all.length - kept.length
    if (kept.length === 0) {
      showToast(all.length === 0 ? '便签库是空的，没有可导出的内容' : '全部条目均为敏感遮罩，默认不导出')
      return
    }
    const accepted = await confirm({
      title: '导出全库汇总文件？',
      description:
        '将生成一个 Markdown 汇总文件（浏览器下载）。敏感遮罩条目默认排除，明文不会写入导出文件；导出件离开应用后不再有遮罩保护，请妥善保管。',
      confirmLabel: `导出 ${kept.length} 条`,
      tone: 'warning',
      details: [
        { label: '导出条数', value: `${kept.length} 条` },
        { label: '敏感排除', value: maskedCount > 0 ? `${maskedCount} 条（明文未写入）` : '无' },
      ],
    })
    if (!accepted) return
    const digest = buildLibraryDigest(kept, { excludedMasked: maskedCount })
    const fileName = safeLibraryFileName()
    if (downloadTextFile(fileName, digest)) {
      showToast(`已导出 ${kept.length} 条 → ${fileName}`)
    } else {
      void copyWithToast(digest, '下载通道不可用，汇总全文已复制到剪贴板')
    }
  } catch (err: unknown) {
    showToast(`导出失败: ${getErrorMessage(err)}`)
  } finally {
    exporting.value = false
  }
}

// ---- 行/页微操作 ----
async function handleTogglePin(item: MemoItem) {
  try {
    const pinned = await MemoAPI.MemoService.TogglePin(item.id)
    showToast(pinned ? '已置顶便签' : '已取消置顶')
    await loadMemos()
  } catch (err: unknown) {
    showToast(`操作失败: ${getErrorMessage(err)}`)
  }
}

async function handleToggleMask(item: MemoItem) {
  try {
    const masked = await MemoAPI.MemoService.ToggleMask(item.id)
    showToast(masked ? '已开启脱敏遮罩' : '已揭示明文')
    await loadMemos()
  } catch (err: unknown) {
    showToast(`操作失败: ${getErrorMessage(err)}`)
  }
}

async function handleDeleteMemo(id: string) {
  const accepted = await confirm({
    title: '确定删除这条便签吗？',
    description: '删除后无法恢复。',
    confirmLabel: '删除',
    tone: 'danger',
  })
  if (!accepted) return
  try {
    await MemoAPI.MemoService.Delete(id)
    if (currentId.value === id) currentId.value = ''
    showToast('已删除便签')
    await loadMemos()
  } catch (err: unknown) {
    showToast(`删除失败: ${getErrorMessage(err)}`)
  }
}

// ---- 一键全删 ----
async function handleClearAll() {
  const total = stats.value.totalCount
  const accepted = await confirm({
    title: '清空全部便签？',
    description: `将一次删光全部 ${total} 条便签（含置顶与敏感遮罩条目），并同步销毁迁移留底、损坏取证副本等数据残片。此操作不可恢复。`,
    confirmLabel: `全删 ${total} 条`,
    tone: 'danger',
  })
  if (!accepted) return
  try {
    const deleted = await MemoAPI.MemoService.ClearAll()
    currentId.value = ''
    showToast(`已全删 ${deleted} 条便签`)
    await loadMemos()
  } catch (err: unknown) {
    showToast(`全删失败: ${getErrorMessage(err)}`)
  }
}

// 剪贴板两级策略已收编进 useClipboard（toast 文案与原实现逐字一致）
async function copyMemoContent(content: string) {
  await copyWithToast(content, '已复制便签内容到剪贴板')
}

// Esc 出口纪律（旧"编辑浮层→详情浮层"链的等价降维）：溢出菜单 → 书写态回阅读页 → 收起当前页
function onGlobalKey(e: KeyboardEvent) {
  if (e.key !== 'Escape') return
  if (menuOpen.value) menuOpen.value = false
  else if (showEditor.value) showEditor.value = false
  else if (currentId.value) currentId.value = ''
}

// memo:changed 由局域网投递/多端联动触发：setup 期订阅防丢早期事件，卸载自动注销
useWailsEvent('memo:changed', () => {
  loadMemos()
})

onMounted(() => {
  loadMemos()
  refreshQuickSheet()
  window.addEventListener('keydown', onGlobalKey)
})

onUnmounted(() => {
  window.removeEventListener('keydown', onGlobalKey)
  document.removeEventListener('mousedown', onDocMouseDown)
  ++loadSeq // 作废在飞请求，卸载后不得回写状态
  if (searchTimer) {
    clearTimeout(searchTimer)
    searchTimer = null
  }
})

// 选中即确保该行在栏内可见（键盘 Tab 流与事件驱动换选都吃这条）
watch(currentId, async () => {
  if (!currentId.value) return
  await nextTick()
  const row = document.querySelector('.memo-row.is-current')
  if (row && typeof row.scrollIntoView === 'function') row.scrollIntoView({ block: 'nearest' })
})
</script>

<template>
  <div class="page-workbench memo-page">
    <PageHeader title="随手记" subtitle="本地便签库：敲一行回车即存，长文代码翻右页；支持手机局域网投递，随数据目录迁移不丢失。" />

    <!-- 动作失败/加载失败整页可见（带重试出口） -->
    <section v-if="errorMsg" class="banner banner-error" role="alert">
      {{ errorMsg }}
      <button type="button" class="btn btn-small btn-secondary retry-btn" @click="loadMemos">重试</button>
    </section>

    <!-- 顶栏：一行轻工具条（检索/标签/置顶；导出与全删退右端溢出菜单。无卡片容器、无统计字） -->
    <div class="memo-tools">
      <div class="search-box">
        <span class="search-icon"><MemoGlyph name="search" /></span>
        <input
          v-model="searchKw"
          type="text"
          class="text-input search-input"
          placeholder="搜标题、正文、代码或标签…"
          aria-label="搜索便签"
          @input="onSearchInput"
        />
        <button v-if="searchKw" class="memo-clear-btn" title="清空搜索" aria-label="清空搜索" @click="searchKw = ''; loadMemos()">
          <MemoGlyph name="close" :size="13" />
        </button>
      </div>
      <div class="memo-chips" aria-label="标签与置顶过滤">
        <button class="tag-chip" :class="{ active: selectedTag === '' }" @click="handleSelectTag('')">
          全部 ({{ stats.totalCount }})
        </button>
        <button
          v-for="(count, tag) in stats.tagCloud"
          :key="tag"
          class="tag-chip"
          :class="{ active: selectedTag === tag }"
          @click="handleSelectTag(String(tag))"
        >
          {{ tag }} <span class="tag-count">({{ count }})</span>
        </button>
        <button
          class="tag-chip chip-pin"
          :class="{ active: filterPinned === true }"
          title="只看置顶便签"
          @click="filterPinned = filterPinned === true ? null : true; loadMemos()"
        >
          <MemoGlyph name="pin" :size="12" /> 只看置顶
        </button>
      </div>
      <button v-if="hasFilter" class="btn btn-ghost btn-small clear-filter-btn" @click="clearFilters">清除过滤</button>
      <div ref="overflowEl" class="memo-overflow">
        <button
          class="memo-more-btn"
          aria-label="更多操作"
          :aria-haspopup="true"
          :aria-expanded="menuOpen"
          title="更多操作（浮窗速记/导出全库/一键全删）"
          @click="menuOpen = !menuOpen"
        >
          <MemoGlyph name="more" :size="17" />
        </button>
        <div v-if="menuOpen" class="memo-menu" role="menu">
          <button class="menu-item" role="menuitem" title="唤出悬浮速记卡（与全局热键同一入口）" @click="menuOpen = false; openQuickSheet()">
            <MemoGlyph name="sheet" /> 浮窗速记
          </button>
          <button
            class="menu-item"
            role="menuitem"
            :disabled="stats.totalCount === 0 || exporting"
            :title="`导出全库为单个 Markdown 汇总文件（敏感遮罩条目默认排除），需确认`"
            @click="menuOpen = false; handleExportLibrary()"
          >
            <MemoGlyph name="download" /> {{ exporting ? '导出中…' : '导出全库' }}
          </button>
          <button
            class="menu-item menu-danger"
            role="menuitem"
            :disabled="stats.totalCount === 0"
            title="一次删光全部便签（含敏感遮罩条目），需强确认"
            @click="menuOpen = false; handleClearAll()"
          >
            <MemoGlyph name="trash" /> 一键全删{{ stats.totalCount ? ` (${stats.totalCount})` : '' }}
          </button>
        </div>
      </div>
    </div>

    <!-- 摊开的两页：左索引栏 + 右页面板（≤760 容器查询叠放，页面板在上——先落笔再翻账） -->
    <div class="memo-desk">
      <nav class="memo-index" aria-label="便签索引">
        <div v-if="loading && memos.length === 0" class="state-box">正在读取本地便签库…</div>

        <div v-else-if="memos.length === 0" class="state-box">
          <template v-if="hasFilter">
            没有符合当前过滤条件的便签。
            <button type="button" class="state-action" @click="clearFilters">清除过滤</button>
          </template>
          <template v-else>
            还没有一条便签——在右边速记入口敲一行、按回车就有了。
          </template>
        </div>

        <section v-for="g in groupedMemos" :key="g.key" class="memo-group" :aria-label="g.label">
          <h2 class="group-head">
            {{ g.label }}
            <span class="group-count mono">{{ g.items.length }}</span>
          </h2>
          <article
            v-for="item in g.items"
            :key="item.id"
            class="memo-row"
            :class="[`spine-${item.colorTag || 'blue'}`, { 'is-pinned': item.isPinned, 'is-current': item.id === currentId }]"
          >
            <div class="memo-row-body">
              <span class="memo-row-top">
                <span
                  class="memo-title"
                  :class="{ 'title-void': !item.title.trim() }"
                  role="button"
                  tabindex="0"
                  :title="item.title || '无标题便签'"
                  @click="selectRow(item)"
                  @keydown.enter.prevent="selectRow(item)"
                  @keydown.space.prevent="selectRow(item)"
                ><MemoGlyph v-if="item.isPinned" name="pin" :size="12" class="title-pin" />{{ item.title || '无标题便签' }}</span>
                <time class="memo-time" :title="new Date(item.updatedAt).toLocaleString()">{{ fmtAgo(item.updatedAt) }}</time>
              </span>
              <span
                class="memo-excerpt mono"
                :class="{ masked: item.isMasked }"
                :aria-label="item.isMasked ? '敏感信息已遮罩' : '便签首行摘要'"
                @click="selectRow(item)"
              ><template v-if="item.isMasked">••••••••••••••••••••••••••••••</template><template v-else>{{ rowExcerpt(item.content) || '（空便签）' }}</template></span>
              <span v-if="item.tags && item.tags.length" class="memo-row-tags">
                <span
                  v-for="t in item.tags"
                  :key="t"
                  class="tag-pill tag-pill-clickable memo-tag"
                  :class="{ 'memo-tag-active': selectedTag === t }"
                  role="button"
                  tabindex="0"
                  @click="handleSelectTag(t)"
                  @keydown.enter.prevent="handleSelectTag(t)"
                  @keydown.space.prevent="handleSelectTag(t)"
                >
                  {{ t }}
                </span>
              </span>
            </div>
            <span class="memo-row-actions">
              <button class="memo-icon-btn" :title="item.isMasked ? '揭示敏感信息' : '脱敏遮罩保护'" :aria-label="item.isMasked ? '揭示敏感信息' : '脱敏遮罩保护'" @click="handleToggleMask(item)">
                <MemoGlyph :name="item.isMasked ? 'eye' : 'eye-off'" />
              </button>
              <button class="memo-icon-btn" :title="item.isPinned ? '取消置顶' : '固定置顶'" :aria-label="item.isPinned ? '取消置顶' : '固定置顶'" @click="handleTogglePin(item)">
                <MemoGlyph name="pin" />
              </button>
            </span>
          </article>
        </section>

        <!-- 悬浮速记卡热键配置（低频件收栏底折叠；实况拉不到整段收起不给假配置） -->
        <section v-if="sheetState" class="memo-sheet-settings" aria-label="悬浮速记卡">
          <button class="sheet-disclosure" :aria-expanded="sheetOpen" @click="sheetOpen = !sheetOpen">
            <span class="sheet-disc-name">悬浮速记卡 · 全局热键</span>
            <span class="sheet-disc-now mono">{{ sheetState.hotkey || '—' }}</span>
            <span v-if="sheetState.hotkey === ''" class="chip chip-neutral memo-hotkey-off">热键已停用</span>
            <MemoGlyph name="chevron" :size="13" class="sheet-disc-chevron" />
          </button>
          <div v-show="sheetOpen" class="sheet-body">
            <p class="sheet-desc">
              随处唤出极简速记小窗：回车即存、失焦即收。留空 = 停用（菜单「浮窗速记」不受影响）；改键即时注册，被占用会报错并保旧键。
            </p>
            <div class="sheet-row">
              <input
                v-model="sheetHotkeyDraft"
                class="text-input memo-hotkey-input mono"
                type="text"
                placeholder="Ctrl+Alt+N"
                aria-label="速记卡全局热键"
              />
              <UiButton variant="secondary" small :disabled="sheetSaving" @click="saveQuickHotkey">
                {{ sheetSaving ? '保存中…' : '保存热键' }}
              </UiButton>
            </div>
          </div>
          <div v-if="sheetHotkeyMissing" class="banner banner-warn" role="note">
            热键「{{ sheetState.hotkey }}」未在位：多半已被其它程序抢占。换个组合保存，或留空停用——期间可用菜单「浮窗速记」唤出。
          </div>
        </section>
      </nav>

      <section class="memo-pane" aria-label="当前页">
        <!-- 速记入口：一行克制输入（不是大卡片；回车即存/组字豁免/自动带过滤标签语义原样） -->
        <div class="memo-quick">
          <input
            v-model="quickText"
            class="text-input quick-input"
            type="text"
            placeholder="想到什么敲一行，回车即存…"
            aria-label="速记内容"
            @keydown.enter="onQuickEnter"
          />
          <input
            v-model="quickTagsInput"
            class="text-input quick-tags"
            type="text"
            placeholder="#标签"
            aria-label="速记标签"
            title="速记标签：空格/逗号分隔，自动补 #；速记不带标题，正文即全貌"
            @keydown.enter="onQuickEnter"
          />
          <span
            v-if="selectedTag"
            class="tag-pill memo-tag memo-tag-autofill"
            :title="`当前正按 ${selectedTag} 过滤，速记会自动带上该标签`"
          >{{ selectedTag }}</span>
          <UiButton variant="primary" small :disabled="!quickText.trim() || quickSaving" @click="commitQuick">
            {{ quickSaving ? '记录中…' : '记录' }}
          </UiButton>
          <button class="memo-pen-btn" title="写长便签：标题、多行代码、Markdown、标签、色彩标识" @click="openCreateModal">
            <MemoGlyph name="pen" :size="14" /> 长便签
          </button>
        </div>

        <!-- 书写态（编辑/新建同页展开；阅读态即其背页，双浮层退役） -->
        <div v-if="showEditor" class="pane-write" @keydown.ctrl.enter.prevent="handleSaveMemo">
          <div class="pane-head">
            <span class="pane-kind">{{ isEditing ? '编辑便签' : '新建便签' }}</span>
            <div class="seg" role="group" aria-label="编辑/预览切换">
              <button class="seg-btn" :class="{ active: editMode === 'write' }" @click="editMode = 'write'">书写</button>
              <button class="seg-btn" :class="{ active: editMode === 'preview' }" @click="editMode = 'preview'">预览</button>
            </div>
          </div>

          <div v-show="editMode === 'write'" class="write-body">
            <input
              v-model="editForm.title"
              type="text"
              class="text-input pane-title-input"
              placeholder="标题（可留空，正文首行示人）"
              aria-label="便签标题"
            />
            <UiClipboardField
              v-model="editForm.content"
              mono
              :rows="10"
              placeholder="在此粘贴文本、命令行、cURL、SQL 或 JWT…（支持 Markdown 子集：标题/粗斜体/代码块/列表/链接/引用）"
            />
            <div class="pane-color-tags">
              <span class="pane-block-label">色彩</span>
              <div class="color-row">
                <div
                  v-for="c in COLOR_OPTIONS"
                  :key="c.val"
                  class="color-circle"
                  :style="{ background: c.hex }"
                  :class="{ selected: editForm.colorTag === c.val }"
                  :title="c.name"
                  @click="editForm.colorTag = c.val"
                ></div>
              </div>
              <span class="pane-block-label">标签</span>
              <div class="tags-input-box">
                <span v-for="t in editForm.tags" :key="t" class="tag-pill memo-tag memo-tag-removable">
                  {{ t }} <span class="memo-tag-x" title="移除标签" @click="removeTag(t)">✕</span>
                </span>
                <input
                  v-model="editForm.tagInput"
                  type="text"
                  class="text-input tag-entry"
                  placeholder="例如 SQL, Token, 常用（回车添加，自动补 #）"
                  aria-label="添加标签"
                  @keydown.enter.prevent="addTagFromInput"
                />
              </div>
            </div>
          </div>

          <div v-show="editMode === 'preview'" class="write-body">
            <div v-if="editForm.content.trim()" class="md-preview" v-html="editorPreviewHtml" />
            <p v-else class="state-box preview-void">正文还是空的——先回「书写」写点东西。</p>
          </div>

          <footer class="pane-foot">
            <span class="foot-hint">Ctrl+Enter 保存 · Esc 放弃草稿</span>
            <UiButton variant="secondary" small @click="cancelEdit">取消</UiButton>
            <UiButton variant="primary" small @click="handleSaveMemo">
              {{ isEditing ? '保存修改' : '立即创建' }}
            </UiButton>
          </footer>
        </div>

        <!-- 阅读态（旧详情浮层所得全数搬进本页；MD 自动渲染/等宽原文/遮罩圆点三分支照旧） -->
        <div v-else-if="currentItem" class="pane-read">
          <header class="pane-head">
            <h3 class="pane-title" :class="{ 'title-void': !currentItem.title.trim() }">
              {{ currentItem.title || '无标题便签' }}
            </h3>
          </header>
          <div class="pane-meta">
            <span class="chip" :class="`chip-tag-${currentItem.colorTag || 'blue'}`" aria-hidden="true">■</span>
            <span v-if="currentItem.isPinned" class="chip chip-neutral">已置顶</span>
            <span v-if="currentItem.isMasked" class="chip chip-danger">敏感遮罩中</span>
            <span class="mono meta-time">建 {{ fmtDate(currentItem.createdAt) }} · 改 {{ fmtDate(currentItem.updatedAt) }}</span>
          </div>
          <div class="pane-body">
            <div v-if="currentItem.isMasked" class="memo-content-box masked mono" aria-label="敏感信息已遮罩">
              ••••••••••••••••••••••••••••••••••••••••••••••••••••••••••••••
            </div>
            <!-- v-html 安全前提：renderMarkdown 先整体转义再结构替换，链接 http/https 白名单，
                 spec 锁 XSS 用例；masked 条目永不进渲染分支 -->
            <div v-else-if="paneHtml" class="md-preview" v-html="paneHtml" />
            <pre v-else class="memo-plain">{{ currentItem.content || '（空便签）' }}</pre>
          </div>
          <div v-if="currentItem.tags && currentItem.tags.length" class="pane-tags">
            <span
              v-for="t in currentItem.tags"
              :key="t"
              class="tag-pill tag-pill-clickable memo-tag"
              :class="{ 'memo-tag-active': selectedTag === t }"
              role="button"
              tabindex="0"
              @click="handleSelectTag(t)"
              @keydown.enter.prevent="handleSelectTag(t)"
              @keydown.space.prevent="handleSelectTag(t)"
            >{{ t }}</span>
          </div>
          <footer class="pane-foot">
            <!-- 遮罩条目不给复制全文/导出：遮罩语义是"明文不落眼、不落他人剪贴板也不落成文件" -->
            <UiButton v-if="!currentItem.isMasked" variant="secondary" small class="memo-copy-btn" @click="copyMemoContent(currentItem.content)">
              <MemoGlyph name="copy" :size="13" /> 复制全文
            </UiButton>
            <UiButton v-if="!currentItem.isMasked" variant="secondary" small title="导出为与文件库同格式的 .md（含 frontmatter，可回灌）" @click="exportFromPane">
              <MemoGlyph name="download" :size="13" /> 导出 .md
            </UiButton>
            <UiButton variant="secondary" small @click="maskFromPane">
              <MemoGlyph :name="currentItem.isMasked ? 'eye' : 'eye-off'" :size="13" /> {{ currentItem.isMasked ? '揭示明文' : '脱敏遮罩' }}
            </UiButton>
            <UiButton variant="secondary" small @click="handleTogglePin(currentItem)">
              <MemoGlyph name="pin" :size="13" /> {{ currentItem.isPinned ? '取消置顶' : '固定置顶' }}
            </UiButton>
            <span class="foot-spacer" aria-hidden="true"></span>
            <button class="btn btn-ghost btn-small text-danger memo-del-btn" @click="handleDeleteMemo(currentItem.id)">
              <MemoGlyph name="trash" :size="13" /> 删除
            </button>
            <UiButton variant="primary" small @click="editFromPane">
              <MemoGlyph name="pen" :size="13" /> 编辑
            </UiButton>
          </footer>
        </div>

        <!-- 闲页：未选中任何一条时的安静版心（书写起点二选一：速记行回车 / 这里开长便签） -->
        <div v-else class="pane-idle">
          <p class="idle-note">
            左边是账，这里翻到正在看（写）的那一页。
            点任意一条即读，读完就地改；一行闪念走上面的速记入口。
          </p>
          <UiButton variant="secondary" small @click="openCreateModal">写一条长便签</UiButton>
        </div>
      </section>
    </div>
  </div>
</template>

<style scoped>
/* 治理说明（机主配色裁决 2026-09-26 沿用）：本视图 scoped 层【禁止】重写共享原子类
   （components.css :where 家族：btn 全族、text-input、tag-pill、chip、state-box、
   banner、mono、text-muted/text-danger 等）与全局 token 变量——跨页面观感一律
   以共享件为准。页面私有需求全部走 memo-/pane-/quick-/sheet-/menu- 私有类，
   与共享类同挂叠加组合（如 tag-pill + tag-pill-clickable：外观基座与可点交互档归全局；
   tag-pill + memo-tag-active：选中态色差等页面私有色差仍归私有，MemoCard 亦未内置此差）。 */

.memo-page { display: flex; flex-direction: column; gap: 12px; container: memo / inline-size; }
.retry-btn { margin-left: 8px; }

/* ---------- 顶栏工具条：一行轻件，零卡片容器 ---------- */
.memo-tools { display: flex; align-items: center; gap: 8px; min-width: 0; }
.search-box { position: relative; flex: 0 1 250px; min-width: 140px; display: flex; align-items: center; }
.search-icon { position: absolute; left: 9px; top: 50%; transform: translateY(-50%); color: var(--color-text-subtle); display: inline-flex; pointer-events: none; }
.search-input { width: 100%; padding-left: 30px; padding-right: 26px; min-height: var(--control-h-md); }
.memo-clear-btn {
  position: absolute; right: 6px; top: 50%; transform: translateY(-50%);
  background: none; border: none; color: var(--color-text-subtle); cursor: pointer;
  display: inline-flex; padding: 2px; border-radius: var(--radius-micro);
}
.memo-clear-btn:hover { background: var(--surface-hover); color: var(--color-text); }
.memo-chips { display: flex; align-items: center; gap: 6px; min-width: 0; flex: 1; overflow-x: auto; scrollbar-width: thin; padding: 2px 0; }
.tag-chip {
  flex: none; display: inline-flex; align-items: center; gap: 4px;
  background: var(--surface-soft); border: 1px solid var(--color-border); color: var(--color-text-muted);
  padding: 2px 10px; border-radius: var(--radius-pill); font-size: var(--text-sm); cursor: pointer;
  transition: background var(--motion-fast) ease, color var(--motion-fast) ease, border-color var(--motion-fast) ease;
}
.tag-chip:hover { background: var(--surface-hover); color: var(--color-text); }
.tag-chip.active { background: var(--color-primary); border-color: var(--color-primary); color: var(--color-on-primary); }
.tag-count { opacity: 0.8; font-size: var(--text-xs); }
.clear-filter-btn { flex: none; }
.memo-overflow { position: relative; flex: none; }
.memo-more-btn {
  display: inline-flex; align-items: center; justify-content: center;
  width: var(--control-h-md); height: var(--control-h-md);
  background: none; border: 1px solid var(--btn-outline-edge); border-radius: var(--radius-control);
  color: var(--color-text-muted); cursor: pointer;
  transition: background var(--motion-fast) ease, color var(--motion-fast) ease;
}
.memo-more-btn:hover { background: var(--surface-hover); color: var(--color-text); }
.memo-menu {
  position: absolute; right: 0; top: calc(100% + 6px); z-index: 60; min-width: 172px;
  background: var(--surface-panel); border: 1px solid var(--color-border); border-radius: var(--radius-control);
  box-shadow: var(--shadow-small); padding: 4px; display: flex; flex-direction: column; gap: 2px;
}
.menu-item {
  display: flex; align-items: center; gap: 8px; text-align: left;
  background: none; border: none; border-radius: var(--radius-micro); cursor: pointer;
  padding: 7px 10px; font-size: var(--text-sm); color: var(--color-text); white-space: nowrap;
}
.menu-item:hover:not(:disabled) { background: var(--surface-hover); }
.menu-item:disabled { opacity: 0.55; cursor: not-allowed; }
.menu-danger { color: var(--state-danger); }
.menu-danger:hover:not(:disabled) { background: var(--btn-danger-outline-hover); }

/* ---------- 摊开的两页 ---------- */
.memo-desk {
  display: grid; grid-template-columns: minmax(280px, 336px) minmax(0, 1fr); gap: 16px;
  align-items: stretch; min-width: 0;
}

/* 左索引栏：贴线行（笔记本侧页速记标），非卡片群岛 */
.memo-index {
  display: flex; flex-direction: column; gap: 2px; min-width: 0;
  max-height: calc(100dvh - 196px); min-height: 300px; overflow-y: auto;
  padding-right: 2px; scrollbar-width: thin;
}
.memo-group { display: flex; flex-direction: column; }
.group-head {
  position: sticky; top: 0; z-index: 2;
  display: flex; align-items: center; gap: 6px; margin: 0; padding: 5px 0 4px 8px;
  background: var(--surface-page);
  font-size: var(--text-xs); font-weight: 600; color: var(--color-text-subtle); letter-spacing: 0.4px;
}
.group-count { font-size: var(--text-micro); color: var(--color-text-subtle); background: var(--surface-hover); border-radius: var(--radius-pill); padding: 0 6px; }

.memo-row {
  display: flex; align-items: stretch; gap: 2px; min-width: 0;
  border-left: 3px solid transparent; border-radius: var(--radius-micro);
  transition: background var(--motion-fast) ease;
}
.memo-row:hover { background: var(--surface-hover); }
.memo-row.is-pinned { background: var(--surface-soft); }
.memo-row.is-pinned:hover { background: var(--surface-hover); }
.memo-row.is-current { background: var(--surface-selected); }
/* 色彩标识是用户数据值（colorTag 持久化进后端），非主题表面——封边保留原始色板（与 memoMetrics 五色同源） */
.spine-blue { border-left-color: #3b82f6; }
.spine-emerald { border-left-color: #10b981; }
.spine-amber { border-left-color: #f59e0b; }
.spine-rose { border-left-color: #f43f5e; }
.spine-purple { border-left-color: #8b5cf6; }

.memo-row-body { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 1px; padding: 5px 4px 5px 8px; }
.memo-row-top { display: flex; align-items: baseline; gap: 8px; min-width: 0; }
.memo-title {
  margin: 0; font-size: var(--text-base); font-weight: 600; color: var(--color-text);
  min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; cursor: pointer;
}
.memo-title:hover { color: var(--color-primary); }
.title-pin { margin-right: 4px; vertical-align: -2px; color: var(--color-primary); }
.title-void { color: var(--color-text-subtle); font-weight: 400; font-style: italic; }
.memo-time { margin-left: auto; flex: none; font-size: var(--text-micro); color: var(--color-text-subtle); font-variant-numeric: tabular-nums; }
.memo-excerpt {
  display: block; font-size: var(--text-xs); line-height: 1.5; color: var(--color-text-muted);
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap; cursor: pointer; min-width: 0;
}
.memo-excerpt.masked { color: var(--color-text-subtle); user-select: none; letter-spacing: 1.5px; }
.memo-row-tags { display: flex; flex-wrap: nowrap; gap: 3px; padding-top: 1px; overflow: hidden; }
/* 标签丸外观基座归全局 .tag-pill 原子；可点档走 .tag-pill-clickable；本层只剩选中态私有色差。 */
.memo-tag-active { background: var(--color-primary-soft); color: var(--color-primary); }

.memo-row-actions { display: flex; flex-direction: column; justify-content: center; gap: 2px; flex: none; padding-right: 2px; opacity: 0.55; transition: opacity var(--motion-fast) ease; }
.memo-row:hover .memo-row-actions, .memo-row:focus-within .memo-row-actions, .memo-row.is-current .memo-row-actions { opacity: 1; }
.memo-icon-btn {
  background: none; border: none; cursor: pointer; color: var(--color-text-muted);
  padding: 3px; border-radius: var(--radius-micro); display: inline-flex;
  transition: background var(--motion-fast) ease, color var(--motion-fast) ease;
}
.memo-icon-btn:hover { background: var(--surface-hover); color: var(--color-text); }

/* 栏底：悬浮速记卡折叠配置（低频件，收起态一行字即够；未在位警示恒外露） */
.memo-sheet-settings { margin-top: 8px; border-top: 1px solid var(--color-border); display: flex; flex-direction: column; gap: 6px; }
.sheet-disclosure {
  display: flex; align-items: center; gap: 6px; width: 100%;
  background: none; border: none; cursor: pointer; padding: 8px 2px 6px;
  font-size: var(--text-xs); color: var(--color-text-muted);
}
.sheet-disclosure:hover { color: var(--color-text); }
.sheet-disc-name { font-weight: 600; }
.sheet-disc-now { margin-left: auto; }
.sheet-disc-chevron { transition: transform var(--motion-fast) ease; }
.sheet-disclosure[aria-expanded="true"] .sheet-disc-chevron { transform: rotate(180deg); }
.sheet-body { display: flex; flex-direction: column; gap: 6px; padding: 0 2px 4px; }
.sheet-desc { margin: 0; font-size: var(--text-xs); color: var(--color-text-subtle); line-height: 1.6; }
.sheet-row { display: flex; align-items: center; gap: 8px; }
.memo-hotkey-input { width: 150px; flex: none; }
.memo-hotkey-off { flex: none; }
.memo-sheet-settings .banner-warn { padding: 8px 10px; font-size: var(--text-xs); }

/* ---------- 右页面板 ---------- */
.memo-pane {
  display: flex; flex-direction: column; gap: 10px; min-width: 0;
  max-height: calc(100dvh - 196px); min-height: 300px;
}

/* 速记入口：一行克制输入 */
.memo-quick { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; flex: none; }
.quick-input { flex: 1 1 180px; min-width: 120px; min-height: var(--control-h-md); }
.quick-tags { flex: 0 1 84px; min-width: 64px; min-height: var(--control-h-md); }
.memo-tag-autofill { color: var(--color-primary); background: var(--color-primary-soft); flex: none; }
.memo-pen-btn {
  display: inline-flex; align-items: center; gap: 5px; flex: none;
  background: none; border: none; cursor: pointer;
  color: var(--color-text-muted); font-size: var(--text-sm); padding: 4px 6px; border-radius: var(--radius-micro);
  transition: background var(--motion-fast) ease, color var(--motion-fast) ease;
}
.memo-pen-btn:hover { background: var(--surface-hover); color: var(--color-primary); }

/* 页面板头尾（书写/阅读共用骨架） */
.pane-head { display: flex; align-items: center; gap: 10px; min-width: 0; flex: none; }
.pane-kind { font-size: var(--text-sm); font-weight: 600; color: var(--color-text-muted); flex: none; }
.pane-title {
  margin: 0; font-size: var(--text-md); font-weight: 700; color: var(--color-text);
  flex: 1; min-width: 0; overflow-wrap: anywhere;
}
.pane-meta { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; flex: none; }
.meta-time { font-size: var(--text-xs); color: var(--color-text-subtle); }
.pane-body { overflow-y: auto; min-height: 0; flex: 1; max-width: 72ch; }
.pane-tags { display: flex; flex-wrap: wrap; gap: 6px; flex: none; }
.pane-foot { display: flex; align-items: center; justify-content: flex-end; gap: 8px; flex-wrap: wrap; flex: none; }
.foot-spacer { flex: 1; }
.foot-hint { margin-right: auto; font-size: var(--text-xs); color: var(--color-text-subtle); }

/* 书写态 */
.pane-write { display: flex; flex-direction: column; gap: 8px; min-height: 0; flex: 1; }
.write-body { display: flex; flex-direction: column; gap: 6px; overflow-y: auto; min-height: 0; max-width: 72ch; flex: 1; }
.pane-title-input { min-height: var(--control-h-md); font-size: var(--text-md); font-weight: 600; }
.pane-color-tags { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; min-width: 0; margin-top: 4px; }
.pane-block-label { font-size: var(--text-xs); color: var(--color-text-muted); flex: none; }
.color-row { display: flex; gap: 10px; padding: 2px 0; flex: none; }
.color-circle { width: 22px; height: 22px; border-radius: 50%; cursor: pointer; transition: transform var(--motion-fast) ease; }
.color-circle:hover { transform: scale(1.15); }
.color-circle.selected { outline: 2px solid var(--color-text); outline-offset: 2px; }
.tags-input-box {
  display: flex; flex-wrap: wrap; gap: 6px; align-items: center; min-width: 0; flex: 1 1 220px;
  border: 1px solid var(--color-border); border-radius: var(--radius-control); padding: 5px 8px; background: var(--surface-soft);
}
.tag-entry { flex: 1 1 120px; min-width: 0; border: none; background: transparent; padding: 3px 2px; outline: none; color: var(--color-text); font-size: var(--text-base); }
.memo-tag-removable { display: inline-flex; align-items: center; gap: 4px; background: var(--surface-hover); }
.memo-tag-x { cursor: pointer; font-size: var(--text-micro); color: var(--color-text-subtle); }
.memo-tag-x:hover { color: var(--state-danger); }
.preview-void { background: transparent; border-style: dashed; }

/* 阅读态正文（遮罩圆点/纯文本兜底两分支与旧详情浮层所得逐字同语义） */
.memo-content-box {
  font-size: var(--text-sm); line-height: 1.55; color: var(--color-text);
  background: var(--surface-soft); border: 1px solid var(--color-border); border-radius: var(--radius-control);
  padding: 8px 10px; white-space: pre-wrap; word-break: break-word;
}
.memo-content-box.masked { color: var(--color-text-subtle); user-select: none; letter-spacing: 2px; }
.memo-plain {
  margin: 0; font-family: var(--font-mono); font-size: var(--text-sm); line-height: 1.6;
  white-space: pre-wrap; word-break: break-word; color: var(--color-text);
  background: var(--surface-soft); border: 1px solid var(--color-border); border-radius: var(--radius-control);
  padding: 10px 12px;
}

/* 色彩 chip 与封边同理：用户数据值（同 memoMetrics 五色豁免口径），非主题表面 */
.chip-tag-blue { background: #3b82f6; color: #fff; }
.chip-tag-emerald { background: #10b981; color: #fff; }
.chip-tag-amber { background: #f59e0b; color: #fff; }
.chip-tag-rose { background: #f43f5e; color: #fff; }
.chip-tag-purple { background: #8b5cf6; color: #fff; }

/* 闲页 */
.pane-idle {
  flex: 1; display: flex; flex-direction: column; align-items: flex-start; justify-content: center; gap: 12px;
  border: 1px dashed var(--color-border); border-radius: var(--radius-element); padding: 24px; min-height: 200px;
}
.idle-note { margin: 0; max-width: 46ch; font-size: var(--text-sm); color: var(--color-text-subtle); line-height: 1.8; }

/* 页内矢量图标基座（私有件；描边随 currentColor，明暗与色板零特判） */
.memo-glyph { display: inline-block; vertical-align: -0.125em; flex: none; fill: none; stroke: currentColor; stroke-width: 1.7; stroke-linecap: round; stroke-linejoin: round; }

.seg { display: flex; border: 1px solid var(--color-border); border-radius: var(--radius-control); overflow: hidden; flex: none; margin-left: auto; }
.seg-btn {
  background: var(--surface-soft); border: none; color: var(--color-text-muted);
  padding: 3px 12px; font-size: var(--text-sm); cursor: pointer;
  transition: background var(--motion-fast) ease, color var(--motion-fast) ease;
}
.seg-btn:hover { background: var(--surface-hover); color: var(--color-text); }
.seg-btn.active { background: var(--color-primary); color: var(--color-on-primary); }

/* Markdown 阅读/预览皮肤：全走 token，随主题联动 */
.md-preview { font-size: var(--text-base); line-height: 1.65; color: var(--color-text); overflow-wrap: anywhere; }
.md-preview :deep(h1), .md-preview :deep(h2), .md-preview :deep(h3),
.md-preview :deep(h4), .md-preview :deep(h5), .md-preview :deep(h6) {
  margin: 12px 0 6px; line-height: 1.4; color: var(--color-text);
}
.md-preview :deep(h1) { font-size: var(--text-lg); }
.md-preview :deep(h2) { font-size: var(--text-md); }
.md-preview :deep(h3), .md-preview :deep(h4), .md-preview :deep(h5), .md-preview :deep(h6) { font-size: var(--text-base); }
.md-preview :deep(h1:first-child), .md-preview :deep(h2:first-child), .md-preview :deep(h3:first-child) { margin-top: 0; }
.md-preview :deep(p) { margin: 6px 0; }
.md-preview :deep(ul), .md-preview :deep(ol) { margin: 6px 0; padding-left: 22px; }
.md-preview :deep(li) { margin: 2px 0; }
.md-preview :deep(code) {
  font-family: var(--font-mono); font-size: var(--text-sm); background: var(--surface-hover);
  border: 1px solid var(--color-border); border-radius: var(--radius-micro); padding: 1px 5px;
}
.md-preview :deep(pre) {
  background: var(--surface-soft); border: 1px solid var(--color-border); border-radius: var(--radius-control);
  padding: 10px 12px; overflow-x: auto; margin: 8px 0;
}
.md-preview :deep(pre code) { background: none; border: none; padding: 0; line-height: 1.55; }
.md-preview :deep(blockquote) {
  margin: 8px 0; padding: 2px 12px; border-left: 3px solid var(--color-border-strong);
  color: var(--color-text-muted); background: var(--surface-soft); border-radius: 0 var(--radius-control) var(--radius-control) 0;
}
.md-preview :deep(hr) { border: none; border-top: 1px solid var(--color-border); margin: 12px 0; }
.md-preview :deep(a) { color: var(--color-primary); }
.md-preview :deep(table) { border-collapse: collapse; }
.md-preview :deep(del) { color: var(--color-text-subtle); }

/* 叠放档（容器查询看主区实际宽度）：页面板在上、索引在下——"先落笔，再翻账" */
@container memo (max-width: 760px) {
  .memo-desk { grid-template-columns: minmax(0, 1fr); }
  .memo-pane { order: -1; max-height: none; }
  .memo-index { max-height: 40dvh; }
  .pane-body, .write-body { max-width: none; }
}
</style>
