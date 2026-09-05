<script setup lang="ts">
// 跨端投递箱页签：移动端投递的文本/链接记录列表（复制/删除/清空动作 emit 回视图编排层）。
import type { DropItem } from '../../../bindings/hanxi/internal/modules/fileshare/models'

defineProps<{
  /** 投递条目（原 dropInbox）。 */
  items: DropItem[]
}>()

const emit = defineEmits<{ copy: [content: string]; remove: [id: string]; clear: [] }>()
</script>

<template>
  <div class="tab-content">
    <div class="card">
      <div class="card-header flex-between">
        <h3 class="card-title">📱 手机投递文本与链接记录</h3>
        <button
          v-if="items.length > 0"
          class="btn-secondary btn-sm"
          @click="emit('clear')"
        >
          🗑️ 清空投递箱
        </button>
      </div>
      <div v-if="items.length === 0" class="empty-state py-8">
        <div class="empty-icon">📭</div>
        <h3>暂无投递内容</h3>
        <p>在手机扫码打开的网页中输入任意文本或链接，点击「立即投递」即可秒级传送到此处。</p>
      </div>
      <div v-else class="inbox-list">
        <div v-for="item in items" :key="item.id" class="inbox-item">
          <div class="inbox-main">
            <div class="inbox-header flex-between mb-1">
              <div class="inbox-source text-muted">
                来自 {{ item.senderIp }}
                <span v-if="item.isUrl" class="tag-pill tag-blue ml-2">🌐 网址链接</span>
                <span v-else class="tag-pill ml-2">📝 纯文本</span>
              </div>
              <div class="inbox-time font-mono text-muted text-sm">
                {{ new Date(item.createdAt).toLocaleTimeString() }}
              </div>
            </div>
            <div class="inbox-content font-mono select-all">
              {{ item.content }}
            </div>
          </div>
          <div class="inbox-item-actions flex gap-2">
            <button
              class="btn-secondary btn-sm"
              @click="emit('copy', item.content)"
            >
              📋 复制
            </button>
            <button
              class="btn-secondary btn-sm text-danger"
              @click="emit('remove', item.id)"
            >
              🗑️ 删除
            </button>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
/* 以下样式自 FileShareView.vue 原 scoped 块随标记逐字迁移，声明与 token 引用不动。
   §9.6-3 治理：跨页签逐字同形方言原子（flex-between/gap-2/ml-2/btn-sm/tag-pill/tag-blue）
   已上收 components.css；.btn-secondary 家族/.empty-state/.py-8/.text-muted/.mb-1/
   .empty-icon/.text-danger 按裁决保留局部（同名不同形或单份副本）。 */
.inbox-list {
  display: flex;
  flex-direction: column;
  gap: 12px;
  margin-top: 12px;
}

.inbox-item {
  background: var(--surface-soft);
  border: 1px solid var(--color-border);
  border-radius: 8px;
  padding: 12px 16px;
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 16px;
}

.inbox-main {
  flex: 1;
}

.inbox-content {
  background: var(--surface-panel);
  padding: 8px 12px;
  border-radius: 6px;
  border: 1px solid var(--color-border);
  font-size: 13px;
  white-space: pre-wrap;
  word-break: break-all;
  max-height: 140px;
  overflow-y: auto;
}

/* .flex-between/.gap-2/.ml-2 已上收 components.css（§9.6-3）；
   .mb-1 本目录仅此一份、.py-8 留局部（MemoView 无定义同名使用点，见 §9.6 报告） */
.mb-1 { margin-bottom: 4px; }
.py-8 { padding-top: 32px; padding-bottom: 32px; }

.btn-secondary {
  background: var(--surface-panel);
  border: 1px solid var(--color-border);
  color: var(--color-text);
  border-radius: 6px;
  padding: 6px 12px;
  font-size: 12px;
  cursor: pointer;
}

.btn-secondary:hover {
  background: var(--surface-soft);
}

/* .btn-sm/.tag-pill/.tag-blue 已上收 components.css（§9.6-3） */

/* .text-danger/.text-muted 留局部：存在无定义同名使用点（MemoView）或不同形副本
   （HomeView text-muted），裸收会改其观感，见 §9.6 待裁决 */
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

.inbox-item {
  border-radius: 10px;
}

.btn-secondary {
  transition: transform 0.15s ease, border-color 0.15s ease, background 0.15s ease, box-shadow 0.15s ease;
}

.btn-secondary:hover:not(:disabled) {
  transform: translateY(-1px);
}

.btn-secondary:focus-visible {
  outline: 2px solid var(--color-primary-glow);
  outline-offset: 2px;
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

@media (max-width: 760px) {
  .inbox-item {
    align-items: stretch;
    flex-direction: column;
  }

  .inbox-item-actions {
    justify-content: flex-end;
  }
}
</style>
