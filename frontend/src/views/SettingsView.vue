<script setup lang="ts">
import { ref, onMounted } from 'vue'
import * as AppAPI from '../../bindings/hubkit/internal/app'
import type { AppInfo } from '../../bindings/hubkit/internal/app/models'
import { getErrorMessage } from '../utils/errors'
import { useToast } from '../composables/useToast'
import { useNotification } from '../composables/useNotification'

const { showToast } = useToast()
const { pushToast } = useNotification()

const appInfo = ref<AppInfo | null>(null)
const autoStart = ref(false)
const minimizeToTray = ref(true)
const logRetainDays = ref(7)
const savingGeneral = ref(false)

async function refresh() {
  try {
    appInfo.value = await AppAPI.AppService.GetAppInfo()
    const gen = await AppAPI.AppService.GetGeneralSettings()
    autoStart.value = gen.autoStart
    minimizeToTray.value = gen.minimizeToTray
    logRetainDays.value = gen.logRetainDays || 7
  } catch (e: unknown) {
    showToast(`获取系统信息失败: ${getErrorMessage(e)}`)
  }
}

async function updateGeneralSettings() {
  savingGeneral.value = true
  try {
    await AppAPI.AppService.SetGeneralSettings({
      autoStart: autoStart.value,
      minimizeToTray: minimizeToTray.value,
      logRetainDays: Number(logRetainDays.value) || 7,
    })
    showToast('常规偏好设置已更新')
  } catch (e: unknown) {
    showToast(`保存设置失败: ${getErrorMessage(e)}`)
    // 回滚刷新
    await refresh()
  } finally {
    savingGeneral.value = false
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

async function triggerTestNotification() {
  try {
    // 立即通过前端通知管道弹出卡片 (确保前台响应零延迟)
    pushToast({
      id: `test_${Date.now()}`,
      moduleId: 'system',
      title: 'HubKit 统一通知',
      message: '这是一条即时测试通知：窗口激活时为应用内卡片！',
      level: 'success',
      route: '/settings',
      timestamp: Date.now(),
      read: false,
    })
    // 同时派发给后端通知中心记录历史与总线广播
    await AppAPI.AppService.SendTestNotification()
  } catch (e: unknown) {
    showToast(`后端通知分发异常: ${getErrorMessage(e)}`)
  }
}

async function triggerDelayedTestNotification() {
  try {
    await AppAPI.AppService.SendDelayedTestNotification(4)
    showToast('已设定 4 秒倒计时！请立即将主窗口最小化或点击右上角关闭(后台)，稍后将弹出 Windows 原生气泡通知')
  } catch (e: unknown) {
    showToast(`触发延迟测试通知失败: ${getErrorMessage(e)}`)
  }
}

onMounted(refresh)
</script>

<template>
  <section class="page settings-page">
    <div class="header-row">
      <div>
        <h1>设置</h1>
        <p class="subtitle">查看系统存储目录、常规运行偏好与开发者快捷工具直达。</p>
      </div>
    </div>

    <!-- 常规设置与系统常驻 -->
    <div class="section-card">
      <div class="card-header">
        <div>
          <h2>常规与运行偏好 (General Preferences)</h2>
          <p class="hint">配置 Windows 系统开机自启、关闭窗口行为与日志轮转策略。</p>
        </div>
      </div>

      <div class="pref-list">
        <label class="pref-item">
          <div class="pref-info">
            <span class="pref-title">开机自动启动</span>
            <span class="pref-desc">开启后将在 Windows 启动时以最小化模式静默常驻后台</span>
          </div>
          <input type="checkbox" v-model="autoStart" @change="updateGeneralSettings" :disabled="savingGeneral" class="switch" />
        </label>

        <label class="pref-item">
          <div class="pref-info">
            <span class="pref-title">关闭主窗口时最小化到系统托盘</span>
            <span class="pref-desc">点击右上角关闭按钮时保留后台托盘与已启动的 frpc 实例，而不是直接退出</span>
          </div>
          <input type="checkbox" v-model="minimizeToTray" @change="updateGeneralSettings" :disabled="savingGeneral" class="switch" />
        </label>

        <div class="pref-item">
          <div class="pref-info">
            <span class="pref-title">脱敏运行日志保留天数</span>
            <span class="pref-desc">自动清理过期日志文件，避免长期运行占用过多磁盘空间</span>
          </div>
          <div class="input-inline">
            <input type="number" v-model.number="logRetainDays" @change="updateGeneralSettings" min="1" max="90" class="input-number" />
            <span class="input-unit">天</span>
          </div>
        </div>
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

        <div class="dir-item">
          <div class="dir-info">
            <div class="dir-title">
              <span class="dir-name">通知通道测试</span>
              <span class="dir-badge">Native & Toast</span>
            </div>
            <code class="dir-path">前台卡片测试 & 后台 4s 倒计时气泡测试</code>
          </div>
          <div class="btn-group-inline">
            <button class="btn btn-secondary btn-small" @click="triggerTestNotification" title="前台即时测试">🔔 前台卡片</button>
            <button class="btn btn-primary-alt btn-small" @click="triggerDelayedTestNotification" title="4秒倒计时后派发，用于最小化后测试原生系统通知">⏱️ 4秒后后台气泡</button>
          </div>
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

.pref-list {
  display: flex; flex-direction: column; gap: 8px;
}
.pref-item {
  background: var(--bg-app); border: 1px solid var(--border-color); border-radius: 6px;
  padding: 12px 14px; display: flex; justify-content: space-between; align-items: center; gap: 16px;
  cursor: pointer;
}
.pref-info { display: flex; flex-direction: column; gap: 2px; }
.pref-title { font-size: 13px; font-weight: 600; color: var(--text-main); }
.pref-desc { font-size: 11px; color: var(--text-muted); }

.switch {
  width: 18px; height: 18px; cursor: pointer; accent-color: var(--accent);
}
.input-inline { display: flex; align-items: center; gap: 6px; }
.input-number {
  width: 60px; padding: 4px 8px; border: 1px solid var(--border-color);
  border-radius: 4px; background: var(--bg-card); color: var(--text-main); font-size: 13px;
}
.input-unit { font-size: 12px; color: var(--text-muted); }

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
.btn-group-inline { display: flex; align-items: center; gap: 6px; }
.btn-small { padding: 4px 10px; font-size: 12px; white-space: nowrap; }
</style>