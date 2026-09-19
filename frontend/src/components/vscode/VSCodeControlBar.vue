<script setup lang="ts">
import type { Snapshot } from '../../../bindings/hanxi/internal/modules/vscode/instance/models'
import type { Status } from '../../../bindings/hanxi/internal/modules/vscode/models'
import type { VSCodeForm } from '../../adapters/vscode'
import { fmtDuration } from '../../utils/format'
import { toolStateMeta } from '../../constants/status'
import UiBanner from '../ui/UiBanner.vue'

const props = defineProps<{
  form: VSCodeForm
  snap: Snapshot | null
  installedApp: Status['installed'] | null
  uptime: number
  busy: boolean
  canOpenDir: boolean
}>()

const emit = defineEmits<{
  open: [form: VSCodeForm]
  quit: [form: VSCodeForm]
  'open-dir': []
  'select-versions': []
}>()

function banner(): { tone: 'warn' | 'error' | 'ok'; text: string } | null {
  const snap = props.snap
  if (!snap) return null
  if (snap.state === 'external') {
    return props.form === 'installer'
      ? { tone: 'warn', text: '检测到运行中的安装版 VS Code（与您日常使用同实例组，互斥体探测）。可唤起窗口；如需退出请在 VS Code 窗口内操作。' }
      : { tone: 'warn', text: '检测到外部启动的便携版实例（托管目录内、非 Hanxi 拉起）。可唤起其窗口；如需退出请在该实例内操作。' }
  }
  if (snap.state === 'failed') return { tone: 'error', text: snap.error || 'VS Code 异常退出' }
  if (snap.state === 'running' && props.form === 'installer') {
    return { tone: 'ok', text: '安装版正在运行：与您日常使用的 VS Code 共用配置（%APPDATA%\\Code）与实例组；其应用内自动更新可能领先托管记录，属已知行为。' }
  }
  if (snap.state === 'running' && props.form === 'portable') {
    return { tone: 'ok', text: '便携版正在运行：配置与扩展全部自包含于版本目录 data\\ 内，与日常 VS Code 完全隔离。' }
  }
  return null
}
</script>

<template>
  <div class="control-bar vscode-control-bar">
    <div class="control-top">
      <div class="control-status">
        <span class="form-tag" :class="form">{{ form === 'portable' ? '便携版' : '安装版' }}</span>
        <span class="vc-status-light" :class="snap?.state || ''"></span>
        <span class="status-word">{{ toolStateMeta(snap?.state || '').text }}</span>
        <template v-if="snap?.state === 'running' && snap.version">
          <span class="ver-pill">{{ snap.version }}</span>
          <span v-if="snap.pid" class="mono pid-tag">PID {{ snap.pid }}</span>
        </template>
        <span v-if="snap?.state === 'running'" class="mono uptime-tag">⏱ {{ fmtDuration(uptime) }}</span>
      </div>
      <div class="control-btns">
        <button
          class="btn btn-secondary btn-small"
          :disabled="busy || snap?.state === 'starting'"
          :title="form === 'portable' ? '启动便携版并打开窗口（数据自包含于 data\\）' : '启动本机安装版 VS Code'"
          @click="emit('open', form)"
        >🗔 打开窗口</button>
        <button
          class="btn btn-danger-outline btn-small"
          :disabled="busy || !['running', 'starting', 'external'].includes(snap?.state || '')"
          :title="snap?.state === 'external' ? '外部实例不越权终止' : '关闭窗口消息，宽限后 JobObject 兜底'"
          @click="emit('quit', form)"
        >⏻ 退出</button>
        <button v-if="form === 'portable' && canOpenDir" class="btn btn-ghost btn-small" @click="emit('open-dir')">📂 位置</button>
      </div>
    </div>

    <UiBanner v-if="banner()" :tone="banner()!.tone" class="slim">{{ banner()!.text }}</UiBanner>
    <div v-else-if="form === 'portable' && (snap?.state ?? '') === 'stopped'" class="hint-line">
      便携版尚未运行：点击「打开窗口」启动（data\ 全自包含，与日常 VS Code 隔离）；未下载过请先到<button class="inline-action" @click="emit('select-versions')">「版本管理」</button>下载便携版或导入本地目录。
    </div>
    <div v-else-if="form === 'installer' && installedApp?.installed && (snap?.state ?? '') === 'stopped'" class="hint-line">
      本机安装版 VS Code {{ installedApp.version }}：可直接托管启停。注意与日常使用共实例组（唤窗互达），且应用内自动更新可能令此处版本记录漂移。
    </div>
    <div v-else-if="form === 'installer' && !installedApp?.installed" class="hint-line">
      本机暂无安装版：可在<button class="inline-action" @click="emit('select-versions')">「版本管理 → 安装版」</button>通道静默安装（用户级、免 UAC）；日常只写代码建议直接用便携版托管。
    </div>
  </div>
</template>

<style scoped>
.form-tag {
  flex-shrink: 0;
  padding: 2px 8px;
  border-radius: var(--radius-pill);
  background: var(--surface-hover);
  color: var(--color-text-muted);
  font-size: var(--text-xs);
  font-weight: 700;
}
.form-tag.installer { background: var(--state-information-soft); color: var(--state-information); }
.vc-status-light { width: 10px; height: 10px; border-radius: 50%; background: var(--color-text-subtle); flex-shrink: 0; }
.vc-status-light.running { background: var(--state-positive); box-shadow: 0 0 0 3px var(--state-positive-glow); }
.vc-status-light.starting { background: var(--color-primary); animation: hx-pulse 1s infinite; }
.vc-status-light.external { background: var(--state-warning); box-shadow: 0 0 0 3px var(--state-warning-glow); }
.vc-status-light.failed { background: var(--state-danger); box-shadow: 0 0 0 3px var(--state-danger-glow); }
.inline-action {
  display: inline;
  padding: 0;
  border: 0;
  background: transparent;
  color: var(--color-primary);
  font: inherit;
  cursor: pointer;
}
.inline-action:hover { text-decoration: underline; }
</style>
