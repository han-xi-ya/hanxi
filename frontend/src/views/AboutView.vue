<script setup lang="ts">
import { ref, onMounted } from 'vue'
import * as AppAPI from '../../bindings/hubkit/internal/app'
import type { AppInfo } from '../../bindings/hubkit/internal/app'

const info = ref<AppInfo | null>(null)
const exts = ref<any[]>([])

onMounted(async () => {
  info.value = await AppAPI.AppService.GetAppInfo()
  exts.value = (await AppAPI.AppService.ListModules()) ?? []
})
</script>

<template>
  <section class="page">
    <h1>关于</h1>
    <p v-if="info">HubKit v{{ info.version }} · {{ info.goos }}/{{ info.goarch }}</p>
    <p>以 frpc 为核心的内网穿透开发客户端，局域网扫描/释放端口/公网 IP 为内置模块。</p>
    <ul v-if="exts.length">
      <li v-for="e in exts" :key="e.id">
        {{ e.name }} <code>{{ e.id }}</code> v{{ e.version }}
        <span :class="e.enabled ? 'ok' : 'off'">{{ e.enabled ? '启用' : '禁用' }}</span>
      </li>
    </ul>
  </section>
</template>