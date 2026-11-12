<script setup lang="ts">
// 终端会话 chips 栏（纯展示 + 事件上抛，编排全在 useTerminal）：
// 左=新建入口条（四档 Shell **平铺直开**，每档一枚「📁 选目录后开」副钮），
// 右=横向滚动的会话芯片条 + 刷新 + 计数。
// 2026-11-12 机主要求"新建直接平铺"：原下拉一次点击只看见"新建"两个字，
// 要开哪档得先展开再找——现在四档常显、一次点击即起会话，目录副钮同样常显
// （动作不得只随 hover 或展开出现，component-patterns 在册纪律）。
// 状态字恒带文字不单靠颜色；窄屏两条各自横滚不折行，滚动条按本件色板自绘。
// 芯片即选择器：点谁接谁（v0 单通道语义——每个终端页一条连接，切页即断）。
import UiStatusChip from '../ui/UiStatusChip.vue'
import AppIcon from '../ui/AppIcon.vue'
import { TERM_PROFILES, profileLabel, rowStatusText, rowStatusTone } from '../../composables/useTerminal'
import type { ConnStatus, TermProfileId, TermRow } from '../../composables/useTerminal'

defineProps<{
  sessions: TermRow[]
  currentId: string
  /** 当前会话的显示通道状态（其余芯片按账本 alive 显示）。 */
  connStatus: ConnStatus
  /** OpenTerminal 在途：入口钮整组禁用+文案换档，连点只许一发。 */
  creating: boolean
  /** 目录选择对话框在途（模态等人手）：整组禁用，不许第二发。 */
  picking: boolean
}>()

const emit = defineEmits<{
  (e: 'select', id: string): void
  (e: 'create', profileId: TermProfileId): void
  (e: 'create-dir', profileId: TermProfileId): void
  (e: 'rename', id: string): void
  (e: 'kill', id: string): void
  (e: 'refresh'): void
}>()

/** 直开：一次点击就起会话（无中间菜单）。 */
function openProfile(profileId: TermProfileId): void {
  emit('create', profileId)
}

/** 先选目录再起：AI 档（claude/omp）的项目上下文跟着 cwd 走，故副钮常显。 */
function openProfileWithDir(profileId: TermProfileId): void {
  emit('create-dir', profileId)
}

/** 目录段显示名：取路径末段（盘符/共享路径不装精确，hover title 给全串）。 */
function dirLeaf(dir: string): string {
  const parts = dir.split(/[\\/]+/).filter((p) => p.length > 0)
  return parts[parts.length - 1] ?? dir
}

</script>

<template>
  <div class="tc-bar">
    <!-- 新建入口：四档平铺直开，每档自带「📁」副钮（先选目录再起） -->
    <div class="tc-launch" role="group" aria-label="新建终端会话">
      <span class="tc-launch-k">新建</span>
      <div class="tc-launch-row">
        <div
          v-for="p in TERM_PROFILES"
          :key="p.id"
          class="tc-launch-slot"
          :class="`tc-tone-${p.tone}`"
        >
          <button
            type="button"
            class="tc-open"
            :disabled="creating || picking"
            :title="`${p.hint}｜点一下即起会话`"
            @click="openProfile(p.id)"
          >
            <span class="tc-open-dot" aria-hidden="true"></span>
            <span class="tc-open-label">{{ picking ? '选目录中…' : creating ? '创建中…' : p.label }}</span>
          </button>
          <button
            type="button"
            class="tc-open-dir"
            :disabled="creating || picking"
            :aria-label="`在指定目录新建 ${p.label}`"
            :title="`先选目录再新建 ${p.label}（弹原生选择器；AI 档的项目上下文随之定）`"
            @click="openProfileWithDir(p.id)"
          ><AppIcon name="folder" :size="14" /></button>
        </div>
      </div>
    </div>

    <!-- 会话条：点芯片即接入；改名/Kill 常显不藏 hover -->
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
          :title="`${profileLabel(row.profileId)} · pid ${row.pid} · ${row.dir ? '目录 ' + row.dir : '继承 hanxi 目录'}｜v0：连接即断、进程活着，切回需重新接入`"
          @click="emit('select', row.id)"
        >
          <span class="tc-title">{{ row.title || profileLabel(row.profileId) }}</span>
          <span v-if="row.dir" class="tc-dir" aria-hidden="true">{{ dirLeaf(row.dir) }}</span>
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
      <span v-if="sessions.length === 0" class="tc-none">还没有会话——左侧点一个档即开；已有名录点芯片即接入。</span>
    </div>

    <div class="tc-tools">
      <button type="button" class="btn btn-secondary btn-small tc-refresh" title="重拉终端名录（不新建进程）" @click="emit('refresh')">刷新</button>
      <span class="tc-count">共 {{ sessions.length }} 个</span>
    </div>
  </div>
</template>

<style scoped>
/* 两行栅格：第一行=新建入口条（整宽平铺），第二行=会话条 + 刷新/计数。
 * 入口条与会话条各自横滚，窄屏不折行、不挤压终端画面高度。 */
.tc-bar {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  align-items: center;
  gap: 8px 10px;
  min-width: 0;
}

/* ---------- 新建入口：四档平铺 ---------- */
.tc-launch {
  grid-column: 1 / -1;
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}
.tc-launch-k {
  flex: none;
  font-size: var(--text-xs);
  letter-spacing: 0.08em;
  color: var(--color-text-subtle);
}
.tc-launch-row {
  display: flex;
  align-items: stretch;
  gap: 6px;
  min-width: 0;
  overflow-x: auto;
  padding: 2px 0 4px;
}

/* 逐档色相：四档各占一个可分色相，且明暗两轴都成立。
 * 命令行=主色青、PowerShell=信息蓝、Claude=琥珀；omp 用**独立紫**——
 * 蓝青调和只会得到"另一种蓝"（实测与 PowerShell 撞色），紫在两轴下都与三者可分。
 * 紫值按主题各给一档（暗侧提亮保对比），不做机械反色；色点+档名双通道表意，
 * 色盲也不丢信息。 */
.tc-tone-shell { --tc-hue: var(--color-primary); }
.tc-tone-ps { --tc-hue: var(--state-information); }
.tc-tone-claude { --tc-hue: var(--state-warning); }
.tc-tone-omp { --tc-hue: #6d3fd4; }
:root[data-theme="dark"] .tc-tone-omp { --tc-hue: #b795ff; }

.tc-launch-slot {
  display: inline-flex;
  align-items: stretch;
  flex: none;
  border: 1px solid color-mix(in srgb, var(--tc-hue) 32%, var(--color-border));
  background: color-mix(in srgb, var(--tc-hue) 9%, var(--surface-panel));
  overflow: hidden;
  transition: border-color var(--motion-base) ease, background var(--motion-base) ease,
    box-shadow var(--motion-base) ease, transform var(--motion-fast) ease;
}
.tc-launch-slot:hover {
  border-color: color-mix(in srgb, var(--tc-hue) 60%, var(--color-border));
  background: color-mix(in srgb, var(--tc-hue) 14%, var(--surface-panel));
  box-shadow: var(--shadow-small);
  transform: translateY(-1px);
}
.tc-launch-slot:focus-within {
  border-color: var(--tc-hue);
  box-shadow: 0 0 0 2px color-mix(in srgb, var(--tc-hue) 22%, transparent);
}

.tc-open {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  min-height: var(--control-h-md);
  padding: 4px 12px;
  border: 0;
  background: none;
  font: inherit;
  font-size: var(--text-sm);
  font-weight: 500;
  white-space: nowrap;
  color: color-mix(in srgb, var(--tc-hue) 78%, var(--color-text));
  cursor: pointer;
}
.tc-open:disabled { opacity: 0.55; cursor: default; }
.tc-launch-slot:hover .tc-open:not(:disabled) { color: color-mix(in srgb, var(--tc-hue) 92%, var(--color-text)); }

/* 色点：常显的档身份标记，hover 时轻微放大（100ms，只动 1px 级） */
.tc-open-dot {
  width: 7px;
  height: 7px;
  flex: none;
  border-radius: 50%;
  background: var(--tc-hue);
  box-shadow: 0 0 0 3px color-mix(in srgb, var(--tc-hue) 18%, transparent);
  transition: transform var(--motion-fast) ease;
}
.tc-launch-slot:hover .tc-open-dot { transform: scale(1.18); }
/* busy 期文案换档但宽度不跳（checklist：控件不跳宽） */
.tc-open-label { min-width: 8ch; text-align: left; }
.tc-open-dir {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  flex: none;
  min-width: 30px;
  border: 0;
  border-left: 1px solid color-mix(in srgb, var(--tc-hue) 26%, var(--color-border));
  background: color-mix(in srgb, var(--tc-hue) 12%, transparent);
  /* 图标必须显式取色：button 不继承父钮的 color，缺这行会落到浏览器默认黑 */
  color: color-mix(in srgb, var(--tc-hue) 72%, var(--color-text));
  cursor: pointer;
  transition: background var(--motion-base) ease, color var(--motion-base) ease;
}
.tc-open-dir:hover:not(:disabled) {
  background: color-mix(in srgb, var(--tc-hue) 26%, transparent);
  color: color-mix(in srgb, var(--tc-hue) 92%, var(--color-text));
}
.tc-open-dir:disabled { opacity: 0.5; cursor: default; }

/* ---------- 会话条 ---------- */
.tc-chips {
  grid-column: 1;
  display: flex;
  align-items: center;
  gap: 8px;
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
  transition: border-color var(--motion-base) ease, background var(--motion-base) ease;
}
.tc-slot-current {
  border-color: color-mix(in srgb, var(--color-primary) 45%, var(--color-border));
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
/* 芯片上的目录末段：弱信号，hover title 给全串。 */
.tc-dir {
  margin-left: 6px;
  font-size: var(--text-xs);
  color: var(--color-text-muted);
  max-width: 9ch;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.tc-actions { display: inline-flex; gap: 2px; }
.tc-actions .btn { padding: 1px 6px; }

.tc-tools {
  grid-column: 2;
  display: flex;
  align-items: center;
  gap: 8px;
  flex: none;
}
.tc-count {
  font-size: var(--text-xs);
  color: var(--color-text-subtle);
  font-variant-numeric: tabular-nums;
}

/* ---------- 滚动条：按本件色板自绘，不用浏览器默认 ---------- */
.tc-launch-row,
.tc-chips {
  scrollbar-width: thin;
  scrollbar-color: color-mix(in srgb, var(--color-primary) 45%, var(--color-border)) transparent;
}
.tc-launch-row::-webkit-scrollbar,
.tc-chips::-webkit-scrollbar { height: 6px; }
.tc-launch-row::-webkit-scrollbar-track,
.tc-chips::-webkit-scrollbar-track {
  background: color-mix(in srgb, var(--color-primary) 8%, transparent);
  border-radius: var(--radius-pill);
}
.tc-launch-row::-webkit-scrollbar-thumb,
.tc-chips::-webkit-scrollbar-thumb {
  background: color-mix(in srgb, var(--color-primary) 42%, var(--color-border));
  border-radius: var(--radius-pill);
}
.tc-launch-row::-webkit-scrollbar-thumb:hover,
.tc-chips::-webkit-scrollbar-thumb:hover { background: var(--color-primary); }

/* 粗指针：触控目标抬到 44px，色点与字档不变 */
@media (pointer: coarse) {
  .tc-open, .tc-open-dir { min-height: 44px; }
  .tc-chip, .tc-actions .btn, .tc-refresh { min-height: 44px; }
}
/* 窄屏：入口条整宽横滚，"新建"字样让位给档位本身 */
@media (max-width: 720px) {
  .tc-launch-k { display: none; }
  .tc-open-label { min-width: 7ch; }
}
@media (prefers-reduced-motion: reduce) {
  .tc-launch-slot, .tc-open-dot, .tc-slot, .tc-open-dir { transition: none; }
  .tc-launch-slot:hover { transform: none; }
}
</style>
