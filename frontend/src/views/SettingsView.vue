<script setup lang="ts">
import { ref, onMounted } from 'vue'
import * as AppAPI from '../../bindings/hubkit/internal/app'
import type { AppInfo } from '../../bindings/hubkit/internal/app/models'
import { getErrorMessage } from '../utils/errors'
import { useToast } from '../composables/useToast'

const { showToast } = useToast()

const appInfo = ref<AppInfo | null>(null)

async function refresh() {
  try {
    appInfo.value = await AppAPI.AppService.GetAppInfo()
  } catch (e: unknown) {
    showToast(`获取系统信息失败: ${getErrorMessage(e)}`)
  }
}

async function openFolder(path?: string) {
  if (!path) return
  try {
    await AppAPI.AppService.OpenPath(path)
  } catch (e: unknown) {
    showToast(`打开目录失败: ${getErrorMessage(e)}`)
  }
}

async function openHosts() {
  try {
    await AppAPI.AppService.OpenHostsFile()
    showToast('已调起编辑器打开 hosts 文件')
  } catch (e: unknown) {
    showToast(`打开 hosts 失败: ${getErrorMessage(e)}`)
  }
}

async function openNetworkPanel() {
  try {
    await AppAPI.AppService.OpenNetworkConnections()
    showToast('已打开网络连接面板')
  } catch (e: unknown) {
    showToast(`打开网络面板失败: ${getErrorMessage(e)}`)
  }
}

async function openEnvSettings() {
  try {
    await AppAPI.AppService.OpenSystemEnvSettings()
    showToast('已打开环境变量设置')
  } catch (e: unknown) {
    showToast(`打开环境变量失败: ${getErrorMessage(e)}`)
  }
}

onMounted(refresh)
</script>

<template>
  <section class="page settings-page">
    <div class="header-row">
      <div>
        <h1>设置</h1>
        <p class="subtitle">查看系统存储目录、运行模式与开发者快捷工具直达。</p>
      </div>
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

    <!-- 开发者系统快捷直达 -->
    <div class="section-card">
      <div class="card-header">
        <div>
          <h2>系统快捷直达 (System Quick Launch)</h2>
          <p class="hint">一键调起 Windows 常用系统配置文件与网络管理组件。</p>
        </div>
      </div>

      <div class="dir-grid">
        <div class="dir-item">
          <div class="dir-info">
            <div class="dir-title">
              <span class="dir-name">系统 hosts 文件</span>
              <span class="dir-badge">域名映射</span>
            </div>
            <code class="dir-path">C:\Windows\System32\drivers\etc\hosts</code>
          </div>
          <button class="btn btn-primary-alt btn-small" @click="openHosts">📝 记事本编辑</button>
        </div>

        <div class="dir-item">
          <div class="dir-info">
            <div class="dir-title">
              <span class="dir-name">网络适配器管理</span>
              <span class="dir-badge">ncpa.cpl</span>
            </div>
            <code class="dir-path">控制面板 · 网络连接列表</code>
          </div>
          <button class="btn btn-secondary btn-small" @click="openNetworkPanel">🌐 打开管理</button>
        </div>

        <div class="dir-item">
          <div class="dir-info">
            <div class="dir-title">
              <span class="dir-name">系统环境变量</span>
              <span class="dir-badge">sysdm.cpl</span>
            </div>
            <code class="dir-path">Path 与用户/系统变量配置</code>
          </div>
          <button class="btn btn-secondary btn-small" @click="openEnvSettings">⚙️ 打开设置</button>
        </div>
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

.btn { padding: 6px 14px; border-radius: 6px; font-size: 13px; font-weight: 500; cursor: pointer; border: 1px solid transparent; transition: all 0.15s ease; }
.btn-primary-alt { background: #0969da; color: #fff; border-color: #0969da; }
.btn-primary-alt:hover:not(:disabled) { background: #0854ad; }
.btn-secondary { background: #fff; border-color: var(--border-color); color: var(--text-main); }
.btn-secondary:hover:not(:disabled) { background: var(--bg-hover); }
.btn-small { padding: 4px 10px; font-size: 12px; white-space: nowrap; }
</style>