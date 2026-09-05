<script setup lang="ts">
// 快传服务概览指标卡：服务状态 / 实时速率 / 累计传输 / 跨端投递四张卡。
// 纯展示壳——数值来自 useFileShareServer 状态，投递条数由视图层以 inboxCount 传入。
import type { ServerStatus } from '../../../bindings/hanxi/internal/modules/fileshare/models'
import { formatBytes, formatSpeed } from '../../composables/fileShareFormat'

defineProps<{
  status: ServerStatus
  /** 投递箱条数（原模板 dropInbox.length）。 */
  inboxCount: number
}>()
</script>

<template>
  <section class="overview-grid section-gap" aria-label="快传服务概览">
    <article class="overview-card overview-card-primary">
      <div class="metric-icon" aria-hidden="true">⌁</div>
      <div>
        <div class="stat-label">服务状态</div>
        <div class="stat-val">{{ status.isRunning ? '在线可访问' : '等待启动' }}</div>
        <div class="stat-sub">{{ status.activeConnections }} 个活跃连接</div>
      </div>
    </article>
    <article class="overview-card">
      <div class="metric-icon metric-icon-green" aria-hidden="true">↕</div>
      <div>
        <div class="stat-label">实时传输速率</div>
        <div class="stat-val stat-val-compact font-mono">
          <span class="text-upload">↑ {{ formatSpeed(status.uploadRate) }}</span>
          <span class="text-download">↓ {{ formatSpeed(status.downloadRate) }}</span>
        </div>
        <div class="stat-sub">上传 / 下载</div>
      </div>
    </article>
    <article class="overview-card">
      <div class="metric-icon metric-icon-blue" aria-hidden="true">Σ</div>
      <div>
        <div class="stat-label">累计传输</div>
        <div class="stat-val stat-val-compact font-mono">
          {{ formatBytes(status.uploadBytes + status.downloadBytes) }}
        </div>
        <div class="stat-sub">{{ status.uploadCount }} 次上传 · {{ status.downloadCount }} 次下载</div>
      </div>
    </article>
    <article class="overview-card">
      <div class="metric-icon metric-icon-amber" aria-hidden="true">✦</div>
      <div>
        <div class="stat-label">跨端投递</div>
        <div class="stat-val">{{ inboxCount }} <span class="unit">条</span></div>
        <div class="stat-sub">文本与链接碎片</div>
      </div>
    </article>
  </section>
</template>

<style scoped>
/* 以下样式自 FileShareView.vue 原 scoped 块随标记逐字迁移，声明与 token 引用不动 */
.stat-label {
  font-size: 12px;
  color: var(--color-text-muted);
  margin-bottom: 6px;
}

.stat-val {
  font-size: 20px;
  font-weight: 700;
  color: var(--color-text);
  margin-bottom: 4px;
}

.stat-val .unit {
  font-size: 13px;
  font-weight: normal;
  color: var(--color-text-muted);
}

.stat-sub {
  font-size: 11px;
  color: var(--color-text-subtle);
}

.text-upload {
  color: var(--state-positive);
}

.text-download {
  color: var(--state-information);
}

.overview-grid {
  display: grid;
  grid-template-columns: 1.2fr repeat(3, 1fr);
  gap: 12px;
}

.overview-card {
  display: flex;
  align-items: center;
  gap: 14px;
  min-width: 0;
  padding: 16px;
  background: var(--surface-panel);
  border: 1px solid var(--color-border);
  border-radius: 13px;
  transition: transform 0.18s ease, border-color 0.18s ease, box-shadow 0.18s ease;
}

.overview-card:hover {
  transform: translateY(-1px);
  border-color: var(--state-information-soft);
  box-shadow: 0 8px 22px var(--shadow-panel);
}

.overview-card-primary {
  background: linear-gradient(135deg, var(--color-primary-soft), var(--surface-panel));
  border-color: var(--color-primary-glow);
}

.metric-icon {
  display: inline-flex;
  flex: 0 0 auto;
  align-items: center;
  justify-content: center;
  width: 40px;
  height: 40px;
  color: var(--color-primary);
  font-size: 19px;
  font-weight: 700;
  background: var(--color-primary-soft);
  border-radius: 11px;
}

.metric-icon-green { color: var(--state-positive); background: var(--state-positive-soft); }
.metric-icon-blue { color: var(--state-information); background: var(--state-information-soft); }
.metric-icon-amber { color: var(--state-warning); background: var(--state-warning-soft); }

.stat-label { margin-bottom: 4px; }
.stat-val { margin-bottom: 3px; font-size: 18px; }
.stat-val-compact {
  display: flex;
  flex-wrap: wrap;
  gap: 3px 10px;
  font-size: 13px;
}

@media (max-width: 1100px) {
  .overview-grid {
    grid-template-columns: repeat(2, 1fr);
  }
}

@media (max-width: 760px) {
  .overview-grid {
    grid-template-columns: 1fr;
  }
}

@media (max-width: 460px) {
  .overview-grid {
    grid-template-columns: 1fr;
  }
}
</style>
