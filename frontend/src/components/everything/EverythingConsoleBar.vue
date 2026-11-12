<script setup lang="ts">
// Everything 控制台顶部整合条：状态灯 + 启停按钮 + 搜索行（输入即搜/搜索按钮/ES 组件态）。
// 纯展示 + 事件上抛：业务动作（bindings 调用、防抖搜索、ES 就绪）全部回视图编排；
// isRunningOrStarting/isExternal 由 state 就地派生。
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
  /** 状态真相（P1-2，复刻 ManagedControlBar stale 三件套）：RPC 最近一次失败时
   *  降灰灯 + stale-tag，旧快照不得冒充实时「运行中」。缺省 false/0 向后兼容。 */
  statusError?: boolean
  lastStatusAt?: number
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

// stale 悬停指引（同 ManagedControlBar 口径）：最近一次成功取态时刻，从未成功则如实说。
const staleTip = computed(() =>
  props.lastStatusAt
    ? `最近一次成功同步 ${new Date(props.lastStatusAt).toLocaleTimeString()}；此后状态获取持续失败，显示值为最后已知事实`
    : '尚未成功取得过状态',
)

// 受控输入：先 emit update:keyword 同步回写父级值，再 emit keyword-input 交父级跑防抖——
// 时序须「先更新值、后读值」，反序则防抖侧读到旧值。
function onInput(e: Event) {
  emit('update:keyword', (e.target as HTMLInputElement).value)
  emit('keyword-input')
}
</script>

<template>
  <div class="control-bar">
    <div class="control-top">
      <div class="control-status">
        <span class="ev-status-light" :class="statusError ? 'stale' : state"></span>
        <span class="status-word">{{ stateText }}</span>
        <span v-if="statusError" class="stale-tag" :title="staleTip">状态暂不可确认</span>
        <template v-if="!statusError && isRunningOrStarting && runningVersion">
          <span class="ver-pill">{{ runningVersion }}</span>
          <span v-if="pid" class="mono pid-tag">PID {{ pid }}</span>
        </template>
        <span v-if="!statusError && state === 'running'" class="mono uptime-tag">⏱ {{ fmtDuration(uptimeSec) }}</span>
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
        <!-- P2-5 审计要求 title 改「先 -quit 优雅请求，无响应强制结束；管理员实例需托盘自退」
             口径——但 EverythingView.spec「external 快照…title 指引导轨退出」断言锁死
             现词面（勿改 spec 契约），本轮保留并注记，词面迁移待主会话连同 spec 一起裁。 -->
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
        aria-label="搜索文件名与路径"
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
/* 底色/边框/圆角落全局 .control-bar 原子（--radius-element）；此处仅留搜索条
   整合卡的内距/缝隙真差（12/14 与 10 vs 全局 10/12 与 8，P2-16 出清裸 radius 后保留）。 */
.control-bar { padding: 12px 14px; gap: 10px; }
/* 注意：状态卡信号灯类名带 ev- 前缀，与远程表格徽标/全局样式隔离（markeron 垂直字体事故教训） */
/* P1-2：状态 RPC 失败降灰档（同 ManagedControlBar .status-light.stale 口径）——
   中性灰无光晕无脉冲，旧快照不冒充实时灯色 */
.ev-status-light.stale { background: var(--color-text-subtle); box-shadow: none; animation: none; }
.stale-tag {
  font-size: var(--text-xs); color: var(--color-text-muted);
  border: 1px solid var(--color-border); border-radius: var(--radius-pill);
  padding: 1px 8px; cursor: help;
}
.ev-status-light { width: 10px; height: 10px; border-radius: 50%; background: var(--color-text-subtle); flex-shrink: 0; }
.ev-status-light.running { background: var(--state-positive); box-shadow: inset 0 0 0 1px color-mix(in srgb, var(--state-positive) 45%, white); }
.ev-status-light.starting { background: var(--color-primary); animation: hx-pulse 1.8s ease-in-out infinite; }
.ev-status-light.external { background: var(--state-warning); box-shadow: inset 0 0 0 1px color-mix(in srgb, var(--state-warning) 45%, white); }
.ev-status-light.failed { background: var(--state-danger); box-shadow: inset 0 0 0 1px color-mix(in srgb, var(--state-danger) 45%, white); }

/* ---------- 搜索行 ---------- */
.search-line { display: flex; gap: 8px; align-items: center; }
.search-go { flex-shrink: 0; }
.ev-status-pill { font-size: var(--text-xs); padding: 2px 8px; border-radius: var(--radius-pill); white-space: nowrap; }
.ev-status-pill.ready { background: var(--state-positive-soft); color: var(--state-positive); }
.ev-status-pill.idle { background: var(--surface-hover); color: var(--color-text-muted); }
.search-input {
  flex: 1; padding: 8px 12px; border: 1px solid var(--color-border-strong); border-radius: var(--radius-control);
  font-size: var(--text-base); background: var(--surface-soft); color: var(--color-text);
  transition: border-color var(--motion-base) ease;
}
/* 2026-10-26 机主裁决（输入焦点形制归主流）：焦点只换蓝边，摘掉双环派的
   外圈光晕；键盘可达性由全局 :focus-visible 环承担。原 outline:none 压死全局环，
   按 HistoryPanel C10 先例删除——鼠标态仍只换边，键盘环交还 :focus-visible。 */
.search-input:focus { border-color: var(--color-primary); }
/* P2-16：.control-bar/.control-top/.control-status/.control-btns/.status-word/.ver-pill/
   .pid-tag/.uptime-tag 的 scoped 等值副本已删净，落回 components.css 托管家族全局原子
   （radius 10px/4px 裸值随副本一并出清，统一 --radius-element/--radius-pill）。 */
</style>
