<script setup lang="ts">
// 设置分区·外观主题：三分段（跟随系统 / 浅色 / 深色）。
// 拆分自原 SettingsView 单页：主题持久化走 useTheme 单例（后端 SetTheme +
// DOM data-theme + 原生标题栏联动），本视图只做选择器，不碰持久化细节。
import { computed } from 'vue'
import { useTheme } from '../../composables/useTheme'
import PageHeader from '../../components/ui/PageHeader.vue'
import AppIcon from '../../components/ui/AppIcon.vue'

const { themeMode, setThemeMode } = useTheme()

// 跟随系统时展示实际生效态，消解「选了 system 但界面是深色」的观感歧义
const hint = computed(() => {
  if (themeMode.value === 'system') return '当前随 Windows 亮暗设置自动切换'
  return themeMode.value === 'dark' ? '深色为独立标定的配色（含原生标题栏），不是简单反色' : '浅色为工作台默认标定'
})
</script>

<template>
  <section class="page">
    <PageHeader title="外观主题" subtitle="主题持久化在后端设置中，便携模式随 data/ 目录迁移；「跟随系统」将随 Windows 亮暗设置自动切换。" />

    <div class="card pref-list">
      <div class="setting-row">
        <span class="setting-main">
          <span class="setting-name">界面主题</span>
          <span class="setting-desc">{{ hint }}</span>
        </span>
        <div class="theme-seg" role="radiogroup" aria-label="界面主题">
          <button
            type="button" class="theme-seg-btn" :class="{ active: themeMode === 'system' }"
            role="radio" :aria-checked="themeMode === 'system'" @click="setThemeMode('system')"
          >
            <AppIcon name="monitor" :size="14" />
            <span>跟随系统</span>
          </button>
          <button
            type="button" class="theme-seg-btn" :class="{ active: themeMode === 'light' }"
            role="radio" :aria-checked="themeMode === 'light'" @click="setThemeMode('light')"
          >
            <AppIcon name="sun" :size="14" />
            <span>浅色</span>
          </button>
          <button
            type="button" class="theme-seg-btn" :class="{ active: themeMode === 'dark' }"
            role="radio" :aria-checked="themeMode === 'dark'" @click="setThemeMode('dark')"
          >
            <AppIcon name="moon" :size="14" />
            <span>深色</span>
          </button>
        </div>
      </div>
    </div>
  </section>
</template>

<style scoped>
.pref-list { display: flex; flex-direction: column; gap: 8px; }

/* 分段控件：语义 token 直引 */
.theme-seg { display: flex; background: var(--surface-hover); border: 1px solid var(--color-border); border-radius: var(--radius-control); padding: 3px; gap: 2px; }
.theme-seg-btn {
  display: inline-flex; align-items: center; gap: 6px; border: none; background: transparent;
  padding: 6px 14px; border-radius: 6px; font-size: 13px; color: var(--color-text-muted);
  cursor: pointer; transition: background var(--motion-base) ease, color var(--motion-base) ease;
}
.theme-seg-btn:hover { color: var(--color-text); }
.theme-seg-btn.active { background: var(--surface-panel); color: var(--color-primary); font-weight: 600; box-shadow: var(--shadow-small); }
</style>
