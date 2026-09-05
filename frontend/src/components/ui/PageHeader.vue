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
</style>
