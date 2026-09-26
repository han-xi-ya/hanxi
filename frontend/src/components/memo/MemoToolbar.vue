<script setup lang="ts">
// MemoToolbar：检索与过滤的顶部轻工具条容器件（随手记重设计 · 组件库四路之四）。
//
// 定位是"条"不是"卡"：本体零面板底色/零描边（不挂 .panel），只负责把搜索框、
// 过滤位、标签弹层位与「更多」溢出菜单排在一条水平呼吸带上——大容器感归宿主
// 版面自己决定，本件不抢戏。
//
// 职责边界（刻意做窄）：
//   - 搜索框【无防抖】：每次 input 原样即时发 update:search，350ms 防抖与
//     请求序号守卫留在视图侧（现 MemoView.onSearchInput 的 loadSeq 语义）——
//     组件不知道也不该知道后端的存在；
//   - #filters 插槽：置顶过滤丸 / 清除过滤 / 计数等视图私有状态件（含选中态
//     色差等页面私有色差仍归宿主 scoped 层）；
//   - #tagfilter 插槽：标签过滤弹层位，自带 position:relative 锚点，宿主的
//     触发丸与浮层都放进来即可贴条内定位；
//   - #more 插槽：「更多」溢出菜单的内容位（导出全库/一键全删等低频动作由
//     宿主填按钮）。组件只管菜单开合（触发钮 aria-expanded、点外关闭、Esc
//     关闭、菜单内点击冒泡收尾自动关闭，并透传 close() 供宿主显式收口）；
//     危险确认对话框与其全部业务后果留在视图，本件对"点了什么"零感知。
//     开合状态经 moreChange 事件如实外播（只报"开没开"不报内容）——宿主的
//     全局 Esc 阶梯需要知道"菜单优先于页面自身收合"，否则一次 Esc 连降两级。
import { onBeforeUnmount, ref } from 'vue'

const props = withDefaults(
  defineProps<{
    /** 当前搜索词（受控：组件只回显，防抖与拉取归宿主） */
    search?: string
    /** 搜索框占位文案 */
    placeholder?: string
    /** 搜索框无障碍名 */
    label?: string
  }>(),
  { search: '', placeholder: '搜索…', label: '搜索便签' },
)

const emit = defineEmits<{
  /** 即时透传输入值（无节流无防抖——防抖策略外置归宿主） */
  (e: 'update:search', value: string): void
  /** 溢出菜单开合外播（true/false 都发，含点击项/卸载触发的自动收合） */
  (e: 'moreChange', open: boolean): void
}>()

const rootEl = ref<HTMLElement | null>(null)
const moreOpen = ref(false)

function openMore(on: boolean) {
  const changed = moreOpen.value !== on
  moreOpen.value = on
  if (changed) emit('moreChange', on)
  if (on) {
    // 点外即收：捕获后的 document click 只挂开合期，卸载/关闭必摘（防泄露监听）
    document.addEventListener('click', onDocClick, true)
    window.addEventListener('keydown', onKey)
  } else {
    document.removeEventListener('click', onDocClick, true)
    window.removeEventListener('keydown', onKey)
  }
}

function onDocClick(e: MouseEvent) {
  if (!rootEl.value?.contains(e.target as Node | null)) openMore(false)
}

function onKey(e: KeyboardEvent) {
  if (e.key === 'Escape') openMore(false)
}

function toggleMore() {
  openMore(!moreOpen.value)
}

function onSearchInput(e: Event) {
  emit('update:search', (e.target as HTMLInputElement).value)
}

onBeforeUnmount(() => openMore(false))
</script>

<template>
  <div ref="rootEl" class="mt-bar">
    <div class="mt-search">
      <input
        class="text-input mt-search-input"
        type="text"
        :value="search"
        :placeholder="placeholder"
        :aria-label="label"
        @input="onSearchInput"
      />
      <button
        v-if="search"
        type="button"
        class="mt-clear"
        title="清空搜索"
        @click="emit('update:search', '')"
      >✕</button>
    </div>

    <!-- 视图私有过滤件位（置顶丸/清除过滤/计数等） -->
    <slot name="filters" />

    <!-- 标签过滤弹层槽：自带 relative 锚点，弹层挂这里贴条定位 -->
    <span class="mt-tag-slot"><slot name="tagfilter" /></span>

    <div v-if="$slots.more" class="mt-more">
      <button
        type="button"
        class="btn btn-ghost btn-small mt-more-btn"
        :aria-expanded="moreOpen ? 'true' : 'false'"
        aria-haspopup="true"
        @click="toggleMore"
      >
        更多 <span class="mt-caret" aria-hidden="true">▾</span>
      </button>
      <div v-if="moreOpen" class="mt-menu" @click="openMore(false)">
        <slot name="more" :close="() => openMore(false)" />
      </div>
    </div>
  </div>
</template>

<style scoped>
/* 轻工具条：无底色无描边，一条 flex 呼吸带 */
.mt-bar {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  min-width: 0;
}
.mt-search { position: relative; flex: 1 1 240px; min-width: 0; }
.mt-search-input { width: 100%; padding-right: 30px; min-height: var(--control-h-md); }
.mt-clear {
  position: absolute; right: 8px; top: 50%; transform: translateY(-50%);
  background: none; border: none; color: var(--color-text-subtle);
  cursor: pointer; font-size: var(--text-sm);
}
.mt-clear:hover { color: var(--color-text); }
/* 弹层槽无内容时不占呼吸位（:empty 兼顾纯空白文本节点由插槽默认内容兜底） */
.mt-tag-slot { position: relative; display: inline-flex; align-items: center; }
.mt-tag-slot:empty { display: none; }

.mt-more { position: relative; flex: none; }
.mt-caret { font-size: var(--text-micro); }
.mt-menu {
  position: absolute; right: 0; top: calc(100% + 4px); z-index: 60;
  min-width: 168px;
  display: flex; flex-direction: column; gap: 2px;
  padding: 6px;
  background: var(--surface-panel);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-control);
  box-shadow: var(--shadow-panel);
}
</style>
