<script setup lang="ts">
// 内嵌终端视图壳（v0）：编排单源在 composables/useTerminal.ts（帧协议、通道
// 状态机、名录 RPC、xterm 装配全在那），本视图只做接线 + 页面骨架 + 状态条/
// 搜索条/覆盖层排布。形制照 AiChatView：视图薄、纯 UI 态不上收编排层。
//
// v0 语义如实标注（机主验收第 0 条）：本页是**显示通道**不是进程宿主——
// 切页/关页只断通道，后端进程照跑；重进本页需重新连接，断线期间的输出不补。
// 模块停用弹回 / EnsureModuleActive 门禁由 App.vue 依 navigation.ts 的 moduleId
// 自动接管，视图内零门禁代码。
//
// 覆盖层四态（空态要解释为什么空、下一步是什么——quality-checklist）：
// loading（首拉名录）/ error（首拉失败给重试）/ empty（无会话引导新建）/
// dead（选中会话已退出：画面已随切换清空，Kill 清账或新建同型）。
// xterm 容器**恒在**（不经 v-if 门控）：FitAddon 量的是 parentElement 的
// 已解析高度，v-if 拆掉容器就断了"挂载即可 open"的前提。
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import PageHeader from '../components/ui/PageHeader.vue'
import UiBanner from '../components/ui/UiBanner.vue'
import UiButton from '../components/ui/UiButton.vue'
import TermSessionChips from '../components/terminal/TermSessionChips.vue'
import { useTerminal } from '../composables/useTerminal'
import type { TermProfileId } from '../composables/useTerminal'

const {
  sessions,
  currentId,
  currentRow,
  listLoading,
  listError,
  openError,
  creating,
  refresh,
  selectSession,
  createSession,
  renameSession,
  killSession,
  connStatus,
  connNote,
  canReconnect,
  reconnectNow,
  searchOpen,
  searchText,
  toggleSearch,
  runSearch,
  closeSearch,
  mountTerminal,
  disposeTerminal,
} = useTerminal()

const screenEl = ref<HTMLElement | null>(null)

onMounted(async () => {
  // 容器必须先有 DOM 才能 open xterm；ref 在 onMounted 时已就位（骨架无 v-if 门）
  if (screenEl.value) mountTerminal(screenEl.value)
  await nextTick()
  void refresh()
})

// KeepAlive 驱逐/路由销毁都要还给 xterm：dispose 先断通道再拆引擎（顺序在 composable 内定死）
onBeforeUnmount(disposeTerminal)

function onCreate(profileId: TermProfileId): void {
  void createSession(profileId)
}

const overlay = computed<'loading' | 'error' | 'empty' | 'dead' | null>(() => {
  if (listLoading.value && sessions.value.length === 0) return 'loading'
  if (listError.value && sessions.value.length === 0) return 'error'
  if (!currentRow.value) return 'empty'
  if (!currentRow.value.alive) return 'dead'
  return null
})

const overlayMessage = computed(() => {
  switch (overlay.value) {
    case 'loading': return '正在读取终端名录…'
    case 'empty': return '还没有终端会话。用上方「新建」选一个 Shell——命令行 / PowerShell / Claude Code。'
    case 'dead': return '该会话的进程已退出，画面已随切换清空。Kill 清账，或新建同型会话。'
    default: return ''
  }
})
</script>

<template>
  <div class="page-fluid term-page">
    <PageHeader
      title="内嵌终端"
      subtitle="v0 竖切：一个会话一条显示通道（GoStream terminal:io）。切页/关页只断通道，后端进程照跑；窗口重开需重新连接，断线期间的输出不补。渲染器为 DOM（xterm 6，未上 WebGL）。"
    />

    <UiBanner v-if="openError" tone="error" class="tm-banner">
      <span>新建终端失败（已有会话不受影响）：{{ openError }}</span>
      <button type="button" class="btn btn-small btn-secondary" @click="openError = ''">知道了</button>
    </UiBanner>

    <UiBanner v-if="listError && sessions.length > 0" tone="error" class="tm-banner">
      <span>名录刷新失败（不影响当前终端画面）：{{ listError }}</span>
      <button type="button" class="btn btn-small btn-secondary" @click="refresh">重试</button>
    </UiBanner>

    <div class="tm-desk">
      <TermSessionChips
        :sessions="sessions"
        :current-id="currentId"
        :conn-status="connStatus"
        :creating="creating"
        @select="selectSession"
        @create="onCreate"
        @rename="renameSession"
        @kill="killSession"
        @refresh="refresh"
      />

      <section class="tm-pane">
        <div class="tm-status">
          <span class="tm-note" role="status" aria-live="polite">{{ connNote }}</span>
          <div class="tm-status-tools">
            <UiButton v-if="canReconnect" small variant="secondary" class="tm-reconnect" title="对存活进程重开显示通道（重发 open 帧握手）" @click="reconnectNow">立即重连</UiButton>
            <UiButton
              small
              variant="ghost"
              class="tm-search-toggle"
              :aria-expanded="searchOpen"
              title="搜索已渲染输出（Ctrl+F）"
              @click="toggleSearch"
            >搜索</UiButton>
          </div>
        </div>

        <div v-if="searchOpen" class="tm-search" role="group" aria-label="搜索终端输出">
          <input
            v-model="searchText"
            class="tm-search-input"
            type="text"
            placeholder="搜索输出内容…"
            aria-label="搜索关键词"
            @keydown.enter.exact.prevent="runSearch(true)"
            @keydown.shift.enter.prevent="runSearch(false)"
            @keydown.esc="closeSearch"
          >
          <button type="button" class="btn btn-secondary btn-small" title="上一个匹配（Shift+Enter）" @click="runSearch(false)">上一个</button>
          <button type="button" class="btn btn-secondary btn-small" title="下一个匹配（Enter）" @click="runSearch(true)">下一个</button>
          <button type="button" class="btn btn-ghost btn-small" title="关闭搜索（Esc）" @click="closeSearch">关闭</button>
        </div>

        <div class="tm-screen-wrap">
          <div ref="screenEl" class="tm-screen" aria-label="终端画面"></div>
          <div v-if="overlay" class="tm-overlay" :class="{ 'tm-overlay-error': overlay === 'error' }">
            <template v-if="overlay === 'error'">
              <span>终端名录加载失败：{{ listError }}</span>
              <button type="button" class="btn btn-small btn-secondary" @click="refresh">重试</button>
            </template>
            <template v-else>{{ overlayMessage }}</template>
          </div>
        </div>
      </section>
    </div>
  </div>
</template>

<style scoped>
/* 工作台单列高 pane：状态条/搜索条/屏幕同栏堆叠，页体不产生纵向滚动
   （checklist：终端滚在自己的容器里）；窄屏 chips 条自身横滚（组件内已管） */
.tm-desk {
  display: flex;
  flex-direction: column;
  gap: 10px;
  min-width: 0;
}

.tm-banner {
  display: flex;
  align-items: center;
  gap: 10px;
}

.tm-pane {
  display: flex;
  flex-direction: column;
  min-height: 0;
  min-width: 0;
  height: clamp(420px, calc(100vh - 208px), 900px);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-panel);
  background: var(--surface-panel);
  overflow: hidden;
}

.tm-status {
  flex: none;
  display: flex;
  align-items: center;
  gap: 10px;
  min-height: 36px;
  padding: 0 12px;
  border-bottom: 1px solid var(--color-border);
  background: var(--surface-soft);
}
.tm-note {
  flex: 1;
  min-width: 0;
  font-size: var(--text-xs);
  color: var(--color-text-muted);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.tm-status-tools {
  display: flex;
  align-items: center;
  gap: 6px;
  flex: none;
}

.tm-search {
  flex: none;
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 6px 12px;
  border-bottom: 1px solid var(--color-border);
  background: var(--surface-soft);
}
.tm-search-input {
  flex: 1;
  min-width: 0;
  font-family: var(--font-mono);
  font-size: var(--text-sm);
  padding: 4px 8px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-control);
  background: var(--surface-panel);
  color: var(--color-text);
}

.tm-screen-wrap {
  position: relative;
  flex: 1;
  min-height: 0;
}
.tm-screen {
  position: absolute;
  inset: 0;
  padding: 10px;
}
/* xterm 根节点撑满容器（FitAddon 量的是 .tm-screen 的已解析高度） */
.tm-screen :deep(.xterm) { height: 100%; }

.tm-overlay {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 10px;
  padding: 20px;
  text-align: center;
  font-size: var(--text-sm);
  color: var(--color-text-muted);
  background: var(--surface-panel);
}
.tm-overlay-error { color: var(--state-danger); }

@media (pointer: coarse) {
  .tm-search .btn, .tm-status-tools .btn, .tm-overlay .btn { min-height: 44px; }
}
</style>
