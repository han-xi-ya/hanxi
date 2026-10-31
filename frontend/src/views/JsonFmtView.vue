<script setup lang="ts">
// JsonFmtView：JSON 基础美化器——2026-10-31 收纳裁决后的主干三件套：
// 输入区 / 工具条（缩进档·压缩·键排序·复制）/ 输出区。
// 产品口径 = 「粘贴即出结果」：输入变化约 300ms 防抖自动 Beautify，开关变化立即
// 重算；手动「美化」钮只是兜底保留（并会吃掉待发防抖，不双发）。
// 一切判定（解析、尺寸闸、行列定位、失败方向判断）都交给后端
// internal/modules/jsonfmt 的 Beautify：前端不装第二套解析器、不预校验——
// 两处校验迟早漂移，后端是唯一真相，其 error/hint/line/col/snippet/warnings
// 原样上屏、一行不吞。
// 曾经长出来的规则族（日志清洗、宽松修复、多段抽取、展开转义、查找、替换、
// 统计）已整体退役进 git 历史——别在这里凭记忆复活。
// 脏文本的出路是就地「AI 一键美化」（2026-10-31 机主澄清：不是把人赶去聊天模块）：
// 按下先弹外发确认（内容离开本机 + 耗时按几十秒算）→ 后端 ExtractByAI 过契约
// 验收与本地排版 → 结果就地替换失败块。在途/失败/过期未采用三通道分列，
// 纪律从 ExtractByAI 一代逐条继承（#126 哨兵、#128 有效性判定、#132 单飞、#133 严验收）。
import { computed, nextTick, onActivated, onMounted, onUnmounted, ref, watch } from 'vue'
import * as JsonFmtAPI from '../../bindings/hanxi/internal/modules/jsonfmt'
import type { JsonFmtRequest, JsonFmtResult } from '../../bindings/hanxi/internal/modules/jsonfmt/models'
import { getErrorMessage } from '../utils/errors'
import PageHeader from '../components/ui/PageHeader.vue'
import UiBanner from '../components/ui/UiBanner.vue'
import UiStatusChip from '../components/ui/UiStatusChip.vue'
import UiClipboardField from '../components/ui/UiClipboardField.vue'
import UiButton from '../components/ui/UiButton.vue'
import { useClipboard } from '../composables/useClipboard'
import { useConfirm } from '../composables/useConfirm'
import { handOff, takeForFmt } from '../composables/useJsonChatBridge'

const { copyWithToast } = useClipboard()
const { confirm } = useConfirm()

// 视图间导航：App.vue 对动态视图统一监听 @navigate（HomeView 先例形制）。
// 只服务于次要链接「带到 AI 聊天细聊」——主出路是就地「AI 一键美化」，
// 这条留给"想反复追问模型"的场景，也让桥的 toChat 通道有真实生产者。
const emit = defineEmits<{ navigate: [route: string] }>()

function takeToChat(): void {
  handOff(inputText.value)
  emit('navigate', '/ext/aichat')
}

// 自动节拍：快过用户贴完回看的间隔，慢过连续击键的抖动
const DEBOUNCE_MS = 300

const KIND_LABEL: Record<string, string> = { object: '对象', array: '数组', scalar: '标量' }
const kindLabel = (k: string) => KIND_LABEL[k] ?? k

// —— 两个开关（低频项不抢主路径：缩进 2、键排序默认关保原顺序）——
const indent = ref(2) // 2 | 4 | 0 = 压缩输出
const sortKeys = ref(false)

const inputText = ref('')
const running = ref(false)
const lastResult = ref<JsonFmtResult | null>(null)
// RPC 调用本身失败（传输层）与后端"解析了说不对"（ok=false）两件事分开：
// 前者是服务不可用（如模块未就绪），后者是内容问题——行/列/snippet 走错误横幅。
const transportError = ref('')
// 回程来源条：聊天侧「在 JSON 美化器中打开」送来的块，自动美化后要说明出处
const bridgeNote = ref('')
// —— AI 一键美化三通道（ExtractByAI 一代的 aiBusy/aiAsking/aiError/aiNote 分列
// 纪律原样继承）：失败与"过期未采用"都不砸已在屏上的结果。 ——
const aiBusy = ref(false)
// 确认待决期另设一闸：aiBusy 是 await confirm 之后才置的，只靠它禁钮，
// 弹窗期间按钮一直可点——双击就是两发并发外发（aiAsking 教训在册）。
const aiAsking = ref(false)
const aiError = ref('')
const aiNote = ref('')

// 输入框收展：纯 UI 位（rows 8 ↔ 16，照 AiChatInput/MsgBoardView draftExpanded 先例）
const draftExpanded = ref(false)

// 防抖定时器：自管 + 卸载清理（PortScanView.debounceTimer 同姿势）
let debounceTimer: ReturnType<typeof setTimeout> | null = null
// 过期响应门禁（PortScan/LanScanner 同族乱序先例）：防抖连点与开关即算可让两发
// 在途并存，晚到的旧结果不得倒灌覆盖新结果；输入清空同样作废在飞那发。
let reqSeq = 0

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
  const req: JsonFmtRequest = { text, indent: indent.value, sortKeys: sortKeys.value }
  const seq = ++reqSeq
  running.value = true
  try {
    const res = await JsonFmtAPI.JsonFmtService.Beautify(req)
    if (seq !== reqSeq) return // 已有更新一发（或输入已清空）：整段作废
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

// AI 一键美化：本地主干读不动的脏文本，就地交本机模型认结构（后端
// classifyFailure 的指路文案落点）。三道闸一件不少：按下 → 外发确认 → 才发
// （粘贴/防抖/开关永不触发它）；耗时按几十秒算的期望在点之前建立，不让人
// 在第 20 秒认定界面卡死（#128）。
async function aiBeautify(): Promise<void> {
  const text = inputText.value
  if (isBlank(text) || aiBusy.value || aiAsking.value) return
  const bytes = new TextEncoder().encode(text).length
  aiAsking.value = true // 先闸后问：弹窗期间也不许第二下点进来
  const accepted = await confirm({
    title: '把这段原文交给本机 AI 美化？',
    description:
      `将把输入框里的全部 ${bytes} 字节原文交给本机 Claude Code——内容会离开这台机器` +
      '（进入模型会话），不只是本机处理。返回结果会先过本地契约验收（不扫描不修复不猜）' +
      '并由本地渲染器排版后才上屏。耗时按几十秒算（实测一次提取约一分钟量级），' +
      '期间可以继续编辑，但改过输入这一发结果就不再采用。',
    confirmLabel: '交给 AI 美化',
    tone: 'warning',
  })
  aiAsking.value = false
  if (!accepted) return

  aiBusy.value = true
  aiError.value = ''
  aiNote.value = ''
  try {
    const res = await JsonFmtAPI.JsonFmtService.ExtractByAI({
      text, indent: indent.value, sortKeys: sortKeys.value,
    } as JsonFmtRequest)
    if (inputText.value !== text) {
      // 过期不等于失败：几十秒后屏上没动静又不说原因，用户只会觉得工具没反应。
      aiNote.value = 'AI 已返回，但未采用：输入在这期间被改过，那份结果对应的不是现在这段文本。要的话再按一次。'
      return
    }
    // 采用即摘掉更早本地发的覆盖权（aiAdopted 纪律的 reqSeq 等价实现）：
    // 本地一发只要几十毫秒、AI 一发几十秒——不设这道闸，晚到的"解析失败"
    // 会把刚到手的结果顶掉，用户看到的是成果凭空消失。
    reqSeq++
    running.value = false
    transportError.value = ''
    lastResult.value = res
  } catch (err: unknown) {
    // 失败不砸屏：上一轮结果（含失败块）原样留着，AI 的错误单通道播报——
    // "AI 不行"不该把用户已经拿在手里的东西一起砸掉。
    aiError.value = getErrorMessage(err)
  } finally {
    aiBusy.value = false
  }
}

watch(inputText, () => {
  bridgeNote.value = '' // 文本已被用户动过，来源条的使命完成
  aiNote.value = '' // "过期未采用"说的就是"你改了输入"——改后使命完成
  aiError.value = '' // 上一发的失败原因对新文本不再成立，不留着误导
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
watch([indent, sortKeys], () => {
  if (!isBlank(inputText.value)) void beautifyNow()
})

// 回程消费：聊天侧「在 JSON 美化器中打开」送来的块填入输入并直接跑一次美化。
// onMounted + onActivated 双挂：App.vue 的 KeepAlive 让二次导航只走 activated，
// 只挂 onMounted 会把回程文本永远晾在桥里（首次挂载两钩子先后触发，读后即清天然幂等）。
// await nextTick 不是客套：赋值先让 watch 走完（它会清来源条并排一发防抖），
// 随后 beautifyNow 吃掉那发防抖——顺序反了就是"双发美化 + 来源条被吞"。
async function consumeInbound(): Promise<void> {
  const text = takeForFmt()
  if (text === null) return
  inputText.value = text
  await nextTick()
  bridgeNote.value = '来自 AI 聊天的 JSON 块，已带入并自动美化。'
  void beautifyNow()
}
onMounted(() => { void consumeInbound() })
onActivated(() => { void consumeInbound() })

onUnmounted(() => {
  if (debounceTimer) {
    clearTimeout(debounceTimer)
    debounceTimer = null
  }
})

const isEmptyInput = computed(() => isBlank(inputText.value))
const failedResult = computed(() => lastResult.value !== null && !lastResult.value.ok)
const warnings = computed(() => lastResult.value?.warnings ?? [])
</script>

<template>
  <section class="page-fluid jfmt-page">
    <PageHeader
      title="JSON 美化"
      subtitle="标准 JSON 的格式化 / 压缩 / 键排序：数字原样搬运不改精度、键序默认保留；失败报行列与原文片段并给方向判断，读不动的可就地「AI 一键美化」（需确认，内容会离开本机）。"
    />

    <UiBanner v-if="bridgeNote" tone="info">{{ bridgeNote }}</UiBanner>
    <!-- AI 三通道分列（ExtractByAI 一代纪律）：在途播报、失败不砸屏、过期未采用
         单列说明为什么屏上没动静——三者各一条，混写会让用户查错方向 -->
    <UiBanner v-if="aiBusy" tone="info" role="status">
      AI 正在从原文里认结构：一次往返几十秒属正常（实测约一分钟量级），期间可继续编辑，
      但改过输入这一发结果就不再采用。
    </UiBanner>
    <UiBanner v-if="aiError" tone="error">
      <div>{{ aiError }}</div>
      <div class="jfmt-err-hint">AI 美化失败不影响上面的结果；「未装命令行/未登录」类错误修好环境再按一次即可。</div>
    </UiBanner>
    <UiBanner v-if="aiNote" tone="info">{{ aiNote }}</UiBanner>

    <!-- 主干三件套纵排：输入 → 工具条 → 输出（DOM 序即阅读序，零 order 戏法） -->
    <div class="jfmt-desk">
      <div class="card jfmt-pane">
        <UiClipboardField
          v-model="inputText"
          label="输入"
          mono
          :rows="draftExpanded ? 16 : 8"
          paste-mode="replace"
          placeholder="把 JSON 贴进来，或在此按 Ctrl+V…"
        />
        <button
          type="button"
          class="btn btn-ghost btn-small jfmt-expand"
          :aria-pressed="draftExpanded"
          :title="draftExpanded ? '收窄输入框' : '拉高输入框'"
          @click="draftExpanded = !draftExpanded"
        >
          {{ draftExpanded ? '收起' : '展开' }}
        </button>
      </div>

      <!-- 工具条：缩进档（含压缩档=indent 0）/ 键排序 / 美化兜底 / 复制 -->
      <div class="card jfmt-toolbar">
        <div class="btn-group jfmt-opts">
          <label class="jfmt-opt-item">
            缩进
            <select v-model.number="indent" class="select-input jfmt-select" title="输出缩进；「压缩」= 单行无空白">
              <option :value="2">2 空格</option>
              <option :value="4">4 空格</option>
              <option :value="0">压缩</option>
            </select>
          </label>
          <label class="toggle-label" title="把键按字母序重排；默认关，保留原顺序">
            <input v-model="sortKeys" type="checkbox" />
            <span>键排序</span>
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
          <UiButton
            class="jfmt-copy-btn"
            variant="secondary"
            small
            :disabled="!lastResult || !lastResult.ok"
            title="复制格式化结果"
            @click="copyWithToast(lastResult!.output, '已复制格式化结果')"
          >
            📋 复制
          </UiButton>
        </div>
      </div>

      <!-- 输出区 -->
      <div class="card jfmt-pane">
        <!-- 在途态：在册 .state-box + .live-pulse（opacity 呼吸、零投影），明显但不吵；
             重算时上一轮结果保留在下方，不做整块闪断 -->
        <div v-if="running" class="state-box" role="status">
          <span class="jfmt-pulse live-pulse" aria-hidden="true"></span>
          <span>正在格式化…</span>
        </div>

        <template v-if="lastResult">
          <!-- 失败块：错误 + 行列 + snippet + hint 一件不丢（后端 classifyFailure 的
               指路文案原样显示），外加「带去 AI 聊天」——脏文本的出路由文案说到做到 -->
          <UiBanner v-if="failedResult" tone="error">
            <div class="jfmt-err">
              <div>{{ lastResult.error }}</div>
              <div v-if="lastResult.line > 0">
                位置：第 <span class="mono">{{ lastResult.line }}</span> 行<template v-if="lastResult.col > 0">
                  第 <span class="mono">{{ lastResult.col }}</span> 列</template>
              </div>
              <div v-if="lastResult.snippet" class="mono jfmt-err-snip">{{ lastResult.snippet }}</div>
              <div v-if="lastResult.hint" class="jfmt-err-hint">{{ lastResult.hint }}</div>
              <div class="jfmt-err-hint">输入原样保留，修正后自动重算。</div>
              <div class="jfmt-err-actions">
                <UiButton
                  class="jfmt-ai-btn"
                  variant="secondary"
                  small
                  :disabled="aiBusy || aiAsking"
                  title="内容将发给本机 AI（会离开这台机器）；先确认再发，返回过本地验收与排版；一次往返几十秒"
                  @click="aiBeautify"
                >
                  {{ aiAsking ? '等待确认…' : aiBusy ? 'AI 认结构中…' : '✨ AI 一键美化' }}
                </UiButton>
                <button
                  type="button"
                  class="btn btn-ghost btn-small jfmt-chat-link"
                  title="把原文带进 AI 聊天并预填提取指令（适合想反复追问的场景）；发送与确认仍在聊天侧"
                  @click="takeToChat"
                >
                  或带到 AI 聊天细聊
                </button>
              </div>
            </div>
          </UiBanner>
          <template v-else>
            <!-- 如实交代行：kind 取后端原值翻中文标签，前端零二次计算 -->
            <div class="jfmt-meta">
              <UiStatusChip v-if="lastResult.kind" tone="neutral">类型 {{ kindLabel(lastResult.kind) }}</UiStatusChip>
            </div>
            <div class="jfmt-out-bar">
              <span class="jfmt-out-label">输出</span>
            </div>
            <pre class="jfmt-out mono">{{ lastResult.output }}</pre>
          </template>
        </template>

        <UiBanner v-if="transportError" tone="error">
          调用格式化服务失败：{{ transportError }}。稍后可点「美化」重试。
        </UiBanner>

        <!-- 清洗警告逐条上屏，一条不吞（超限文案也由后端经 error 通道原样呈现） -->
        <div v-if="warnings.length > 0" class="jfmt-warns">
          <UiBanner v-for="(w, i) in warnings" :key="i" tone="warn" class="slim">{{ w }}</UiBanner>
        </div>

        <div v-if="!lastResult && !transportError && !running" class="empty-hint">
          把 JSON 贴进上方输入框（或点「从剪贴板粘贴」），约 0.3 秒后结果自动出现在这里。
        </div>
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
}
.jfmt-desk {
  display: flex;
  flex-direction: column;
  gap: 14px;
  min-width: 0;
}
.jfmt-pane {
  /* 边框与内距都是装饰成本：这个页面要的是展示面积 */
  padding: 8px 10px;
  display: flex;
  flex-direction: column;
  gap: 10px;
  min-width: 0;
}
.jfmt-toolbar {
  padding: 6px 10px;
}
.jfmt-expand { align-self: flex-start; }

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

.jfmt-meta {
  display: flex;
  align-items: baseline;
  flex-wrap: wrap;
  gap: 6px 14px;
  font-size: var(--text-sm);
}
.jfmt-out-bar {
  display: flex;
  align-items: baseline;
}
.jfmt-out-label {
  font-size: var(--text-xs);
  color: var(--color-text-subtle);
}

/* 输出只读区：无查找就没有着色需求，pre 原样呈现后端 output，可勾选可滚动 */
.jfmt-out {
  margin: 0;
  padding: 8px 10px;
  min-height: 120px;
  max-height: 62vh;
  overflow: auto;
  background: var(--surface-page);
  border: 1px solid var(--color-border);
  border-radius: 6px;
  font-family: var(--font-mono);
  font-size: var(--text-sm);
  line-height: 1.5;
  white-space: pre;
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
.jfmt-err-actions {
  display: flex;
  gap: 8px;
  margin-top: 4px;
}

.jfmt-warns {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

@media (pointer: coarse) {
  .jfmt-expand, .jfmt-opts .btn { min-height: 44px; }
}
</style>
