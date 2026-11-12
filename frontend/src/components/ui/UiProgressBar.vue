<script setup lang="ts">
import { computed } from 'vue'

// 进度条：下载/扫描等定量进度共用件；非法值（NaN/Infinity）归零。
// label 生成 aria-label（读屏报「什么任务 40%」而非裸百分比）；
// indeterminate 用于不定量任务——去掉 aria-valuenow（不假报百分比），条纹往返动效，
// 受 base.css prefers-reduced-motion 全局兜底。
const props = withDefaults(defineProps<{
  percent: number
  /** 可访问名：描述这是什么任务的进度（如「下载进度」「扫描进度」） */
  label?: string
  /** 不定量模式：无确定百分比时用，不假报数值 */
  indeterminate?: boolean
}>(), {
  indeterminate: false,
})

const safePercent = computed(() => {
  const n = Number(props.percent)
  return Number.isFinite(n) ? Math.min(100, Math.max(0, n)) : 0
})
</script>

<template>
  <div
    class="ui-progress"
    :class="{ 'ui-progress-indeterminate': indeterminate }"
    role="progressbar"
    :aria-valuenow="indeterminate ? undefined : safePercent"
    aria-valuemin="0"
    aria-valuemax="100"
    :aria-label="label"
  >
    <div v-if="!indeterminate" class="ui-progress-inner" :style="{ width: `${safePercent}%` }"></div>
  </div>
</template>

<style scoped>
/* 原子层暂无进度条类，此处即标准形；颜色全部走语义 token，双主题自适应 */
.ui-progress { height: 6px; background: var(--surface-hover); border-radius: var(--radius-pill); overflow: hidden; }
.ui-progress-inner { height: 100%; background: var(--color-primary); border-radius: var(--radius-pill); transition: width var(--motion-base) linear; }
/* 不定量：主色软底上往返滑动的高亮段，只表达「进行中」不假报数值 */
.ui-progress-indeterminate {
  background: linear-gradient(
    90deg,
    var(--surface-hover) 0%,
    var(--color-primary-soft) 50%,
    var(--surface-hover) 100%
  );
  background-size: 200% 100%;
  animation: ui-progress-slide 1.4s ease-in-out infinite;
}
@keyframes ui-progress-slide {
  0% { background-position: 200% 0; }
  100% { background-position: -200% 0; }
}
</style>