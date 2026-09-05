<script setup lang="ts">
// 通用图标壳：constants/icons.ts 注册表的内联 SVG 渲染器（§8 AppIcon 阶段1）。
// 装饰性图标默认 aria-hidden；需要独立可访问名时传 label（role=img + aria-label）。
// 尺寸用 size（px 数字或 CSS 长度），默认 1em 跟随字号。
import { computed } from 'vue'
import { ICON_PATHS, type IconName } from '../../constants/icons'

const props = withDefaults(defineProps<{
  name: IconName
  size?: number | string
  label?: string
}>(), { size: '1em' })

const paths = computed(() => ICON_PATHS[props.name] as readonly string[])
const cssSize = computed(() => (typeof props.size === 'number' ? `${props.size}px` : props.size))
</script>

<template>
  <svg
    class="app-icon"
    viewBox="0 0 24 24"
    width="24" height="24"
    :style="{ width: cssSize, height: cssSize }"
    fill="none"
    stroke="currentColor"
    stroke-width="1.8"
    stroke-linecap="round"
    stroke-linejoin="round"
    :aria-hidden="label ? undefined : 'true'"
    :role="label ? 'img' : undefined"
    :aria-label="label"
  >
    <path v-for="(d, i) in paths" :key="i" :d="d" />
  </svg>
</template>

<style scoped>
.app-icon { display: inline-block; vertical-align: -0.125em; flex: none; }
</style>
