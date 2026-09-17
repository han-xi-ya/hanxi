<script setup lang="ts">
// 设置分区·常规偏好：开机自启 / 关窗行为 / 日志保留天数。
// 拆分自原 SettingsView 单页（逻辑逐字保留）：改动即写后端 SetGeneralSettings，
// 失败回滚重拉；提示走全局顶层卡片（useToast），本页不挂局部 toast。
import { ref, onMounted } from 'vue'
import * as AppAPI from '../../../bindings/hanxi/internal/app'
import { getErrorMessage } from '../../utils/errors'
import { useToast } from '../../composables/useToast'
import PageHeader from '../../components/ui/PageHeader.vue'

const { showToast } = useToast()

const autoStart = ref(false)
const minimizeToTray = ref(true)
const logRetainDays = ref(7)
const saving = ref(false)

async function refresh() {
  try {
    const gen = await AppAPI.AppService.GetGeneralSettings()
    autoStart.value = gen.autoStart
    minimizeToTray.value = gen.minimizeToTray
    logRetainDays.value = gen.logRetainDays || 7
  } catch (e: unknown) {
    showToast(`获取常规设置失败: ${getErrorMessage(e)}`)
  }
}

async function updateGeneralSettings() {
  saving.value = true
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
    saving.value = false
  }
}

onMounted(refresh)
</script>

<template>
  <section class="page">
    <PageHeader title="常规偏好" subtitle="开机自启、窗口关闭行为与脱敏日志轮转策略，修改后即时保存。" />

    <div class="card pref-list">
      <label class="setting-row setting-row-tappable">
        <span class="setting-main">
          <span class="setting-name">开机自动启动</span>
          <span class="setting-desc">开启后将在 Windows 启动时以最小化模式静默常驻后台</span>
        </span>
        <input type="checkbox" v-model="autoStart" @change="updateGeneralSettings" :disabled="saving" class="switch" />
      </label>

      <label class="setting-row setting-row-tappable">
        <span class="setting-main">
          <span class="setting-name">关闭主窗口时最小化到系统托盘</span>
          <span class="setting-desc">点击右上角关闭按钮时保留后台托盘与已启动的托管实例，而不是直接退出</span>
        </span>
        <input type="checkbox" v-model="minimizeToTray" @change="updateGeneralSettings" :disabled="saving" class="switch" />
      </label>

      <div class="setting-row">
        <span class="setting-main">
          <span class="setting-name">脱敏运行日志保留天数</span>
          <span class="setting-desc">自动清理过期日志文件，避免长期运行占用过多磁盘空间（1–90 天）</span>
        </span>
        <span class="input-inline">
          <input type="number" v-model.number="logRetainDays" @change="updateGeneralSettings" min="1" max="90" class="input-number" />
          <span class="input-unit">天</span>
        </span>
      </div>
    </div>
  </section>
</template>

<style scoped>
/* 行骨架复用全局 .setting-row / .card 原子（components.css），此处仅布局微调与控件皮 */
.pref-list { display: flex; flex-direction: column; gap: 8px; }

.switch { width: 18px; height: 18px; cursor: pointer; accent-color: var(--color-primary); flex: none; }
.input-inline { display: flex; align-items: center; gap: 6px; }
.input-number {
  width: 64px; padding: 5px 8px; border: 1px solid var(--color-border);
  border-radius: var(--radius-control); background: var(--surface-panel); color: var(--color-text); font-size: var(--text-base);
}
.input-unit { font-size: var(--text-sm); color: var(--color-text-muted); }
</style>
