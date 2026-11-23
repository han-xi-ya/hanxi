<script setup lang="ts">
// 设置分区·常规偏好：开机自启 / 关窗行为 / 日志保留天数。
// 改动即写后端 SetGeneralSettings，失败回滚重拉；提示走全局顶层卡片（useToast），
// 本页不挂局部 toast。
import { ref, onMounted } from 'vue'
import * as AppAPI from '../../../bindings/hanxi/internal/app'
import { getErrorMessage } from '../../utils/errors'
import { useToast } from '../../composables/useToast'
import PageHeader from '../../components/ui/PageHeader.vue'

const { showToast, showErrorToast } = useToast()

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
    showErrorToast(`获取常规设置失败: ${getErrorMessage(e)}`)
  }
}

/** 数字框 clamp：越界/清空先写回 [min,max] 再落盘，杜绝「所见非所存」
 *（Number()||fallback 会把 0 静默改写落盘而输入框仍显 0）。 */
function clampNumber(value: number | string, min: number, max: number, fallback: number): number {
  const n = Number(value)
  if (!Number.isFinite(n)) return fallback
  return Math.min(max, Math.max(min, Math.trunc(n)))
}

function onLogRetainChange() {
  logRetainDays.value = clampNumber(logRetainDays.value, 1, 90, 7)
  void updateGeneralSettings()
}

async function updateGeneralSettings() {
  saving.value = true
  try {
    await AppAPI.AppService.SetGeneralSettings({
      autoStart: autoStart.value,
      minimizeToTray: minimizeToTray.value,
      logRetainDays: clampNumber(logRetainDays.value, 1, 90, 7),
    })
    showToast('常规偏好设置已更新')
  } catch (e: unknown) {
    showErrorToast(`保存设置失败: ${getErrorMessage(e)}`)
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
          <span v-if="logRetainDays < 1 || logRetainDays > 90" class="range-hint" role="alert">超出 1–90 范围，保存时将修正到边界</span>
        </span>
        <span class="input-inline">
          <input type="number" v-model.number="logRetainDays" @change="onLogRetainChange" min="1" max="90" class="input-number" />
          <span class="input-unit">天</span>
        </span>
      </div>
    </div>
  </section>
</template>

<style scoped>
/* 行骨架与控件皮（.setting-row / .card / .switch / .input-number / .input-unit /
   .pref-list）均已落回 components.css 全局原子；此处只留本区差分：
   天数框 64px 窄档，以及 .input-inline——后者因 LanScannerView 同名异形
   （行内编辑框本体）未上收全局，收了的漏染风险见 components.css 同段注记 */
.input-inline { display: flex; align-items: center; gap: 6px; }
.input-number { width: 64px; }
/* 越界即时反馈：交互后仍不合法才描边（:user-invalid 不扰未触碰的框），
   焦点环仍归 base.css 全局 :focus-visible */
.input-number:user-invalid { border-color: var(--state-danger); }
.range-hint { font-size: var(--text-xs); color: var(--state-danger); }
</style>
