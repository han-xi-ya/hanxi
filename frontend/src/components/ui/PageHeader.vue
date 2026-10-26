<script setup lang="ts">
// 页头：标题 + 可选副标题 + 右侧操作区；复用 .header-row/.subtitle 原子类。
defineProps<{
  title: string
  subtitle?: string
}>()
</script>

<template>
  <div class="header-row">
    <div class="heading-group">
      <slot name="icon" />
      <div>
        <h1>{{ title }}</h1>
        <p v-if="subtitle" class="subtitle">{{ subtitle }}</p>
      </div>
    </div>
    <slot name="actions" />
  </div>
</template>

<style scoped>
/* 图标与标题成组，避免被 header-row 原子的 space-between 拆散 */
.heading-group { display: flex; align-items: center; gap: 12px; min-width: 0; }

/* 2026-10-26 机主裁决（头部密度归一取紧档）：有副标题时标题↔副标题
   收到 6px，与 About/Home 自绘头同谱；无副标题的页头不动，页头→内容
   间距维持原子档，避免连带压缩。h1 的 16px 底距来自 :where 原子（零特异
   性），本 scoped h1 属性选择器必胜。 */
h1:has(+ .subtitle) { margin-bottom: 6px; }
</style>
