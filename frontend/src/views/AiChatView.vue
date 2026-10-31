<script setup lang="ts">
// AI 聊天视图壳：编排单源在 composables/useAiChat.ts（契约面、状态机、门禁矩阵、
// rAF 合帧、确认策略全部在那），界面拆分照 WechatBotView 形制——
// components/aichat/ 三纯展示壳（SessionList 会话栏 / Flow 消息流 / Input 输入区），
// 本视图只保留 composable 解构、props/事件接线、页面骨架与容器查询降级。
//
// 事件订阅在 useAiChat 的 setup 同步期生效（注册点不在 onMounted，早期事件不丢）；
// onMounted 只做 ListSessions 装载。模块停用弹回 / EnsureModuleActive 门禁由
// App.vue 依 navigation.ts 的 moduleId 自动接管，视图内零门禁代码。
//
// 纯 UI 折叠态留在本地（输入框 rows 3/8 收展），不上收 composable——
// useWechatBot 头注同款分界。
//
// JSON 美化器双向桥（useJsonChatBridge，2026-10-31 收纳裁决的对口）：入向——挂载/
// 激活时消费 toChat 文本，按编排模板预填进输入框（**不自动发送**——发送仍由人按下，
// 每会话首条的外发确认链原样复用）；出向——流内收口回合的 JSON 卡按下后 handToFmt
// + navigate 回美化器。桥的读写全在本视图这一层，composable 与展示壳都不感知。
import { computed, onActivated, onMounted, ref, watch } from 'vue'
import PageHeader from '../components/ui/PageHeader.vue'
import UiBanner from '../components/ui/UiBanner.vue'
import UiButton from '../components/ui/UiButton.vue'
import UiStatusChip from '../components/ui/UiStatusChip.vue'
import AiChatSessionList from '../components/aichat/AiChatSessionList.vue'
import AiChatFlow from '../components/aichat/AiChatFlow.vue'
import AiChatInput from '../components/aichat/AiChatInput.vue'
import { useAiChat } from '../composables/useAiChat'
import type { SessionRow } from '../composables/useAiChat'
import {
  EXTRACT_JSON_PROMPT,
  composeExtractPrompt,
  handToFmt,
  peekForChat,
  takeForChat,
} from '../composables/useJsonChatBridge'

const {
  sessions,
  currentId,
  currentRow,
  refresh,
  listLoading,
  listError,
  currentItems,
  currentStats,
  inputText,
  canSend,
  sendDisabledReason,
  sendPhase,
  busy,
  waitingPlaceholder,
  flowRef,
  clearUnread,
  selectSession,
  createSession,
  renameSession,
  closeSession,
  deleteSession,
  send,
} = useAiChat()

// 输入框收展：纯 UI 位，视图本地（rows 3 ↔ 8，照 MsgBoardView draftExpanded）
const draftExpanded = ref(false)

// 视图间导航：App.vue 对动态视图统一监听 @navigate（HomeView 先例形制）
const emit = defineEmits<{ navigate: [route: string] }>()

onMounted(refresh)

// —— JSON 美化器双向桥 ——
// 来源条：入向带入后说明文本出处；用户把输入框清空（发送即清空）时使命完成。
const bridgeNote = ref('')

// 入向消费：桥里有 toChat 文本就按编排模板预填进输入框——只预填不发送，
// 按下发送的权力留给用户（外发确认链照旧）。onMounted + onActivated 双挂：
// KeepAlive 下二次导航只走 activated；首次挂载两钩子先后触发，读后即清天然幂等。
function consumeInbound(): void {
  const text = takeForChat()
  if (text === null) return
  inputText.value = composeExtractPrompt(text)
  bridgeNote.value = '来自 JSON 美化器的文本已按提取模板预填在输入框——发送仍由你按下，外发确认链照旧。'
}
onMounted(consumeInbound)
onActivated(consumeInbound)
watch(inputText, (v) => {
  if (bridgeNote.value && v === '') bridgeNote.value = ''
})

// 「提取 JSON」快捷钮的可用性判定：桥文本（若有）或输入框内容，有一样就能提取。
const canExtract = computed(() => peekForChat() !== null || inputText.value.trim() !== '')
const extractHint = computed(() =>
  canExtract.value
    ? ''
    : '没有待提取文本——先在输入框贴入文本，或从 JSON 美化器按「带去 AI 聊天」送过来',
)

function prefillExtract(): void {
  // 桥文本优先（最新送达的原文），其次当前输入框内容（逐字照编排模板包装）。
  // 已知取舍：两者都有时草稿被桥文本顶掉——来源页刚离开、这里刚按下，
  // 用户此刻的意图就是处理送来的那段（见桥文件头注）。
  const bridgeText = peekForChat()
  const src = bridgeText ?? inputText.value
  if (src.trim() === '') return
  // 重复按下不叠第二层模板：包装幂等（桥路径走完即清空，不会再进这条）
  if (bridgeText === null && inputText.value.startsWith(EXTRACT_JSON_PROMPT)) return
  inputText.value = composeExtractPrompt(src)
  if (bridgeText !== null) {
    takeForChat() // 消费掉，同一份不二次弹入
    bridgeNote.value = ''
  }
}

// 出向：流内收口回合的 JSON 卡按下——写桥 + 跳美化器，真格式化回后端做。
function openInJsonFmt(jsonText: string): void {
  handToFmt(jsonText)
  emit('navigate', '/ext/jsonfmt')
}

// —— 呈现（工具条 chip：状态恒带字，色不单传；glow 只给真实在途）——
function chipText(r: SessionRow): string {
  if (r.sending || r.turnInFlight) return '回复中'
  if (r.state === 'dead') return '已终止'
  if (r.archived || r.state === 'closed') return '已封存'
  return '空闲'
}

function chipTone(r: SessionRow): 'positive' | 'information' | 'warning' | 'danger' | 'neutral' {
  if (r.sending || r.turnInFlight) return 'information'
  if (r.state === 'dead') return 'danger'
  if (r.archived || r.state === 'closed') return 'neutral'
  return 'positive'
}

const inputPlaceholder = computed(() => {
  if (!currentRow.value) return '想问什么？直接输入即可——发送前会自动为你开一个新会话…'
  if (currentRow.value.archived) return '该会话已封存——发送下一条会自动复进（复用模型侧对话档案）'
  return '想问什么？内容将发送给本机常驻 AI…'
})

function stopCurrent(): void {
  if (currentRow.value) void closeSession(currentRow.value.id)
}
</script>

<template>
  <div class="page-fluid aichat-page">
    <PageHeader
      title="AI 聊天"
      subtitle="驱动本机 Claude Code 的会话式问答：一个会话一个常驻进程，上下文连续；回答按内容块上屏（非逐 token）；内容会离开这台机器，每个会话首条消息需确认。"
    />

    <div v-if="listLoading && sessions.length === 0" class="state-box">正在读取会话名录…</div>
    <div v-else-if="listError && sessions.length === 0" class="state-box state-error">
      会话名录加载失败：{{ listError }}
      <button type="button" class="btn btn-small btn-secondary" @click="refresh">重试</button>
    </div>

    <div v-else class="ac-desk">
      <AiChatSessionList
        :sessions="sessions"
        :current-id="currentId"
        @select="selectSession"
        @create="createSession"
        @rename="renameSession"
        @close="closeSession"
        @delete="deleteSession"
        @refresh="refresh"
      />

      <section class="ac-pane">
        <div class="ac-toolbar">
          <h2 class="ac-pane-title" :title="currentRow?.title">{{ currentRow?.title ?? '未选择会话' }}</h2>
          <UiStatusChip v-if="currentRow" :tone="chipTone(currentRow)">{{ chipText(currentRow) }}</UiStatusChip>
          <!-- aria-live 分层：流本体不挂 live（块流会淹死读屏），回合级迁移才写这一句 -->
          <span class="ac-announce" role="status" aria-live="polite">{{ currentRow?.announce ?? '' }}</span>
        </div>

        <UiBanner v-if="listError" tone="error" class="ac-list-banner">
          <span>名录刷新失败（不影响当前会话）：{{ listError }}</span>
          <button type="button" class="btn btn-small btn-secondary" @click="refresh">重试</button>
        </UiBanner>

        <AiChatFlow
          v-if="currentRow"
          ref="flowRef"
          :items="currentItems"
          :stats-by-turn="currentStats"
          :waiting="waitingPlaceholder"
          :unread="currentRow.unread"
          @reached-bottom="clearUnread"
          @open-json="openInJsonFmt"
        />
        <div v-else class="state-box ac-no-session">
          还没有会话——直接输入并发送即可，会自动为你开一个（建会话只记名录、不起进程，第一条消息发出前内容不会离开这台机器）。
        </div>

        <UiBanner v-if="currentRow?.sendError" tone="error" slim class="ac-send-banner">
          这一句没出去：{{ currentRow.sendError }}
        </UiBanner>

        <!-- 入向来源条：文本出处要说出来——预填不是用户打的，静默上屏就是冒充 -->
        <UiBanner v-if="bridgeNote" tone="info" slim class="ac-bridge-banner">{{ bridgeNote }}</UiBanner>

        <AiChatInput
          v-model="inputText"
          :phase="sendPhase"
          :can-send="canSend"
          :disabled-reason="sendDisabledReason"
          :placeholder="inputPlaceholder"
          :show-stop="busy"
          :expanded="draftExpanded"
          :can-extract="canExtract"
          :extract-hint="extractHint"
          @send="send"
          @stop="stopCurrent"
          @toggle-expand="draftExpanded = !draftExpanded"
          @extract-json="prefillExtract"
        />
      </section>
    </div>
  </div>
</template>

<style scoped>
/* 双栏工作台：≥840 会话栏 clamp 夹持（左栏宽度封顶，不吃对话区）；
   窄档会话栏收为顶部横滑 chips（降级逻辑在 SessionList 的 @container 内） */
.ac-desk {
  container: ac / inline-size;
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  gap: 14px;
}
@container ac (min-width: 840px) {
  .ac-desk {
    grid-template-columns: clamp(240px, 22cqi, 300px) minmax(0, 1fr);
  }
}

/* 右栏：内部唯一滚动容器（消息流），页体不产生纵向滚动 */
.ac-pane {
  display: flex;
  flex-direction: column;
  min-height: 0;
  min-width: 0;
  height: clamp(420px, calc(100vh - 208px), 900px);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-element);
  background: var(--surface-panel);
  overflow: hidden;
}

.ac-toolbar {
  flex: none;
  display: flex;
  align-items: center;
  gap: 8px;
  min-height: 40px;
  padding: 0 14px;
  border-bottom: 1px solid var(--color-border);
}
.ac-pane-title {
  margin: 0;
  font-size: var(--text-md);
  font-weight: 600;
  color: var(--color-text);
  min-width: 0;
  max-width: 60%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
/* 回合级播报位：对读屏在场、对视觉隐去 */
.ac-announce {
  position: absolute;
  width: 1px; height: 1px; margin: -1px; padding: 0;
  overflow: hidden; clip: rect(0 0 0 0); white-space: nowrap; border: 0;
}

.ac-list-banner { margin: 10px 14px 0; display: flex; align-items: center; gap: 10px; }
.ac-send-banner { margin: 0 14px; }
.ac-bridge-banner { margin: 0 14px; }

/* 200% 缩放守则：文本容器 min-width:0 + 气泡内 overflow-wrap 在 Flow 内护底 */
@media (pointer: coarse) {
  .ac-list-banner .btn, .ac-send-banner .btn { min-height: 44px; }
}
</style>
