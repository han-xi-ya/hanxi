<script setup lang="ts">
// 快捷菜单模块页：全局右键长按唤出能力的状态与条目预览（只读）。
// 条目编辑刻意不在此重复造面——与托盘右键菜单共用 settings.TrayMenu，统一在设置页管理。
import { ref, shallowRef, onMounted } from 'vue'
import * as QuickMenuAPI from '../../bindings/hanxi/internal/modules/quickmenu'
import type { MenuItem, Status } from '../../bindings/hanxi/internal/modules/quickmenu/models'
import { getErrorMessage } from '../utils/errors'

const emit = defineEmits<{
  (e: 'navigate', route: string): void
}>()

const status = shallowRef<Status | null>(null)
const items = shallowRef<MenuItem[]>([])
const loading = ref(true)
const errorMsg = ref('')

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
    const [st, list] = await Promise.all([
      QuickMenuAPI.QuickMenuService.GetStatus(),
      QuickMenuAPI.QuickMenuService.ListItems(),
    ])
    status.value = st
    items.value = list ?? []
  } catch (err) {
    errorMsg.value = getErrorMessage(err)
  } finally {
    loading.value = false
  }
}

onMounted(refresh)
</script>

<template>
  <div class="page">
    <div class="header-row">
      <h1>快捷菜单</h1>
      <span
        v-if="status"
        class="chip"
        :class="status.trapActive ? 'chip-positive' : 'chip-warning'"
      >{{ status.trapActive ? '监听在位' : '钩子未启用' }}</span>
    </div>
    <p class="subtitle">
      在任意界面按住鼠标右键约 <b class="mono">{{ status ? status.holdMs : 450 }}ms</b>
      即刻在光标处弹出快捷启动菜单（无需松手）；按住中移动超过
      <b class="mono">{{ status ? status.moveTol : 16 }}px</b> 视为拖拽、自动让位给应用原生右键。
      普通右键（提前松开）完全不受影响，任务栏与托盘区亦自动让位。
    </p>

    <div v-if="loading" class="state-box">正在读取快捷菜单状态…</div>
    <div v-else-if="errorMsg" class="state-box state-error">
      加载失败：{{ errorMsg }}
      <button type="button" class="btn btn-small btn-secondary" @click="refresh">重试</button>
    </div>

    <template v-else>
      <section class="panel">
        <h2 class="sec-title">当前条目</h2>
        <p class="sec-note">
          与托盘右键菜单共用同一份配置（显示设置页勾选的条目），共
          <b class="mono">{{ items.length }}</b> 项。
        </p>

        <div v-if="items.length === 0" class="empty-state">
          <p>尚未配置任何条目。先到设置页为托盘菜单添加要快速启动的程序、托管命令或页面。</p>
        </div>
        <ul v-else class="item-list">
          <li v-for="item in items" :key="item.index" class="item-row">
            <span class="item-main">
              <span class="item-label">{{ item.label }}</span>
              <span v-if="item.hint" class="item-hint mono">{{ item.hint }}</span>
            </span>
            <span class="item-kind">{{ typeLabel(item.type) }}</span>
          </li>
        </ul>

        <div class="panel-foot">
          <button type="button" class="btn btn-primary btn-small" @click="emit('navigate', '/settings')">
            前往设置页配置
          </button>
        </div>
      </section>

      <section class="panel usage">
        <h2 class="sec-title">使用说明</h2>
        <ul class="usage-list">
          <li>长按触发后菜单贴靠在光标处，屏幕边缘与多显示器下会自动钳位，不会被裁掉。</li>
          <li>点击条目即刻启动；<kbd>Esc</kbd> 或点击菜单外部（失焦）收起，鼠标离开即停不影响后续操作。</li>
          <li>条目"命令"类会先懒初始化对应托管模块；"页面"类会唤出主窗口并导航。</li>
          <li>不需要此能力时，在设置页模块管理中将"快捷菜单"停用即可（全局钩子随停用即时摘除）。</li>
        </ul>
      </section>
    </template>
  </div>
</template>

<style scoped>
.sec-title {
  font-size: 14px;
  font-weight: 600;
  margin: 0 0 6px;
  color: var(--color-text);
}
.sec-note {
  font-size: 12px;
  color: var(--color-text-muted);
  margin: 0 0 10px;
  line-height: 1.6;
}

/* 条目预览：与弹窗同构的"名称 + 类型标记"行语法，hint 用 mono 呈现机器值 */
.item-list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.item-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  align-items: center;
  gap: 10px;
  padding: 8px 10px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-control);
  background: var(--surface-soft);
}
.item-main {
  display: flex;
  flex-direction: column;
  min-width: 0;
  gap: 2px;
}
.item-label {
  font-size: 13px;
  font-weight: 500;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.item-hint {
  font-size: 11px;
  color: var(--color-text-subtle);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.item-kind {
  font-size: 10px;
  color: var(--color-text-muted);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-pill);
  padding: 1px 7px;
  white-space: nowrap;
}

.panel-foot {
  display: flex;
  justify-content: flex-end;
  margin-top: 12px;
}

.usage {
  margin-top: 16px;
}
.usage-list {
  margin: 0;
  padding-left: 18px;
  display: flex;
  flex-direction: column;
  gap: 6px;
  font-size: 12px;
  color: var(--color-text-muted);
  line-height: 1.65;
}
kbd {
  font-family: var(--font-mono);
  font-size: 11px;
  border: 1px solid var(--color-border-strong);
  border-bottom-width: 2px;
  border-radius: 4px;
  padding: 0 5px;
  background: var(--surface-soft);
  color: var(--color-text);
}
</style>
