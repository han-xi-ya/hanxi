<script setup lang="ts">
// 扩展页占位：具体扩展视图在对应里程碑实现（M2 lan/publicip，M3 portkill）。
defineProps<{ title?: string }>()
</script>

<template>
  <section class="page">
    <h1>{{ title ?? '扩展' }}</h1>
    <p>此扩展页面正在建设中。</p>
  </section>
</template>