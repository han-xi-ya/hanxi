<script setup lang="ts">
import type { TracerouteSummary } from '../../../bindings/hanxi/internal/modules/publicip/models'
import { DIAG_QUICK_TARGETS, rttTier, useDiagFormModel } from './useDiagFormModel'

// 路由追踪面板：目标/最大跳数表单 + 常用目标 + 跳跃节点汇总表与明细表（纯展示）。
// 目标与跳数经 v-model 上抛由视图持有；根节点为多根 fragment（表单条/错误框/结果卡），
// 渲染后仍是 .network-page 的直接子节点，DOM 与拆分前一致。
// 表单骨架（v-model 样板/快捷目标动作/禁用式）与 Ping 面板共吃 useDiagFormModel 单源。
const props = defineProps<{
  target: string
  maxHops: number
  loading: boolean
  result: TracerouteSummary | null
  error: string
}>()

const emit = defineEmits<{
  'update:target': [value: string]
  'update:maxHops': [value: number]
  run: []
}>()

// count 位在本文档语境即「最大跳数」，档位选项与词面留本面板
const { targetModel, countModel: maxHopsModel, runDisabled, quickTarget } = useDiagFormModel({
  target: () => props.target,
  count: () => props.maxHops,
  loading: () => props.loading,
  updateTarget: (value) => emit('update:target', value),
  updateCount: (value) => emit('update:maxHops', value),
  run: () => emit('run'),
})

// 本面板快捷目标列表：成员取选自 DIAG_QUICK_TARGETS 词面单源（不取腾讯 DNS——Ping 独有档）
const quickTargets: readonly { label: string; value: string }[] = [
  DIAG_QUICK_TARGETS.aliDns,
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
          placeholder="如 1.1.1.1、8.8.8.8 或 www.taobao.com"
          @keyup.enter="emit('run')"
        />
      </div>
      <div class="input-wrap count-wrap">
        <span class="input-label">最大跳数:</span>
        <select v-model="maxHopsModel" class="select-input">
          <option :value="15">15 跳</option>
          <option :value="20">20 跳</option>
          <option :value="30">30 跳</option>
        </select>
      </div>
      <button class="btn btn-primary" :disabled="runDisabled" @click="emit('run')">
        {{ loading ? '正在追踪路由节点…' : '开始追踪' }}
      </button>
    </div>

    <!-- 快捷常用目标（词面单源 DIAG_QUICK_TARGETS；Ping 独家的腾讯 DNS 不入本表） -->
    <div class="quick-targets">
      <span class="quick-label">常用目标:</span>
      <button v-for="t in quickTargets" :key="t.value" class="btn-quick" @click="quickTarget(t.value)">{{ t.label }} ({{ t.value }})</button>
    </div>
  </div>

  <div v-if="error" class="error-box">{{ error }}</div>

  <!-- 路由追踪结果 -->
  <div v-if="result" class="diag-card">
    <div class="diag-summary-bar">
      <div class="summary-col">
        <span class="s-label">追踪目标</span>
        <span class="s-val">{{ result.target }} <code>({{ result.ip }})</code></span>
      </div>
      <div class="summary-col">
        <span class="s-label">总跳数</span>
        <span class="s-val">{{ result.hops ? result.hops.length : 0 }} 跳</span>
      </div>
      <div class="summary-col">
        <span class="s-label">追踪状态</span>
        <span class="s-val" :class="result.complete ? 'text-success' : 'text-warn'">
          {{ result.complete ? '已到达目标主机' : '追踪结束 (未完全响应)' }}
        </span>
      </div>
    </div>

    <div class="table-container">
      <table class="tbl">
        <thead>
          <tr>
            <th style="width: 80px;">跳数</th>
            <th>跳跃节点 IP</th>
            <th style="width: 140px;">往返延迟 (RTT)</th>
            <th>节点状态</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="h in result.hops" :key="h.hop">
            <td><strong>#{{ h.hop }}</strong></td>
            <td>
              <code v-if="h.ip !== '*'" class="node-ip">{{ h.ip }}</code>
              <span v-else class="text-subtle">* * * (节点不响应 ICMP)</span>
            </td>
            <td>
              <span v-if="h.success" class="rtt-tag" :class="rttTier(h.rttMs)">
                {{ h.rttMs.toFixed(1) }} ms
              </span>
              <span v-else class="text-subtle">—</span>
            </td>
            <td>
              <!-- emoji 退役（AppIcon 纪律，同 NotificationToast 先例）：语义由词面+info 色档承载 -->
              <span v-if="h.ip === result.ip" class="status-badge target">最终目标</span>
              <span v-else-if="h.success" class="status-badge ok">路由正常</span>
              <span v-else class="status-badge timeout">请求超时</span>
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
   .rtt-tag 基形与 .text-subtle 已按 §9.6-10 裁决全局定档，等值副本删净落回；
   fast/medium/slow 色档副本已归家族共享件 diagRtt.css（波 2C）；
   .text-warn 全局不设（该原子名仅本面板消费），留局部。 */
.text-warn {
  color: var(--state-warning);
}

.node-ip {
  font-family: var(--font-mono);
  font-weight: 600;
}

/* 状态徽章 timeout/target 档位为本面板独有变体 */
.status-badge.timeout {
  background: var(--surface-hover);
  color: var(--color-text-subtle);
}
.status-badge.target {
  background: var(--state-information-soft);
  color: var(--state-information);
  font-weight: 600;
}
</style>
