<script setup lang="ts">
// 扩展页占位：后端注册了路由、但前端尚未建档对应视图时兜底渲染
// （constants/navigation.ts 的 placeholderComponent）。已有独立视图的模块不走此处。
// 状态容器走全局空态原子（图标+说明+下一步动作），不再裸标题；
// 「返回模块中心」经 App.vue 已接线的 @navigate 上抛（声明 emits 即可用）。
import PageContainer from '../components/ui/PageContainer.vue'
import UiEmptyState from '../components/ui/UiEmptyState.vue'
import UiButton from '../components/ui/UiButton.vue'
import AppIcon from '../components/ui/AppIcon.vue'

defineProps<{ title?: string }>()
const emit = defineEmits<{
  (e: 'navigate', route: string): void
}>()
</script>

<template>
  <PageContainer>
    <UiEmptyState>
      <AppIcon name="box" :size="32" class="placeholder-icon" />
      <h1 class="placeholder-title">{{ title ?? '扩展' }}</h1>
      <p>该模块前端视图尚未建档，此扩展页面正在建设中。</p>
      <UiButton @click="emit('navigate', '/modules')">返回模块中心</UiButton>
    </UiEmptyState>
  </PageContainer>
</template>

<style scoped>
/* 空态原子内标题/图标就近排版：字号取既有档位，颜色随语义 token */
.placeholder-icon {
  color: var(--color-text-subtle);
}
.placeholder-title {
  margin: 0;
  font-size: var(--text-lg);
  font-weight: 700;
}
</style>