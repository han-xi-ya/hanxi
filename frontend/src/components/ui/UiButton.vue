<script setup lang="ts">
import { computed } from 'vue'

// 通用按钮：components.css .btn 原子家族的薄包装（docs/FRONTEND.md §4）。
// disabled/type/title 等 attrs 原生透传到底钮；默认 type=button，外部传入可覆盖。
type UiButtonVariant = 'primary' | 'secondary' | 'danger' | 'ghost'

const props = withDefaults(defineProps<{
  variant?: UiButtonVariant
  small?: boolean
  block?: boolean
  /** 在途态：禁用 + aria-busy + 内联 spinner，文案不变（保宽防布局抖动） */
  busy?: boolean
}>(), {
  variant: 'secondary', small: false, block: false, busy: false,
})

// danger 映射 .btn-danger-outline（设计规范：破坏性动作用描边红）
const variantClass = computed(() =>
  props.variant === 'danger' ? 'btn-danger-outline' : `btn-${props.variant}`,
)
</script>

<template>
  <button
    type="button"
    class="btn"
    :class="[variantClass, { 'btn-small': small, 'ui-btn-block': block }]"
    :disabled="busy || undefined"
    :aria-busy="busy || undefined"
  >
    <span v-if="busy" class="ui-btn-spin" aria-hidden="true"></span>
    <slot />
  </button>
</template>

<style scoped>
.ui-btn-block { width: 100%; }
/* 在途 spinner：currentColor 描边随按钮前景，尺寸随字号；reduced-motion 由 base.css 全局兜底 */
.ui-btn-spin {
  width: 1em;
  height: 1em;
  flex: none;
  border: 2px solid currentColor;
  border-right-color: transparent;
  border-radius: var(--radius-pill);
  animation: ui-btn-spin 0.75s linear infinite;
}
@keyframes ui-btn-spin {
  to { transform: rotate(360deg); }
}
</style>