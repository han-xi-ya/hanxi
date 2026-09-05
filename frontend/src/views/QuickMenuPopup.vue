<script setup lang="ts">
// 快捷菜单光标弹窗：独立 frameless 顶层窗口的全部内容（main.ts 按 #quickmenu hash 分流挂载）。
// 条目与托盘右键菜单共用一份配置（后端 launcher 分发），每次唤出事件重拉，改配置免重启。
import { ref, shallowRef, computed, onMounted, onBeforeUnmount } from 'vue'
import * as QuickMenuAPI from '../../bindings/hanxi/internal/modules/quickmenu'
import type { MenuItem } from '../../bindings/hanxi/internal/modules/quickmenu/models'
import { useWailsEvent } from '../composables/useWailsEvent'
import { getErrorMessage } from '../utils/errors'

const items = shallowRef<MenuItem[]>([])
const loading = ref(true)
const errorMsg = ref('')
const listEl = ref<HTMLElement | null>(null)

// 类型标记以文字表达（不依赖颜色/emoji 传达语义）
const TYPE_LABEL: Record<string, string> = {
  exe: '程序',
  command: '命令',
  route: '页面',
}
const typeLabel = (t: string) => TYPE_LABEL[t] ?? '条目'

async function refresh() {
  loading.value = true
  errorMsg.value = ''
  try {
    items.value = (await QuickMenuAPI.QuickMenuService.ListItems()) ?? []
  } catch (err) {
    errorMsg.value = getErrorMessage(err)
  } finally {
    loading.value = false
  }
}

async function launch(item: MenuItem) {
  // 后端先收起弹窗再异步派发，失败经统一通知 Hub 反馈，此处只兜绑定层异常。
  try {
    await QuickMenuAPI.QuickMenuService.Launch(item.index)
  } catch (err) {
    errorMsg.value = getErrorMessage(err)
  }
}

function dismiss() {
  QuickMenuAPI.QuickMenuService.Dismiss().catch(() => { /* 窗口已在收起路径上 */ })
}

function openSettings() {
  QuickMenuAPI.QuickMenuService.OpenSettings().catch((err: unknown) => {
    errorMsg.value = getErrorMessage(err)
  })
}

// 菜单式键盘导航：Esc 关闭，↑/↓ 在条目间循环移动焦点，Enter 由原生 button 承担。
function onKeydown(ev: KeyboardEvent) {
  if (ev.key === 'Escape') {
    ev.preventDefault()
    dismiss()
    return
  }
  if (ev.key !== 'ArrowDown' && ev.key !== 'ArrowUp') return
  const rows = listEl.value?.querySelectorAll<HTMLButtonElement>('button.menu-row')
  if (!rows || rows.length === 0) return
  ev.preventDefault()
  const current = Array.prototype.indexOf.call(rows, document.activeElement)
  const step = ev.key === 'ArrowDown' ? 1 : -1
  rows[(current + step + rows.length) % rows.length]?.focus()
}

const ready = computed(() => !loading.value && !errorMsg.value)

// 每次后端唤出弹窗：重拉条目（设置页可能刚改过托盘共用配置，弹窗常驻不重启）。
useWailsEvent('quickmenu:opening', () => {
  refresh()
})

onMounted(() => {
  window.addEventListener('keydown', onKeydown)
  refresh()
})
onBeforeUnmount(() => {
  window.removeEventListener('keydown', onKeydown)
})
</script>

<template>
  <!-- 禁掉 WebView2 默认右键菜单：弹窗本身就是"右键"的产物 -->
  <div class="popup" @contextmenu.prevent>
    <header class="popup-head">
      <span class="popup-title">快捷菜单</span>
      <span v-if="ready && items.length > 0" class="popup-meta">{{ items.length }} 项 · Esc 关闭</span>
    </header>

    <div v-if="loading" class="popup-state">正在加载条目…</div>

    <div v-else-if="errorMsg" class="popup-state state-error">
      <span class="popup-state-msg">加载失败：{{ errorMsg }}</span>
      <button type="button" class="popup-state-action" @click="refresh">重试</button>
    </div>

    <div v-else-if="items.length === 0" class="popup-state">
      <span class="popup-state-msg">还没有条目。快捷菜单与托盘菜单共用配置，先添加要快速启动的程序。</span>
      <button type="button" class="popup-state-action" @click="openSettings">配置条目</button>
    </div>

    <nav v-else ref="listEl" class="menu" aria-label="快捷启动条目">
      <button
        v-for="item in items"
        :key="item.index"
        type="button"
        class="menu-row"
        :title="item.hint || item.label"
        @click="launch(item)"
      >
        <span class="menu-label">{{ item.label }}</span>
        <span class="menu-kind">{{ typeLabel(item.type) }}</span>
      </button>
    </nav>
  </div>
</template>

<style scoped>
/* 弹窗是贴靠在光标处的紧凑菜单面板：单层实底 + 描边即全部层次（frameless 方窗，
   不做圆角/投影——Wails beta.10 Windows 侧无窗口透明能力，圆角会露出实底直角）。 */
.popup {
  display: flex;
  flex-direction: column;
  height: 100vh;
  background: var(--surface-panel);
  border: 1px solid var(--color-border-strong);
  color: var(--color-text);
  font-family: var(--font-text);
  overflow: hidden;
}

.popup-head {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 8px;
  padding: 9px 12px 7px;
  border-bottom: 1px solid var(--color-border);
  flex: none;
}
.popup-title {
  font-size: 12px;
  font-weight: 600;
  letter-spacing: 0.02em;
}
.popup-meta {
  font-size: 10px;
  color: var(--color-text-subtle);
}

/* 列表区：条目多时内部滚动，页面本体永不横滚 */
.menu {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  padding: 4px;
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.menu-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  align-items: center;
  gap: 8px;
  width: 100%;
  min-height: 38px;
  padding: 6px 10px;
  border: none;
  border-radius: var(--radius-control);
  background: transparent;
  color: var(--color-text);
  font: inherit;
  font-size: 13px;
  text-align: left;
  cursor: pointer;
  transition: background var(--motion-fast) ease;
}
.menu-row:hover {
  background: var(--surface-hover);
}
.menu-row:focus-visible {
  outline: 2px solid var(--color-primary);
  outline-offset: -2px;
}
.menu-row:active {
  background: var(--surface-selected);
}
.menu-label {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.menu-kind {
  font-size: 10px;
  color: var(--color-text-subtle);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-pill);
  padding: 1px 7px;
  white-space: nowrap;
}

/* 加载 / 错误 / 空态：同一套语法的行内状态块 */
.popup-state {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 10px;
  padding: 20px 16px;
  text-align: center;
  font-size: 12px;
  color: var(--color-text-muted);
}
.popup-state-msg {
  line-height: 1.6;
}
.state-error .popup-state-msg {
  color: var(--state-danger);
}
.popup-state-action {
  min-height: 30px;
  padding: 4px 14px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-control);
  background: var(--surface-soft);
  color: var(--color-text);
  font: inherit;
  font-size: 12px;
  cursor: pointer;
  transition: background var(--motion-fast) ease, border-color var(--motion-fast) ease;
}
.popup-state-action:hover {
  background: var(--surface-hover);
  border-color: var(--color-border-strong);
}
.popup-state-action:focus-visible {
  outline: 2px solid var(--color-primary);
  outline-offset: 1px;
}
</style>
