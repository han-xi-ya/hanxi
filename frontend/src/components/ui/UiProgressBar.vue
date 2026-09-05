<script setup lang="ts">
import { computed } from 'vue'

// 进度条：下载/扫描等定量进度共用件；非法值（NaN/Infinity）归零。
const props = defineProps<{
  percent: number
}>()

const safePercent = computed(() => {
  const n = Number(props.percent)
  return Number.isFinite(n) ? Math.min(100, Math.max(0, n)) : 0
})
</script>

<template>
  <div
    class="ui-progress"
    role="progressbar"
    :aria-valuenow="safePercent"
    aria-valuemin="0"
    aria-valuemax="100"
  >
    <div class="ui-progress-inner" :style="{ width: `${safePercent}%` }"></div>
  </div>
</template>

<style scoped>
/* 原子层暂无进度条类，此处即标准形；颜色全部走语义 token，双主题自适应 */
.ui-progress { height: 6px; background: var(--surface-hover); border-radius: var(--radius-pill); overflow: hidden; }
.ui-progress-inner { height: 100%; background: var(--color-primary); border-radius: var(--radius-pill); transition: width var(--motion-base) linear; }
</style>
