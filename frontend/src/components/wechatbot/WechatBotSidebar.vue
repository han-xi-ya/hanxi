<script setup lang="ts">
// 微信机器人左侧账号栏：折叠开关 + 账号卡片列表（头像、监听状态灯、建联徽章、悬浮操作组）。
// 自 WechatBotView.vue 随 DOM 逐字迁出的纯展示壳——账号数据与全部业务动作
// （扫码绑定、切换监听、重命名、删除、选中同步）仍由 useWechatBot/视图编排层承担，
// 本组件只负责渲染与事件上抛；getAvatarColor 为跨组件共用呈现函数（自视图逐字迁入）。
import type { WechatAccountState } from '../../../bindings/hanxi/internal/modules/wechat/models'
import { getAvatarColor } from '../../composables/useWechatBot'

defineProps<{
  accounts: WechatAccountState[]
  /** 当前选中账号 id（原模板 currentAccount?.id 的等价传参）。 */
  currentAccountId: string
  /** 侧边栏折叠态（视图本地 UI 状态，头部展开按钮与其共用，故不上收本组件）。 */
  collapsed: boolean
}>()

const emit = defineEmits<{
  'select': [id: string]
  'toggle-collapse': []
  'bind': []
  'toggle-listener': [acc: WechatAccountState]
  'rename': [acc: WechatAccountState]
  'delete': [acc: WechatAccountState]
}>()
</script>

<template>
  <!-- 1. 左侧：账号与会话管理栏 -->
  <aside class="sidebar-pane" :class="{ collapsed: collapsed }">
    <!-- 侧边栏头部 -->
    <div class="sidebar-header">
      <div v-if="!collapsed" class="sidebar-title">
        <span class="logo-emoji">🤖</span>
        <h3>微信机器人</h3>
      </div>
      <div class="sidebar-header-actions">
        <button
          v-if="!collapsed"
          class="btn-bind-account"
          @click="emit('bind')"
          title="扫码绑定新微信账号"
        >
          <span class="plus-icon">+</span> 绑定账号
        </button>
        <button
          class="btn-toggle-sidebar"
          :title="collapsed ? '展开侧边栏' : '收起侧边栏'"
          @click="emit('toggle-collapse')"
        >
          {{ collapsed ? '▶' : '◀' }}
        </button>
      </div>
    </div>

    <!-- 账号列表容器 -->
    <div class="account-list-scroll">
      <div v-if="accounts.length === 0" class="empty-accounts">
        <div class="empty-icon">📱</div>
        <div v-if="!collapsed" class="empty-text">暂无绑定的微信账号</div>
        <button class="btn-empty-bind" @click="emit('bind')" :title="'立即扫码绑定'">
          {{ collapsed ? '+' : '立即扫码绑定' }}
        </button>
      </div>

      <div
        v-for="acc in accounts"
        :key="acc.id"
        class="account-item-card"
        :class="{ active: acc.id === currentAccountId, 'is-mini': collapsed }"
        :title="collapsed ? `${acc.remarkName} (${acc.isListening ? '监听中' : '未监听'})` : undefined"
        @click="emit('select', acc.id)"
      >
        <!-- 账号头像 (对应备注的微信号) -->
        <div class="bot-avatar" :style="{ backgroundColor: getAvatarColor(acc.remarkName || acc.id) }">
          {{ (acc.remarkName || '微').slice(0, 1) }}
          <span
            class="status-dot-badge"
            :class="{ online: acc.isListening, offline: !acc.isListening }"
            :title="acc.isListening ? '监听中' : '未监听'"
          ></span>
        </div>

        <!-- 账号信息摘要 (展开状态下显示) -->
        <template v-if="!collapsed">
          <div class="account-meta">
            <div class="meta-row-main">
              <span class="bot-remark" :title="acc.remarkName">{{ acc.remarkName }}</span>
              <span class="token-status-badge" :class="{ ready: !!acc.contextToken }">
                {{ acc.contextToken ? '已建联' : '待激活' }}
              </span>
            </div>
            <div class="meta-row-sub">
              <span class="account-sub-id" :title="acc.ilinkUserId || acc.id">
                {{ acc.ilinkUserId ? acc.ilinkUserId.split('@')[0] : acc.id }}
              </span>
            </div>
          </div>

          <!-- 悬浮快捷操作按钮组 -->
          <div class="hover-actions-group" @click.stop>
            <button
              class="action-icon-btn"
              :title="acc.isListening ? '暂停监听' : '启动监听'"
              @click="emit('toggle-listener', acc)"
            >
              {{ acc.isListening ? '⏸' : '▶' }}
            </button>
            <button class="action-icon-btn" title="重命名" @click="emit('rename', acc)">✏️</button>
            <button class="action-icon-btn danger" title="删除账号" @click="emit('delete', acc)">🗑️</button>
          </div>
        </template>
      </div>
    </div>
  </aside>
</template>

<style scoped>
/* 以下样式自 WechatBotView.vue 原 scoped 块随标记逐字迁移，声明与 token 引用不动 */
.sidebar-pane {
  width: 270px;
  background: var(--surface-soft);
  border-right: 1px solid var(--color-border);
  display: flex;
  flex-direction: column;
  flex-shrink: 0;
  transition: width 0.2s cubic-bezier(0.4, 0, 0.2, 1);
}

.sidebar-pane.collapsed {
  width: 64px;
}

.sidebar-header {
  padding: 12px 10px;
  display: flex;
  justify-content: space-between;
  align-items: center;
  border-bottom: 1px solid var(--color-border);
  background: var(--surface-panel);
  min-height: 48px;
}

.sidebar-header-actions {
  display: flex;
  align-items: center;
  gap: 6px;
}

.btn-toggle-sidebar {
  background: transparent;
  border: 1px solid var(--color-border);
  border-radius: 4px;
  color: var(--color-text-muted);
  font-size: 11px;
  width: 24px;
  height: 24px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
  transition: all 0.15s ease;
}

.btn-toggle-sidebar:hover {
  background: var(--surface-hover);
  color: var(--color-text);
}

.sidebar-title {
  display: flex;
  align-items: center;
  gap: 6px;
}

.logo-emoji {
  font-size: 16px;
}

.sidebar-title h3 {
  font-size: 14px;
  font-weight: 600;
  color: var(--color-text);
  margin: 0;
}

/* 注：.btn-bind-account 家族与视图本体「未选中占位」大按钮共用（跨组件边界），
   按「标记与样式同迁、不做语义改动」原则在两侧各留一份逐字副本（§9.6 登记）。 */
.btn-bind-account {
  background: var(--state-positive);
  color: var(--color-text-inverse);
  border: none;
  padding: 4px 10px;
  border-radius: 6px;
  font-size: 12px;
  font-weight: 500;
  cursor: pointer;
  display: inline-flex;
  align-items: center;
  gap: 3px;
  transition: all 0.15s ease;
}

.btn-bind-account:hover {
  background: var(--state-positive);
}

.btn-bind-account.large {
  padding: 8px 18px;
  font-size: 13px;
  margin-top: 14px;
}

.account-list-scroll {
  flex: 1;
  overflow-y: auto;
  padding: 8px;
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.empty-accounts {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  padding: 32px 8px;
  text-align: center;
  color: var(--color-text-muted);
}

.empty-icon {
  font-size: 28px;
  margin-bottom: 6px;
}

.empty-text {
  font-size: 13px;
  margin-bottom: 12px;
}

.btn-empty-bind {
  background: var(--state-positive);
  color: var(--color-text-inverse);
  border: none;
  padding: 5px 14px;
  border-radius: 6px;
  font-size: 12px;
  font-weight: 500;
  cursor: pointer;
}

.account-item-card {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 9px 10px;
  border-radius: 8px;
  cursor: pointer;
  transition: all 0.15s ease;
  background: transparent;
  position: relative;
}

.account-item-card.is-mini {
  justify-content: center;
  padding: 8px 0;
}

.account-item-card:hover {
  background: var(--surface-hover);
}

.account-item-card.active {
  background: var(--surface-hover);
}

.bot-avatar {
  width: 38px;
  height: 38px;
  border-radius: 8px;
  color: #ffffff; /* 功能例外：数据值头像底上恒白不随主题 */
  display: flex;
  align-items: center;
  justify-content: center;
  font-weight: 600;
  font-size: 15px;
  position: relative;
  flex-shrink: 0;
}

.status-dot-badge {
  position: absolute;
  bottom: -2px;
  right: -2px;
  width: 10px;
  height: 10px;
  border-radius: 50%;
  border: 2px solid var(--surface-panel);
}

.status-dot-badge.online {
  background: var(--state-positive);
}

.status-dot-badge.offline {
  background: var(--color-text-subtle);
}

.account-meta {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 3px;
}

.meta-row-main {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 4px;
}

.bot-remark {
  font-size: 13px;
  font-weight: 600;
  color: var(--color-text);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.token-status-badge {
  font-size: 10px;
  padding: 1px 5px;
  border-radius: 4px;
  background: var(--surface-hover);
  color: var(--color-text-muted);
  flex-shrink: 0;
}

.token-status-badge.ready {
  background: var(--state-positive-soft);
  color: var(--state-positive);
}

.meta-row-sub {
  display: flex;
  align-items: center;
}

.account-sub-id {
  font-size: 11px;
  color: var(--color-text-subtle);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-family: var(--font-mono);
}

.hover-actions-group {
  display: none;
  align-items: center;
  gap: 2px;
}

.account-item-card:hover .hover-actions-group {
  display: flex;
}

.action-icon-btn {
  background: transparent;
  border: none;
  font-size: 12px;
  padding: 3px 5px;
  border-radius: 4px;
  cursor: pointer;
  color: var(--color-text-muted);
  transition: all 0.1s;
}

.action-icon-btn:hover {
  background: var(--surface-hover);
}

.action-icon-btn.danger:hover {
  background: var(--state-danger-soft);
  color: var(--state-danger);
}
</style>
