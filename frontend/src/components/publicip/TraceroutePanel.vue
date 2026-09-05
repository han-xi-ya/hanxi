<script setup lang="ts">
import { computed } from 'vue'
import type { TracerouteSummary } from '../../../bindings/hanxi/internal/modules/publicip/models'

// 路由追踪面板：目标/最大跳数表单 + 常用目标 + 跳跃节点汇总表与明细表（纯展示）。
// 目标与跳数经 v-model 上抛由视图持有；根节点为多根 fragment（表单条/错误框/结果卡），
// 渲染后仍是 .network-page 的直接子节点，DOM 与拆分前一致。
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

const targetModel = computed({
  get: () => props.target,
  set: (value: string) => emit('update:target', value),
})

const maxHopsModel = computed({
  get: () => props.maxHops,
  set: (value: number) => emit('update:maxHops', value),
})

// 常用目标：先回填再发起。emit 同步送达视图，故 run 读到的已是新值，与拆分前同一执行序。
function quickTarget(value: string) {
  emit('update:target', value)
  emit('run')
}
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
      <button class="btn btn-primary" :disabled="loading || !target.trim()" @click="emit('run')">
        {{ loading ? '正在追踪路由节点…' : '开始追踪' }}
      </button>
    </div>

    <!-- 快捷常用目标 -->
    <div class="quick-targets">
      <span class="quick-label">常用目标:</span>
      <button class="btn-quick" @click="quickTarget('223.5.5.5')">阿里 DNS (223.5.5.5)</button>
      <button class="btn-quick" @click="quickTarget('1.1.1.1')">Cloudflare (1.1.1.1)</button>
      <button class="btn-quick" @click="quickTarget('8.8.8.8')">Google (8.8.8.8)</button>
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
              <span v-if="h.success" class="rtt-tag" :class="h.rttMs < 50 ? 'fast' : h.rttMs < 150 ? 'medium' : 'slow'">
                {{ h.rttMs.toFixed(1) }} ms
              </span>
              <span v-else class="text-subtle">—</span>
            </td>
            <td>
              <span v-if="h.ip === result.ip" class="status-badge target">🎯 最终目标</span>
              <span v-else-if="h.success" class="status-badge ok">路由正常</span>
              <span v-else class="status-badge timeout">请求超时</span>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>

<style scoped>
/* Phase 6 后续治理（§9.6-1）：诊断工具皮家族（.tool-panel/.diag-card/.table-container/
   .status-badge 等）已上收 components.css 共享原子，本层只留本面板真差异。
   .rtt-tag 有意保留局部（LanScannerView 同名副本未声明 font-family，裸收会漏染）。 */
.text-warn {
  color: var(--state-warning);
}
.text-subtle {
  color: var(--color-text-subtle);
}

.rtt-tag {
  font-family: var(--font-mono);
  font-weight: 600;
  font-size: 12px;
}
.rtt-tag.fast {
  color: var(--state-positive);
}
.rtt-tag.medium {
  color: var(--state-warning);
}
.rtt-tag.slow {
  color: var(--state-danger);
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
