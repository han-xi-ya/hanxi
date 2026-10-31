<script setup lang="ts">
// AI 消息流（右栏主体）：纯展示壳 + 滚动机构宿主。数据由编排层（useAiChat）按
// 当前会话喂入，本件不感知 sessionId——和 WechatBotChatFlow 的分工口径一致。
//
// 滚动机构直接消费 useLogTails（aichat 消息流为该件第三消费方，"日志面板家族"
// 名实随附扩展）：autoScroll 闩 / 40px 贴底阈值 / @scroll.passive 挂法 /
// nextTick 闩控落底，聊天流语义与之逐字吻合。本件经 defineExpose 把闩与落底
// 动作交给编排层触达（句柄契约：wechatbot 同款，nextTick 时序即契约）。
//
// 虚拟滚动否决留档（防回退）：仓内对"高频流式长列表"的既有答案是 rAF 合帧 +
// 稳定 key + 原地裁头（useLogTails 家族先例：frpc 2000 行 / ddns-go 400 行均无
// 虚拟滚动）；本流 FLOW_CAP=600 条/会话，裁头保尾在编排层，v-for 恒用 item.id
// 稳定 key（:index key 会让满仓裁剪整表换键重补丁——frpc perf 改造注释原话）。
// 若未来实测仍卡，第一杠杆是降 FLOW_CAP，不是引库造第二套渲染系统。
//
// 呈现函数就近安放（§9.6 口径）：fmtTime / formatTurnStats 只服务本模板。
// turn-stats 行原样取后端字段做单位换算（毫秒/字节/$），前端零二次统计。
// 文本粒度是 content 块快照不是逐 token：流内不装打字机，块出现即整块上屏。
//
// JSON 回程小卡（2026-10-31 jsonfmt 收纳裁决的入向对口）：assistant 回合收口后
// 扫描该轮文本里的 ```json 块，本地 JSON.parse 只当判卷（collectOpenableJsonBlocks），
// 过了才挂「在 JSON 美化器中打开」卡——按下上抛 open-json(块原文)，交接与跳页归视图；
// 本件不当第二个渲染器，真美化回 jsonfmt 后端做（大整数保真在那边）。
// 收口信号取 statsByTurn 在场：turn-stats（EventTurnDone）是回合终态唯一权威；
// dead 轮无 stats、文本本就可能残缺，不钓用户去打开半成品——是如实，不是遗漏。
import { computed, watch } from 'vue'
import UiBanner from '../ui/UiBanner.vue'
import UiButton from '../ui/UiButton.vue'
import { useLogTails } from '../../composables/useLogTails'
import { collectOpenableJsonBlocks } from '../../composables/useJsonChatBridge'
import { FLOW_CAP } from '../../composables/useAiChat'
import type { TimelineItem, TurnStats } from '../../composables/useAiChat'

const props = defineProps<{
  items: TimelineItem[]
  /** 当前会话的 turnId → stats 映射（Map 由编排层版本号驱动整体替换语义）。 */
  statsByTurn: Map<string, TurnStats>
  /** 回合在途且队尾不是本轮块流：渲染等待占位（几十秒口径由文案说出）。 */
  waiting: boolean
  /** 脱锁/后台期间新增条数（悬浮钮报数）。 */
  unread: number
}>()

const emit = defineEmits<{
  /** 闩重新咬合（滚回底部）：编排层据此清未读计数。 */
  'reached-bottom': []
  /** 「在 JSON 美化器中打开」按下：交块原文给视图写桥并导航，本件不碰桥。 */
  'open-json': [text: string]
}>()

const tails = useLogTails({ maxLines: FLOW_CAP })

watch(tails.autoScroll, (near) => {
  if (near) emit('reached-bottom')
})

function scrollToBottom(force = false): void {
  if (force) tails.autoScroll.value = true
  void tails.scrollToBottom()
}

// 句柄以方法暴露闩位：defineExpose 的公开代理自动解包 Ref，裸 Ref 交出去拿到的是
// 值而不是壳，编排层 isFollowing() 不依赖该解包语义。
defineExpose({ isFollowing: () => tails.autoScroll.value, scrollToBottom })

// —— 呈现 ——
function fmtTime(ts: number): string {
  return new Date(ts).toLocaleTimeString()
}

function fmtDuration(ms: number): string {
  return ms < 1000 ? `${ms} ms` : `${(ms / 1000).toFixed(1)} s`
}

function fmtBytes(bytes: number): string {
  return bytes < 1024 ? `${bytes} B` : `${(bytes / 1024).toFixed(1)} KB`
}

function formatTurnStats(s: TurnStats): string {
  return `耗时 ${fmtDuration(s.elapsedMs)} · 首字 ${fmtDuration(s.ttftMs)} · 入 ${fmtBytes(s.inBytes)} · 出 ${fmtBytes(s.outBytes)} · 费用 $${s.totalCostUsd.toFixed(4)}`
}

/**
 * stats 行锚点：该 turnId 最后一项之后。每轮一条 turn-stats（EventTurnDone 是
 * 回合终态唯一权威），锚到用户轮次尾部即"乐观气泡 ↔ stats"同链可对账。
 */
const statsAnchorIds = computed(() => {
  const lastByTurn = new Map<string, number>()
  for (const it of props.items) {
    if (it.turnId && props.statsByTurn.has(it.turnId)) lastByTurn.set(it.turnId, it.id)
  }
  return new Set(lastByTurn.values())
})

/** 收口回合的 assistant 项 → 可打开的 JSON 块（判卷即过滤，非法块不入表）。 */
const jsonBlocksByItem = computed(() => {
  const m = new Map<number, string[]>()
  for (const it of props.items) {
    if (it.kind !== 'assistant' || !it.turnId || !props.statsByTurn.has(it.turnId)) continue
    const blocks = collectOpenableJsonBlocks(it.text)
    if (blocks.length > 0) m.set(it.id, blocks)
  }
  return m
})

/** 卡 title 的内容预览：压平空白取前 60 字，只当"这是哪块"的指认，不是渲染。 */
function blockPreview(b: string): string {
  const flat = b.replace(/\s+/g, ' ').trim()
  return flat.length > 60 ? `${flat.slice(0, 60)}…` : flat
}

</script>

<template>
  <div class="ac-flow-wrap">
    <div
      :ref="(el) => { tails.bodyRef.value = el as HTMLElement | null }"
      class="ac-flow"
      tabindex="0"
      role="log"
      aria-label="会话消息流"
      @scroll.passive="tails.onBodyScroll"
    >
      <div v-if="items.length === 0 && !waiting" class="state-box ac-empty">
        该会话还没有消息——历史消息不在本机内存里，本次运行内收发的内容会保留在这里。
        在下方输入并回车，第一条消息会先经过外发确认。
      </div>

      <template v-for="item in items" :key="item.id">
        <div v-if="item.kind === 'user'" class="ac-msg ac-msg--user">
          <span class="ac-msg-time">{{ fmtTime(item.ts) }}</span>
          <p class="ac-msg-text">{{ item.text }}</p>
        </div>
        <div v-else-if="item.kind === 'assistant'" class="ac-msg ac-msg--assistant">
          <span class="ac-msg-time">{{ fmtTime(item.ts) }}</span>
          <p class="ac-msg-text">{{ item.text }}</p>
        </div>
        <UiBanner v-else-if="item.kind === 'error'" tone="error" class="ac-msg-error">
          {{ item.text }}
        </UiBanner>
        <div v-else class="ac-note-pill">
          <span>{{ item.text }}</span>
          <span class="ac-note-time">{{ fmtTime(item.ts) }}</span>
        </div>
        <!-- 回程小卡：该轮收口且块能 parse 才在场；多块逐块列，逐块可开 -->
        <div v-if="jsonBlocksByItem.has(item.id)" class="ac-json-bar">
          <UiButton
            v-for="(b, i) in jsonBlocksByItem.get(item.id)!"
            :key="`${item.id}-${i}`"
            variant="secondary"
            small
            class="ac-json-card"
            :title="`解析通过：${blockPreview(b)}`"
            @click="emit('open-json', b)"
          >
            在 JSON 美化器中打开<span v-if="jsonBlocksByItem.get(item.id)!.length > 1">（第 {{ i + 1 }} 块）</span>
          </UiButton>
        </div>
        <div v-if="statsAnchorIds.has(item.id)" class="ac-stats" role="note">
          {{ formatTurnStats(statsByTurn.get(item.turnId)!) }}
        </div>
      </template>

      <div v-if="waiting" class="state-box ac-waiting" role="status">
        <span class="live-pulse ac-wait-dot" aria-hidden="true"></span>
        AI 正在回复…（常驻会话单轮实测 2.5~8 秒；期间可切去看别的会话，回复会持续回到本会话）
      </div>
    </div>

    <button
      v-if="!tails.autoScroll.value"
      type="button"
      class="btn btn-secondary btn-small ac-back"
      @click="scrollToBottom(true)"
    >
      ↓ 回到最新消息<span v-if="unread > 0">（{{ unread }} 条新内容）</span>
    </button>
  </div>
</template>

<style scoped>
/* 消息流是右栏内部唯一滚动容器（页体不产生纵向滚动）；min-width:0 护住窄档 */
.ac-flow-wrap { position: relative; flex: 1; min-height: 0; display: flex; }
.ac-flow {
  flex: 1; min-height: 0; min-width: 0;
  overflow-y: auto;
  padding: 14px 16px;
  display: flex; flex-direction: column; gap: 10px;
}
.ac-flow:focus-visible { outline: 2px solid var(--focus-ring); outline-offset: -2px; }

.ac-empty { margin: auto 0; }

.ac-msg {
  max-width: 80%;
  min-width: 0;
  padding: 8px 12px;
  border-radius: var(--radius-control);
  box-shadow: var(--shadow-small);
}
.ac-msg-time {
  display: block;
  font-family: var(--font-mono);
  font-variant-numeric: tabular-nums;
  font-size: var(--text-micro);
  color: var(--color-text-subtle);
  margin-bottom: 2px;
}
.ac-msg-text {
  margin: 0;
  font-size: var(--text-base);
  line-height: 1.6;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  user-select: text;
}

/* 用户气泡：primary-soft 底 + 25% primary 混边（design-system 强调边档），
 * 暗档分离不足时只许加边到 30% 混，不加新色值 */
.ac-msg--user {
  align-self: flex-end;
  background: var(--color-primary-soft);
  border: 1px solid color-mix(in srgb, var(--color-primary) 25%, var(--color-border));
  color: var(--color-text);
}
.ac-msg--assistant {
  align-self: flex-start;
  background: var(--surface-panel);
  border: 1px solid var(--color-border);
  color: var(--color-text);
}

/* note：居中 system 胶囊（wechatbot .system-pill 形制自绘，不 import 其 scoped） */
.ac-note-pill {
  align-self: center;
  max-width: 90%;
  display: flex; align-items: center; gap: 6px;
  background: var(--surface-hover);
  color: var(--color-text-muted);
  border-radius: var(--radius-pill);
  padding: 4px 12px;
  font-size: var(--text-sm);
  overflow-wrap: anywhere;
}
.ac-note-time { font-family: var(--font-mono); font-size: var(--text-micro); color: var(--color-text-subtle); }

/* JSON 回程小卡行：贴左跟随气泡、多块换行不撑破；细边框成组不与气泡抢体块 */
.ac-json-bar {
  align-self: flex-start;
  max-width: 80%;
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

/* 回合账目行：数字全 mono + tabular-nums，单位换算即止 */
.ac-stats {
  align-self: flex-start;
  font-family: var(--font-mono);
  font-variant-numeric: tabular-nums;
  font-size: var(--text-xs);
  color: var(--color-text-subtle);
  padding: 0 2px;
}

.ac-msg-error { align-self: stretch; max-width: 100%; }

/* 在途占位：live-pulse 只给真实在途（done/idle/dead 一律静态），文案说出耗时量级 */
.ac-waiting { margin-top: 2px; }
.ac-wait-dot {
  width: 8px; height: 8px; border-radius: 50%;
  background: var(--color-primary);
  display: inline-block; flex: none;
}

.ac-back {
  position: absolute;
  right: 16px;
  bottom: 12px;
  z-index: 2;
}

@media (pointer: coarse) { .ac-back { min-height: 44px; } }
</style>
