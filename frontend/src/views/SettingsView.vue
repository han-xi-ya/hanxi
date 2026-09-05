<script setup lang="ts">
import { ref, computed, watch, nextTick, onMounted } from 'vue'
import * as AppAPI from '../../bindings/hanxi/internal/app'
import type { AppInfo, TrayMenuOption } from '../../bindings/hanxi/internal/app/models'
import type { TrayMenuItem } from '../../bindings/hanxi/internal/settings/models'
import { getErrorMessage } from '../utils/errors'
import { useToast } from '../composables/useToast'
import { useTheme } from '../composables/useTheme'

const { showToast } = useToast()
const { themeMode, setThemeMode } = useTheme()

const appInfo = ref<AppInfo | null>(null)
const autoStart = ref(false)
const minimizeToTray = ref(true)
const logRetainDays = ref(7)
const savingGeneral = ref(false)

// —— 托盘右键菜单配置 ——
const trayLoading = ref(true)
const trayOptions = ref<TrayMenuOption[]>([])
const trayItems = ref<TrayMenuItem[]>([])
const savingTray = ref(false)
const trayDirty = ref(false)
const pickingExe = ref(false)
const exeForm = ref({ path: '', args: '', label: '' })

// 候选项默认标签索引：type|ref → label（行内自定义名为空时回退展示）
const optionLabels = computed(() => {
  const map = new Map<string, string>()
  for (const opt of trayOptions.value) map.set(`${opt.type}|${opt.ref}`, opt.label)
  return map
})

watch(trayItems, () => { trayDirty.value = true }, { deep: true })

async function refreshTray() {
  trayLoading.value = true
  try {
    const [opts, items] = await Promise.all([
      AppAPI.AppService.ListTrayMenuOptions(),
      AppAPI.AppService.GetTrayMenu(),
    ])
    trayOptions.value = opts ?? []
    trayItems.value = (items ?? []).map(i => ({ ...i }))
    await nextTick() // 等 watcher 冲刷后再复位脏标记，避免加载即"未保存"
    trayDirty.value = false
  } catch (e: unknown) {
    showToast(`获取托盘配置失败: ${getErrorMessage(e)}`)
  } finally {
    trayLoading.value = false
  }
}

function exeBaseName(p: string): string {
  const base = p.split(/[\\/]/).pop() || p
  return base.replace(/\.[^.\\/]+$/, '')
}

function defaultLabelFor(item: TrayMenuItem): string {
  if (item.type === 'exe') return exeBaseName(item.path) || '外部程序'
  return optionLabels.value.get(`${item.type}|${item.ref}`) || item.ref
}

function typeLabel(type: string): string {
  if (type === 'command') return '工具命令'
  if (type === 'route') return '页面'
  return '外部程序'
}

function isOptionAdded(opt: TrayMenuOption): boolean {
  return trayItems.value.some(i => i.type === opt.type && i.ref === opt.ref)
}

function toggleOption(opt: TrayMenuOption) {
  const idx = trayItems.value.findIndex(i => i.type === opt.type && i.ref === opt.ref)
  if (idx >= 0) {
    trayItems.value.splice(idx, 1)
  } else {
    trayItems.value.push({ type: opt.type, ref: opt.ref, path: '', args: '', label: '', enabled: true })
  }
}

function moveItem(i: number, delta: number) {
  const j = i + delta
  const arr = trayItems.value
  if (j < 0 || j >= arr.length) return
  ;[arr[i], arr[j]] = [arr[j], arr[i]]
}

function removeItem(i: number) {
  trayItems.value.splice(i, 1)
}

async function browseExe() {
  pickingExe.value = true
  try {
    const p = await AppAPI.AppService.PickExeFile()
    if (p) exeForm.value.path = p // 取消时为空串，保持现值
  } catch (e: unknown) {
    showToast(`打开文件选择框失败: ${getErrorMessage(e)}`)
  } finally {
    pickingExe.value = false
  }
}

function addExe() {
  const path = exeForm.value.path.trim()
  if (!path) {
    showToast('请先填写或选择程序路径')
    return
  }
  trayItems.value.push({
    type: 'exe',
    ref: '',
    path,
    args: exeForm.value.args.trim(),
    label: exeForm.value.label.trim(),
    enabled: true,
  })
  exeForm.value = { path: '', args: '', label: '' }
}

async function saveTrayMenu() {
  savingTray.value = true
  try {
    await AppAPI.AppService.SetTrayMenu(trayItems.value)
    trayDirty.value = false
    showToast('托盘右键菜单已保存并即时生效')
  } catch (e: unknown) {
    showToast(`保存托盘菜单失败: ${getErrorMessage(e)}`)
    await refreshTray()
  } finally {
    savingTray.value = false
  }
}

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
    // 走完整统一通知管道：后端 notify → notify:received 事件 → 顶层卡片
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

onMounted(() => {
  refresh()
  refreshTray()
})
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

    <!-- 外观主题 -->
    <div class="section-card">
      <div class="card-header">
        <div>
          <h2>外观 (Theme)</h2>
          <p class="hint">主题持久化在后端设置中，便携模式随 data/ 目录迁移；「跟随系统」将随 Windows 亮暗设置自动切换。</p>
        </div>
      </div>

      <div class="pref-list">
        <div class="pref-item">
          <div class="pref-info">
            <span class="pref-title">界面主题</span>
            <span class="pref-desc">深色为独立标定的配色（含原生标题栏），不是简单反色</span>
          </div>
          <div class="theme-seg" role="radiogroup" aria-label="界面主题">
            <button type="button" class="theme-seg-btn" :class="{ active: themeMode === 'system' }" role="radio" :aria-checked="themeMode === 'system'" @click="setThemeMode('system')">跟随系统</button>
            <button type="button" class="theme-seg-btn" :class="{ active: themeMode === 'light' }" role="radio" :aria-checked="themeMode === 'light'" @click="setThemeMode('light')">浅色</button>
            <button type="button" class="theme-seg-btn" :class="{ active: themeMode === 'dark' }" role="radio" :aria-checked="themeMode === 'dark'" @click="setThemeMode('dark')">深色</button>
          </div>
        </div>
      </div>
    </div>

    <!-- 托盘右键菜单配置 -->
    <div class="section-card">
      <div class="card-header">
        <div>
          <h2>托盘右键菜单 (Tray Menu)</h2>
          <p class="hint">自定义右键托盘图标时的快捷入口：启动托管工具、直达模块页面或拉起外部程序，保存后立即生效。</p>
        </div>
      </div>

      <div v-if="trayLoading" class="tray-state">正在加载托盘配置…</div>

      <template v-else>
        <!-- 当前菜单条目（顺序即右键菜单显示顺序） -->
        <div v-if="trayItems.length > 0" class="tray-list">
          <div
            v-for="(item, i) in trayItems"
            :key="`${item.type}|${item.ref}|${item.path}|${i}`"
            class="tray-row"
          >
            <div class="tray-row-main">
              <span class="tray-tag" :class="`tray-tag-${item.type}`">{{ typeLabel(item.type) }}</span>
              <input
                class="tray-input tray-name"
                v-model="item.label"
                :placeholder="defaultLabelFor(item)"
                maxlength="30"
                title="自定义菜单显示名，留空使用默认名称"
              />
              <code class="tray-ref" :title="item.type === 'exe' ? item.path : item.ref">
                {{ item.type === 'exe' ? item.path : item.ref }}<template v-if="item.type === 'exe' && item.args"> {{ item.args }}</template>
              </code>
            </div>
            <div class="tray-row-actions">
              <button class="btn btn-secondary btn-small" :disabled="i === 0" @click="moveItem(i, -1)" title="上移">↑</button>
              <button class="btn btn-secondary btn-small" :disabled="i === trayItems.length - 1" @click="moveItem(i, 1)" title="下移">↓</button>
              <button class="btn btn-secondary btn-small tray-remove" @click="removeItem(i)" title="从托盘菜单移除">✕ 移除</button>
            </div>
          </div>
        </div>
        <div v-else class="tray-state tray-empty">尚未配置托盘条目：在下方"可选条目"中勾选，或添加外部程序。</div>

        <!-- 外部程序添加 -->
        <div class="exe-form">
          <span class="exe-form-title">添加外部程序</span>
          <div class="exe-form-row">
            <input class="tray-input tray-exe-path" v-model="exeForm.path" placeholder="程序绝对路径，如 C:\Tools\app.exe 或桌面快捷方式 .lnk" />
            <button class="btn btn-secondary btn-small" :disabled="pickingExe" @click="browseExe">📂 浏览…</button>
          </div>
          <div class="exe-form-row">
            <input class="tray-input tray-exe-args" v-model="exeForm.args" placeholder="启动参数（可选，空格分隔，引号可包空格）" />
            <input class="tray-input tray-exe-label" v-model="exeForm.label" placeholder="菜单名称（可选）" maxlength="30" />
            <button class="btn btn-primary-alt btn-small" @click="addExe">＋ 添加</button>
          </div>
        </div>

        <!-- 候选条目目录 -->
        <div>
          <span class="exe-form-title">可选条目</span>
          <div class="option-grid">
            <label v-for="opt in trayOptions" :key="`${opt.type}|${opt.ref}`" class="option-item" :title="opt.moduleName ? `来自模块：${opt.moduleName}` : opt.ref">
              <input type="checkbox" class="switch" :checked="isOptionAdded(opt)" @change="toggleOption(opt)" />
              <span class="option-label">{{ opt.label }}</span>
            </label>
          </div>
          <div v-if="trayOptions.length === 0" class="tray-state tray-empty">暂无候选条目（托管工具命令与模块页面导航均为空）。</div>
        </div>

        <div class="tray-footer">
          <span class="hint">修改后需点击保存；条目对应模块被禁用时，点击菜单项会收到错误提示。</span>
          <button class="btn btn-primary-alt" :disabled="savingTray || !trayDirty" @click="saveTrayMenu">
            {{ savingTray ? '保存中…' : '保存托盘菜单' }}
          </button>
        </div>
      </template>
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
.subtitle { color: var(--color-text-muted); font-size: 13px; margin: 4px 0 0; }
.toast { background: var(--color-text); color: #fff; padding: 6px 14px; border-radius: 6px; font-size: 12px; animation: fadeIn 0.2s ease; }

.section-card {
  background: var(--surface-panel); border: 1px solid var(--color-border);
  border-radius: 8px; padding: 16px 18px; display: flex; flex-direction: column; gap: 14px;
}
.card-header h2 { font-size: 15px; font-weight: 600; margin: 0; }
.hint { font-size: 12px; color: var(--color-text-muted); margin: 4px 0 0; }
.mode-tag { color: var(--color-primary); }

.pref-list {
  display: flex; flex-direction: column; gap: 8px;
}
.pref-item {
  background: var(--surface-page); border: 1px solid var(--color-border); border-radius: 6px;
  padding: 12px 14px; display: flex; justify-content: space-between; align-items: center; gap: 16px;
  cursor: pointer;
}
.pref-info { display: flex; flex-direction: column; gap: 2px; }
.pref-title { font-size: 13px; font-weight: 600; color: var(--color-text); }
.pref-desc { font-size: 11px; color: var(--color-text-muted); }

.switch {
  width: 18px; height: 18px; cursor: pointer; accent-color: var(--color-primary);
}
.input-inline { display: flex; align-items: center; gap: 6px; }
.input-number {
  width: 60px; padding: 4px 8px; border: 1px solid var(--color-border);
  border-radius: 4px; background: var(--surface-panel); color: var(--color-text); font-size: 13px;
}
.input-unit { font-size: 12px; color: var(--color-text-muted); }

.dir-grid {
  display: grid; grid-template-columns: repeat(auto-fill, minmax(360px, 1fr)); gap: 10px;
}
.dir-item {
  background: var(--surface-page); border: 1px solid var(--color-border); border-radius: 6px;
  padding: 12px 14px; display: flex; justify-content: space-between; align-items: center; gap: 12px;
}
.dir-info { display: flex; flex-direction: column; gap: 4px; min-width: 0; }
.dir-title { display: flex; align-items: center; gap: 6px; }
.dir-name { font-size: 13px; font-weight: 600; color: var(--color-text); }
.dir-badge { font-size: 10px; color: var(--color-text-muted); background: var(--surface-hover); padding: 1px 6px; border-radius: 4px; }
.dir-path {
  font-family: Consolas, monospace; font-size: 11px; color: var(--color-text-subtle);
  white-space: nowrap; overflow: hidden; text-overflow: ellipsis; max-width: 260px;
}

.tray-state {
  background: var(--surface-page); border: 1px dashed var(--color-border); border-radius: 6px;
  padding: 12px 14px; font-size: 12px; color: var(--color-text-muted);
}
.tray-list { display: flex; flex-direction: column; gap: 8px; }
.tray-row {
  background: var(--surface-page); border: 1px solid var(--color-border); border-radius: 6px;
  padding: 8px 12px; display: flex; justify-content: space-between; align-items: center; gap: 12px; flex-wrap: wrap;
}
.tray-row-main { display: flex; align-items: center; gap: 8px; min-width: 0; flex: 1; }
.tray-row-actions { display: flex; gap: 4px; }
.tray-tag {
  font-size: 10px; color: var(--color-text-muted); background: var(--surface-hover);
  padding: 1px 6px; border-radius: 4px; white-space: nowrap; border: 1px solid var(--color-border);
}
.tray-tag-command { color: var(--color-primary); border-color: var(--color-primary); }
.tray-ref {
  font-family: Consolas, monospace; font-size: 11px; color: var(--color-text-subtle);
  white-space: nowrap; overflow: hidden; text-overflow: ellipsis; min-width: 0;
}
.tray-input {
  padding: 5px 8px; border: 1px solid var(--color-border); border-radius: 4px;
  background: var(--surface-panel); color: var(--color-text); font-size: 12px;
}
.tray-name { width: 170px; flex-shrink: 0; }
.tray-remove:hover:not(:disabled) { border-color: var(--state-danger); color: var(--state-danger); }

.exe-form {
  background: var(--surface-page); border: 1px dashed var(--color-border); border-radius: 6px;
  padding: 10px 12px; display: flex; flex-direction: column; gap: 8px;
}
.exe-form-title { font-size: 12px; font-weight: 600; color: var(--color-text); }
.exe-form-row { display: flex; gap: 8px; align-items: center; }
.tray-exe-path { flex: 1; min-width: 0; }
.tray-exe-args { flex: 1; min-width: 0; }
.tray-exe-label { width: 170px; flex-shrink: 0; }

.option-grid {
  display: grid; grid-template-columns: repeat(auto-fill, minmax(260px, 1fr));
  gap: 6px 12px; margin-top: 8px;
}
.option-item {
  background: var(--surface-page); border: 1px solid var(--color-border); border-radius: 6px;
  padding: 7px 10px; display: flex; align-items: center; gap: 8px; cursor: pointer; min-width: 0;
}
.option-item:hover { background: var(--surface-hover); }
.option-label { font-size: 12px; color: var(--color-text); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }

.tray-footer { display: flex; justify-content: space-between; align-items: center; gap: 16px; flex-wrap: wrap; }

.btn { padding: 6px 14px; border-radius: 6px; font-size: 13px; font-weight: 500; cursor: pointer; border: 1px solid transparent; transition: all 0.15s ease; }
.btn:disabled { opacity: 0.55; cursor: not-allowed; }
.btn-primary-alt { background: var(--state-information); color: var(--color-on-primary); border-color: var(--state-information); }
.btn-primary-alt:hover:not(:disabled) { background: color-mix(in srgb, var(--state-information) 82%, black); }
.btn-secondary { background: #fff; border-color: var(--color-border); color: var(--color-text); }
.btn-secondary:hover:not(:disabled) { background: var(--surface-hover); }
.btn-group-inline { display: flex; align-items: center; gap: 6px; }
.btn-small { padding: 4px 10px; font-size: 12px; white-space: nowrap; }

/* 外观主题分段控件（新增代码按重构纪律直接引用语义 token） */
.theme-seg { display: flex; background: var(--surface-hover); border: 1px solid var(--color-border); border-radius: var(--radius-control); padding: 3px; gap: 2px; }
.theme-seg-btn { border: none; background: transparent; padding: 6px 14px; border-radius: 6px; font-size: 13px; color: var(--color-text-muted); cursor: pointer; transition: background var(--motion-base) ease, color var(--motion-base) ease; }
.theme-seg-btn:hover { color: var(--color-text); }
.theme-seg-btn.active { background: var(--surface-panel); color: var(--color-primary); font-weight: 600; box-shadow: var(--shadow-small); }
</style>