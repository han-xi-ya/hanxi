<script setup lang="ts">
// 扩展页占位：后端注册了路由、但前端尚未建档对应视图时兜底渲染（constants/navigation.ts 的
// placeholderComponent）。早期里程碑点名的 lan/publicip/portkill 等均已有独立视图，不再走此处。
defineProps<{ title?: string }>()
</script>

<template>
  <section class="page">
    <h1>{{ title ?? '扩展' }}</h1>
    <p>此扩展页面正在建设中。</p>
  </section>
</template>