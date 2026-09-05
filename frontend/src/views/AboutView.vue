<script setup lang="ts">
import { ref, onMounted } from 'vue'
import * as AppAPI from '../../bindings/hanxi/internal/app'
import type { AppInfo } from '../../bindings/hanxi/internal/app'
import type { ModuleInfo } from '../../bindings/hanxi/internal/extapi/models'
import { getErrorMessage } from '../utils/errors'

const info = ref<AppInfo | null>(null)
const modules = ref<ModuleInfo[]>([])
const loading = ref(true)
const loadError = ref('')

onMounted(async () => {
  try {
    const [appInfo, moduleList] = await Promise.all([
      AppAPI.AppService.GetAppInfo(),
      AppAPI.AppService.ListModules(),
    ])
    info.value = appInfo
    modules.value = moduleList ?? []
  } catch (error: unknown) {
    // 错误归一入口全站统一（原 instanceof 手写分支语义等价，收编进 getErrorMessage）
    loadError.value = getErrorMessage(error)
  } finally {
    loading.value = false
  }
})
</script>

<template>
  <section class="page about-page">
    <header class="about-header">
      <div class="product-mark" aria-hidden="true">HX</div>
      <div>
        <p class="eyebrow">开源工具工作台</p>
        <h1>{{ info?.name ?? 'Hanxi' }}</h1>
        <p class="product-description">
          {{ info?.description ?? '集中安装、管理与运行常用开源软件' }}
        </p>
      </div>
    </header>

    <div v-if="loading" class="state-panel">正在加载产品信息…</div>
    <div v-else-if="loadError" class="state-panel error-panel">加载产品信息失败：{{ loadError }}</div>

    <template v-else>
      <section v-if="info" class="info-panel" aria-label="运行信息">
        <div class="info-item">
          <span>版本</span>
          <code>{{ info.version }}</code>
        </div>
        <div class="info-item">
          <span>平台</span>
          <code>{{ info.goos }}/{{ info.goarch }}</code>
        </div>
        <div class="info-item">
          <span>运行模式</span>
          <code>{{ info.mode === 'portable' ? '便携模式' : '标准模式' }}</code>
        </div>
        <div class="info-item info-path">
          <span>数据目录</span>
          <code>{{ info.baseDir }}</code>
        </div>
      </section>

      <section class="modules-panel">
        <div class="section-heading">
          <div>
            <h2>集成工具</h2>
            <p>所有工具均以平等模块接入，可按需启用与停用。</p>
          </div>
          <span class="module-count">{{ modules.length }} 项</span>
        </div>

        <div v-if="modules.length" class="module-list">
          <div v-for="module in modules" :key="module.id" class="module-row">
            <div class="module-main">
              <strong>{{ module.name }}</strong>
              <span>{{ module.description }}</span>
            </div>
            <div class="module-meta">
              <code>v{{ module.version }}</code>
              <span class="status-badge" :class="module.enabled ? 'enabled' : 'disabled'">
                {{ module.enabled ? '已启用' : '未启用' }}
              </span>
            </div>
          </div>
        </div>
        <div v-else class="state-panel">暂无已注册工具。</div>
      </section>
    </template>
  </section>
</template>

<style scoped>
.about-page {
  max-width: 920px;
}

.about-header {
  display: flex;
  align-items: center;
  gap: 16px;
  margin-bottom: 20px;
}

.product-mark {
  width: 52px;
  height: 52px;
  display: grid;
  place-items: center;
  flex: 0 0 52px;
  border: 1px solid color-mix(in srgb, var(--color-primary) 28%, var(--color-border));
  border-radius: 14px;
  background: color-mix(in srgb, var(--color-primary) 10%, var(--surface-panel));
  color: var(--color-primary);
  font-size: 16px;
  font-weight: 750;
}

.eyebrow {
  margin: 0 0 4px;
  color: var(--color-primary);
  font-size: 12px;
  font-weight: 650;
}

h1,
h2,
p {
  margin-top: 0;
}

h1 {
  margin-bottom: 6px;
  font-size: 24px;
}

.product-description,
.section-heading p {
  margin-bottom: 0;
  color: var(--color-text-muted);
}

.info-panel,
.modules-panel,
.state-panel {
  border: 1px solid var(--color-border);
  border-radius: 12px;
  background: var(--surface-panel);
}

.info-panel {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  margin-bottom: 16px;
  overflow: hidden;
}

.info-item {
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding: 14px 16px;
  border-right: 1px solid var(--color-border);
}

.info-item span {
  color: var(--color-text-subtle);
  font-size: 12px;
}

.info-item code,
.module-meta code {
  color: var(--color-text);
  font-family: var(--font-mono);
  font-size: 12px;
}

.info-path {
  grid-column: 1 / -1;
  border-top: 1px solid var(--color-border);
  border-right: 0;
}

.info-path code {
  overflow-wrap: anywhere;
}

.modules-panel {
  padding: 16px;
}

.section-heading {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  margin-bottom: 12px;
}

.section-heading h2 {
  margin-bottom: 4px;
  font-size: 16px;
}

.section-heading p {
  font-size: 13px;
}

.module-count,
.status-badge {
  flex: 0 0 auto;
  border-radius: 999px;
  font-size: 11px;
  font-weight: 650;
}

.module-count {
  padding: 4px 9px;
  background: var(--surface-hover);
  color: var(--color-text-muted);
}

.module-list {
  border: 1px solid var(--color-border);
  border-radius: 9px;
  overflow: hidden;
}

.module-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  min-height: 54px;
  padding: 11px 13px;
  background: var(--surface-panel);
}

.module-row + .module-row {
  border-top: 1px solid var(--color-border);
}

.module-main {
  display: flex;
  min-width: 0;
  flex-direction: column;
  gap: 3px;
}

.module-main strong {
  font-size: 13px;
}

.module-main span {
  overflow: hidden;
  color: var(--color-text-muted);
  font-size: 12px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.module-meta {
  display: flex;
  align-items: center;
  gap: 10px;
}

.status-badge {
  padding: 4px 8px;
  border: 1px solid currentColor;
}

.status-badge.enabled {
  color: var(--state-positive);
  background: color-mix(in srgb, var(--state-positive) 8%, transparent);
}

.status-badge.disabled {
  color: var(--color-text-subtle);
  background: var(--surface-hover);
}

.state-panel {
  padding: 18px;
  color: var(--color-text-muted);
}

.error-panel {
  color: var(--state-danger);
}

@media (max-width: 720px) {
  .info-panel {
    grid-template-columns: 1fr;
  }

  .info-item {
    border-right: 0;
    border-bottom: 1px solid var(--color-border);
  }

  .info-path {
    grid-column: auto;
    border-top: 0;
    border-bottom: 0;
  }

  .module-row {
    align-items: flex-start;
    flex-direction: column;
  }

  .module-meta {
    width: 100%;
    justify-content: space-between;
  }
}
</style>
