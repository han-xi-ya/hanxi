<script setup lang="ts">
// Everything 控制台顶部整合条：状态灯 + 启停按钮 + 搜索行（输入即搜/搜索按钮/ES 组件态），
// 自 EverythingView 随 DOM 逐字迁出。纯展示 + 事件上抛：业务动作（bindings 调用、防抖搜索、
// ES 就绪）全部回视图编排；isRunningOrStarting/isExternal 由 state 就地派生（原视图同款定义）。
import { computed } from 'vue'
import type { DownloadTicket } from '../../../bindings/hanxi/internal/modules/everything/models'
import { fmtDuration } from '../../utils/format'

const props = defineProps<{
  state: string
  stateText: string
  busy: boolean
  runningVersion: string
  pid?: number
  uptimeSec: number
  searching: boolean
  esReady: boolean
  esBusy: boolean
  esProgress: DownloadTicket | null
  keyword: string
}>()

const emit = defineEmits<{
  'update:keyword': [value: string]
  'keyword-input': []
  'keyword-enter': []
  'composition-start': []
  'composition-end': []
  'search': []
  'install-es': []
  'start-background': []
  'open-window': []
  'quit': []
}>()

const isRunningOrStarting = computed(() => props.state === 'running' || props.state === 'starting')
const isExternal = computed(() => props.state === 'external')

// 受控输入：先回写父级 keyword（同步），再通知父级跑防抖逻辑——
// 保持与原视图 v-model + @input 同帧「先更新值、后读值」的时序逐字一致。
function onInput(e: Event) {
  emit('update:keyword', (e.target as HTMLInputElement).value)
  emit('keyword-input')
}
</script>

<template>
  <div class="control-bar">
    <div class="control-top">
      <div class="control-status">
        <span class="ev-status-light" :class="state"></span>
        <span class="status-word">{{ stateText }}</span>
        <template v-if="isRunningOrStarting && runningVersion">
          <span class="ver-pill">{{ runningVersion }}</span>
          <span v-if="pid" class="mono pid-tag">PID {{ pid }}</span>
        </template>
        <span v-if="state === 'running'" class="mono uptime-tag">⏱ {{ fmtDuration(uptimeSec) }}</span>
      </div>
      <div class="control-btns">
        <button
          class="btn btn-primary btn-small"
          :disabled="busy || state === 'running' || state === 'starting' || isExternal"
          :title="state === 'running' ? '已在运行' : isExternal ? '外部实例已在运行' : '后台驻留建索引（不弹窗）'"
          @click="emit('start-background')"
        >▶ 启动后台</button>
        <button
          class="btn btn-secondary btn-small"
          :disabled="busy || state === 'starting'"
          :title="state === 'running' ? '唤起搜索窗口' : state === 'starting' ? '启动中…' : '直接启动并打开搜索窗口'"
          @click="emit('open-window')"
        >🗔 打开窗口</button>
        <button
          class="btn btn-danger-outline btn-small"
          :disabled="busy || (state !== 'running' && state !== 'starting' && !isExternal)"
          :title="isExternal ? '外部实例请在 Everything 托盘退出' : '优雅落盘索引库后退出'"
          @click="emit('quit')"
        >⏻ 退出</button>
      </div>
    </div>

    <div class="search-line">
      <input
        class="search-input"
        type="text"
        :value="keyword"
        placeholder="输入即搜 —— 支持 Everything 语法（如 *.go | D:\项目）"
        @input="onInput"
        @keyup.enter="emit('keyword-enter')"
        @compositionstart="emit('composition-start')"
        @compositionend="emit('composition-end')"
      />
      <button class="btn btn-primary btn-small search-go" :disabled="searching || busy" @click="emit('search')">
        {{ searching ? '搜索中…' : '⇥ 搜索' }}
      </button>
      <span class="ev-status-pill" :class="esReady ? 'ready' : 'idle'">
        {{ esProgress ? '组件安装中…' : esReady ? 'ES 就绪' : 'ES 未装' }}
      </span>
      <button
        v-if="!esReady && !esProgress"
        class="link-button"
        :disabled="esBusy"
        @click="emit('install-es')"
      >安装</button>
    </div>
  </div>
</template>

<style scoped>
/* ---------- 顶部整合控制条（状态 + 按钮 + 搜索框，紧凑单卡） ---------- */
.control-bar {
  background: var(--surface-panel); border: 1px solid var(--color-border); border-radius: 10px;
  padding: 12px 14px; display: flex; flex-direction: column; gap: 10px;
}
.control-top { display: flex; justify-content: space-between; align-items: center; gap: 10px; flex-wrap: wrap; }
.control-status { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
/* 注意：状态卡信号灯类名带 ev- 前缀，与远程表格徽标/全局样式隔离（markeron 垂直字体事故教训） */
.ev-status-light { width: 10px; height: 10px; border-radius: 50%; background: var(--color-text-subtle); flex-shrink: 0; }
.ev-status-light.running { background: var(--state-positive); box-shadow: 0 0 0 3px var(--state-positive-glow); }
.ev-status-light.starting { background: var(--color-primary); animation: hx-pulse 1s infinite; }
.ev-status-light.external { background: var(--state-warning); box-shadow: 0 0 0 3px var(--state-warning-glow); }
.ev-status-light.failed { background: var(--state-danger); box-shadow: 0 0 0 3px var(--state-danger-glow); }
.status-word { font-size: 15px; font-weight: 700; color: var(--color-text); }
.ver-pill { font-family: var(--font-mono); font-size: 12px; background: var(--surface-hover); border: 1px solid var(--color-border); border-radius: 4px; padding: 1px 8px; color: var(--color-text); }
.pid-tag { font-size: 11px; color: var(--color-text-subtle); }
.uptime-tag { font-size: 11px; color: var(--color-text-subtle); }
.control-btns { display: flex; gap: 8px; flex-wrap: wrap; }

/* ---------- 搜索行 ---------- */
.search-line { display: flex; gap: 8px; align-items: center; }
.search-go { flex-shrink: 0; }
.ev-status-pill { font-size: 11px; padding: 2px 8px; border-radius: var(--radius-pill); white-space: nowrap; }
.ev-status-pill.ready { background: var(--state-positive-soft); color: var(--state-positive); }
.ev-status-pill.idle { background: var(--surface-hover); color: var(--color-text-muted); }
.search-input {
  flex: 1; padding: 8px 12px; border: 1px solid var(--color-border-strong); border-radius: var(--radius-control);
  font-size: 13px; background: var(--surface-soft); color: var(--color-text); outline: none;
  transition: border-color var(--motion-base) ease, box-shadow var(--motion-base) ease;
}
.search-input:focus { border-color: var(--color-primary); box-shadow: 0 0 0 2px var(--color-primary-glow); }
</style>
