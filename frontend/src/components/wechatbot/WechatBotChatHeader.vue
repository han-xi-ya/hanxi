<script setup lang="ts">
// 微信机器人聊天窗口顶部导航栏：账号头像/名称/监听徽章 + 目标用户内联编辑条 + 右侧
// 刷新凭据与监听开关按钮。自 WechatBotView.vue 随 DOM 逐字迁出的纯展示壳——
// 目标用户保存、Token 刷新、监听切换等业务动作全部上抛视图编排层（useWechatBot）执行；
// 折叠态与编辑态为视图本地 UI 状态，经 props 传入、动作上抛；受控输入回写父级 ref，
// 与原 v-model 指向同一 ref 的语义逐字等价。
import { computed } from 'vue'
import type { WechatAccountState } from '../../../bindings/hanxi/internal/modules/wechat/models'
import { getAvatarColor } from '../../composables/useWechatBot'

const props = defineProps<{
  /** 当前选中账号（视图 v-else 分支保证非空）。 */
  account: WechatAccountState
  /** 侧边栏折叠态（true 时展示 📑 展开按钮）。 */
  collapsed: boolean
  /** 目标用户是否处于内联编辑态。 */
  editingTargetUser: boolean
  /** 目标用户输入框当前值（与父级 toUserIdInput 同一 ref，受控回写）。 */
  targetUserInput: string
  /** Context Token 刷新进行中（按钮禁用态与文案开关）。 */
  refreshingToken: boolean
}>()

const emit = defineEmits<{
  'expand': []
  'edit-target': []
  'cancel-edit': []
  'save-target': []
  'refresh-token': []
  'toggle-listener': [acc: WechatAccountState]
  'update:targetUserInput': [value: string]
}>()

// 受控输入：读写均代理父级 ref（等价于拆分前模板直接 v-model="toUserIdInput"）。
const targetUserModel = computed({
  get: () => props.targetUserInput,
  set: (v: string) => emit('update:targetUserInput', v),
})
</script>

<template>
  <!-- 2.1 聊天窗口顶部导航栏 (Chat Header) -->
  <header class="chat-header">
    <div class="header-left">
      <button
        v-if="collapsed"
        class="btn-expand-header"
        title="展开侧边栏"
        @click="emit('expand')"
      >
        📑
      </button>
      <div class="header-avatar" :style="{ backgroundColor: getAvatarColor(account.remarkName) }">
        {{ (account.remarkName || '微').slice(0, 1) }}
      </div>
      <div class="header-info">
        <div class="header-title-line">
          <span class="bot-name">{{ account.remarkName }}</span>
          <span class="listen-badge" :class="{ online: account.isListening }">
            <span class="listen-dot"></span>
            {{ account.isListening ? '实时长轮询中' : '未开启监听' }}
          </span>
        </div>
        <div class="header-target-bar">
          <span class="target-label">目标用户:</span>
          <template v-if="!editingTargetUser">
            <span class="target-val" :title="targetUserInput || '点击修改目标微信用户'">
              {{ targetUserInput || account.ilinkUserId || '未配置目标' }}
            </span>
            <button class="btn-edit-link" @click="emit('edit-target')">修改</button>
          </template>
          <template v-else>
            <input
              type="text"
              v-model="targetUserModel"
              class="target-input-inline"
              placeholder="输入目标微信 User ID"
              @keydown.enter="emit('save-target')"
            />
            <button class="btn-save-inline" @click="emit('save-target')">保存</button>
            <button class="btn-cancel-inline" @click="emit('cancel-edit')">取消</button>
          </template>
        </div>
      </div>
    </div>

    <div class="header-right-actions">
      <button
        v-if="!account.isListening"
        class="btn-hdr-action"
        :disabled="refreshingToken"
        @click="emit('refresh-token')"
        title="手动请求最新 Context Token"
      >
        {{ refreshingToken ? '↻ 刷新中…' : '↻ 刷新凭据' }}
      </button>
      <button
        class="btn-hdr-action primary-toggle"
        :class="{ active: account.isListening }"
        @click="emit('toggle-listener', account)"
      >
        {{ account.isListening ? '停止监听' : '启动监听' }}
      </button>
    </div>
  </header>
</template>

<style scoped>
/* 以下样式自 WechatBotView.vue 原 scoped 块随标记逐字迁移，声明与 token 引用不动 */
.btn-expand-header {
  background: transparent;
  border: 1px solid var(--color-border);
  border-radius: 4px;
  font-size: 13px;
  padding: 3px 6px;
  cursor: pointer;
  margin-right: 4px;
  transition: background 0.15s ease;
}

.btn-expand-header:hover {
  background: var(--surface-hover);
}

.chat-header {
  height: 54px;
  padding: 0 18px;
  background: var(--surface-panel);
  border-bottom: 1px solid var(--color-border);
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-shrink: 0;
}

.header-left {
  display: flex;
  align-items: center;
  gap: 10px;
}

.header-avatar {
  width: 34px;
  height: 34px;
  border-radius: 6px;
  color: #ffffff; /* 功能例外：数据值头像底上恒白不随主题 */
  display: flex;
  align-items: center;
  justify-content: center;
  font-weight: 600;
  font-size: 14px;
  flex-shrink: 0;
}

.header-info {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.header-title-line {
  display: flex;
  align-items: center;
  gap: 8px;
}

.bot-name {
  font-size: 14px;
  font-weight: 600;
  color: var(--color-text);
}

.listen-badge {
  font-size: 11px;
  padding: 1px 7px;
  border-radius: 10px;
  background: var(--surface-hover);
  color: var(--color-text-muted);
  display: inline-flex;
  align-items: center;
  gap: 4px;
}

.listen-badge.online {
  background: var(--state-positive-soft);
  color: var(--state-positive);
}

.listen-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: currentColor;
}

.header-target-bar {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
}

.target-label {
  color: var(--color-text-subtle);
}

.target-val {
  color: var(--color-text);
  font-family: var(--font-mono);
}

.btn-edit-link {
  background: transparent;
  border: none;
  color: var(--color-primary);
  cursor: pointer;
  padding: 0 2px;
  font-size: 11px;
}

.target-input-inline {
  padding: 2px 6px;
  border: 1px solid var(--color-border);
  border-radius: 4px;
  font-size: 11px;
  font-family: var(--font-mono);
  width: 200px;
  outline: none;
}

.target-input-inline:focus {
  border-color: var(--color-primary);
}

.btn-save-inline {
  background: var(--state-positive);
  color: var(--color-text-inverse);
  border: none;
  padding: 2px 7px;
  border-radius: 4px;
  font-size: 11px;
  cursor: pointer;
}

.btn-cancel-inline {
  background: var(--surface-hover);
  color: var(--color-text-muted);
  border: none;
  padding: 2px 7px;
  border-radius: 4px;
  font-size: 11px;
  cursor: pointer;
}

.header-right-actions {
  display: flex;
  align-items: center;
  gap: 8px;
}

.btn-hdr-action {
  background: var(--surface-panel);
  border: 1px solid var(--color-border);
  padding: 5px 12px;
  border-radius: 6px;
  font-size: 12px;
  font-weight: 500;
  color: var(--color-text);
  cursor: pointer;
  transition: all 0.15s ease;
}

.btn-hdr-action:hover {
  background: var(--surface-hover);
}

.btn-hdr-action.primary-toggle.active {
  background: var(--state-positive-soft);
  border-color: var(--state-positive);
  color: var(--state-positive);
}
</style>
