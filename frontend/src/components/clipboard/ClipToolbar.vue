<script setup lang="ts">
// ClipToolbar：剪贴板顶栏一行轻工具条（形态承接随手记 v2 MemoToolbar 的
// "条不是卡"裁决：本体零面板底色零描边，只排呼吸带）。
//
// R-F2 呼吸带秩序（skill「Operation panel」：危险动作与主动线空间隔离、
// 不给同等视觉权重）：搜索 → kind 过滤 chips → 记录状态灯 → 新片段（轻创造）
// → 「更多」溢出菜单。清空全部历史是低频毁灭动作，收进溢出菜单右端远位，
// 菜单里仍是 danger 色 + 视图侧强确认闸双保险——离主动线一格，不误触。
//
// 职责边界（刻意做窄，视图契约对位）：
//   - 搜索框【无防抖】：每次 input 原样即时发 update:search，350ms 防抖与
//     请求序号守卫留在视图侧；回车单发 search-enter（输入法组字中的回车豁免
//     ——只结束组字不触发查询，随手记同款纪律）；
//   - kind 过滤 chips（全部/文本/图片/文件/片段）受控回显：点亮哪档发哪档，
//     组件零过滤逻辑；「片段」档是前端合成档（manual 标志位过滤），口径见
//     clipboardFormat.ClipFilter 注记；
//   - 暂停记录开关：状态灯（绿=在录带活体脉冲 / 琥珀=已暂停）+ 文字双通道，
//     不靠颜色单独表意；在途 pausing 禁用防连点；实况以视图回灌为准
//     （订阅 clipboard:paused 事件，托盘侧翻牌这里也跟着亮）；
//   - 「新片段」「清空」只发意图事件（create-snippet / clear-all），prompt 与
//     danger 确认链全部留在视图——破坏性动作的闸不归展示件；
//   - 溢出菜单只管开合纪律（MemoToolbar 同谱）：触发钮 aria-expanded、点外
//     （document click capture）收、Esc 收、菜单内点击冒泡收尾自动收；开合
//     状态经 moreChange 如实外播——宿主的全局 Esc 阶梯需要知道"菜单在场时
//     Esc 只收菜单"，否则一次 Esc 连降两级。本件对"点了什么"零感知。
import { onBeforeUnmount, ref } from 'vue'
import ClipGlyph from './ClipGlyph.vue'
import { CLIP_FILTER_LABELS, type ClipFilter } from './clipboardFormat'

withDefaults(
  defineProps<{
    /** 当前搜索词（受控：组件只回显，防抖与拉取归宿主） */
    search?: string
    /** 当前 kind 过滤档（受控） */
    kind?: ClipFilter
    /** 暂停态（回灌自 clipboard:paused 事件与 GetStatus） */
    paused?: boolean
    /** SetPaused 在途（禁用开关防连点） */
    pausing?: boolean
    /** 清空钮禁用（库空或状态未知时不给按） */
    clearDisabled?: boolean
  }>(),
  { search: '', kind: 'all', paused: false, pausing: false, clearDisabled: true },
)

const emit = defineEmits<{
  /** 即时透传输入值（无节流无防抖——防抖策略外置归宿主） */
  (e: 'update:search', value: string): void
  /** 回车立查（宿主清抖发作即刻拉取） */
  (e: 'search-enter'): void
  (e: 'update:kind', kind: ClipFilter): void
  (e: 'toggle-paused'): void
  (e: 'create-snippet'): void
  (e: 'clear-all'): void
  /** 溢出菜单开合外播（true/false 都发，含点击项/卸载触发的自动收合） */
  (e: 'moreChange', open: boolean): void
}>()

const FILTERS: readonly ClipFilter[] = ['all', 'text', 'image', 'file', 'manual']
// 「全部」档无图标（文字即身份），其余档类别图标与行图标同源同名
const FILTER_GLYPHS: Partial<Record<ClipFilter, string>> = {
  text: 'text',
  image: 'image',
  file: 'file',
  manual: 'sticky',
}

const rootEl = ref<HTMLElement | null>(null)
const moreOpen = ref(false)

function openMore(on: boolean) {
  const changed = moreOpen.value !== on
  moreOpen.value = on
  if (changed) emit('moreChange', on)
  if (on) {
    // 点外即收：捕获期的 document click 只挂开合期，卸载/关闭必摘（防泄露监听）
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

function onInput(e: Event) {
  emit('update:search', (e.target as HTMLInputElement).value)
}

function onKeydown(e: KeyboardEvent) {
  // 中文输入法组字中的回车只结束候选，不触发查询（随手记同款豁免）
  if (e.key === 'Enter' && !e.isComposing) emit('search-enter')
}

onBeforeUnmount(() => openMore(false))
</script>

<template>
  <div ref="rootEl" class="ct-bar">
    <div class="ct-search">
      <input
        class="text-input ct-search-input"
        type="text"
        :value="search"
        placeholder="搜内容摘要、来源窗口、文件名或标签…"
        aria-label="搜索剪贴板历史"
        @input="onInput"
        @keydown="onKeydown"
      />
      <button
        v-if="search"
        type="button"
        class="ct-clear"
        title="清空搜索"
        @click="emit('update:search', '')"
      >✕</button>
    </div>

    <div class="ct-chips" role="group" aria-label="按类别过滤">
      <button
        v-for="f in FILTERS"
        :key="f"
        type="button"
        class="ct-chip"
        :class="{ active: kind === f }"
        :aria-pressed="kind === f ? 'true' : 'false'"
        @click="emit('update:kind', f)"
      >
        <ClipGlyph v-if="f !== 'all'" :name="FILTER_GLYPHS[f] ?? 'text'" :size="12" />
        {{ CLIP_FILTER_LABELS[f] }}
      </button>
    </div>

    <button
      type="button"
      class="ct-pause"
      :aria-pressed="paused ? 'true' : 'false'"
      :disabled="pausing"
      :title="paused ? '剪贴板内容正不被记录（含新复制的密码窗口也不入库）；点击恢复记录' : '点击暂停记录：期间所有复制不入库（托盘同名开关同步）'"
      @click="emit('toggle-paused')"
    >
      <span
        class="ct-dot"
        :class="paused ? 'ct-dot-paused' : 'ct-dot-live live-pulse'"
        aria-hidden="true"
      ></span>
      <ClipGlyph :name="paused ? 'play' : 'pause'" :size="12" />
      {{ pausing ? '切换中…' : paused ? '已暂停记录' : '记录中' }}
    </button>

    <button
      type="button"
      class="btn btn-secondary btn-small ct-create"
      title="手建一条常驻固定片段（不被容量淘汰），需输入内容"
      @click="emit('create-snippet')"
    >
      <ClipGlyph name="plus" :size="13" /> 新片段
    </button>

    <div class="ct-more">
      <button
        type="button"
        class="btn btn-ghost btn-small ct-more-btn"
        :aria-expanded="moreOpen ? 'true' : 'false'"
        aria-haspopup="true"
        title="低频动作：清空全部历史（需强确认）"
        @click="openMore(!moreOpen)"
      >
        更多 <span class="ct-caret" aria-hidden="true">▾</span>
      </button>
      <div v-if="moreOpen" class="ct-menu" @click="openMore(false)">
        <button
          type="button"
          class="ct-menu-item ct-menu-danger"
          role="menuitem"
          :disabled="clearDisabled"
          title="清空全部历史与图片 blob（含置顶与固定片段），需二次确认"
          @click="emit('clear-all')"
        >
          <ClipGlyph name="trash" :size="13" /> 清空全部历史
        </button>
      </div>
    </div>
  </div>
</template>

<style scoped>
/* 轻工具条：无底色无描边，一条 flex 呼吸带（MemoToolbar 同谱） */
.ct-bar {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  min-width: 0;
}
.ct-search { position: relative; flex: 1 1 220px; min-width: 140px; }
.ct-search-input { width: 100%; padding-right: 30px; min-height: var(--control-h-md); }
.ct-clear {
  position: absolute; right: 8px; top: 50%; transform: translateY(-50%);
  background: none; border: none; color: var(--color-text-subtle);
  cursor: pointer; font-size: var(--text-sm);
}
.ct-clear:hover { color: var(--color-text); }

/* 右端动作簇贴条尾（chips 占中段，margin-right:auto 把开关/建段/更多推到行尾；
   换行时随呼吸带自然下落，不留悬空动作） */
.ct-chips {
  display: flex; align-items: center; gap: 4px; flex: none;
  margin-right: auto; min-width: 0;
  overflow-x: auto; scrollbar-width: thin; padding: 2px 0;
}
.ct-chip {
  flex: none; display: inline-flex; align-items: center; gap: 4px;
  background: var(--surface-soft); border: 1px solid var(--color-border); color: var(--color-text-muted);
  padding: 2px 10px; border-radius: var(--radius-pill); font-size: var(--text-sm); cursor: pointer;
  transition: background var(--motion-fast) ease, color var(--motion-fast) ease, border-color var(--motion-fast) ease;
}
.ct-chip:hover { background: var(--surface-hover); color: var(--color-text); }
.ct-chip.active { background: var(--color-primary); border-color: var(--color-primary); color: var(--color-on-primary); }
.ct-chip:focus-visible { outline: 2px solid var(--focus-ring); outline-offset: 2px; }

.ct-pause {
  flex: none; display: inline-flex; align-items: center; gap: 6px;
  background: var(--surface-soft); border: 1px solid var(--color-border); border-radius: var(--radius-pill);
  padding: 3px 10px; min-height: 26px; font-size: var(--text-sm); color: var(--color-text-muted); cursor: pointer;
  transition: background var(--motion-fast) ease, color var(--motion-fast) ease;
}
.ct-pause:hover:not(:disabled) { background: var(--surface-hover); color: var(--color-text); }
.ct-pause:focus-visible { outline: 2px solid var(--focus-ring); outline-offset: 2px; }
.ct-pause:disabled { opacity: 0.6; cursor: not-allowed; }
/* 状态灯：色点只作辅证，语义恒由文字承载（不用颜色单独表意） */
.ct-dot { width: 8px; height: 8px; border-radius: 50%; flex: none; }
.ct-dot-live { background: var(--state-positive); }
.ct-dot-paused { background: var(--state-warning); }

/* 溢出菜单（容器皮肤与开合纪律照 MemoToolbar 同谱；菜单条目留本件——
   剪贴板面只有这一件低频动作，不借道视图插槽） */
.ct-more { position: relative; flex: none; }
.ct-caret { font-size: var(--text-micro); }
.ct-menu {
  position: absolute; right: 0; top: calc(100% + 4px); z-index: 60;
  min-width: 168px;
  display: flex; flex-direction: column; gap: 2px;
  padding: 6px;
  background: var(--surface-panel);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-control);
  box-shadow: var(--shadow-panel);
}
.ct-menu-item {
  display: flex; align-items: center; gap: 8px; text-align: left;
  background: none; border: none; border-radius: var(--radius-micro); cursor: pointer;
  padding: 7px 10px; font-size: var(--text-sm); color: var(--color-text); white-space: nowrap;
}
.ct-menu-item:hover:not(:disabled) { background: var(--surface-hover); }
.ct-menu-item:disabled { opacity: 0.55; cursor: not-allowed; }
.ct-menu-item:focus-visible { outline: 2px solid var(--focus-ring); outline-offset: -2px; }
.ct-menu-danger { color: var(--state-danger); }
.ct-menu-danger:hover:not(:disabled) { background: var(--btn-danger-outline-hover); }
</style>
