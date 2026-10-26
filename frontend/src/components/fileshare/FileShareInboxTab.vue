<script setup lang="ts">
// 跨端投递箱页签：移动端投递的文本/链接记录列表（复制/删除/清空动作 emit 回视图编排层）。
import type { DropItem } from '../../../bindings/hanxi/internal/modules/fileshare/models'

defineProps<{
  /** 移动端投递的文本/链接条目列表（视图侧 dropInbox）。 */
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
          class="btn btn-secondary btn-small"
          @click="emit('clear')"
        >
          🗑️ 清空投递箱
        </button>
      </div>
      <div v-if="items.length === 0" class="empty-state">
        <div class="empty-icon">📭</div>
        <h3>暂无投递内容</h3>
        <p>在手机扫码打开的网页中输入任意文本或链接，点击「立即投递」即可秒级传送到此处。</p>
      </div>
      <div v-else class="inbox-list">
        <div v-for="item in items" :key="item.id" class="inbox-item">
          <div class="inbox-main">
            <div class="inbox-header flex-between mb-1">
              <div class="inbox-source text-subtle">
                来自 {{ item.senderIp }}
                <span v-if="item.isUrl" class="tag-pill tag-blue ml-2">🌐 网址链接</span>
                <span v-else class="tag-pill ml-2">📝 纯文本</span>
              </div>
              <div class="inbox-time font-mono text-subtle text-sm">
                {{ new Date(item.createdAt).toLocaleTimeString() }}
              </div>
            </div>
            <div class="inbox-content font-mono select-all">
              {{ item.content }}
            </div>
          </div>
          <div class="inbox-item-actions flex gap-2">
            <button
              class="btn btn-secondary btn-small"
              @click="emit('copy', item.content)"
            >
              📋 复制
            </button>
            <button
              class="btn btn-secondary btn-small text-danger"
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
/* 原子归一在册（§9.6-3/§9.6-10）：跨页签逐字同形方言原子（flex-between/gap-2/ml-2/
   btn-sm/tag-pill/tag-blue）与 .btn-secondary 家族、.text-danger/.text-subtle 均走全局，
   本件不挂副本；按裁决保留局部的只有 .empty-state/.mb-1/.empty-icon（同名不同形或单份副本）。 */
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
  border-radius: var(--radius-chip);
  border: 1px solid var(--color-border);
  font-size: var(--text-base);
  white-space: pre-wrap;
  word-break: break-all;
  max-height: 140px;
  overflow-y: auto;
}

/* .mb-1 本目录仅此一份，留局；.py-8 副本曾被后位 .empty-state padding 级联压死，
   挂点已删（§9.6-10 在 FileShare 侧清零）。 */
.mb-1 { margin-bottom: 4px; }

.empty-icon {
  font-size: var(--text-3xl);
  margin-bottom: 8px;
}

.workspace-body > .tab-content > .card {
  border-radius: 12px;
  box-shadow: none;
}

.inbox-item {
  border-radius: 10px;
}

/* 空态居中/虚线卡基形由全局 .empty-state 承担，本块有效差仅加大号 44px 内距档
   （与全局 24px 差 >±2px，且带 icon+h3 结构；是否入全局 .empty-state-lg 待裁） */
.empty-state {
  padding: 44px 20px;
  background: var(--surface-panel);
  border: 1px dashed var(--color-border);
  border-radius: 12px;
}

.empty-state h3 {
  margin: 0 0 7px;
  color: var(--color-text);
  font-size: var(--text-md);
}

.empty-state p {
  max-width: 620px;
  margin: 0 auto;
  font-size: var(--text-sm);
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
