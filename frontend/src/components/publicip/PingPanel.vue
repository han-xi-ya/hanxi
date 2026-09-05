<script setup lang="ts">
import { computed } from 'vue'
import type { PingSummary } from '../../../bindings/hanxi/internal/modules/publicip/models'

// Ping 连通性测试面板：目标/次数表单 + 常用目标 + 结果汇总表与明细表（纯展示）。
// 目标与次数经 v-model 上抛由视图持有——网卡芯片的「⚡ 快捷 Ping」需跨 Tab 回填并立即发起。
// 根节点为多根 fragment（表单条/错误框/结果卡），渲染后仍是 .network-page 的直接子节点，DOM 与拆分前一致。
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

const targetModel = computed({
  get: () => props.target,
  set: (value: string) => emit('update:target', value),
})

const countModel = computed({
  get: () => props.count,
  set: (value: number) => emit('update:count', value),
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
      <button class="btn btn-primary" :disabled="loading || !target.trim()" @click="emit('run')">
        {{ loading ? 'Ping 探测中…' : '发起 Ping' }}
      </button>
    </div>

    <!-- 快捷常用目标 -->
    <div class="quick-targets">
      <span class="quick-label">常用目标:</span>
      <button class="btn-quick" @click="quickTarget('223.5.5.5')">阿里 DNS (223.5.5.5)</button>
      <button class="btn-quick" @click="quickTarget('119.29.29.29')">腾讯 DNS (119.29.29.29)</button>
      <button class="btn-quick" @click="quickTarget('1.1.1.1')">Cloudflare (1.1.1.1)</button>
      <button class="btn-quick" @click="quickTarget('8.8.8.8')">Google (8.8.8.8)</button>
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
              <span v-if="r.success" class="rtt-tag" :class="r.rttMs < 50 ? 'fast' : r.rttMs < 150 ? 'medium' : 'slow'">
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

<style scoped>
/* Phase 6 后续治理（§9.6-1）：诊断工具皮家族（.tool-panel/.diag-card/.table-container/
   .status-badge 等）已上收 components.css 共享原子，本层只留本面板真差异。
   .rtt-tag 有意保留局部（LanScannerView 同名副本未声明 font-family，裸收会漏染）。 */
.val-warn {
  color: var(--state-danger);
}
/* .text-danger 暂留局部：MemoView 等处存在无定义同名使用点，全局原子会改其观感（§9.6 待裁决） */
.text-danger {
  color: var(--state-danger);
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

/* 状态徽章 fail 态承载动态错误文本（可能较长），本面板独有 */
.status-badge.fail {
  background: var(--state-danger-soft);
  color: var(--state-danger);
}
</style>
