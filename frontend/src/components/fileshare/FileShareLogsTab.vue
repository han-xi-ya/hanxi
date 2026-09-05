<script setup lang="ts">
// 实时传输审计页签：局域网访问与传输事件表（保留最新 50 条，上限逻辑在 useFileShareServer）。
import type { TransferEvent } from '../../../bindings/hanxi/internal/modules/fileshare/models'
import { formatBytes } from '../../composables/fileShareFormat'

defineProps<{
  /** 审计日志（原 transferLogs）。 */
  logs: TransferEvent[]
}>()
</script>

<template>
  <div class="tab-content">
    <div class="card">
      <div class="card-header flex-between">
        <h3 class="card-title">📋 局域网访问与传输审计</h3>
        <span class="text-muted text-sm font-mono">保留最新 50 条</span>
      </div>
      <div v-if="logs.length === 0" class="empty-state py-8">
        <div class="empty-icon">📜</div>
        <h3>暂无传输事件</h3>
        <p>客户端下载或上传文件时，此处将实时展示 IP、文件名、传输大小与状态。</p>
      </div>
      <div v-else class="table-responsive">
        <table class="table">
          <thead>
            <tr>
              <th>时间</th>
              <th>类型</th>
              <th>文件名 / 内容</th>
              <th>大小</th>
              <th>客户端 IP</th>
              <th>状态</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="(log, idx) in logs" :key="idx">
              <td class="font-mono text-xs">{{ new Date(log.timestamp).toLocaleTimeString() }}</td>
              <td>
                <span
                  class="tag-pill"
                  :class="{
                    'tag-success': log.type === 'upload',
                    'tag-blue': log.type === 'download',
                    'tag-amber': log.type === 'drop',
                  }"
                >
                  {{ log.type === 'upload' ? '↑ 上传' : (log.type === 'download' ? '↓ 下载' : '投递') }}
                </span>
              </td>
              <td class="font-mono text-sm max-w-xs truncate" :title="log.filename">
                {{ log.filename }}
              </td>
              <td class="font-mono text-xs">{{ log.size > 0 ? formatBytes(log.size) : '-' }}</td>
              <td class="font-mono text-xs">{{ log.clientIp }}</td>
              <td>
                <span :class="log.success ? 'text-success' : 'text-danger'">
                  {{ log.success ? '✓ 成功' : '✕ 失败' }}
                </span>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  </div>
</template>

<style scoped>
/* 以下样式自 FileShareView.vue 原 scoped 块随标记逐字迁移，声明与 token 引用不动。
   §9.6-3 治理：flex-between/tag-pill/tag-blue 与语义色 text-success 已上收 components.css；
   .py-8/.text-danger/.text-muted/.empty-state/.empty-icon 按裁决保留局部（见 §9.6 报告）。 */
.table {
  width: 100%;
  border-collapse: collapse;
}

.table th,
.table td {
  padding: 10px 12px;
  text-align: left;
  border-bottom: 1px solid var(--color-border);
}

.table th {
  font-size: 12px;
  font-weight: 600;
  color: var(--color-text-muted);
  background: var(--surface-soft);
}

/* .flex-between/.tag-pill/.tag-blue/.text-success 已上收 components.css（§9.6-3）；
   .py-8/.text-danger/.text-muted 留局部（同名不同形或无定义使用点，见 §9.6 报告） */
.py-8 { padding-top: 32px; padding-bottom: 32px; }
.tag-success { background: var(--state-positive-soft); color: var(--state-positive); }
.tag-amber { background: var(--state-warning-soft); color: var(--state-warning); }

.text-danger { color: var(--state-danger); }
.text-muted { color: var(--color-text-subtle); }

.empty-state {
  text-align: center;
  color: var(--color-text-subtle);
}

.empty-icon {
  font-size: 36px;
  margin-bottom: 8px;
}

.workspace-body > .tab-content > .card {
  border-radius: 12px;
  box-shadow: none;
}

.table-responsive {
  max-width: 100%;
  overflow-x: auto;
  border: 1px solid var(--color-border);
  border-radius: 9px;
}

.table {
  min-width: 720px;
}

.empty-state {
  padding: 44px 20px;
  background: var(--surface-panel);
  border: 1px dashed var(--color-border);
  border-radius: 12px;
}

.empty-state h3 {
  margin: 0 0 7px;
  color: var(--color-text);
  font-size: 15px;
}

.empty-state p {
  max-width: 620px;
  margin: 0 auto;
  font-size: 12px;
  line-height: 1.6;
}
</style>
