<script setup lang="ts">
import { computed } from 'vue'

// 通用按钮：components.css .btn 原子家族的薄包装（docs/FRONTEND.md §4）。
// disabled/type/title 等 attrs 原生透传到底钮；默认 type=button，外部传入可覆盖。
type UiButtonVariant = 'primary' | 'secondary' | 'danger' | 'ghost'

const props = withDefaults(defineProps<{
  variant?: UiButtonVariant
  small?: boolean
  block?: boolean
}>(), {
  variant: 'secondary', small: false, block: false,
})

// danger 映射 .btn-danger-outline（设计规范：破坏性动作用描边红）
const variantClass = computed(() =>
  props.variant === 'danger' ? 'btn-danger-outline' : `btn-${props.variant}`,
)
</script>

<template>
  <button type="button" class="btn" :class="[variantClass, { 'btn-small': small, 'ui-btn-block': block }]">
    <slot />
  </button>
</template>

<style scoped>
.ui-btn-block { width: 100%; }
</style>
