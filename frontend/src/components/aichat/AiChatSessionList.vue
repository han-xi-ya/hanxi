<script setup lang="ts">
// AI 聊天左栏会话列表：资源列表行形制（component-patterns「Resource list」），
// 纯展示 + 动作上抛，不碰 RPC——运行态字段（state/sending/turnInFlight/unread）
// 由编排层（useAiChat）逐行喂入。
//
// 行内二级动作钮（重命名/封存/删除）为**常显微钮**——checklist 纪律"动作不得
// 只随 hover 出现"；不嵌套按钮进行点击，整行主体是独立 button。
// 状态不靠颜色单传：每行状态字（回复中/空闲/已封存/已终止）恒在场。
//
// 窄档（<840px，.ac-desk 容器查询）收为顶部横滑 chips 行：标题+状态点，选中描边
// （设计稿 §3.5——不照抄 wechat 整栏堆叠，栏高会吃掉整个视口）；行级动作与计数
// 在 chips 档收起，主操作（新建/选择）不丢。
import UiButton from '../ui/UiButton.vue'
import type { SessionRow } from '../../composables/useAiChat'

const props = defineProps<{
  sessions: SessionRow[]
  currentId: string
}>()

const emit = defineEmits<{
  select: [id: string]
  create: []
  rename: [id: string]
  close: [id: string]
  delete: [id: string]
  refresh: []
}>()

// —— 呈现函数（就近消费件）——
function isBusy(s: SessionRow): boolean {
  return s.sending || s.turnInFlight
}

function stateText(s: SessionRow): string {
  if (isBusy(s)) return '回复中'
  if (s.state === 'dead') return '已终止'
  if (s.archived || s.state === 'closed') return '已封存'
  return '空闲'
}

function fmtStamp(ts: number): string {
  const d = new Date(ts)
  const now = new Date()
  const sameDay = d.getFullYear() === now.getFullYear() && d.getMonth() === now.getMonth() && d.getDate() === now.getDate()
  return sameDay ? d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }) : d.toLocaleDateString([], { month: 'numeric', day: 'numeric' })
}
</script>

<template>
  <aside class="ac-sessions">
    <header class="ac-sess-head">
      <UiButton variant="primary" small class="ac-create" title="新建本地会话（只记名录，不起进程不外发；第一条消息才会启动）" @click="emit('create')">
        ＋ 新建会话
      </UiButton>
      <button type="button" class="btn btn-ghost btn-small ac-refresh" title="重新拉取会话名录" @click="emit('refresh')">刷新</button>
      <span class="ac-count">共 {{ sessions.length }} 个会话</span>
    </header>

    <div v-if="sessions.length === 0" class="state-box ac-sessions-empty">
      还没有会话。点「新建会话」开始——第一条消息发出前，内容不会离开这台机器。
    </div>

    <ul v-else class="ac-sess-list" role="list">
      <li
        v-for="s in sessions"
        :key="s.id"
        class="ac-sess"
        :class="{ 'is-active': s.id === props.currentId }"
      >
        <button
          type="button"
          class="ac-sess-main"
          :aria-current="s.id === props.currentId ? 'true' : undefined"
          @click="emit('select', s.id)"
        >
          <span class="ac-sess-title" :title="s.title">{{ s.title }}</span>
          <span class="ac-sess-meta">
            <span v-if="isBusy(s)" class="live-pulse ac-dot" aria-hidden="true"></span>
            <span class="ac-sess-state">{{ stateText(s) }}</span>
            <span class="ac-sess-time">{{ fmtStamp(s.createdAt) }}</span>
          </span>
        </button>
        <span v-if="s.unread > 0" class="ac-unread" :title="`新增 ${s.unread} 条消息`">{{ s.unread }}</span>
        <span class="ac-sess-actions">
          <button type="button" class="btn btn-ghost btn-small" aria-label="重命名会话" title="重命名（封存中的会话改名后解除封存）" @click="emit('rename', s.id)">改名</button>
          <button
            type="button"
            class="btn btn-ghost btn-small"
            aria-label="封存会话"
            :disabled="s.archived"
            :title="s.archived ? '已封存：发送新消息会自动复进' : '结束本轮并回收进程（对话档案保留，下一条自动复进）'"
            @click="emit('close', s.id)"
          >
            封存
          </button>
          <button type="button" class="btn btn-ghost btn-small ac-del" aria-label="删除会话" title="从名录除名（本次运行的消息流清空；CLI 侧档案由 claude 自管）" @click="emit('delete', s.id)">删除</button>
        </span>
      </li>
    </ul>
  </aside>
</template>

<style scoped>
.ac-sessions {
  display: flex; flex-direction: column; gap: 10px;
  min-width: 0; min-height: 0;
}
.ac-sess-head { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; min-height: 40px; }
.ac-create { flex: none; }
.ac-count { margin-left: auto; font-size: var(--text-xs); color: var(--color-text-subtle); white-space: nowrap; }

.ac-sess-list {
  list-style: none; margin: 0; padding: 0;
  flex: 1; min-height: 0;
  display: flex; flex-direction: column; gap: 6px;
  overflow-y: auto;
}

.ac-sess {
  display: flex; align-items: center; gap: 6px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-control);
  background: var(--surface-panel);
  padding: 2px 4px 2px 2px;
  min-width: 0;
}
.ac-sess.is-active {
  border-color: color-mix(in srgb, var(--color-primary) 45%, var(--color-border));
  background: var(--color-primary-soft);
}
.ac-sess-main {
  flex: 1; min-width: 0;
  display: flex; flex-direction: column; gap: 2px; align-items: flex-start;
  border: 0; background: transparent; color: var(--color-text);
  font: inherit; text-align: left;
  padding: 7px 8px;
  border-radius: var(--radius-control);
  cursor: pointer;
}
.ac-sess-main:hover { background: var(--surface-hover); }
.ac-sess-main:focus-visible { outline: 2px solid var(--focus-ring); outline-offset: 1px; }
.ac-sess-title {
  max-width: 100%;
  min-width: 0;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  font-size: var(--text-sm); font-weight: 500;
}
.ac-sess-meta { display: flex; align-items: center; gap: 6px; font-size: var(--text-micro); color: var(--color-text-subtle); }
.ac-sess-state { white-space: nowrap; }
.ac-dot { width: 7px; height: 7px; border-radius: 50%; background: var(--color-primary); display: inline-block; flex: none; }

/* 未读：数字报数（状态不靠颜色单传），非零才在场 */
.ac-unread {
  flex: none;
  min-width: 18px; height: 18px; padding: 0 5px;
  border-radius: var(--radius-pill);
  background: var(--state-information-soft);
  color: var(--state-information);
  font-size: var(--text-micro); font-weight: 600;
  font-variant-numeric: tabular-nums;
  display: inline-flex; align-items: center; justify-content: center;
}

/* 行内动作常显（hover-only 违 checklist）；窄档随 meta 一起收起 */
.ac-sess-actions { display: flex; gap: 2px; flex: none; }
.ac-sess-actions .btn { opacity: 0.75; }
.ac-del:hover:not(:disabled) { color: var(--state-danger); background: var(--state-danger-soft); }

/* 窄档（<840）：整栏堆叠会吃掉视口，收为横滑 chips 行（面包屑同款"窄屏横滚不折行"） */
@container ac (max-width: 839.98px) {
  .ac-sessions { gap: 8px; }
  .ac-sess-list { flex-direction: row; overflow-x: auto; overflow-y: hidden; padding-bottom: 2px; }
  .ac-sess { flex: none; padding: 2px; }
  .ac-sess-main { padding: 5px 10px; flex-direction: row; align-items: center; gap: 6px; }
  .ac-sess-meta, .ac-sess-time, .ac-sess-actions, .ac-count { display: none; }
  .ac-sess-state { display: none; }
}

@media (pointer: coarse) {
  .ac-sess-main { min-height: 44px; }
  .ac-sess-actions .btn { min-height: 44px; }
}
</style>
