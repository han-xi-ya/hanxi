<script setup lang="ts">
// MemoCard：一条便签的行/卡双形态呈现件（随手记重设计 · 组件库四路之四）。
//
// 形态契约：variant='grid' 是现在的卡片形态（多行摘要块），variant='list' 是
// 单行摊列形态（行高紧凑、摘要只吃首非空行、超长交给 CSS ellipsis）——两形态
// 共用同一 DOM 结构（head/preview/foot/tools 四区），差异全在 CSS grid 模板
// 区域重排，呈现逻辑零分叉。
//
// 敏感遮罩纪律【硬编码进组件，消费方无从旁路】：masked 态下
//   ① title/text 两个明文面一律不渲染（title 换成固定文案「敏感便签」、正文
//      换成圆点占位）——比现视图（masked 仍显示标题）更严，是对契约
//      「遮罩态永不渲染明文」的从严落实，接线方删旧卡时注意此观感差异；
//   ② HTML title 属性也是泄漏面：masked 态不给任何元素挂含明文的 title 提示
//      （hover 弹提示同样是"渲染"）；
//   ③ MD 徽标随之熄灭——暴露"正文含 Markdown 结构"本身就是结构泄漏。
// 撤遮罩出口（toggleMask 钮）刻意常驻：遮罩纪律是"不给人看"，不是"不给解"，
// 现 UI 的揭示功能（卡片 👁️ 与详情浮层「揭示明文」）实证在位，emit 保留。
//
// 交互契约：标题区是键盘可达的 select 入口（role=button + Enter/Space），
// 摘要块同挂 click→select（鼠标侧与现视图"点内容开详情"一致）；标签丸可点
// 发 tagSelect（复刻现卡"点标签即过滤"，丸体键盘 Enter/Space 同发）。
// 编辑/复制/删除不进组件 emits——那些是视图侧动作（含确认框与剪贴板策略），
// 经 #actions 插槽注入本件右上角操作区，危险逻辑留视图。
//
// 色板治理注记（沿用现视图裁决）：左封边色来自 colorTag 用户数据
// （memoColorHex 单源五色），非主题表面，不算 scoped 层新色值；除此之外
// 全部吃 token 与全局原子类（mono / chip / tag-pill 家族）。
import { computed } from 'vue'
import { looksLikeMarkdown } from '../../utils/markdown'
import { cardPreview, firstMeaningfulLine, fmtAgo, memoColorHex } from './memoMetrics'

const props = withDefaults(
  defineProps<{
    /** 便签标题原文（masked 态被组件扣下不渲染） */
    title: string
    /** 正文原文（masked 态被组件扣下不渲染；摘要切行/单行化在组件内完成） */
    text: string
    /** 标签数组（MemoItem.tags 可为 null，此处一并容忍） */
    tags?: string[] | null
    /** 色彩标识（blue/emerald/amber/rose/purple；未知值回落 blue，与现视图同口径） */
    color?: string
    pinned: boolean
    masked: boolean
    /** ISO 时间串（相对时间与悬浮全量时间双呈现） */
    updatedAt: string
    /** 形态：grid=卡片（多行摘要）/ list=单行摊列 */
    variant: 'list' | 'grid'
  }>(),
  { tags: () => [], color: '' },
)

const emit = defineEmits<{
  /** 点开阅读（标题键盘/摘要点击共用，无载荷——条目上下文由宿主 v-for 持有） */
  (e: 'select'): void
  (e: 'togglePin'): void
  (e: 'toggleMask'): void
  /** 点击/键盘激活标签丸，请求按该标签过滤 */
  (e: 'tagSelect', tag: string): void
}>()

// 遮罩态固定文案：不含任何条目明文，圆点位数与现视图详情浮层遮罩档一致
const MASK_TITLE = '敏感便签'
const MASK_DOTS = '•'.repeat(48)

const isList = computed(() => props.variant === 'list')
const shownTitle = computed(() => {
  if (props.masked) return MASK_TITLE
  return props.title.trim() ? props.title : '无标题便签'
})
const titleVoid = computed(() => !props.masked && !props.title.trim())
// masked 永不进 title 属性——悬浮提示与渲染文本同纪律
const titleTip = computed(() => (props.masked ? undefined : shownTitle.value))
const showMdFlag = computed(() => !props.masked && looksLikeMarkdown(props.text))
const previewText = computed(() => {
  if (props.masked) return MASK_DOTS
  const raw = isList.value ? firstMeaningfulLine(props.text) : cardPreview(props.text)
  return raw || '（空便签）'
})
const colorStyle = computed(() => ({ borderLeftColor: memoColorHex(props.color) }))
const agoText = computed(() => fmtAgo(props.updatedAt))
const timeTip = computed(() => {
  const d = new Date(props.updatedAt)
  return Number.isNaN(d.getTime()) ? undefined : d.toLocaleString()
})
const tagList = computed(() => props.tags ?? [])
</script>

<template>
  <article
    class="mc-card"
    :class="[isList ? 'mc-list' : 'mc-grid', { 'is-pinned': pinned }]"
    :style="colorStyle"
  >
    <header class="mc-head">
      <h3
        class="mc-title"
        :class="{ 'mc-void': titleVoid }"
        role="button"
        tabindex="0"
        :title="titleTip"
        @click="emit('select')"
        @keydown.enter.prevent="emit('select')"
        @keydown.space.prevent="emit('select')"
      >
        {{ shownTitle }}
      </h3>
      <span
        v-if="showMdFlag"
        class="chip chip-information mc-md"
        title="含 Markdown 结构，点开渲染"
      >MD</span>
    </header>

    <div
      class="mc-preview mono"
      :class="{ masked }"
      :aria-label="masked ? '敏感信息已遮罩' : '便签内容摘要'"
      @click="emit('select')"
    >
      {{ previewText }}
    </div>

    <footer class="mc-foot">
      <span v-if="tagList.length" class="mc-tags">
        <span
          v-for="t in tagList"
          :key="t"
          class="tag-pill tag-pill-clickable mc-tag"
          role="button"
          tabindex="0"
          @click="emit('tagSelect', t)"
          @keydown.enter.prevent="emit('tagSelect', t)"
          @keydown.space.prevent="emit('tagSelect', t)"
        >{{ t }}</span>
      </span>
      <time class="mc-time" :title="timeTip">{{ agoText }}</time>
    </footer>

    <span class="mc-tools">
      <button
        type="button"
        class="mc-btn"
        :title="masked ? '揭示敏感信息' : '脱敏遮罩保护'"
        @click="emit('toggleMask')"
      >
        {{ masked ? '👁️' : '🕶️' }}
      </button>
      <button
        type="button"
        class="mc-btn"
        :class="{ 'mc-on': pinned }"
        :title="pinned ? '取消置顶' : '固定置顶'"
        @click="emit('togglePin')"
      >
        📌
      </button>
      <!-- 视图侧动作透传位（编辑/复制/删除等，确认与剪贴板逻辑归宿主） -->
      <slot name="actions" />
    </span>
  </article>
</template>

<style scoped>
/* 四区单结构、两形态只换 grid 模板：head(标题+MD) / preview(摘要) / foot(标签+时间) / tools(操作) */
.mc-card {
  display: grid;
  row-gap: 8px;
  column-gap: 6px;
  padding: 12px 14px;
  background: var(--surface-panel);
  border: 1px solid var(--color-border);
  /* 左封边 4px：色彩标识是用户数据值（memoColorHex 内联注入），非主题表面 */
  border-left-width: 4px;
  border-left-style: solid;
  border-radius: var(--radius-control);
  transition: box-shadow var(--motion-fast) ease, transform var(--motion-fast) ease,
    background var(--motion-fast) ease;
}
.mc-grid {
  grid-template-columns: minmax(0, 1fr) auto;
  grid-template-areas:
    'head tools'
    'prev prev'
    'foot foot';
  align-items: center;
}
.mc-list {
  grid-template-columns: minmax(96px, auto) minmax(0, 1fr) auto auto;
  grid-template-areas: 'head prev foot tools';
  align-items: center;
  padding: 7px 12px;
  background: transparent;
  /* 行档平时只露用户色封边（左沿由内联 borderLeftColor 供给，天然压过本条），
     其余三边收进 hover 再给 */
  border-color: transparent;
}
/* hover 微抬：卡片档抬 1px 给浅投影；行档只做表面染色（行列表做位移会抖） */
.mc-grid:hover { box-shadow: var(--shadow-panel); transform: translateY(-1px); }
.mc-list:hover { background: var(--surface-soft); border-color: var(--color-border); }

/* 置顶档：整圈描边换主色（左封边为内联用户色，天然压过本类——色彩身份与置顶信号共存） */
.mc-card.is-pinned { border-color: var(--color-primary); }
.mc-card.is-pinned.mc-list:hover { border-color: var(--color-primary); }

.mc-head { grid-area: head; display: flex; align-items: center; gap: 6px; min-width: 0; }
.mc-title {
  margin: 0;
  flex: 0 1 auto;
  min-width: 0;
  font-size: var(--text-md);
  font-weight: 600;
  color: var(--color-text);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  cursor: pointer;
  border-radius: var(--radius-micro);
}
.mc-title:hover { color: var(--color-primary); }
.mc-title:focus-visible { outline: 2px solid var(--focus-ring); outline-offset: 2px; }
.mc-void { color: var(--color-text-subtle); font-weight: 400; font-style: italic; }
/* 列表档标题限宽吃 ellipsis，防长题把摘要挤没 */
.mc-list .mc-title { max-width: min(240px, 34vw); }
.mc-md { flex: none; font-size: var(--text-micro); padding: 1px 6px; }

.mc-preview {
  grid-area: prev;
  min-width: 0;
  font-size: var(--text-sm);
  line-height: 1.55;
  color: var(--color-text);
  background: var(--surface-soft);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-micro);
  padding: 8px 10px;
  cursor: pointer;
}
.mc-grid .mc-preview {
  max-height: 150px;
  overflow: hidden;
  white-space: pre-wrap;
  word-break: break-word;
}
.mc-list .mc-preview {
  padding: 3px 8px;
  border-color: transparent;
  background: transparent;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.mc-list .mc-preview:hover { background: var(--surface-soft); border-color: var(--color-border); }
.mc-preview.masked { color: var(--color-text-subtle); user-select: none; letter-spacing: 2px; }

.mc-foot { grid-area: foot; display: flex; align-items: center; gap: 8px; min-width: 0; }
.mc-tags { display: flex; flex-wrap: wrap; gap: 4px; min-width: 0; }
.mc-list .mc-tags { flex-wrap: nowrap; overflow: hidden; }
.mc-tag { font-size: var(--text-xs); }
.mc-time {
  margin-left: auto;
  flex: none;
  font-size: var(--text-xs);
  color: var(--color-text-subtle);
  font-variant-numeric: tabular-nums;
}
.mc-list .mc-time { margin-left: 0; }

.mc-tools { grid-area: tools; display: flex; align-items: center; gap: 2px; flex: none; }
.mc-btn {
  background: none;
  border: none;
  cursor: pointer;
  font-size: var(--text-base);
  padding: 2px 4px;
  border-radius: var(--radius-micro);
  transition: background var(--motion-fast) ease;
}
.mc-btn:hover { background: var(--surface-hover); }
.mc-btn:focus-visible { outline: 2px solid var(--focus-ring); outline-offset: 1px; }
.mc-on { filter: saturate(1.3); }
</style>
