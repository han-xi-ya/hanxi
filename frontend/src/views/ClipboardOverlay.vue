<script setup lang="ts">
// 剪贴板浮层（剪贴板内置 A5 线 · R-F1 体验改造轮）：后端 ClipboardService 按全局
// 热键（Ctrl+Alt+V）唤出的独立 frameless 真透明置顶小窗内容（main.ts 按
// #clipboardoverlay hash 分流挂载并打 .popup-shell——接线归收口阶段，卡体由本页
// 自绘，透明窗纪律 #50：窗口本体不允许有任何实底露出，投影外的边距全部透明）。
//
// 契约锚点 docs/plans/2026-09-26-clipboard-contract.md §4/§6/§7 与 §12 v1.7：
// 后端只经 @/adapters/clipboard 门面访问（未接线时抛中文可读错误 → 浮层内错误条，
// 不白屏），事件用 useWailsEvent + CLIP_EV 常量订阅。
//
// ── R-F1 对机主四条差评的逐条回应（本轮核心翻转，旧注释口径以本节为准）──
//   1. 真缩略图（R-G2）：image 行直接渲染 entry.thumb（64px JPEG dataURL，
//      List 即带、前端零额外请求），48px 图框展示；thumb 缺失（历史存量未回填、
//      原图超 8MiB 跳过生成或 dataURL 解码失败）才回落「图」字形位 + 尺寸元数据行。
//      旧版"刻意不调 Get、浮层不显图"的决策由 R-G2 的 List 内嵌 thumb 打破，作废。
//   2. 键盘 3 步闭环（R-G1）：唤起（热键）→ ↑↓ 环游/输入即搜定位 → ⏎/1-9 选取，
//      选取即 clipboardPaste(id)：回填 + 焦点还给唤出浮层时的原窗 + 自动 Ctrl+V，
//      不再只是 Set。目标纯键盘全程 ≤3 步；↑↓ 从旧版"首尾夹紧"改为环绕环游。
//   3. 信息密度：行形制从 26px 单行重排为两行（13px preview+徽标 / 11px 来源·
//      尺寸·标签·时间），来源窗口标题与 autoTags 从 tooltip 升到可见面；
//      640×520 窗几何（Go 侧冻结）内一屏约 9-10 行，不挤不空。
//   4. image/file 首版边界（R-G1 裁决"维持首版回填边界"）：前端**不按 kind 分叉**、
//      一律调 Paste，后端对非 text 返回可读边界说明 → 浮层 err 提示上台、保窗。
//      后端未来放开图片粘贴时前端零改动即点亮；旧版按 kind 走 Set 的路由一并废除。
//
// 其余交互契约（沿用既有正确行为，不回退）：
//   - 顶栏一行 = 搜索框（打开即聚焦；opening 事件清稿重聚焦重拉）+ 暂停角标 + 计数；
//   - 列表 = 置顶组（pinned 与 manual 同组，行有徽标）在前、近期在后（服务端序已
//     新→旧，filter 稳定保序）；
//   - 数字键取舍：1-9 直选与搜索框聚焦天然打架——搜索词为空时裸数字键拦截为直选
//     （preventDefault 不落字），有词时数字让位给搜索词；Alt+1-9 任何时候直选；
//   - Enter = 直选环上项、Esc 收窗；组字豁免走三重盾（isComposing ||
//     compositionstart/end 本地旗标 || key==='Process'，CommandPalette 形制）；
//   - 收窗：Paste 成功或 Esc 后先 input.blur 摘光标、必调 CollapseOverlay 显式 RPC
//     请 Go 藏窗（QuickMemoSheet 走 HideQuickSheet 显式 RPC 同先例；A8 对抗审查 H1
//     钉死：顶层独立窗里 window.blur() 按 WHATWG 是 no-op，唤不动 Go 的
//     WindowLostFocus，"失焦即收"在生产态收不掉窗）。RPC 失败静默——藏窗竞态/未接线
//     时 Go 侧热键与 TTL 两道闸兜底。绝不用 window.close 越权；Paste 失败保窗给轻
//     提示（不吞反馈，换一条还能重试）；
//   - 行 hover/环上出快操作：置顶切换（就地回排）/ 复制不关窗（连取，语义=Set 只
//     回填不抢焦点，与主界面行复制钮同款口径 R-G1）；
//   - clipboard:updated 空搜索时乐观顶置（去重语义与服务端一致），带词时静默重拉；
//     removed 本地摘行；paused 事件直接喂角标，不等 GetStatus。
// 独立窗没有工作台 toast 宿主（族规同 QuickMemoSheet/SnipCardView）：动作反馈走
// 卡内轻提示槽位（常驻占位防跳动、错误恒小字），列表级失败走浮层内错误条。
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import {
  CLIP_EV,
  clipboardCollapseOverlay,
  clipboardGetStatus,
  clipboardList,
  clipboardPaste,
  clipboardSet,
  clipboardTogglePin,
} from '../adapters/clipboard'
import type { ClipEntry, ClipKind, ClipPaused, ClipRemoved } from '../types/clipboard'
import { useWailsEvent } from '../composables/useWailsEvent'
import { getErrorMessage } from '../utils/errors'
import { fmtSize } from '../utils/format'

/** 取数上限：浮层两行制一屏 9-10 行，滚动到 100 条即免翻找的甜点位（契约 §4 limit≤500 内）。 */
const LIST_LIMIT = 100
const FLY_MS = 200 // 入场动画 160ms + 收尾余量（同速记卡钩子定时器口径）

const query = ref('')
const raw = ref<ClipEntry[]>([])
const paused = ref(false)
const loading = ref(false)
const errorBar = ref('')
const tip = ref('')
const tipKind = ref<'ok' | 'err'>('ok')
const selected = ref(0)
const entering = ref(false)
const composing = ref(false) // IME 组字旗标（CommandPalette 同谱三重盾的本地那面）
const searchEl = ref<HTMLInputElement | null>(null)
const listEl = ref<HTMLElement | null>(null)

// dataURL 解码失败（截断/损坏的存量 thumb）按 id 拉黑回落字形位：thumb 内容寻址
// 恒不变，标记跨重拉保留、随浮层窗 TTL 销毁自然过期，无需清理路径。
const brokenThumbs = ref<ReadonlySet<string>>(new Set())

// 竞态收口序号：连打搜索词时乱序返回的旧响应整体丢弃，列表恒跟最新一次请求走。
let reqSeq = 0
let enterTimer: number | undefined
let settling = false // Paste 提交单发闸：连点/回车撞键不双发粘贴链

const isTop = (e: ClipEntry): boolean => !!(e.pinned || e.manual)
// 置顶组在前（pinned 与手建 manual 都是不淘汰项，同组顶置；manual 行另挂「定」徽标）
const rows = computed<ClipEntry[]>(() => [
  ...raw.value.filter(isTop),
  ...raw.value.filter((e) => !isTop(e)),
])

const KIND_GLYPH: Record<ClipKind, string> = { text: '文', image: '图', file: '件' }
const KIND_TITLE: Record<ClipKind, string> = { text: '文本', image: '图片', file: '文件' }

// 未知 kind 兜底（L4）：前后端版本错位时 wire 可能带进新类别，不渲空位、
// tooltip 回显原文 kind 供排障（前端 types/clipboard.ts 联合是编译期契约非运行时保证）。
function kindGlyph(e: ClipEntry): string {
  return KIND_GLYPH[e.kind] ?? '?'
}
function kindTitle(e: ClipEntry): string {
  return KIND_TITLE[e.kind] ?? `未知类型(${e.kind})`
}

/** 可渲缩略图：仅 image 且带 thumb 且未解码失败过；其余回落「图」字形位（R-G2）。 */
function showThumb(e: ClipEntry): string | null {
  return e.kind === 'image' && e.thumb && !brokenThumbs.value.has(e.id) ? e.thumb : null
}

function onThumbFail(id: string) {
  const next = new Set(brokenThumbs.value)
  next.add(id)
  brokenThumbs.value = next
}

/**
 * epoch 毫秒 → 刚刚 / N 分钟前 / N 小时前 / 月日（跨年带年份）。
 * 与 memoMetrics.fmtAgo 三档进位同式但入参域不同（那边收 ISO 串，剪贴板时间戳
 * 是毫秒 number），不跨模块硬凑；now 可注入测试。
 */
function fmtAgoMs(ms: number, now: number = Date.now()): string {
  if (!ms) return '—'
  const min = Math.floor((now - ms) / 60000)
  if (min < 1) return '刚刚'
  if (min < 60) return `${min} 分钟前`
  if (min < 24 * 60) return `${Math.floor(min / 60)} 小时前`
  const d = new Date(ms)
  const pad = (n: number) => String(n).padStart(2, '0')
  const md = `${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
  return d.getFullYear() === new Date(now).getFullYear() ? md : `${d.getFullYear()}-${md}`
}

/** 展示时间取最近活跃（去重顶置会刷 lastUsedAt，createdAt 恒为首次复制时间）。 */
const entryTime = (e: ClipEntry): number => e.lastUsedAt || e.createdAt

function rowPreview(e: ClipEntry): string {
  if (e.preview) return e.preview
  if (e.kind === 'file') return e.files?.[0] ?? '(无路径)'
  if (e.kind === 'image') return '(图片)'
  return '(空内容)'
}

/** 行内元数据列（第二行）：image 给尺寸·大小、file 给件数；thumb 缺位时这是识图主线索。 */
function rowMeta(e: ClipEntry): string {
  if (e.kind === 'image') {
    const dim = e.width && e.height ? `${e.width}×${e.height}` : ''
    return [dim, fmtSize(e.byteSize)].filter(Boolean).join(' · ')
  }
  if (e.kind === 'file') {
    const n = e.files?.length ?? 0
    return n > 1 ? `共 ${n} 项` : ''
  }
  return ''
}

function rowTitle(e: ClipEntry): string {
  const parts = [rowPreview(e)]
  const files = e.files ?? []
  if (e.kind === 'file' && files.length > 1) parts.push(files.join('\n'))
  if (e.sourceApp) parts.push(`来源: ${e.sourceApp}`)
  if (e.autoTags?.length) parts.push(`标签: ${e.autoTags.join(' / ')}`)
  return parts.join('\n')
}

function showTip(msg: string, kind: 'ok' | 'err') {
  tip.value = msg
  tipKind.value = kind
}

async function fetchList() {
  const seq = ++reqSeq
  loading.value = true
  try {
    const data = await clipboardList(query.value.trim(), '', LIST_LIMIT)
    if (seq !== reqSeq) return
    raw.value = Array.isArray(data) ? data : []
    if (selected.value >= raw.value.length) selected.value = 0
    errorBar.value = ''
  } catch (err: unknown) {
    // adapter 未接线是同步 throw（need()），一并走这里；有账在屏时后台失败只轻提示
    // 不掀可用列表，空账（含首次唤出）才让错误条上台——浮层任何状态下都有可见反馈
    if (seq !== reqSeq) return
    reportClipError(getErrorMessage(err))
  } finally {
    if (seq === reqSeq) loading.value = false
  }
}

async function fetchStatus() {
  try {
    const st = await clipboardGetStatus()
    paused.value = !!st?.paused
  } catch (err: unknown) {
    reportClipError(getErrorMessage(err))
  }
}

// 刻意不叫 reportError：与浏览器全局 API 混名读码有坑
function reportClipError(msg: string) {
  if (raw.value.length) showTip(`刷新失败: ${msg}`, 'err')
  else errorBar.value = msg
}

// 搜索词变化即重拉（后端 List 的 q 子串匹配 Preview/SourceApp/Files/AutoTags）：
// 不防抖——Wails 绑定是进程内调用，单键成本远低于一帧渲染，竞态由 reqSeq 收口。
watch(query, () => {
  selected.value = 0
  void fetchList()
})

/**
 * 收窗走显式 RPC：顶层独立窗里 window.blur() 按 WHATWG 是 no-op，唤不动 Go 的
 * WindowLostFocus（A8 对抗审查 H1），故 CollapseOverlay 必调（QuickMemoSheet 走
 * HideQuickSheet 显式 RPC 同先例）；input.blur 只作光标摘离。RPC 失败静默——
 * 窗口已无处可收的竞态里 Go 侧失焦/热键/TTL 三道闸照旧兜底。
 */
function collapseSelf() {
  searchEl.value?.blur()
  void hideOverlay()
}

async function hideOverlay() {
  try {
    await clipboardCollapseOverlay()
  } catch {
    /* 未接线/藏窗竞态都无妨：不弹错打扰，Go 侧收口出口是最终裁决 */
  }
}

/**
 * 选取即粘贴（R-F1/R-G1）：Paste = 回填 + 焦点还给唤出浮层时的原窗 + 自动 Ctrl+V。
 * 不按 kind 前端分叉：image/file 后端回可读边界说明（首版仅 text 支持自动粘贴），
 * 原样上提示、保窗——错误话术由后端单源，未来放开图片粘贴前端零改动即点亮。
 */
async function pick(e: ClipEntry) {
  if (settling) return
  settling = true
  try {
    await clipboardPaste(e.id)
    collapseSelf()
  } catch (err: unknown) {
    showTip(`粘贴失败: ${getErrorMessage(err)}`, 'err') // 保窗：换一条还能重试
  } finally {
    settling = false
  }
}

/** 复制不关窗（连取）：只 Set 回填，不抢焦点不发消息键（R-G1 与主界面行复制钮同款语义；自写回环 Go 监听侧已跳过不入新条）。 */
async function copyOnly(e: ClipEntry) {
  try {
    await clipboardSet(e.id)
    showTip('已复制，浮层不关', 'ok')
  } catch (err: unknown) {
    showTip(`复制失败: ${getErrorMessage(err)}`, 'err')
  }
}

async function togglePin(e: ClipEntry) {
  try {
    const next = await clipboardTogglePin(e.id)
    const i = raw.value.findIndex((r) => r.id === e.id)
    if (i >= 0) raw.value.splice(i, 1, next?.id === e.id ? next : { ...raw.value[i], pinned: !raw.value[i].pinned })
    showTip(next?.pinned ? '已置顶' : '已取消置顶', 'ok')
  } catch (err: unknown) {
    showTip(`置顶失败: ${getErrorMessage(err)}`, 'err')
  }
}

function scrollActive() {
  try {
    listEl.value?.querySelector('.clip-row.is-active')?.scrollIntoView?.({ block: 'nearest' })
  } catch {
    /* 测试环境无滚动布局，失败无害 */
  }
}

function onKey(e: KeyboardEvent) {
  // 中文输入法组字中的数字/回车只属于选词，不劫持——三重盾对位 CommandPalette
  // 形制：事件旗标 + compositionstart/end 本地旗标（部分内核 end 后事件旗标已落）
  // + key==='Process'（组字期间按键统一上报的浏览器兜底形态）
  if (e.isComposing || composing.value || e.key === 'Process') return
  if (e.key === 'Escape') {
    e.preventDefault()
    collapseSelf()
    return
  }
  if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
    const n = rows.value.length
    if (!n) return
    e.preventDefault()
    // 环绕环游（R-F1 手感重设计）：底部再下回顶、顶部再上到底，长列表不用倒手划
    const d = e.key === 'ArrowDown' ? 1 : -1
    selected.value = (Math.min(selected.value, n - 1) + d + n) % n
    scrollActive()
    return
  }
  if (e.key === 'Enter') {
    // 焦点落在快操作钮上时 Enter 归钮的原生激活，不双打
    if ((e.target as HTMLElement | null)?.closest?.('button')) return
    if (!rows.value.length) return
    e.preventDefault()
    void pick(rows.value[Math.min(selected.value, rows.value.length - 1)])
    return
  }
  if (/^[1-9]$/.test(e.key) && !e.ctrlKey && !e.metaKey) {
    // 裸数字只在空搜索词时直选（有词时数字是搜索词的一部分，放行输入框）；
    // Alt+数字任何时候直选
    if (!e.altKey && query.value.trim()) return
    const i = Number(e.key) - 1
    if (i >= rows.value.length) return
    e.preventDefault()
    void pick(rows.value[i])
  }
}

// 入场动画由 opening 的 is-enter 钩子驱动（挂载首帧窗还藏着，不白烧动画）——速记卡同谱
function replayEnter() {
  window.clearTimeout(enterTimer)
  entering.value = false
  void nextTick(() => {
    entering.value = true
    enterTimer = window.setTimeout(() => {
      entering.value = false
    }, FLY_MS)
  })
}

useWailsEvent<ClipEntry>(CLIP_EV.updated, (entry) => {
  if (!entry?.id) return
  if (query.value.trim()) {
    void fetchList() // 带词时新条目是否命中未知，静默重拉（错误条不弹，列表留着能用）
    return
  }
  raw.value = [entry, ...raw.value.filter((r) => r.id !== entry.id)].slice(0, LIST_LIMIT)
})
useWailsEvent<ClipRemoved>(CLIP_EV.removed, (p) => {
  if (!p?.id) return
  raw.value = raw.value.filter((r) => r.id !== p.id)
})
useWailsEvent<ClipPaused>(CLIP_EV.paused, (p) => {
  if (p) paused.value = !!p.paused
})
useWailsEvent<void>(CLIP_EV.overlayOpening, () => {
  query.value = '' // 清稿：每次唤出都是全量新会话，搜索词与残错都是干扰
  selected.value = 0
  composing.value = false // 上次若断在组字中途，旗标也是残稿
  tip.value = ''
  errorBar.value = ''
  void fetchList()
  void fetchStatus()
  replayEnter()
  void nextTick(() => searchEl.value?.focus())
})

onMounted(() => {
  window.addEventListener('keydown', onKey)
  void fetchList()
  void fetchStatus()
  void nextTick(() => searchEl.value?.focus())
})
onBeforeUnmount(() => {
  window.removeEventListener('keydown', onKey)
  window.clearTimeout(enterTimer)
})
</script>

<template>
  <div class="clip" role="dialog" aria-label="剪贴板历史浮层">
    <div class="clip-card" :class="{ 'is-enter': entering }">
      <div class="clip-top">
        <input
          ref="searchEl"
          v-model="query"
          class="text-input clip-search"
          type="text"
          placeholder="搜索内容 / 来源 / 标签…（清空后 1-9 直选）"
          aria-label="搜索剪贴板历史"
          @compositionstart="composing = true"
          @compositionend="composing = false"
        />
        <span
          v-if="paused"
          class="chip chip-warning clip-paused"
          title="监听记录已暂停，可在主窗口或托盘恢复"
        >记录已暂停</span>
        <span class="clip-count mono" role="status">{{ loading ? '载入中…' : `${rows.length} 条` }}</span>
      </div>
      <!-- 列表级失败（含 adapter 未接线）：中文错误渲染成浮层内错误条，卡体照旧不白屏 -->
      <p v-if="errorBar" class="clip-errbar" role="alert">剪贴板服务不可用: {{ errorBar }}</p>
      <ul
        v-show="!errorBar"
        ref="listEl"
        class="clip-list"
        role="listbox"
        aria-label="剪贴板条目"
      >
        <li
          v-for="(e, i) in rows"
          :key="e.id"
          class="clip-row"
          :class="{ 'is-active': i === selected }"
          role="option"
          :aria-selected="i === selected"
          :title="rowTitle(e)"
          @mouseenter="selected = i"
          @click="pick(e)"
        >
          <span class="clip-idx mono" aria-hidden="true">{{ i < 9 ? i + 1 : '' }}</span>
          <!-- 真缩略图（R-G2）：64px dataURL 在 48px 图框等比展示；缺位/解码失败回落字形位 -->
          <span v-if="showThumb(e)" class="clip-thumb" aria-hidden="true">
            <img class="clip-thumb-img" :src="showThumb(e)!" alt="" @error="onThumbFail(e.id)" />
          </span>
          <span v-else class="clip-kind mono" aria-hidden="true" :title="kindTitle(e)">{{ kindGlyph(e) }}</span>
          <div class="clip-main">
            <div class="clip-line1">
              <span class="clip-preview">{{ rowPreview(e) }}</span>
              <span v-if="e.sensitive" class="chip chip-danger clip-badge" title="疑似密钥/令牌，MCP 通道整条不外发">敏</span>
              <span v-if="e.manual" class="chip chip-information clip-badge" title="手建固定片段，不参与容量淘汰">定</span>
              <span v-else-if="e.pinned" class="chip chip-neutral clip-badge" title="已置顶">顶</span>
            </div>
            <!-- 第二行可见面（R-F1 信息密度）：来源窗口 · 尺寸/件数 · 嗅探标签 · 相对时间 -->
            <div class="clip-sub">
              <span v-if="e.sourceApp || e.sourceExe" class="clip-src">{{ e.sourceApp || e.sourceExe }}</span>
              <span v-if="rowMeta(e)" class="clip-meta mono">{{ rowMeta(e) }}</span>
              <span v-if="e.autoTags?.length" class="clip-tags mono">{{ e.autoTags.join(' · ') }}</span>
              <span class="clip-time mono">{{ fmtAgoMs(entryTime(e)) }}</span>
            </div>
          </div>
          <span class="clip-acts">
            <button
              type="button"
              class="clip-act"
              tabindex="-1"
              :title="e.pinned ? '取消置顶' : '置顶'"
              @mousedown.prevent
              @click.stop="togglePin(e)"
            >{{ e.pinned ? '解' : '钉' }}</button>
            <button
              type="button"
              class="clip-act"
              tabindex="-1"
              title="复制到剪贴板（只回填不关窗，粘贴请选中条目本身）"
              @mousedown.prevent
              @click.stop="copyOnly(e)"
            >复</button>
          </span>
        </li>
      </ul>
      <p v-if="!loading && !errorBar && !rows.length" class="clip-empty">
        {{ query.trim() ? '没有匹配的条目' : '剪贴板历史还是空的' }}
      </p>
      <div class="clip-foot">
        <span class="clip-hint mono">↑↓ 环选 · ⏎ 或 1-9 粘贴并收 · Alt+1-9 搜索中直选 · Esc 收</span>
        <!-- 提示行常驻占位（速记卡族规）：出现/消失不顶动版面，错误恒小字 -->
        <p class="clip-tip" :class="tip ? (tipKind === 'err' ? 'tip-err' : 'tip-ok') : ''" role="status">{{ tip }}</p>
      </div>
    </div>
  </div>
</template>

<style scoped>
/* 透明窗纪律 #50：.clip 恒透明（14px 边距专门容纳卡体投影，窗口外圈零实底），
   卡体外观本页自绘，配色一律走全局 token；窗口几何 640×520 DIP/摆位/TTL 归 Go 侧。 */
.clip {
  position: fixed;
  inset: 0;
  display: flex;
  flex-direction: column;
  padding: 14px;
  user-select: none;
}
.clip-card {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
  gap: 8px;
  background: var(--surface-panel);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-element);
  box-shadow: var(--shadow-panel);
  padding: 10px 12px 6px;
  transition: border-color var(--motion-base) ease;
}
/* 卡内任一处聚焦：描边向 primary 混 30%（design-system 强调描边配方，N37 零 glow） */
.clip-card:focus-within {
  border-color: color-mix(in srgb, var(--color-primary) 30%, var(--color-border));
}
/* 入场动画由 opening 的 is-enter 钩子驱动（挂载首帧窗还藏着，不做 mount 期白烧） */
.clip-card.is-enter {
  animation: clip-in 160ms ease-out;
}
@keyframes clip-in {
  from { opacity: 0; transform: translateY(-4px); }
  to { opacity: 1; transform: none; }
}

.clip-top {
  flex: none;
  display: flex;
  align-items: center;
  gap: 8px;
}
.clip-search {
  flex: 1 1 auto;
  min-width: 0;
  height: var(--control-h-md);
  padding: 2px 10px;
  font-size: var(--text-base);
  user-select: text;
}
.clip-paused {
  flex: none;
  font-size: var(--text-micro);
}
.clip-count {
  flex: none;
  font-size: var(--text-micro);
  color: var(--color-text-subtle);
  white-space: nowrap;
}

.clip-errbar {
  flex: none;
  margin: 0;
  padding: 4px 8px;
  font-size: var(--text-xs);
  line-height: 1.5;
  color: var(--state-danger);
  background: var(--state-danger-soft);
  border-radius: 6px;
  word-break: break-word;
}

.clip-list {
  flex: 1 1 auto;
  min-height: 0;
  margin: 0;
  padding: 0;
  list-style: none;
  display: flex;
  flex-direction: column;
  gap: 2px;
  overflow-y: auto;
  scrollbar-width: thin;
}
/* 两行制行形（R-F1 密度重排）：一行 preview 主角、二行来源/尺寸/标签/时间副角。
   文本行 ~48px、图片行 ~58px（48 图框），640 宽下"一眼三件"：内容、来路、时效 */
.clip-row {
  flex: none;
  display: flex;
  align-items: center;
  gap: 9px;
  padding: 5px 8px;
  border-radius: var(--radius-control);
  color: var(--color-text);
  cursor: pointer;
}
/* 选中环 = hover 底升一档 + primary 混 30% 内描边（实底+描边表达状态，N37 零 glow；
   环色不单独承担语义——行序号同步转 primary，非色彩通道冗余） */
.clip-row.is-active {
  background: var(--surface-hover);
  box-shadow: inset 0 0 0 1px color-mix(in srgb, var(--color-primary) 30%, var(--color-border));
}
.clip-idx {
  flex: none;
  width: 16px;
  text-align: right;
  font-size: var(--text-micro);
  color: var(--color-text-subtle);
}
.clip-row.is-active .clip-idx {
  color: var(--color-primary);
  font-weight: 600;
}
.clip-kind {
  flex: none;
  width: 24px;
  height: 24px;
  line-height: 24px;
  text-align: center;
  border-radius: var(--radius-micro);
  font-size: var(--text-micro);
  background: var(--surface-soft);
  color: var(--color-text-muted);
}
/* 图框：64px dataURL 等比降到 48px 展示（降采样不糊），描边框住 letterbox 面 */
.clip-thumb {
  flex: none;
  width: 48px;
  height: 48px;
  display: grid;
  place-items: center;
  overflow: hidden;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-micro);
  background: var(--surface-soft);
}
.clip-thumb-img {
  width: 100%;
  height: 100%;
  object-fit: contain;
  display: block;
}
.clip-main {
  flex: 1 1 auto;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.clip-line1 {
  display: flex;
  align-items: center;
  gap: 6px;
  min-width: 0;
  line-height: 1.4;
}
.clip-preview {
  flex: 1 1 auto;
  min-width: 0;
  font-size: var(--text-base);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.clip-badge {
  flex: none;
  font-size: var(--text-micro);
  padding: 0 5px;
}
.clip-sub {
  display: flex;
  align-items: baseline;
  gap: 10px;
  min-width: 0;
  font-size: var(--text-xs);
  line-height: 1.3;
  color: var(--color-text-subtle);
}
.clip-src {
  flex: 0 1 auto;
  min-width: 0;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.clip-meta {
  flex: none;
  white-space: nowrap;
  font-size: var(--text-micro);
}
.clip-tags {
  flex: none;
  max-width: 140px;
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
  font-size: var(--text-micro);
  color: var(--color-text-muted);
}
.clip-time {
  flex: none;
  margin-left: auto;
  white-space: nowrap;
  font-size: var(--text-micro);
}
.clip-acts {
  flex: none;
  display: inline-flex;
  gap: 2px;
  opacity: 0;
}
/* hover 与选中环都亮钮：键盘用户经 ↑↓ 走到行上同样拿得到快操作 */
.clip-row:hover .clip-acts,
.clip-row.is-active .clip-acts {
  opacity: 1;
}
.clip-act {
  border: none;
  background: transparent;
  padding: 3px 5px;
  font-size: var(--text-micro);
  line-height: 1;
  color: var(--color-text-muted);
  border-radius: 4px;
  cursor: pointer;
}
.clip-act:hover {
  background: var(--surface-panel);
  color: var(--color-text);
}

.clip-empty {
  flex: none;
  margin: 0;
  padding: 18px 8px;
  text-align: center;
  font-size: var(--text-xs);
  color: var(--color-text-subtle);
}

.clip-foot {
  flex: none;
  display: flex;
  align-items: baseline;
  gap: 10px;
  min-height: calc(var(--text-xs) * 1.5);
}
.clip-hint {
  flex: none;
  font-size: var(--text-micro);
  color: var(--color-text-subtle);
  white-space: nowrap;
}
.clip-tip {
  flex: 1 1 auto;
  margin: 0;
  text-align: right;
  font-size: var(--text-xs);
  line-height: 1.5;
  color: var(--color-text-subtle);
  word-break: break-word;
}
.clip-tip.tip-ok {
  color: var(--state-positive);
}
.clip-tip.tip-err {
  color: var(--state-danger);
}

/* 减弱动效组件级兜底（族规同 QuickMemoSheet/SnipCardView）：入场归零，
   base.css 全局块仍是最后防线 */
@media (prefers-reduced-motion: reduce) {
  .clip-card,
  .clip-card *,
  .clip-card::before,
  .clip-card::after {
    transition: none;
    animation: none;
  }
}
</style>
