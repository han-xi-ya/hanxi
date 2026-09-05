<script setup lang="ts">
// 微信协议限制规则提示条（可折叠）：纯静态文案壳，关闭动作上抛视图编排层
// （折叠开关 showRulesBanner 为视图本地 UI 状态）。自 WechatBotView.vue 逐字迁出。
defineEmits<{ 'close': [] }>()
</script>

<template>
  <!-- 2.2 微信限制规则说明条 (可折叠) -->
  <div class="clawbot-rules-strip">
    <div class="rules-content">
      <span class="rules-tag">💡 微信协议规范</span>
      <span class="rule-text">① 单微信号限绑1个Bot</span>
      <span class="rule-sep">·</span>
      <span class="rule-text">② 初次需手机微信主动发任意消息建联</span>
      <span class="rule-sep">·</span>
      <span class="rule-text">③ 每下发10次或超24h需主动发消息续期会话</span>
    </div>
    <button class="btn-close-strip" @click="$emit('close')" title="关闭提示">✕</button>
  </div>
</template>

<style scoped>
/* 以下样式自 WechatBotView.vue 原 scoped 块随标记逐字迁移，声明与 token 引用不动 */
.clawbot-rules-strip {
  background: var(--state-warning-soft);
  border-bottom: 1px solid var(--state-warning-soft);
  padding: 6px 14px;
  font-size: 12px;
  color: var(--state-warning);
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-shrink: 0;
}

.rules-content {
  display: flex;
  align-items: center;
  gap: 6px;
  flex-wrap: wrap;
}

.rules-tag {
  font-weight: 600;
  color: var(--state-warning);
}

.rule-sep {
  color: var(--state-warning);
}

.btn-close-strip {
  background: transparent;
  border: none;
  color: var(--state-warning);
  cursor: pointer;
  font-size: 12px;
}
</style>
