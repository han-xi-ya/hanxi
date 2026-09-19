<script setup lang="ts">
// 页面容器：只负责宽度档位与水平居中（映射 components.css .page 家族原子，
// 宽度真相 = tokens.css --container-* 三档）。
// 分工纪律：页面容器不叠加页面级 padding——视口安全边距归 Shell（App.vue
// .content-area），禁止双重 padding。
// fluid 为终端/日志类满宽例外档，命中时替换变体档（variant 不再参与宽度）。
import { computed } from 'vue'

type ContainerVariant = 'standard' | 'workbench' | 'wide'

const props = withDefaults(defineProps<{
  /** 宽度档位：standard 1000px 默认 / workbench 1200px 多面板 / wide 1440px 密集表格 */
  variant?: ContainerVariant
  /** 满宽例外（终端、日志流等铺满内容区） */
  fluid?: boolean
}>(), {
  variant: 'standard',
  fluid: false,
})

// 变体 → 全局原子类映射（原子几何/标题/正文规则逐字同形，仅宽度档不同）
const VARIANT_CLASS: Record<ContainerVariant, string> = {
  standard: 'page',
  workbench: 'page-workbench',
  wide: 'page-wide',
}

const widthClass = computed(() =>
  props.fluid ? 'page-fluid' : VARIANT_CLASS[props.variant],
)
</script>

<template>
  <div :class="widthClass">
    <slot />
  </div>
</template>
