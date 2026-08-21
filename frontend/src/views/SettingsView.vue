<script setup lang="ts">
import { ref, onMounted } from 'vue'
import * as AppAPI from '../../bindings/hubkit/internal/app'
import type { ModuleInfo } from '../../bindings/hubkit/internal/extapi/models'

// 模块管理（MVP）：每个模块（含 frpc 核心）统一启停；
// 设置持久化在 M1 接入 settings 后落地。
const modules = ref<ModuleInfo[]>([])
const toggling = ref<string | null>(null)
const err = ref('')

async function refresh() {
  modules.value = (await AppAPI.AppService.ListModules()) ?? []
}

async function toggle(m: ModuleInfo) {
  toggling.value = m.id
  err.value = ''
  try {
    const updated = await AppAPI.AppService.SetModuleEnabled(m.id, !m.enabled)
    if (updated) {
      await refresh()
    }
  } catch (e: any) {
    err.value = String(e?.message ?? e)
  } finally {
    toggling.value = null
  }
}

onMounted(refresh)
</script>

<template>
  <section class="page">
    <h1>设置</h1>

    <h2>模块管理</h2>
    <p class="hint">工具箱所有能力都是模块，统一启停。停用后界面立即隐藏，后端拒绝调用；彻底移除服务需重启应用（启停状态持久化在 M1 接入）。</p>
    <p v-if="err" class="err">{{ err }}</p>

    <table class="tbl">
      <thead>
        <tr><th>模块</th><th>版本</th><th>级别</th><th>说明</th><th>状态</th><th></th></tr>
      </thead>
      <tbody>
        <tr v-for="m in modules" :key="m.id">
          <td><strong>{{ m.name }}</strong> <code>{{ m.id }}</code></td>
          <td>v{{ m.version }}</td>
          <td>{{ m.level }}</td>
          <td class="desc">{{ m.description }}</td>
          <td><span :class="m.enabled ? 'ok' : 'off'">{{ m.enabled ? '启用' : '已停用' }}</span></td>
          <td>
            <button class="btn" :disabled="toggling === m.id" @click="toggle(m)">
              {{ m.enabled ? '停用' : '启用' }}
            </button>
          </td>
        </tr>
      </tbody>
    </table>
  </section>
</template>

<style scoped>
h2 { font-size: 15px; margin: 20px 0 8px; }
.hint { font-size: 12px; }
.err { color: var(--danger); }
.tbl { width: 100%; border-collapse: collapse; margin-top: 8px; font-size: 13px; }
.tbl th, .tbl td { text-align: left; padding: 6px 8px; border-bottom: 1px solid var(--border); }
.desc { color: var(--fg-dim); font-size: 12px; }
.btn {
  background: var(--bg-2); color: var(--fg); border: 1px solid var(--border);
  padding: 3px 12px; border-radius: 5px; cursor: pointer; font-size: 12px;
}
.btn:hover { border-color: var(--accent); }
.btn:disabled { opacity: .5; cursor: default; }
</style>