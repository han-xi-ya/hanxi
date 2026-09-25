<script setup lang="ts">
// 设置分区·外观主题：明暗三分段（跟随系统 / 浅色 / 深色）+ 色板五分段（双轴，§7.2）。
// 拆分自原 SettingsView 单页：主题持久化走 useTheme 单例（后端 SetTheme/SetAccent +
// DOM data-theme/data-accent + 原生标题栏联动），本视图只做选择器，不碰持久化细节。
import { computed } from 'vue'
import { useTheme, type AccentMode } from '../../composables/useTheme'
import PageHeader from '../../components/ui/PageHeader.vue'
import AppIcon from '../../components/ui/AppIcon.vue'

const { themeMode, setThemeMode, accent, setAccent } = useTheme()

// 跟随系统时展示实际生效态，消解「选了 system 但界面是深色」的观感歧义
const hint = computed(() => {
  if (themeMode.value === 'system') return '当前随 Windows 亮暗设置自动切换'
  return themeMode.value === 'dark' ? '深色为独立标定的配色（含原生标题栏），不是简单反色' : '浅色为工作台默认标定'
})

// 色板预览圆点：取各色板浅色主色（唯一真相 docs/design/themes/preview.html）。
// 圆点必须独立于当前主题硬编码（否则选中别的色板后预览点跟着变色，失去预览意义），
// 属「裸色只在 tokens.css」铁律的登记豁免项——仅存于本视图数据源，不得扩散进组件配色。
const ACCENTS: ReadonlyArray<{ key: AccentMode; label: string; dot: string }> = [
  { key: 'teal', label: '青壳', dot: '#0d8284' },
  { key: 'sky', label: '碧空', dot: '#0064b5' },
  { key: 'iris', label: '鸢尾', dot: '#6741ca' },
  { key: 'jade', label: '青瓷', dot: '#006869' },
  { key: 'onyx', label: '曜石', dot: '#0649b8' },
]
</script>

<template>
  <section class="page">
    <PageHeader title="外观主题" subtitle="明暗与色板双轴持久化在后端设置中，随数据目录整体迁移；「跟随系统」将随 Windows 亮暗设置自动切换。" />

    <div class="card pref-list">
      <div class="setting-row">
        <span class="setting-main">
          <span class="setting-name">界面主题</span>
          <span class="setting-desc">{{ hint }}</span>
        </span>
        <div class="theme-seg seg-theme" role="radiogroup" aria-label="界面主题">
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

      <div class="setting-row">
        <span class="setting-main">
          <span class="setting-name">色板</span>
          <span class="setting-desc">与明暗轴正交的五套色板，即选即生效、持久化于后端设置</span>
        </span>
        <div class="theme-seg seg-accent" role="radiogroup" aria-label="色板">
          <button
            v-for="a in ACCENTS" :key="a.key"
            type="button" class="theme-seg-btn" :class="{ active: accent === a.key }"
            role="radio" :aria-checked="accent === a.key" @click="setAccent(a.key)"
          >
            <span class="accent-dot" :style="{ background: a.dot }" aria-hidden="true" />
            <span>{{ a.label }}</span>
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
  padding: 6px 14px; border-radius: 6px; font-size: var(--text-base); color: var(--color-text-muted);
  cursor: pointer; transition: background var(--motion-base) ease, color var(--motion-base) ease;
}
.theme-seg-btn:hover { color: var(--color-text); }
.theme-seg-btn.active { background: var(--surface-panel); color: var(--color-primary); font-weight: 600; box-shadow: var(--shadow-small); }

/* 色板五段较宽，窄窗口允许换行；圆点为色板预览硬编码值（见 script 注记） */
.seg-accent { flex-wrap: wrap; }
.accent-dot {
  flex: none; width: 12px; height: 12px; border-radius: var(--radius-pill);
  box-shadow: inset 0 0 0 1px var(--color-border-strong);
}
</style>
