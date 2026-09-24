<script setup lang="ts">
// Ctrl+K 命令面板（外壳重设计 · 方案 C 启动器浮层，草图 mockup-c-launcher.html 下半部）。
// 自包含组件：全局热键（Ctrl/⌘+K）toggle、Teleport 浮层、模糊搜索、键盘选单全部自理，
// 暂未接线 App.vue——导航动作只上抛 navigate(route)，路由切换/门禁编排由宿主完成。
// 设计纪律：配色全走 token（--overlay-mask 遮罩 + 2px backdrop-blur 为壳层唯一允许模糊处）；
// 图标零 emoji 零散写 svg——导航项图标沿用 AppSidebar 双轨制（`i:` 前缀经 AppIcon，其余文本回退），
// 面板 chrome 图标（search/goto）按注册表存在性守卫，缺失时优雅降级不渲染。
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import AppIcon from '../ui/AppIcon.vue'
import { appIconName } from '../../constants/appIcons'
import { ICON_NAMES, type IconName } from '../../constants/icons'
import { GROUP_META, type NavGroup } from '../../constants/navigation'
import type { NavEntry } from '../../../bindings/hanxi/internal/extapi/models'

const props = defineProps<{
  /** 后端注册表下发的模块导航清单（可传响应式数组；NavEntry.group 由 bindings 下发） */
  navs: NavEntry[]
  /** 禁用热键与浮层（如宿主处于模态流程中） */
  disabled?: boolean
}>()

const emit = defineEmits<{
  (e: 'navigate', route: string): void
}>()

/** 无 group（后端 omitempty 期）或未知组 id 的收纳段，排在全部已知组之后。 */
const UNGROUPED = { title: '其他', desc: '', icon: '', order: 99 }

function groupMetaOf(group?: string): (typeof GROUP_META)[NavGroup] | typeof UNGROUPED {
  return GROUP_META[group as NavGroup] ?? UNGROUPED
}

// ---------- 搜索匹配 ----------

/** 结果条数上限（草图契约）。 */
const MAX_RESULTS = 12

/** 文本片段：hit=true 的段以 <em> 主色高亮。 */
interface Seg { text: string; hit: boolean }

interface Candidate {
  nav: NavEntry
  groupKey: string
  groupTitle: string
  groupOrder: number
  score: number
  titleSegs: Seg[]
  routeSegs: Seg[]
  groupSegs: Seg[]
}

/** 最左贪心子序列匹配：needle 已小写；不命中返回 null，空 needle 视为全命中（无高亮）。 */
function subseqIndexes(hay: string, needle: string): number[] | null {
  if (!needle) return []
  const h = hay.toLowerCase()
  let pos = -1
  const idx: number[] = []
  for (const ch of needle) {
    const at = h.indexOf(ch, pos + 1)
    if (at < 0) return null
    idx.push(at)
    pos = at
  }
  return idx
}

/** 单字段评分：字段权重（标题 0 < 路由段 1 < 组名 2）× 千 + 首命中位置 + 字符间距（越小越优）。 */
function fieldScore(idx: number[], needleLen: number, weight: number): number {
  const gap = idx[idx.length - 1] - idx[0] + 1 - needleLen
  return weight * 10000 + idx[0] * 10 + gap
}

/** 按命中位置集把文本切成 <em> 片段。 */
function segments(hay: string, idx: number[] | null): Seg[] {
  if (!hay) return []
  if (!idx || !idx.length) return [{ text: hay, hit: false }]
  const hits = new Set(idx)
  const out: Seg[] = []
  let buf = hay[0]
  let bufHit = hits.has(0)
  for (let i = 1; i < hay.length; i++) {
    const h = hits.has(i)
    if (h !== bufHit) {
      out.push({ text: buf, hit: bufHit })
      buf = hay[i]
      bufHit = h
    } else {
      buf += hay[i]
    }
  }
  out.push({ text: buf, hit: bufHit })
  return out
}

function routeTail(route: string): string {
  const parts = route.split('/')
  return parts[parts.length - 1] || route
}

const query = ref('')

/** 全局评分排序取前 12；浏览态再按组分段，查询态保持相关度序。 */
const results = computed<Candidate[]>(() => {
  const q = query.value.trim().toLowerCase()
  const matched: Candidate[] = []
  for (const nav of props.navs) {
    const groupKey = nav.group ?? ''
    const meta = groupMetaOf(groupKey)
    const tail = routeTail(nav.route)
    let score = 0
    let tIdx: number[] | null = null
    let rIdx: number[] | null = null
    let gIdx: number[] | null = null
    if (q) {
      tIdx = subseqIndexes(nav.title, q)
      rIdx = subseqIndexes(tail, q)
      gIdx = subseqIndexes(meta.title, q)
      const cands: number[] = []
      if (tIdx) cands.push(fieldScore(tIdx, q.length, 0))
      if (rIdx) cands.push(fieldScore(rIdx, q.length, 1))
      if (gIdx) cands.push(fieldScore(gIdx, q.length, 2))
      if (!cands.length) continue
      score = Math.min(...cands)
    }
    matched.push({
      nav, groupKey, groupTitle: meta.title, groupOrder: meta.order, score,
      titleSegs: segments(nav.title, tIdx),
      // 高亮渲染在整个 route 串上，故把末段命中索引平移回全串坐标
      routeSegs: segments(nav.route, rIdx ? rIdx.map((i) => i + nav.route.length - tail.length) : null),
      groupSegs: segments(meta.title, gIdx),
    })
  }
  matched.sort(
    (a, b) =>
      a.score - b.score ||
      a.nav.order - b.nav.order ||
      a.nav.title.localeCompare(b.nav.title, 'zh'),
  )
  const top = matched.slice(0, MAX_RESULTS)
  if (!q) {
    // 浏览态：按组分段（组内保持 order 序，Array.sort 稳定）；
    // 查询态：保持相关度序（草图"跳转模块"列表同款，释放端口可先于组序更靠前的条目）。
    top.sort((a, b) => a.groupOrder - b.groupOrder)
  }
  return top
})

// ---------- 浮层状态与键盘 ----------

const open = ref(false)
const selectedIdx = ref(0)
const composing = ref(false)
const inputEl = ref<HTMLInputElement | null>(null)
let previousFocus: Element | null = null
let previousBodyOverflow = ''

function show() {
  query.value = ''
  selectedIdx.value = 0
  open.value = true
}

function hide() {
  open.value = false
  query.value = ''
}

function toggle() {
  if (open.value) hide()
  else show()
}

/** 选中项 → 上抛路由并收起；越界索引静默忽略。 */
function activate(i: number) {
  const item = results.value[i]
  if (!item) return
  emit('navigate', item.nav.route)
  hide()
}

function move(delta: number) {
  const n = results.value.length
  if (!n) return
  selectedIdx.value = (selectedIdx.value + delta + n) % n
}

// 查询变化选中归零；结果收缩时钳制越界选中（循环导航的安全网）。
watch(query, () => { selectedIdx.value = 0 })
watch(results, (list) => {
  if (selectedIdx.value >= list.length) selectedIdx.value = 0
})

// 打开：锁 body 滚动、记录并归还焦点（关闭时还原）；沿用 UiPromptDialog 焦点契约。
watch(open, async (isOpen) => {
  if (isOpen) {
    previousFocus = document.activeElement
    previousBodyOverflow = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    await nextTick()
    inputEl.value?.focus()
  } else {
    document.body.style.overflow = previousBodyOverflow
    if (previousFocus instanceof HTMLElement && previousFocus !== document.activeElement) {
      previousFocus.focus()
    }
    previousFocus = null
  }
})

function onGlobalKeydown(e: KeyboardEvent) {
  if ((e.metaKey || e.ctrlKey) && !e.altKey && (e.key === 'k' || e.key === 'K')) {
    if (props.disabled || e.isComposing || composing.value) return
    e.preventDefault()
    toggle()
    return
  }
  if (!open.value) return
  // 中文输入法 composing 期间不劫持按键（↑↓/数字是候选词键，Enter 是上屏键）。
  if (e.isComposing || composing.value || e.key === 'Process') return
  if (e.key === 'Escape') {
    e.preventDefault()
    hide()
  } else if (e.key === 'ArrowDown') {
    e.preventDefault()
    move(1)
  } else if (e.key === 'ArrowUp') {
    e.preventDefault()
    move(-1)
  } else if (e.key === 'Enter') {
    e.preventDefault()
    activate(selectedIdx.value)
  } else if (/^[1-9]$/.test(e.key) && !e.shiftKey && !e.ctrlKey && !e.metaKey && !e.altKey) {
    e.preventDefault()
    activate(Number(e.key) - 1)
  }
}

onMounted(() => window.addEventListener('keydown', onGlobalKeydown))
onBeforeUnmount(() => {
  window.removeEventListener('keydown', onGlobalKeydown)
  if (open.value) document.body.style.overflow = previousBodyOverflow
})

// ---------- 图标双轨（与 AppSidebar 同契约） ----------

function isRegistered(name: string): boolean {
  return (ICON_NAMES as string[]).includes(name)
}

// 导航项图标三轨（N27 批 B）：登记 `i:` 矢量与 `app:` 真图标走 AppIcon、未登记空占位、
// 其余文本回退——解析单点在 constants/appIcons（模板经 appIconName 直判）。

// 面板 chrome 图标（search/goto）依赖 icons.ts 阶段 2 登记，缺失时优雅降级为不渲染
// （禁止散写 svg / emoji 兜底），登记后自动启用。
const hasSearchIcon = computed(() => isRegistered('search'))
const hasGotoIcon = computed(() => isRegistered('goto'))
</script>

<template>
  <Teleport to="body">
    <div v-if="open" class="palette-mask" @mousedown.self="hide()">
      <section class="palette" role="dialog" aria-modal="true" aria-label="命令面板">
        <div class="p-input">
          <AppIcon v-if="hasSearchIcon" :name="'search' as IconName" :size="17" class="p-search-ic" />
          <input
            ref="inputEl"
            v-model="query"
            type="text"
            placeholder="搜索模块…"
            autocomplete="off"
            spellcheck="false"
            aria-label="搜索模块"
            @compositionstart="composing = true"
            @compositionend="composing = false"
          />
        </div>

        <div class="p-list">
          <template v-for="(item, i) in results" :key="item.nav.route">
            <div v-if="i === 0 || results[i - 1].groupKey !== item.groupKey" class="p-sec">
              <template v-for="(seg, j) in item.groupSegs" :key="j">
                <em v-if="seg.hit">{{ seg.text }}</em>
                <span v-else>{{ seg.text }}</span>
              </template>
            </div>
            <button
              type="button"
              class="p-item"
              :class="{ sel: i === selectedIdx }"
              @mouseenter="selectedIdx = i"
              @click="activate(i)"
            >
              <span class="p-ic">
                <AppIcon v-if="appIconName(item.nav.icon)" :name="appIconName(item.nav.icon)!" :size="14" />
              </span>
              <span class="p-body">
                <span class="p-name">
                  <template v-for="(seg, j) in item.titleSegs" :key="j">
                    <em v-if="seg.hit">{{ seg.text }}</em>
                    <span v-else>{{ seg.text }}</span>
                  </template>
                </span>
                <span class="p-route">
                  <template v-for="(seg, j) in item.routeSegs" :key="j">
                    <em v-if="seg.hit">{{ seg.text }}</em>
                    <span v-else>{{ seg.text }}</span>
                  </template>
                </span>
              </span>
              <AppIcon v-if="i === selectedIdx && hasGotoIcon" :name="'goto' as IconName" :size="14" class="p-goto" />
            </button>
          </template>
          <div v-if="!results.length" class="p-empty">未找到匹配模块</div>
        </div>

        <div class="p-foot">
          <span><kbd>↑↓</kbd> 导航</span>
          <span><kbd>⏎</kbd> 打开</span>
          <span><kbd>1–9</kbd> 快捷启动</span>
          <span><kbd>Esc</kbd> 关闭</span>
        </div>
      </section>
    </div>
  </Teleport>
</template>

<style scoped>
/* 量取 mockup-c-launcher.html 浮层段；颜色/圆角/阴影全走 tokens.css，无新增全局样式。 */
.palette-mask {
  position: fixed;
  inset: 0;
  z-index: 100000; /* 高于通知抽屉(10002)与浮层(9999)，低于 Toast(999999) */
  display: flex;
  justify-content: center;
  align-items: flex-start;
  padding: 9vh 16px 16px;
  background: var(--overlay-mask);
  backdrop-filter: blur(2px); /* 壳层唯一允许模糊处（草图契约，≤4px） */
}
.palette {
  width: 560px;
  max-width: 100%;
  height: fit-content;
  display: flex;
  flex-direction: column;
  background: var(--surface-panel);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-panel);
  box-shadow: var(--shadow-panel);
  overflow: hidden;
  animation: hx-palette-in 120ms ease-out; /* 出现动效：淡入 + 上移 4px，符合 100–180ms 红线 */
}
@keyframes hx-palette-in {
  from { opacity: 0; transform: translateY(-4px); }
  to { opacity: 1; transform: none; }
}
@media (prefers-reduced-motion: reduce) {
  .palette { animation: none; }
}
.p-input {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 14px 16px;
  border-bottom: 1px solid var(--color-border);
  color: var(--color-primary);
}
.p-input input {
  flex: 1;
  min-width: 0;
  border: none;
  outline: none;
  background: transparent;
  color: var(--color-text);
  font-family: inherit;
  font-size: var(--text-md);
  font-weight: 600;
  caret-color: var(--color-primary);
}
.p-input input::placeholder {
  color: var(--color-text-subtle);
  font-weight: 400;
}
.p-list {
  max-height: min(52vh, 420px);
  overflow-y: auto;
  padding: 4px 0 8px;
}
.p-sec {
  padding: 8px 16px 4px;
  font-size: var(--text-micro);
  font-weight: 700;
  letter-spacing: 0.5px;
  text-transform: uppercase;
  color: var(--color-text-subtle);
}
.p-item {
  display: flex;
  align-items: center;
  gap: 10px;
  width: 100%;
  padding: 9px 16px;
  border: none;
  background: transparent;
  text-align: left;
  cursor: pointer;
  font-family: inherit;
  color: var(--color-text);
}
.p-item.sel {
  background: var(--surface-selected);
}
.p-ic {
  width: 26px;
  height: 26px;
  flex: none;
  border-radius: 7px;
  background: var(--surface-selected);
  color: var(--color-primary);
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: var(--text-base);
}
.p-body {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 1px;
}
.p-name {
  font-size: var(--text-base);
  font-weight: 650;
  line-height: 1.35;
}
.p-route {
  font-family: var(--font-mono);
  font-size: var(--text-micro);
  color: var(--color-text-subtle);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.p-sec em,
.p-name em,
.p-route em {
  font-style: normal;
  color: var(--color-primary);
}
.p-goto {
  color: var(--color-text-subtle);
}
.p-empty {
  padding: 26px 16px;
  text-align: center;
  font-size: var(--text-sm);
  color: var(--color-text-subtle);
}
.p-foot {
  display: flex;
  gap: 14px;
  padding: 9px 16px;
  border-top: 1px solid var(--color-border);
  background: var(--surface-soft);
  font-size: var(--text-micro);
  color: var(--color-text-subtle);
}
.p-foot kbd {
  font-family: var(--font-mono);
  font-size: var(--text-micro);
  font-weight: 600;
  color: var(--color-text-muted);
  border: 1px solid var(--color-border-strong);
  border-bottom-width: 2px;
  border-radius: 5px;
  padding: 1px 5px;
  background: var(--surface-panel);
}
</style>
