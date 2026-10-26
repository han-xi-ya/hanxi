<script setup lang="ts">
// MemoEditorPane：随手记页面板的书写态呈现件（MemoView v2 拆分批，自 M4 收口后的
// 视图单体内原样抽出，语义零改动）。
//
// 职责边界（刻意做窄）：本件只持有「草稿面」——表单回填、标签增删、色彩选择、
// 书写/预览分段；保存/取消一律整包上抛宿主（payload 校验合格后才发）。
// API 调用、toast 播报词面、关页与列表重拉全留视图：危险确认与数据链不进呈现件
// （与 MemoCard「编辑/复制/删除归宿主」同一收编纪律）。
// 生命周期契约：宿主以 :key 每次打开重挂载本件（v-if 收起即弃稿）——
// 「换看下一条时书写草稿丢弃」「重开即回填新值」由重挂载天然成立，
// 本件不做组件内二次初始化。
// Markdown 预览皮肤（.md-preview）单源留在宿主视图（`.memo-pane :deep()` 穿透
// 命中本件预览分支与阅读页，两份渲染面同源防漂移）；本件自带 pane-write 家族皮，
// 逐字迁自宿主拆分前的口径（拆分不动相）。
import { computed, ref } from 'vue'
import type { MemoItem } from '../../../bindings/hanxi/internal/modules/memo/models'
import { renderMarkdown } from '../../utils/markdown'
import { useToast } from '../../composables/useToast'
import UiButton from '../ui/UiButton.vue'
import UiClipboardField from '../ui/UiClipboardField.vue'
import { MEMO_COLOR_HEX } from './memoMetrics'

const props = defineProps<{
  /** true=编辑既有便签（回填 item）；false=新建（空白草稿，可带 presetTag）。 */
  editing: boolean
  /** 编辑态的回填源；新建态传 null。 */
  item?: MemoItem | null
  /** 新建时带入的当前过滤标签（与悬浮速记卡无过滤态互补，视图组合语义原样）。 */
  presetTag?: string
}>()

const emit = defineEmits<{
  /** 校验合格的整包草稿（id 空串=新建）。 */
  (e: 'save', payload: { id: string; title: string; content: string; tags: string[]; colorTag: string }): void
  (e: 'cancel'): void
}>()

const { showToast } = useToast()

// 色板名册（中文名展示归本件），十六进制一律吃 memoMetrics 用户色板单源
// （色彩标识是用户数据值，非主题表面——接线收口轮消灭了 hex 的第二份逐字副本）。
const COLOR_OPTIONS = (
  [
    { name: '蓝色', val: 'blue' },
    { name: '翡翠绿', val: 'emerald' },
    { name: '琥珀橙', val: 'amber' },
    { name: '玫瑰红', val: 'rose' },
    { name: '紫罗兰', val: 'purple' },
  ] as const
).map((c) => ({ ...c, hex: MEMO_COLOR_HEX[c.val] }))

const editMode = ref<'write' | 'preview'>('write')
const form = ref({
  title: props.item?.title ?? '',
  content: props.item?.content ?? '',
  tagInput: '',
  tags: props.editing
    ? props.item?.tags
      ? [...props.item.tags]
      : []
    : props.presetTag
      ? [props.presetTag]
      : [],
  colorTag: props.editing ? props.item?.colorTag || 'blue' : 'blue',
})

function addTagFromInput() {
  const val = form.value.tagInput.trim()
  if (!val) return
  const formatted = val.startsWith('#') ? val : '#' + val
  if (!form.value.tags.includes(formatted)) {
    form.value.tags.push(formatted)
  }
  form.value.tagInput = ''
}

function removeTag(tag: string) {
  form.value.tags = form.value.tags.filter((t) => t !== tag)
}

function save() {
  addTagFromInput()
  if (!form.value.title.trim() && !form.value.content.trim()) {
    showToast('标题或内容至少填写一项')
    return
  }
  emit('save', {
    id: props.editing ? props.item?.id ?? '' : '',
    title: form.value.title,
    content: form.value.content,
    tags: [...form.value.tags],
    colorTag: form.value.colorTag,
  })
}

// 书写态预览：所见即阅读态所得（同一渲染器同源）。仅预览档渲染——v-show 双分支
// 常驻挂载，若不加档别，书写态每次击键都会在隐藏面板上重跑全文 Markdown。
const editorPreviewHtml = computed(() =>
  editMode.value === 'preview' ? renderMarkdown(form.value.content) : '',
)
</script>

<template>
  <div class="pane-write" @keydown.ctrl.enter.prevent="save">
    <div class="pane-head">
      <span class="pane-kind">{{ editing ? '编辑便签' : '新建便签' }}</span>
      <div class="seg" role="group" aria-label="编辑/预览切换">
        <button class="seg-btn" :class="{ active: editMode === 'write' }" @click="editMode = 'write'">书写</button>
        <button class="seg-btn" :class="{ active: editMode === 'preview' }" @click="editMode = 'preview'">预览</button>
      </div>
    </div>

    <div v-show="editMode === 'write'" class="write-body">
      <input
        v-model="form.title"
        type="text"
        class="text-input pane-title-input"
        placeholder="标题（可留空，正文首行示人）"
        aria-label="便签标题"
      />
      <UiClipboardField
        v-model="form.content"
        mono
        :rows="10"
        placeholder="在此粘贴文本、命令行、cURL、SQL 或 JWT…（支持 Markdown 子集：标题/粗斜体/代码块/列表/链接/引用）"
      />
      <div class="pane-color-tags">
        <span class="pane-block-label">色彩</span>
        <div class="color-row">
          <div
            v-for="c in COLOR_OPTIONS"
            :key="c.val"
            class="color-circle"
            :style="{ background: c.hex }"
            :class="{ selected: form.colorTag === c.val }"
            :title="c.name"
            @click="form.colorTag = c.val"
          ></div>
        </div>
        <span class="pane-block-label">标签</span>
        <div class="tags-input-box">
          <span v-for="t in form.tags" :key="t" class="tag-pill memo-tag memo-tag-removable">
            {{ t }} <span class="memo-tag-x" title="移除标签" @click="removeTag(t)">✕</span>
          </span>
          <input
            v-model="form.tagInput"
            type="text"
            class="text-input tag-entry"
            placeholder="例如 SQL, Token, 常用（回车添加，自动补 #）"
            aria-label="添加标签"
            @keydown.enter.prevent="addTagFromInput"
          />
        </div>
      </div>
    </div>

    <div v-show="editMode === 'preview'" class="write-body">
      <div v-if="form.content.trim()" class="md-preview" v-html="editorPreviewHtml" />
      <p v-else class="state-box preview-void">正文还是空的——先回「书写」写点东西。</p>
    </div>

    <footer class="pane-foot">
      <span class="foot-hint">Ctrl+Enter 保存 · Esc 放弃草稿</span>
      <UiButton variant="secondary" small @click="emit('cancel')">取消</UiButton>
      <UiButton variant="primary" small @click="save">
        {{ editing ? '保存修改' : '立即创建' }}
      </UiButton>
    </footer>
  </div>
</template>

<style scoped>
/* 皮逐字迁自宿主 MemoView 拆分前的 pane-write 家族（拆分不动相）；
   .pane-head/.pane-kind/.pane-foot 与阅读页同字面，两处各持一份 scoped 副本。 */
.pane-write { display: flex; flex-direction: column; gap: 8px; min-height: 0; flex: 1; }
.pane-head { display: flex; align-items: center; gap: 10px; min-width: 0; flex: none; }
.pane-kind { font-size: var(--text-sm); font-weight: 600; color: var(--color-text-muted); flex: none; }
.pane-foot { display: flex; align-items: center; justify-content: flex-end; gap: 8px; flex-wrap: wrap; flex: none; }
.foot-hint { margin-right: auto; font-size: var(--text-xs); color: var(--color-text-subtle); }
.write-body { display: flex; flex-direction: column; gap: 6px; overflow-y: auto; min-height: 0; max-width: 72ch; flex: 1; }
.pane-title-input { min-height: var(--control-h-md); font-size: var(--text-md); font-weight: 600; }
.pane-color-tags { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; min-width: 0; margin-top: 4px; }
.pane-block-label { font-size: var(--text-xs); color: var(--color-text-muted); flex: none; }
.color-row { display: flex; gap: 10px; padding: 2px 0; flex: none; }
.color-circle { width: 22px; height: 22px; border-radius: 50%; cursor: pointer; transition: transform var(--motion-fast) ease; }
.color-circle:hover { transform: scale(1.15); }
.color-circle.selected { outline: 2px solid var(--color-text); outline-offset: 2px; }
.tags-input-box {
  display: flex; flex-wrap: wrap; gap: 6px; align-items: center; min-width: 0; flex: 1 1 220px;
  border: 1px solid var(--color-border); border-radius: var(--radius-control); padding: 5px 8px; background: var(--surface-soft);
}
/* 样式审计 C10（2026-10-27）：删原 `outline: none`——无边框透明内联输入删后无任何焦点呈现，
   交还 base.css 全局 :focus-visible 环。 */
.tag-entry { flex: 1 1 120px; min-width: 0; border: none; background: transparent; padding: 3px 2px; color: var(--color-text); font-size: var(--text-base); }
.memo-tag-removable { display: inline-flex; align-items: center; gap: 4px; background: var(--surface-hover); }
.memo-tag-x { cursor: pointer; font-size: var(--text-micro); color: var(--color-text-subtle); }
.memo-tag-x:hover { color: var(--state-danger); }
.preview-void { background: transparent; border-style: dashed; }

.seg { display: flex; border: 1px solid var(--color-border); border-radius: var(--radius-control); overflow: hidden; flex: none; margin-left: auto; }
.seg-btn {
  background: var(--surface-soft); border: none; color: var(--color-text-muted);
  padding: 3px 12px; font-size: var(--text-sm); cursor: pointer;
  transition: background var(--motion-fast) ease, color var(--motion-fast) ease;
}
.seg-btn:hover { background: var(--surface-hover); color: var(--color-text); }
.seg-btn.active { background: var(--color-primary); color: var(--color-on-primary); }

/* 叠放档（容器查询归宿主 .memo-page 的 memo 容器，本件元素是其后代） */
@container memo (max-width: 760px) {
  .write-body { max-width: none; }
}
</style>
