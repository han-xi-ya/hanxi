<script setup lang="ts">
import { ref, onMounted } from 'vue'
import * as AppAPI from '../../bindings/hubkit/internal/app'
import type { AppInfo } from '../../bindings/hubkit/internal/app'

const info = ref<AppInfo | null>(null)

onMounted(async () => {
  info.value = await AppAPI.AppService.GetAppInfo()
})
</script>

<template>
  <section class="page">
    <h1>首页</h1>
    <p>frpc 项目列表、最近实例、端口快捷查询将在这里汇聚（M4 实现）。</p>
    <p v-if="info">
      {{ info.name }} v{{ info.version }} · {{ info.goos }}/{{ info.goarch }}
    </p>
  </section>
</template>