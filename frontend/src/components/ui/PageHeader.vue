<script setup lang="ts">
// 页头：标题 + 可选副标题 + 右侧操作区；复用 .header-row/.subtitle 原子类。
defineProps<{
  title: string
  subtitle?: string
}>()
</script>

<template>
  <div class="header-row">
    <div>
      <h1>{{ title }}</h1>
      <p v-if="subtitle" class="subtitle">{{ subtitle }}</p>
    </div>
    <slot name="actions" />
  </div>
</template>
