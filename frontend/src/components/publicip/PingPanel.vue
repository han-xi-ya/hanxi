<script setup lang="ts">
import type { PingSummary } from '../../../bindings/hanxi/internal/modules/publicip/models'
import { DIAG_QUICK_TARGETS, rttTier, useDiagFormModel } from './useDiagFormModel'

// Ping 连通性测试面板：目标/次数表单 + 常用目标 + 结果汇总表与明细表（纯展示）。
// 目标与次数经 v-model 上抛由视图持有——网卡芯片的「⚡ 快捷 Ping」需跨 Tab 回填并立即发起。
// 根节点为多根 fragment（表单条/错误框/结果卡），渲染后仍是 .network-page 的直接子节点，DOM 与拆分前一致。
// 表单骨架（v-model 样板/快捷目标动作/禁用式）与 Traceroute 面板共吃 useDiagFormModel 单源。
const props = defineProps<{
  target: string
  count: number
  loading: boolean
  result: PingSummary | null
  error: string
}>()

const emit = defineEmits<{
  'update:target': [value: string]
  'update:count': [value: number]
  run: []
}>()

const { targetModel, countModel, runDisabled, quickTarget } = useDiagFormModel({
  target: () => props.target,
  count: () => props.count,
  loading: () => props.loading,
  updateTarget: (value) => emit('update:target', value),
  updateCount: (value) => emit('update:count', value),
  run: () => emit('run'),
})

// 本面板快捷目标列表：成员取选自 DIAG_QUICK_TARGETS 词面单源（含腾讯 DNS，Ping 独有）
const quickTargets: readonly { label: string; value: string }[] = [
  DIAG_QUICK_TARGETS.aliDns,
  DIAG_QUICK_TARGETS.tencentDns,
  DIAG_QUICK_TARGETS.cloudflare,
  DIAG_QUICK_TARGETS.google,
]
</script>

<template>
  <div class="tool-panel">
    <div class="tool-form">
      <div class="input-wrap">
        <span class="input-label">目标 IP / 域名:</span>
        <input
          v-model="targetModel"
          class="text-input"
          placeholder="如 1.1.1.1、8.8.8.8 或 www.baidu.com"
          @keyup.enter="emit('run')"
        />
      </div>
      <div class="input-wrap count-wrap">
        <span class="input-label">次数:</span>
        <select v-model="countModel" class="select-input">
          <option :value="4">4 次</option>
          <option :value="8">8 次</option>
          <option :value="16">16 次</option>
        </select>
      </div>
      <button class="btn btn-primary" :disabled="runDisabled" @click="emit('run')">
        {{ loading ? 'Ping 探测中…' : '发起 Ping' }}
      </button>
    </div>

    <!-- 快捷常用目标（词面单源 DIAG_QUICK_TARGETS，本面板取全部四家） -->
    <div class="quick-targets">
      <span class="quick-label">常用目标:</span>
      <button v-for="t in quickTargets" :key="t.value" class="btn-quick" @click="quickTarget(t.value)">{{ t.label }} ({{ t.value }})</button>
    </div>
  </div>

  <div v-if="error" class="error-box">{{ error }}</div>

  <!-- Ping 结果面板 -->
  <div v-if="result" class="diag-card">
    <div class="diag-summary-bar">
      <div class="summary-col">
        <span class="s-label">目标主机</span>
        <span class="s-val">{{ result.target }} <code v-if="result.ip !== result.target">({{ result.ip }})</code></span>
      </div>
      <div class="summary-col">
        <span class="s-label">发包 / 收包</span>
        <span class="s-val">{{ result.sent }} / {{ result.received }}</span>
      </div>
      <div class="summary-col">
        <span class="s-label">丢包率</span>
        <span class="s-val" :class="{ 'val-warn': result.lossRate > 0 }">{{ result.lossRate.toFixed(1) }}%</span>
      </div>
      <div class="summary-col" v-if="result.received > 0">
        <span class="s-label">延迟 (最小/平均/最大)</span>
        <span class="s-val text-success">{{ result.minRtt.toFixed(1) }} / {{ result.avgRtt.toFixed(1) }} / {{ result.maxRtt.toFixed(1) }} ms</span>
      </div>
    </div>

    <div class="table-container">
      <table class="tbl">
        <thead>
          <tr>
            <th style="width: 80px;">序号</th>
            <th>目标 IP</th>
            <th style="width: 120px;">往返耗时 (RTT)</th>
            <th style="width: 100px;">TTL</th>
            <th>结果状态</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="r in result.results" :key="r.seq">
            <td>#{{ r.seq }}</td>
            <td><code>{{ r.ip }}</code></td>
            <td>
              <span v-if="r.success" class="rtt-tag" :class="rttTier(r.rttMs)">
                {{ r.rttMs.toFixed(1) }} ms
              </span>
              <span v-else class="text-danger">—</span>
            </td>
            <td>{{ r.success ? r.ttl : '—' }}</td>
            <td>
              <span v-if="r.success" class="status-badge ok">成功回复</span>
              <span v-else class="status-badge fail">{{ r.errorMsg || '请求超时' }}</span>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>

<style scoped src="./diagRtt.css"></style>

<style scoped>
/* Phase 6 后续治理（§9.6-1）：诊断工具皮家族（.tool-panel/.diag-card/.table-container/
   .status-badge 等）已上收 components.css 共享原子，本层只留本面板真差异。
   .rtt-tag 基形与 .text-danger 已按 §9.6-10 裁决全局定档，等值副本删净落回；
   fast/medium/slow 色档副本已归家族共享件 diagRtt.css（波 2C）。 */
.val-warn {
  color: var(--state-danger);
}

/* 状态徽章 fail 态承载动态错误文本（可能较长），本面板独有 */
.status-badge.fail {
  background: var(--state-danger-soft);
  color: var(--state-danger);
}
</style>
