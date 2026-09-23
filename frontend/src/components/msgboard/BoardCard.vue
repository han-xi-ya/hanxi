<script setup lang="ts">
// BoardCard：留言牌牌面的唯一画法（N6 重设计）——全屏挂牌弹窗与管理页实时
// 预览共用本组件，"所见即所得"由同源保证，不存在第二套像素。
//
// 视觉隐喻是"贴在屏幕上的便利贴"：纸黄底、微旋转、顶部胶带条；纸色与墨色
// 是物理贴喻的本体，刻意不随亮/暗主题反转（豁免纪律沿用原牌面注释：这是
// 设计决定，非未迁移裸色泄漏，区分见踩坑 #33）。
//
// 版式契约：正文第一行 = 主题大字（fontSize 直用），其余行 = 副行小字
// （×0.42）；表情就是正文首字符，无表情即纯文字便利贴——旧配置零迁移。
// 溢出策略沿用"牌是看的不是读的"：超高只裁不滚（max-height 由宿主经
// --bc-max-h 注入：全屏牌给视口比例，预览盒给固定高度）。
import { computed } from 'vue'

const props = defineProps<{
  text: string
  fontSize: number
}>()

// 卡体统一定标：字号与"约 13 个标题字宽"的卡宽上限同源换算（88vw 兜底
// 小屏）——标题/副行/内边距全部吃这条 em 链，缩放行为一致。
const cardStyle = computed(() => ({
  fontSize: `${props.fontSize}px`,
  maxWidth: `min(${Math.round(props.fontSize * 13)}px, 88vw)`,
}))

// 副行字号＝主题字号 ×0.42（显式像素换算，防 em 基准漂移的假联动）
const subStyle = computed(() => ({
  fontSize: `${Math.max(12, Math.round(props.fontSize * 0.42))}px`,
}))

// 主题行/副行拆分（trailing 空行不产生空气副行）
function titleOf(text: string): string {
  return text.split('\n', 1)[0] ?? ''
}
function subOf(text: string): string {
  const rest = text.split('\n').slice(1)
  while (rest.length && rest[rest.length - 1].trim() === '') rest.pop()
  return rest.join('\n').trim()
}
</script>

<template>
  <div class="bc-card" role="presentation" :style="cardStyle">
    <span class="bc-tape" aria-hidden="true"></span>
    <div class="bc-title">{{ titleOf(text) }}</div>
    <div v-if="subOf(text)" class="bc-sub" :style="subStyle">{{ subOf(text) }}</div>
  </div>
</template>

<style scoped>
.bc-card {
  position: relative;
  display: inline-block;
  max-height: var(--bc-max-h, 18em);
  padding: 1.15em 1.5em 1.35em;
  transform: rotate(-1.4deg);
  background: linear-gradient(165deg, #fbdf6e 0%, #f6d358 58%, #eec94a 100%);
  color: #3a2c09;
  box-shadow: 0 18px 44px rgba(0, 0, 0, 0.42), 0 2px 6px rgba(0, 0, 0, 0.28);
  text-align: center;
  user-select: none;
  overflow: hidden; /* 极端超长只裁不滚（与旧牌面同纪律） */
}
/* 顶部胶带条：半透明白色斜贴，压住"纸"的翘起感 */
.bc-tape {
  position: absolute;
  top: -0.42em;
  left: 50%;
  width: 6.4em;
  height: 1.15em;
  transform: translateX(-50%) rotate(1.6deg);
  background: rgba(250, 250, 245, 0.5);
  border-left: 1px dashed rgba(120, 100, 30, 0.25);
  border-right: 1px dashed rgba(120, 100, 30, 0.25);
  box-shadow: 0 1px 2px rgba(0, 0, 0, 0.12);
}
.bc-title {
  font-family: var(--font-display);
  font-weight: 650;
  line-height: 1.32;
  letter-spacing: 0.01em;
  white-space: pre-wrap; /* 手工换行是版式契约的一部分 */
  overflow-wrap: break-word;
}
.bc-sub {
  margin-top: 0.62em;
  font-weight: 500;
  line-height: 1.55;
  opacity: 0.74;
  white-space: pre-wrap;
  overflow-wrap: break-word;
}
</style>
