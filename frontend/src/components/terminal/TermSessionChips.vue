<script setup lang="ts">
// 终端会话 chips 栏（纯展示 + 事件上抛，编排全在 useTerminal）：
// 横向滚动的会话芯片条 + 新建下拉（三档 Shell 固定）+ 刷新。
// 形制对位 AiChatSessionList：常显微动作钮（动作不得只随 hover 出现）、
// 状态字恒带文字不单靠颜色、窄屏横滚不折行（component-patterns 纪律）。
// 芯片即选择器：点谁接谁（v0 单通道语义——每个终端页一条连接，切页即断）。
import { ref } from 'vue'
import UiButton from '../ui/UiButton.vue'
import UiStatusChip from '../ui/UiStatusChip.vue'
import { TERM_PROFILES, profileLabel, rowStatusText, rowStatusTone } from '../../composables/useTerminal'
import type { ConnStatus, TermProfileId, TermRow } from '../../composables/useTerminal'

defineProps<{
  sessions: TermRow[]
  currentId: string
  /** 当前会话的显示通道状态（其余芯片按账本 alive 显示）。 */
  connStatus: ConnStatus
  /** OpenTerminal 在途：新建钮禁用+文案换档，连点只许一发。 */
  creating: boolean
}>()

const emit = defineEmits<{
  (e: 'select', id: string): void
  (e: 'create', profileId: TermProfileId): void
  (e: 'rename', id: string): void
  (e: 'kill', id: string): void
  (e: 'refresh'): void
}>()

const menuOpen = ref(false)

function pickProfile(profileId: TermProfileId): void {
  menuOpen.value = false
  emit('create', profileId)
}

function onNewKeydown(e: KeyboardEvent): void {
  if (e.key === 'Escape') menuOpen.value = false
}

/** 焦点离开成组（含点空白）即收菜单，不造无出口的浮层。 */
function onGroupFocusout(e: FocusEvent): void {
  const el = e.currentTarget as HTMLElement
  if (menuOpen.value && !el.contains(e.relatedTarget as Node | null)) menuOpen.value = false
}
</script>

<template>
  <div class="tc-bar">
    <div class="tc-chips">
      <div
        v-for="row in sessions"
        :key="row.id"
        class="tc-slot"
        :class="{ 'tc-slot-current': row.id === currentId }"
      >
        <button
          type="button"
          class="tc-chip"
          :aria-current="row.id === currentId ? 'true' : undefined"
          :title="`${profileLabel(row.profileId)} · pid ${row.pid}｜v0：连接即断、进程活着，切回需重新接入`"
          @click="emit('select', row.id)"
        >
          <span class="tc-title">{{ row.title || profileLabel(row.profileId) }}</span>
          <UiStatusChip :tone="rowStatusTone(row, row.id === currentId, connStatus)">
            {{ rowStatusText(row, row.id === currentId, connStatus) }}
          </UiStatusChip>
        </button>
        <span class="tc-actions">
          <button
            type="button"
            class="btn btn-ghost btn-small tc-rename"
            aria-label="重命名终端"
            title="重命名（只动账本，不碰进程）"
            @click="emit('rename', row.id)"
          >改名</button>
          <button
            type="button"
            class="btn btn-danger-outline btn-small tc-kill"
            aria-label="Kill 终端"
            title="杀会话并除账（危险，需确认）"
            @click="emit('kill', row.id)"
          >Kill</button>
        </span>
      </div>
      <span v-if="sessions.length === 0" class="tc-none">还没有终端会话——右侧「新建」选一个 Shell；已有名录点芯片即接入。</span>
    </div>

    <div class="tc-tools">
      <div class="tc-new" @keydown="onNewKeydown" @focusout="onGroupFocusout">
        <UiButton
          class="tc-create"
          small
          variant="primary"
          :disabled="creating"
          aria-haspopup="true"
          :aria-expanded="menuOpen"
          title="新建终端会话（命令行 / PowerShell / Claude Code 三档固定）"
          @click="menuOpen = !menuOpen"
        >
          <span class="tc-create-label">{{ creating ? '创建中…' : '新建' }}</span>
          <span aria-hidden="true">▾</span>
        </UiButton>
        <div v-if="menuOpen" class="tc-menu" role="menu" aria-label="选择 Shell">
          <button
            v-for="p in TERM_PROFILES"
            :key="p.id"
            type="button"
            role="menuitem"
            class="tc-menu-item"
            :disabled="creating"
            :title="p.hint"
            @click="pickProfile(p.id)"
          >{{ p.label }}</button>
        </div>
      </div>
      <button type="button" class="btn btn-secondary btn-small tc-refresh" title="重拉终端名录（不新建进程）" @click="emit('refresh')">刷新</button>
      <span class="tc-count">共 {{ sessions.length }} 个</span>
    </div>
  </div>
</template>

<style scoped>
.tc-bar {
  display: flex;
  align-items: flex-start;
  gap: 10px;
  min-width: 0;
}

/* 窄屏横滚不折行（面包屑同款纪律）；滚动条由全局原子弱化 */
.tc-chips {
  display: flex;
  align-items: center;
  gap: 8px;
  flex: 1;
  min-width: 0;
  overflow-x: auto;
  padding-bottom: 2px; /* 滚动条不压芯片描边 */
}
.tc-none {
  font-size: var(--text-xs);
  color: var(--color-text-muted);
  white-space: nowrap;
}

.tc-slot {
  display: flex;
  align-items: center;
  gap: 2px;
  flex: none;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-pill);
  background: var(--surface-soft);
  padding: 2px 3px 2px 2px;
}
.tc-slot-current {
  border-color: color-mix(in srgb, var(--color-primary) 25%, var(--color-border));
  background: var(--surface-hover);
}

.tc-chip {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  min-width: 0;
  border: 0;
  background: none;
  padding: 3px 6px;
  font: inherit;
  color: var(--color-text);
  cursor: pointer;
  border-radius: var(--radius-pill);
}
.tc-chip:hover { color: var(--color-primary); }
.tc-title {
  max-width: 14ch;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: var(--text-sm);
}

.tc-actions { display: inline-flex; gap: 2px; }
.tc-actions .btn { padding: 1px 6px; }

.tc-tools {
  display: flex;
  align-items: center;
  gap: 8px;
  flex: none;
}
.tc-new { position: relative; }
/* busy 文案换档尺寸不动（checklist：控件不跳宽） */
.tc-create { min-width: 88px; }
.tc-create-label { margin-right: 4px; }
.tc-menu {
  position: absolute;
  top: calc(100% + 4px);
  right: 0;
  z-index: 30;
  display: flex;
  flex-direction: column;
  min-width: 148px;
  border: 1px solid var(--color-border-strong);
  border-radius: var(--radius-control);
  background: var(--surface-panel);
  box-shadow: var(--shadow-small);
  padding: 4px;
}
.tc-menu-item {
  border: 0;
  background: none;
  text-align: left;
  padding: 6px 10px;
  border-radius: var(--radius-control);
  font: inherit;
  font-size: var(--text-sm);
  color: var(--color-text);
  cursor: pointer;
}
.tc-menu-item:hover { background: var(--surface-hover); color: var(--color-primary); }
.tc-menu-item:disabled { color: var(--color-text-subtle); cursor: default; background: none; }
.tc-count {
  font-size: var(--text-xs);
  color: var(--color-text-subtle);
  font-variant-numeric: tabular-nums;
}

@media (pointer: coarse) {
  .tc-chip, .tc-actions .btn, .tc-create, .tc-refresh { min-height: 44px; }
}
</style>
