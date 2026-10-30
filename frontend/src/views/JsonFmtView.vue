<script setup lang="ts">
// JsonFmtView：JSON 美化工作台。产品口径 = 「少动作、少等待」——
// 输入变化后约 300ms 防抖自动调 Beautify，开关变化立即重算；手动「美化」钮
// 只是兜底保留（并会吃掉待发防抖，不双发）。
// 一切判定（解析、剔前缀、转义还原、MaxInputBytes 超限）都交给后端
// internal/modules/jsonfmt 的 Beautify：前端不装第二套解析器、不预校验——
// 两处校验迟早漂移，后端是唯一真相，其 error/warnings 文案原样上屏。
import { computed, nextTick, onUnmounted, ref, watch } from 'vue'
import * as JsonFmtAPI from '../../bindings/hanxi/internal/modules/jsonfmt'
import type { JsonFmtRequest, JsonFmtResult, Replace as JsonFmtReplace } from '../../bindings/hanxi/internal/modules/jsonfmt/models'
import { getErrorMessage } from '../utils/errors'
import PageHeader from '../components/ui/PageHeader.vue'
import UiBanner from '../components/ui/UiBanner.vue'
import UiStatusChip from '../components/ui/UiStatusChip.vue'
import UiClipboardField from '../components/ui/UiClipboardField.vue'
import UiButton from '../components/ui/UiButton.vue'
import { useClipboard } from '../composables/useClipboard'
import { useConfirm } from '../composables/useConfirm'

const { copyWithToast } = useClipboard()
// 外发确认走全局 useConfirm 单例（与剪贴板清空的危险动作同一口径），不引原生 window.confirm
const { confirm } = useConfirm()

// 执行替换：唯一写入 activeRepl 的入口（显式按钮，绝不挂防抖）。
// 写进状态而不是当次参数，是为了"之后改缩进/排序时仍带着同一条替换重算"——
// 调个缩进就把刚做的替换弄丢，比不做更让人恼火。
function doReplace() {
  const path = replPath.value.trim()
  if (!path) {
    transportError.value = '替换需要路径（可在查找结果里点「→ 替换」填进来）'
    return
  }
  if (!replValue.value.trim() && !replKey.value.trim()) {
    transportError.value = '请给出新值或新键名，至少一个'
    return
  }
  transportError.value = ''
  activeRepl.value = { path, newValue: replValue.value, newKey: replKey.value.trim() }
  void beautifyNow()
}

// 撤销：原文从未被写入，清掉条件重算一次就是原样。
function undoReplace() {
  activeRepl.value = null
  transportError.value = ''
  void beautifyNow()
}

function usePathFromHit(p: string) {
  replPath.value = p
}

const KIND_LABEL: Record<string, string> = {
  object: '对象',
  array: '数组',
  scalar: '标量',
  multi: '多段',
}
const kindLabel = (k: string) => KIND_LABEL[k] ?? k
const query = ref('')

// 查找/替换/统计三块各占一张卡的话，竖向空间全被边框与内距吃掉，而"看 JSON"才是要大的那块。
// 合成一条工具栏，同一时刻只渲染一块；默认落在查找（替换与统计是"要看才看"）。
const TOOLS = [
  { k: 'find', label: '🔍 查找' },
  { k: 'replace', label: '🔧 替换' },
  { k: 'stats', label: '📊 键路径统计' },
] as const
const tool = ref<'find' | 'replace' | 'stats'>('find')

// 替换的三条输入 + "当前已生效的那条"。已生效的要留着：用户改缩进/排序时
// 应当带着同一条替换重算，否则"调一下缩进就把刚做的替换弄丢了"会很莫名。
const replPath = ref('')
const replValue = ref('')
const replKey = ref('')
const activeRepl = ref<JsonFmtReplace | null>(null)

// 修复处数与明细：宽松语法修复 + 补闭合符合并计数。这两个都改过用户的文本，
// 不能只藏在 tooltip 里——鼠标不悬停也该看得见自己贴的东西被动过。
const repairCount = (r: JsonFmtResult | null) =>
  r ? (r.repairs ?? []).reduce((a, b) => a + b.count, 0) + (r.repaired ?? 0) : 0
const repairText = (r: JsonFmtResult | null) => {
  if (!r) return ''
  const parts = (r.repairs ?? []).map((x) => `${x.kind} ${x.count} 处`)
  if (r.repaired > 0) parts.push(`补了 ${r.repaired} 个闭合符（修复态，末尾内容可能本就残缺）`)
  return parts.join('；')
}

// 自动节拍：快过用户贴完回看的间隔，慢过连续击键的抖动
const DEBOUNCE_MS = 300

// —— 三个开关（低频项不抢主路径：缩进 2、键排序默认关保原顺序、转义还原默认开）——
const indent = ref(2) // 2 | 4 | 0 = 压缩输出
const sortKeys = ref(false)
const unescape = ref(true)

const inputText = ref('')
const running = ref(false)
const lastResult = ref<JsonFmtResult | null>(null)
// RPC 调用本身失败（传输层）与后端"解析了说不对"（ok=false）两件事分开：
// 前者是服务不可用（如模块未就绪），后者是内容问题——行/列/snippet 走错误横幅。
const transportError = ref('')
// AI 提取：在途与失败各占一条通道。失败单列是因为它与"解析失败"（ok=false）、
// "服务调用失败"（transportError）都不是一回事——AI 那条要去看环境（没装 CLI、
// 没登录、超时、返回的不是 JSON）；而且失败时上一轮本地结果必须原样留着，
// 不能因为 AI 不行就把用户已经拿在手里的东西砸掉。
const aiBusy = ref(false)
// 确认框待决期间另有一档：aiBusy 是在 await confirm 之后才置的，只靠它禁用按钮，
// 弹窗那几十秒里按钮一直可点——双击就是两发并发外发（且 finally 会把在途帽提前摘掉）。
const aiAsking = ref(false)
const aiError = ref('')
// AI 成果的"未采用"单独一条：它不是失败，是过期。几十秒后什么都不说更让人摸不着头脑
const aiNote = ref('')

// 防抖定时器：自管 + 卸载清理（PortScanView.debounceTimer 同姿势）
let debounceTimer: ReturnType<typeof setTimeout> | null = null
// 过期响应门禁（PortScan/LanScanner 同族乱序先例）：防抖连点与开关即算可让两发
// 在途并存，晚到的旧结果不得倒灌覆盖新结果；输入清空同样作废在飞那发。
let reqSeq = 0
// AI 成果一旦上屏，就摘掉那些**在它之前**发出的本地发的覆盖权：本地一发只要几十毫秒，
// AI 一发要几十秒，晚到的"解析失败"把已经到手的成果顶掉，用户看到的就是成果凭空消失。
// 用户主动重算（改输入、点美化、调开关）时清掉——那是新的意图，不是旧发倒灌。
let aiAdopted = false

function isBlank(text: string): boolean {
  return text.trim() === ''
}

async function beautifyNow(): Promise<void> {
  if (debounceTimer) {
    clearTimeout(debounceTimer)
    debounceTimer = null
  }
  const text = inputText.value
  if (isBlank(text)) return
  const req: JsonFmtRequest = {
    text,
    indent: indent.value,
    sortKeys: sortKeys.value,
    unescape: unescape.value,
    query: query.value.trim(),
    replace: activeRepl.value ?? undefined,
    // 多段抽取恒开：粘进来的常常是"一段终端输出里好几份 JSON"，让用户手工注释掉
    // 别的段才能美化一段，是设计失败。整段/剥前缀两道仍能成时不会走到抽取。
    extractAll: true,
  }
  const seq = ++reqSeq
  aiAdopted = false // 这是一发由用户动作起算的新结果，旧成果不再享有优先权
  running.value = true
  try {
    const res = await JsonFmtAPI.JsonFmtService.Beautify(req)
    if (seq !== reqSeq) return // 已有更新一发（或输入已清空）：整段作废
    if (aiAdopted) return // 期间 AI 成果已上屏：这一发（多半是失败）不得倒灌顶掉它
    transportError.value = ''
    lastResult.value = res
  } catch (err: unknown) {
    if (seq !== reqSeq) return
    lastResult.value = null
    transportError.value = getErrorMessage(err)
  } finally {
    if (seq === reqSeq) running.value = false // 只有最新一发的结局能摘「在途」帽
  }
}

// AI 提取：本地四道都读不动时的最后一招，**只在用户按下这个钮时发生**。
// 刻意不接进防抖、也不被开关变化触发：粘贴即外发，等于把用户日志悄悄送出机器，
// 那不是行为失当一点，是越权。开关变了要重算，也只用本地 Beautify。
async function extractByAI(): Promise<void> {
  const text = inputText.value
  if (isBlank(text) || aiBusy.value || aiAsking.value) return
  const bytes = new TextEncoder().encode(text).length
  aiAsking.value = true // 先闸后问：弹窗期间也不许第二下点进来
  const accepted = await confirm({
    title: '把这段原文交给本机 AI 提取？',
    description:
      `将把输入框里的全部 ${bytes} 字节原文交给本机 Claude Code——内容会离开这台机器（进入模型会话），` +
      '不只是本机处理。返回结果仍由本地解析器校验：不是合法 JSON 就报错，不会当成成功结果上屏。' +
      '耗时按几十秒算（本机实测一次脏日志提取 66 秒），期间可以继续编辑，但改过输入这一发结果就不再采用。',
    confirmLabel: '交给 AI 提取',
    tone: 'warning',
  })
  if (!accepted) {
    aiAsking.value = false
    return
  }
  aiAsking.value = false

  // 这里刻意不碰 debounceTimer、也不 ++reqSeq：那两处只服务本地那条即时通道。
  // AI 这一发的有效性按「原文还是不是这段」判——拿序号当门禁，会因为用户改了个缩进
  // 就把几十秒的成果整批丢掉；反过来，一发在飞的本地 Beautify 也不该因为按下 AI 就作废。
  const sentText = text
  aiBusy.value = true
  aiError.value = ''
  aiNote.value = ''
  const req: JsonFmtRequest = {
    text,
    indent: indent.value,
    sortKeys: sortKeys.value,
    unescape: unescape.value,
    // AI 这条路只负责"从脏文本里认出结构"：查找与替换不参与（后端只取缩进/排序/转义三项），
    // 这里给空串是如实表示"这次不查不换"，不是传了会生效。提取成功后动一下开关，
    // 就会走本地 Beautify 把 query/replace 补上。
    query: '',
    extractAll: true,
  }
  try {
    const res = await JsonFmtAPI.JsonFmtService.ExtractByAI(req)
    if (inputText.value !== sentText) {
      // 过期不等于失败：几十秒后屏上没动静而又不说原因，用户只会当成工具没反应
      aiNote.value =
        'AI 已返回，但未采用：输入在这期间被改过，那份结果对应的不是现在这段文本。要的话再按一次。'
      return
    }
    transportError.value = ''
    if (res) {
      aiAdopted = true // 此后任何在它之前起算的本地发都失去覆盖权
      lastResult.value = res
    } else {
      aiError.value = 'AI 提取没有返回结果'
    }
  } catch (err: unknown) {
    if (inputText.value !== sentText) return // 用户已经改走，这次失败的播报没有意义
    // 后端在这里只抛"环境不行"和"返回的不是合法 JSON"两类，文案已带下一步，原样上屏
    aiError.value = getErrorMessage(err)
  } finally {
    // 按钮在途期间恒禁用，不会有两发 AI 并存，因此无条件摘帽是对的
    aiBusy.value = false
  }
}

// ---------- 着色与命中定位（纯展示层，不改一个字节数据） ----------
// 输出以前是只读 textarea：那玩意儿既不能上色，也不能把命中原地标出来，
// 搜到了还得自己滚着找——这才是"查找做得不好"的病根。
function esc(t: string): string {
  return t.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
}
// 命中片段在 token 内部包 mark；绝不在生成 HTML 之后再替换（会把标签撕开）
function mark(t: string, q: string): string {
  if (!q) return esc(t)
  const i = t.toLowerCase().indexOf(q.toLowerCase())
  if (i < 0) return esc(t)
  return esc(t.slice(0, i)) + '<mark class="jf-hit">' + esc(t.slice(i, i + q.length)) +
    '</mark>' + mark(t.slice(i + q.length), q)
}
const TOKEN_RE = /("(?:[^"\\]|\\.)*")(\s*:)?|("(?:[^"\\]|\\.)*")|(-?(?:0|[1-9]\d*)(?:\.\d+)?(?:[eE][+-]?\d+)?)|\b(true|false)\b|\b(null)\b|([{}\[\],:])/g
function highlight(text: string, q: string): string {
  let out = ''
  let last = 0
  for (const m of text.matchAll(TOKEN_RE)) {
    const at = m.index ?? 0
    out += mark(text.slice(last, at), q)
    if (m[1]) {
      // 带冒号才是键；裸字符串是值。m[2] 可能 undefined，直接 esc() 会崩。
      if (m[2]) out += `<span class="jf-k">${mark(m[1], q)}</span>${esc(m[2])}`
      else out += `<span class="jf-s">${mark(m[1], q)}</span>`
    } else if (m[3]) out += `<span class="jf-s">${mark(m[3], q)}</span>`
    else if (m[4]) out += `<span class="jf-n">${mark(m[4], q)}</span>`
    else if (m[5]) out += `<span class="jf-b">${mark(m[5], q)}</span>`
    else if (m[6]) out += `<span class="jf-null">${mark(m[6], q)}</span>`
    else out += `<span class="jf-p">${esc(m[7])}</span>`
    last = at + m[0].length
  }
  return out + mark(text.slice(last), q)
}
// 后端字段带 omitempty，output 可能是 undefined：着色不能假设它有值
const outHtml = (t?: string | null) => (t ? highlight(t, query.value.trim()) : '')

// 命中数取自已渲染的 DOM——后端给的是"路径命中"，与输出里看得见的高亮位置
// 不是一回事，拿前者冒充后者就是骗人。
const hitTotal = ref(0)
const hitCur = ref(0)
async function recountHits() {
  await nextTick()
  hitTotal.value = document.querySelectorAll('.jfmt-out .jf-hit').length
  hitCur.value = 0
}
async function jumpHit(step: number) {
  await nextTick()
  const nodes = Array.from(document.querySelectorAll('.jfmt-out .jf-hit')) as HTMLElement[]
  if (!nodes.length) return
  hitCur.value = (hitCur.value + step + nodes.length) % nodes.length
  nodes.forEach((n) => n.classList.remove('jf-hit-cur'))
  nodes[hitCur.value].classList.add('jf-hit-cur')
  nodes[hitCur.value].scrollIntoView({ block: 'center' })
}
watch([query, lastResult], () => { void recountHits() })

watch(inputText, () => {
  if (debounceTimer) {
    clearTimeout(debounceTimer)
    debounceTimer = null
  }
  if (isBlank(inputText.value)) {
    // 空输入：不发 RPC，作废在飞那发，结果区复位空态
    reqSeq++
    running.value = false
    lastResult.value = null
    transportError.value = ''
    return
  }
  debounceTimer = setTimeout(beautifyNow, DEBOUNCE_MS)
})

// 开关是明确低频动作：立即重算，不待防抖节拍
watch([indent, sortKeys, unescape, query], () => {
  if (!isBlank(inputText.value)) beautifyNow()
})

onUnmounted(() => {
  if (debounceTimer) {
    clearTimeout(debounceTimer)
    debounceTimer = null
  }
})

const isEmptyInput = computed(() => isBlank(inputText.value))
const matches = computed(() => lastResult.value?.matches ?? [])
const keyStats = computed(() => lastResult.value?.keyStats ?? [])
const segments = computed(() => lastResult.value?.segments ?? [])
const multiSeg = computed(() => segments.value.length > 1)

const warnings = computed(() => lastResult.value?.warnings ?? [])
</script>

<template>
  <section class="page-fluid jfmt-page">
    <PageHeader
      title="JSON 美化"
      subtitle="贴日志里的 JSON，自动剔行前缀并格式化；缩进、键排序、转义还原可调，贴入约 0.3 秒自动出结果。"
    />

    <div class="jfmt-desk">
      <!-- 左栏：输入 + 开关 -->
      <div class="card jfmt-pane">
        <UiClipboardField
          v-model="inputText"
          label="输入"
          mono
          :rows="14"
          paste-mode="replace"
          placeholder="把带日志前缀的原文贴进来，或在此按 Ctrl+V…"
        />
        <div class="btn-group jfmt-opts">
          <label class="jfmt-opt-item">
            缩进
            <select v-model.number="indent" class="select-input jfmt-select" title="输出缩进；「压缩」= 0 不留白">
              <option :value="2">2 空格</option>
              <option :value="4">4 空格</option>
              <option :value="0">压缩</option>
            </select>
          </label>
          <label class="toggle-label" title="把键按字母序重排；默认关，保留原顺序">
            <input v-model="sortKeys" type="checkbox" />
            <span>键排序</span>
          </label>
          <label class="toggle-label" title="解开被字符串化的嵌套 JSON（最多 3 层）；默认开">
            <input v-model="unescape" type="checkbox" />
            <span>转义还原</span>
          </label>
          <UiButton
            class="jfmt-run-btn"
            variant="secondary"
            small
            :disabled="isEmptyInput || running"
            title="立即重算。通常不必点：贴入与开关变化都会自动触发"
            @click="beautifyNow"
          >
            美化
          </UiButton>
          <!-- AI 提取只认这一下：不进防抖、不进开关重算，点前必过外发确认 -->
          <UiButton
            class="jfmt-ai-btn"
            variant="secondary"
            small
            :disabled="isEmptyInput || aiBusy || aiAsking"
            title="本机四道都读不动时才用：把原文交给本机 Claude Code 提取，返回后仍由本地解析器逐块严格校验；内容会离开这台机器"
            @click="extractByAI"
          >
            🤖 让 AI 提取
          </UiButton>
        </div>
      </div>

      <!-- 右栏：结果 -->
      <div class="card jfmt-pane">
        <!-- 在途态：在册 .state-box + .live-pulse（opacity 呼吸、零投影），明显但不吵；
             重算时上一轮结果保留在下方，不做整块闪断 -->
        <div v-if="running" class="state-box" role="status">
          <span class="jfmt-pulse live-pulse" aria-hidden="true"></span>
          <span>正在格式化…</span>
        </div>

        <!-- AI 在途单独一条：量级是秒到十几秒，跟本地那一下的瞬时重算不能共用一句文案，
             否则用户以为界面卡死了，其实是在等命令行往返 -->
        <div v-if="aiBusy" class="state-box" role="status">
          <span class="jfmt-pulse live-pulse" aria-hidden="true"></span>
          <!-- 耗时口径按实测说：本机一次脏日志提取跑了 66 秒。写"几秒"会让人以为卡死 -->
          <span>AI 提取中：本机命令行往返，几十秒属正常（实测约 1 分钟），期间可继续编辑…</span>
        </div>
        <UiBanner v-if="aiError" tone="error">
          <div>
            <div>AI 提取未完成：{{ aiError }}</div>
            <div class="jfmt-err-hint">
              这只影响 AI 这条路；本地美化与上一轮结果未受影响，可继续用「美化」或调整开关重算。
            </div>
          </div>
        </UiBanner>
        <!-- 过期未采用单列一档：它不是失败，静默丢弃是几十秒白等 -->
        <UiBanner v-if="aiNote" tone="info">{{ aiNote }}</UiBanner>

        <template v-if="lastResult">
          <!-- 如实交代行：数字全部原样取后端字段，前端零二次计算 -->
          <div class="jfmt-meta">
            <UiStatusChip v-if="lastResult.kind" tone="neutral">类型 {{ kindLabel(lastResult.kind) }}</UiStatusChip>
            <span v-if="multiSeg" class="jfmt-meta-item">共 <span class="mono">{{ segments.length }}</span> 段</span>
            <UiStatusChip v-if="repairCount(lastResult) > 0" tone="warning" :title="repairText(lastResult)">
              已自动修复 {{ repairCount(lastResult) }} 处
            </UiStatusChip>
            <span v-if="lastResult.droppedPrefixLines > 0" class="jfmt-meta-item">
              剔除 <span class="mono">{{ lastResult.droppedPrefixLines }}</span> 行日志前缀
            </span>
            <span v-if="lastResult.unwrapped > 0" class="jfmt-meta-item">
              解开 <span class="mono">{{ lastResult.unwrapped }}</span> 层转义
            </span>
            <template v-if="lastResult.ok">
              <span class="jfmt-meta-item">
                键 <span class="mono">{{ lastResult.stats.keys }}</span>
                · 节点 <span class="mono">{{ lastResult.stats.nodes }}</span>
                · 深度 <span class="mono">{{ lastResult.stats.maxDepth }}</span>
              </span>
              <span class="jfmt-meta-item">
                字节 <span class="mono">{{ lastResult.stats.bytesIn }}</span> →
                <span class="mono">{{ lastResult.stats.bytesOut }}</span>
              </span>
            </template>
          </div>

          <UiBanner v-if="!lastResult.ok" tone="error">
            <div class="jfmt-err">
              <div>{{ lastResult.error }}</div>
              <div v-if="lastResult.line > 0">
                位置：第 <span class="mono">{{ lastResult.line }}</span> 行<template v-if="lastResult.col > 0">
                  第 <span class="mono">{{ lastResult.col }}</span> 列</template>
              </div>
              <div v-if="lastResult.snippet" class="mono jfmt-err-snip">{{ lastResult.snippet }}</div>
              <div v-if="lastResult.hint" class="jfmt-err-hint">{{ lastResult.hint }}</div>
              <div class="jfmt-err-hint">输入原样保留，修正后自动重算。</div>
            </div>
          </UiBanner>
          <!-- 多段：逐段呈现，段头带它在原文里的行号——回对日志全靠这个行号。
               "复制全部"用的是顶层 output（各段拼接、无装饰文字），与展示同源。 -->
          <template v-else-if="multiSeg">
            <div class="jfmt-seg-bar">
              <UiButton variant="secondary" small @click="copyWithToast(lastResult!.output, `已复制全部 ${segments.length} 段`)">
                📋 复制全部
              </UiButton>
            </div>
            <div v-for="seg in segments" :key="seg.index" class="jfmt-seg">
              <div class="jfmt-seg-head">
                <span class="jfmt-seg-title">第 {{ seg.index }} 段</span>
                <!-- AI 提取的段不带行号（后端置 0）：那是模型输出的行数，
                     与用户眼前这份原文无关，标出来就是让人照着数错地方 -->
                <span v-if="seg.line > 0" class="mono">原第 {{ seg.line }} 行</span>
                <UiStatusChip tone="neutral">{{ kindLabel(seg.kind) }}</UiStatusChip>
                <span class="mono">{{ seg.stats.keys }} 键 · {{ seg.stats.nodes }} 节点</span>
              </div>
              <pre class="jfmt-out mono" v-html="outHtml(seg.output)"></pre>
            </div>
          </template>
          <template v-else>
            <div class="jfmt-seg-bar">
              <UiButton variant="secondary" small @click="copyWithToast(lastResult!.output, '已复制格式化结果')">
                📋 复制
              </UiButton>
              <span v-if="query.trim()" class="jfmt-meta-item">
                输出内命中 <span class="mono">{{ hitTotal }}</span> 处
                <button type="button" class="btn btn-ghost btn-micro" :disabled="!hitTotal" @click="jumpHit(-1)">↑ 上一个</button>
                <button type="button" class="btn btn-ghost btn-micro" :disabled="!hitTotal" @click="jumpHit(1)">↓ 下一个</button>
              </span>
            </div>
            <pre class="jfmt-out mono" v-html="outHtml(lastResult.output)"></pre>
          </template>

          <!-- 清洗警告逐条上屏，一条不吞（超限文案也由后端经 error 通道原样呈现） -->
          <!-- 传输/校验错误在两种状态下都要可见：以前只在"无结果"分支里渲染，
           于是已有结果时替换填错了点按钮，界面一声不吭。 -->
      <UiBanner v-if="transportError" tone="error">{{ transportError }}</UiBanner>
      <div v-if="repairText(lastResult)" class="jfmt-repair-detail">{{ repairText(lastResult) }}</div>
      <div v-if="warnings.length > 0" class="jfmt-warns">
            <UiBanner v-for="(w, i) in warnings" :key="i" tone="warn" class="slim">{{ w }}</UiBanner>
          </div>
        </template>

        <UiBanner v-else-if="transportError" tone="error">
          调用格式化服务失败：{{ transportError }}。稍后可点「美化」重试。
        </UiBanner>

        <div v-else-if="!aiBusy && !aiError && !aiNote" class="empty-hint">
          把日志里的 JSON 贴进左侧输入框（或点「从剪贴板粘贴」），约 0.3 秒后结果自动出现在这里。
        </div>
      </div>
    </div>

    <!-- 查找与统计放在两栏之下、整页宽：表格需要横向空间。
         数据与输出同源（同一次 Beautify 返回），不另发请求、不前端二次计算。 -->
        <div v-if="lastResult?.ok" class="card jfmt-switch">
      <div class="btn-group">
        <button v-for="t in TOOLS" :key="t.k" type="button" class="btn btn-ghost btn-small"
                :class="{ 'btn-accent-outline': tool === t.k }" @click="tool = t.k">{{ t.label }}</button>
      </div>
    </div>

<div v-if="lastResult && tool === 'find'" class="jfmt-find">
      <div class="btn-group jfmt-find-bar">
        <label class="toggle-label">查找
          <input v-model="query" type="text" class="input jfmt-find-input" placeholder="键名或值里的词，不区分大小写" />
        </label>
        <span v-if="query.trim()" class="jfmt-meta-item">
          命中 <span class="mono">{{ matches.length }}</span> 处<template v-if="lastResult.matchTruncated">（已达上限，后面还有）</template>
        </span>
      </div>
      <div v-if="query.trim() && matches.length === 0" class="empty-hint">
        没有命中：路径与字符串值都会匹配，换个词试试
      </div>
      <div v-if="matches.length" class="table-container jfmt-tall">
        <table class="tbl">
          <thead>
            <tr>
              <th style="width: 36%">路径</th>
              <th>值</th>
              <th style="width: 11%">类型</th>
              <th style="width: 15%">位置</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="(m, i) in matches" :key="`${m.path}-${i}`">
              <td>
                <button type="button" class="btn btn-ghost btn-micro mono jfmt-path" title="点击复制路径"
                        @click="copyWithToast(m.path, '已复制路径')">{{ m.path }}</button>
                <button type="button" class="btn btn-ghost btn-micro" title="填进下方替换框"
                        @click="usePathFromHit(m.path)">→ 替换</button>
              </td>
              <td class="jfmt-hit-val">{{ m.value }}</td>
              <td><span class="mono">{{ m.kind }}</span></td>
              <td><span v-if="m.segment" class="mono">第{{ m.segment }}段 第{{ m.line }}行</span><span v-else class="text-subtle">—</span></td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <!-- 替换卡：只有点「执行替换」才会带上 replace。新值要写成合法 JSON 字面量，
         后端拒收裸词（猜类型比不改更坑），拒绝原因原样上屏。 -->
    <div v-if="lastResult && tool === 'replace'" class="jfmt-find">
      <div class="jfmt-find-bar">
        <span class="jfmt-seg-title">替换（按路径改值或改键名）</span>
        <UiStatusChip v-if="lastResult.replaced > 0" tone="warning">已替换 {{ lastResult.replaced }} 处</UiStatusChip>
      </div>
      <div class="btn-group jfmt-find-bar">
        <label class="toggle-label">路径
          <input v-model="replPath" type="text" class="input jfmt-path-input mono" placeholder="如 data.items[].status" />
        </label>
        <label class="toggle-label">新值（JSON 字面量）
          <input v-model="replValue" type="text" class="input jfmt-path-input mono" placeholder='如 "ok" 或 123 或 null' />
        </label>
        <label class="toggle-label">新键名
          <input v-model="replKey" type="text" class="input jfmt-path-input mono" placeholder="留空则不改名" />
        </label>
        <UiButton variant="primary" small :disabled="running" @click="doReplace">🔧 执行替换</UiButton>
        <UiButton v-if="activeRepl" variant="secondary" small :disabled="running" @click="undoReplace">↩ 撤销</UiButton>
      </div>
      <div v-if="lastResult.replaced === 0 && activeRepl" class="jfmt-repair-detail">
        本次没改动：路径可能没命中（先看查找结果里的准确路径）。原文从未被写入，撤销即回到原样。
      </div>
    </div>

    <div v-if="lastResult && keyStats.length && tool === 'stats'" class="jfmt-find">
      <div class="jfmt-find-bar">
        <span class="jfmt-seg-title">键路径统计</span>
        <span class="jfmt-meta-item">共 <span class="mono">{{ keyStats.length }}</span> 条路径 · 按出现次数排序</span>
      </div>
      <div class="table-container jfmt-tall">
        <table class="tbl">
          <thead>
            <tr>
              <th style="width: 46%">路径</th>
              <th style="width: 12%">次数</th>
              <th>值类型</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="s in keyStats" :key="s.path">
              <td><span class="mono">{{ s.path }}</span></td>
              <td><span class="mono">{{ s.count }}</span></td>
              <td>
                <span v-for="t in s.types" :key="t.type" class="jfmt-type"><span class="mono">{{ t.type }}</span> × <span class="mono">{{ t.count }}</span></span>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  </section>
</template>

<style scoped>
/* 页头/.card/.btn-group/.state-box/.banner(+slim)/.chip/.mono/.empty-hint/.live-pulse/
   .select-input/.toggle-label 全用 components.css 在册原子；本块只放布局粘合剂与真差异
   （jfmt- 前缀私类，零裸色零裸 px 字号，全引 tokens.css 档位）。 */
.jfmt-page {
  display: flex;
  flex-direction: column;
  gap: 14px;
  height: 100%;
  /* 容器查询宿主：两栏降级看页容器实宽（侧栏占位与 Shell 内距已折入），
     与 MemoView/ClipboardView 同姿势；视口 1100 时容器仍容得下双栏，
     760/460 事实断点必落进叠放档 */
  container: jfmt / inline-size;
}
.jfmt-desk {
  display: grid;
  grid-template-columns: minmax(260px, 0.72fr) minmax(0, 1.9fr);
  gap: 14px;
  align-items: stretch;
  min-width: 0;
}
.jfmt-pane {
  /* 边框与内距都是装饰成本：这个页面要的是展示面积 */
  padding: 8px 10px;
  gap: 6px;
  display: flex;
  flex-direction: column;
  gap: 10px;
  min-width: 0;
}

/* 开关行：横排拉开到 14、换行行距维持原子档 */
.jfmt-opts {
  gap: 8px 14px;
}
.jfmt-opt-item {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: var(--text-sm);
  color: var(--color-text-muted);
}
/* 全局 .select-input 是 width:100%（表单竖排形制），行内组需自适应宽 */
.jfmt-select {
  width: auto;
}

.jfmt-pulse {
  width: 8px;
  height: 8px;
  flex: none;
  border-radius: var(--radius-pill);
  background: var(--color-primary);
}

/* 分段条与段头：只用 token，不引入新色档 */
.jfmt-seg-bar { display: flex; justify-content: flex-end; margin-bottom: 4px; }
.jfmt-seg { display: flex; flex-direction: column; gap: 4px; margin-bottom: 10px; }
.jfmt-seg-head {
  display: flex; align-items: center; gap: 8px; flex-wrap: wrap;
  font-size: var(--text-xs); color: var(--color-text-subtle);
}
.jfmt-seg-title { color: var(--color-text-muted); font-weight: 600; }

/* 修复明细：次级文字档，不与主结论抢视线，但必须在场 */
/* 着色输出：替代只读 textarea——能上色，也能原地标出命中并跳转 */
.jfmt-out {
  margin: 0; padding: 8px 10px; min-height: 120px; max-height: 62vh; overflow: auto;
  background: var(--surface-page); border: 1px solid var(--color-border); border-radius: 6px;
  font-family: var(--font-mono); font-size: var(--text-sm); line-height: 1.5; white-space: pre;
}
/* v-html 注入的节点拿不到 scoped 属性，色档必须走 :deep，否则样式根本不生效 */
.jfmt-out :deep(.jf-k) { color: var(--state-information); }
.jfmt-out :deep(.jf-s) { color: var(--state-positive); }
.jfmt-out :deep(.jf-n) { color: var(--color-text); }
.jfmt-out :deep(.jf-b) { color: var(--state-warning); }
.jfmt-out :deep(.jf-null), .jfmt-out :deep(.jf-p) { color: var(--color-text-subtle); }
.jfmt-out :deep(.jf-hit) { background: var(--state-warning-soft); border-radius: 3px; }
.jfmt-out :deep(.jf-hit-cur) { outline: 1px solid var(--state-warning); }

/* 工具栏一行；三块内容共用它，不再各自套一张带边框的卡 */
.jfmt-switch { padding: 6px 10px; }
.jfmt-find { display: flex; flex-direction: column; gap: 8px; }
.jfmt-find-bar { display: flex; align-items: center; gap: 10px 14px; flex-wrap: wrap; }
.jfmt-find-input { width: 300px; min-width: 160px; }
.jfmt-path-input { width: 220px; min-width: 120px; }
.jfmt-tall { max-height: 320px; overflow: auto; }
.jfmt-path { text-align: left; word-break: break-all; }
.jfmt-hit-val { overflow-wrap: anywhere; }
.jfmt-type { margin-right: 8px; font-size: var(--text-xs); color: var(--color-text-muted); }
.jfmt-repair-detail { font-size: var(--text-xs); color: var(--color-text-subtle); overflow-wrap: anywhere; }

.jfmt-meta {
  display: flex;
  align-items: baseline;
  flex-wrap: wrap;
  gap: 6px 14px;
  font-size: var(--text-sm);
}
.jfmt-meta-item {
  display: inline-flex;
  align-items: baseline;
  gap: 4px;
  color: var(--color-text-muted);
}

.jfmt-err {
  display: flex;
  flex-direction: column;
  gap: 4px;
}
/* 长片段折行不撑破：.mono 的 word-break 之外再兜 overflow-wrap（空格粘连串） */
.jfmt-err-snip {
  overflow-wrap: anywhere;
}
.jfmt-err-hint {
  font-size: var(--text-xs);
}

.jfmt-warns {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

/* 窄容器退上下堆叠：输入在前、结果在后，贴合阅读顺序（DOM 序即此序，零 order 戏法） */
@container jfmt (max-width: 760px) {
  .jfmt-desk {
    grid-template-columns: minmax(0, 1fr);
  }
}
</style>
