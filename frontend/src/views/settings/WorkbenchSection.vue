<script setup lang="ts">
// 设置分区·工作台入口：日志查看器与关于页直达。
// 拆分自原 SettingsView 单页：跳转事件上抛宿主 <component @navigate>，
// 统一走 App.vue navigateTo 的门禁链，本视图不自行编排路由。
import PageHeader from '../../components/ui/PageHeader.vue'
import AppIcon from '../../components/ui/AppIcon.vue'

const emit = defineEmits<{ (e: 'navigate', route: string): void }>()
</script>

<template>
  <section class="page">
    <PageHeader title="工作台入口" subtitle="统一日志查看与版本关于信息。" />

    <div class="card entry-list">
      <div class="setting-row">
        <span class="setting-main">
          <span class="setting-name">运行日志</span>
          <span class="setting-desc">模块运行输出与脱敏历史统一查看器</span>
        </span>
        <button class="btn btn-secondary btn-small" @click="emit('navigate', '/logs')">
          <AppIcon name="goto" :size="14" /> 打开
        </button>
      </div>

      <div class="setting-row">
        <span class="setting-main">
          <span class="setting-name">关于 Hanxi</span>
          <span class="setting-desc">版本、技术栈与第三方合规信息</span>
        </span>
        <button class="btn btn-secondary btn-small" @click="emit('navigate', '/about')">
          <AppIcon name="goto" :size="14" /> 打开
        </button>
      </div>
    </div>
  </section>
</template>

<style scoped>
.entry-list { display: flex; flex-direction: column; gap: 8px; }
</style>
