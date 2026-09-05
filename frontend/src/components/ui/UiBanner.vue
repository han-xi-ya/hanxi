<script setup lang="ts">
// 提示条：.banner/.banner-{ok|info|warn|error} 原子类薄包装。
// role=note：补充性信息，不用 alert 打断读屏。
defineProps<{
  tone: 'ok' | 'info' | 'warn' | 'error'
}>()
</script>

<template>
  <div class="banner" :class="`banner-${tone}`" role="note"><slot /></div>
</template>
