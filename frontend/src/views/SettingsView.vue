<script setup lang="ts">
import { ref, onMounted } from 'vue'
import * as AppAPI from '../../bindings/hubkit/internal/app'
import type { ModuleInfo } from '../../bindings/hubkit/internal/extapi/models'
import type { AppInfo } from '../../bindings/hubkit/internal/app/models'

// 模块管理（MVP）：每个模块（含 frpc 核心）统一启停；
const modules = ref<ModuleInfo[]>([])
const appInfo = ref<AppInfo | null>(null)
const toggling = ref<string | null>(null)
const err = ref('')
const toastMsg = ref('')

function showToast(msg: string) {
  toastMsg.value = msg
  setTimeout(() => { toastMsg.value = '' }, 2500)
}

async function refresh() {
  const [mods, info] = await Promise.all([
    AppAPI.AppService.ListModules(),
    AppAPI.AppService.GetAppInfo(),
  ])
  modules.value = mods ?? []
  appInfo.value = info
}

async function openFolder(path?: string) {
  if (!path) return
  try {
    await AppAPI.AppService.OpenPath(path)
  } catch (e: any) {
    showToast(`打开目录失败: ${e?.message ?? e}`)
  }
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
  <section class="page settings-page">
    <div class="header-row">
      <div>
        <h1>设置</h1>
        <p class="subtitle">管理系统目录快捷访问与扩展功能模块启停状态。</p>
      </div>
      <div v-if="toastMsg" class="toast">{{ toastMsg }}</div>
    </div>

    <!-- 系统与数据目录快捷访问 -->
    <div class="section-card">
      <div class="card-header">
        <div>
          <h2>系统与数据目录</h2>
          <p class="hint">当前运行模式：<strong class="mode-tag">{{ appInfo?.mode === 'portable' ? '便携免安装模式 (./data)' : '系统标准模式 (%APPDATA%)' }}</strong></p>
        </div>
      </div>

      <div class="dir-grid">
        <div class="dir-item">
          <div class="dir-info">
            <div class="dir-title">
              <span class="dir-name">配置数据目录</span>
              <span class="dir-badge">配置 & 项目</span>
            </div>
            <code class="dir-path" :title="appInfo?.configDir">{{ appInfo?.configDir || '—' }}</code>
          </div>
          <button class="btn btn-secondary btn-small" @click="openFolder(appInfo?.configDir)">📂 打开目录</button>
        </div>

        <div class="dir-item">
          <div class="dir-info">
            <div class="dir-title">
              <span class="dir-name">日志存储目录</span>
              <span class="dir-badge">脱敏运行日志</span>
            </div>
            <code class="dir-path" :title="appInfo?.logsDir">{{ appInfo?.logsDir || '—' }}</code>
          </div>
          <button class="btn btn-secondary btn-small" @click="openFolder(appInfo?.logsDir)">📂 打开目录</button>
        </div>

        <div class="dir-item">
          <div class="dir-info">
            <div class="dir-title">
              <span class="dir-name">frpc 安装版本目录</span>
              <span class="dir-badge">可执行文件隔离仓</span>
            </div>
            <code class="dir-path" :title="appInfo?.versionsDir">{{ appInfo?.versionsDir || '—' }}</code>
          </div>
          <button class="btn btn-secondary btn-small" @click="openFolder(appInfo?.versionsDir)">📂 打开目录</button>
        </div>

        <div class="dir-item">
          <div class="dir-info">
            <div class="dir-title">
              <span class="dir-name">运行时临时目录</span>
              <span class="dir-badge">动态 TOML & PID</span>
            </div>
            <code class="dir-path" :title="appInfo?.runtimeDir">{{ appInfo?.runtimeDir || '—' }}</code>
          </div>
          <button class="btn btn-secondary btn-small" @click="openFolder(appInfo?.runtimeDir)">📂 打开目录</button>
        </div>
      </div>
    </div>

    <!-- 模块管理 -->
    <div class="section-card">
      <div class="card-header">
        <div>
          <h2>模块管理</h2>
          <p class="hint">工具箱所有能力都是模块，统一启停。停用后界面立即隐藏，后端拒绝调用；持久化配置实时生效。</p>
        </div>
      </div>
      <p v-if="err" class="err">{{ err }}</p>

      <div class="table-container">
        <table class="tbl">
          <thead>
            <tr><th>模块</th><th>版本</th><th>级别</th><th>说明</th><th>状态</th><th style="width: 90px;">操作</th></tr>
          </thead>
          <tbody>
            <tr v-for="m in modules" :key="m.id">
              <td><strong>{{ m.name }}</strong> <code class="mono-code">{{ m.id }}</code></td>
              <td>v{{ m.version }}</td>
              <td>{{ m.level }}</td>
              <td class="desc">{{ m.description }}</td>
              <td><span :class="['status-tag', m.enabled ? 'ok' : 'off']">{{ m.enabled ? '已启用' : '已停用' }}</span></td>
              <td>
                <button class="btn btn-secondary btn-small" :disabled="toggling === m.id" @click="toggle(m)">
                  {{ m.enabled ? '停用' : '启用' }}
                </button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  </section>
</template>

<style scoped>
.settings-page { display: flex; flex-direction: column; gap: 16px; height: 100%; }
.header-row { display: flex; justify-content: space-between; align-items: center; }
.subtitle { color: var(--text-muted); font-size: 13px; margin: 4px 0 0; }
.toast { background: var(--text-main); color: #fff; padding: 6px 14px; border-radius: 6px; font-size: 12px; animation: fadeIn 0.2s ease; }

.section-card {
  background: var(--bg-sidebar); border: 1px solid var(--border-color);
  border-radius: 8px; padding: 16px 18px; display: flex; flex-direction: column; gap: 14px;
}
.card-header h2 { font-size: 15px; font-weight: 600; margin: 0; }
.hint { font-size: 12px; color: var(--text-muted); margin: 4px 0 0; }
.mode-tag { color: var(--accent); }

.dir-grid {
  display: grid; grid-template-columns: repeat(auto-fill, minmax(360px, 1fr)); gap: 10px;
}
.dir-item {
  background: var(--bg-app); border: 1px solid var(--border-color); border-radius: 6px;
  padding: 12px 14px; display: flex; justify-content: space-between; align-items: center; gap: 12px;
}
.dir-info { display: flex; flex-direction: column; gap: 4px; min-width: 0; }
.dir-title { display: flex; align-items: center; gap: 6px; }
.dir-name { font-size: 13px; font-weight: 600; color: var(--text-main); }
.dir-badge { font-size: 10px; color: var(--text-muted); background: var(--bg-hover); padding: 1px 6px; border-radius: 4px; }
.dir-path {
  font-family: Consolas, monospace; font-size: 11px; color: var(--text-subtle);
  white-space: nowrap; overflow: hidden; text-overflow: ellipsis; max-width: 260px;
}

.table-container { border: 1px solid var(--border-color); border-radius: 6px; overflow: hidden; }
.tbl { width: 100%; border-collapse: collapse; font-size: 13px; text-align: left; }
.tbl th { background: var(--bg-app); padding: 10px 12px; font-weight: 600; color: var(--text-muted); border-bottom: 1px solid var(--border-color); }
.tbl td { padding: 10px 12px; border-bottom: 1px solid var(--border-color); color: var(--text-main); }
.tbl tr:last-child td { border-bottom: none; }
.mono-code { font-family: Consolas, monospace; font-size: 11px; color: var(--text-muted); margin-left: 4px; }
.desc { color: var(--text-muted); font-size: 12px; }

.status-tag { font-size: 11px; padding: 2px 8px; border-radius: 12px; font-weight: 500; }
.status-tag.ok { background: #dafbe1; color: var(--success); }
.status-tag.off { background: #ffebe9; color: var(--danger); }

.btn { padding: 6px 14px; border-radius: 6px; font-size: 13px; font-weight: 500; cursor: pointer; border: 1px solid transparent; transition: all 0.15s ease; }
.btn-secondary { background: #fff; border-color: var(--border-color); color: var(--text-main); }
.btn-secondary:hover:not(:disabled) { background: var(--bg-hover); }
.btn-small { padding: 4px 10px; font-size: 12px; white-space: nowrap; }
.err { color: var(--danger); font-size: 12px; }
</style>