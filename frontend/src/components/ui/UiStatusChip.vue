<script setup lang="ts">
// 状态徽标：.chip/.chip-{tone} 原子类薄包装；tone 词表与 tokens 状态色一族对齐。
defineProps<{
  tone: 'positive' | 'information' | 'warning' | 'danger' | 'neutral'
}>()
</script>

<template>
  <span class="chip" :class="`chip-${tone}`"><slot /></span>
</template>
