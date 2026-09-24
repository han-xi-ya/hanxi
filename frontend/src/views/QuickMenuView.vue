<script setup lang="ts">
// 快捷菜单模块页：全局右键长按唤出能力的状态、条目预览与就地编辑。
// N5-C1：条目编辑面与托盘右键菜单同挂共享组件 TrayItemsEditor（数据本就是同一份
// settings.TrayMenu 账），配置改一处两面生效，不再来回跳页。
import { computed, ref, shallowRef, onMounted } from 'vue'
import * as QuickMenuAPI from '../../bindings/hanxi/internal/modules/quickmenu'
import type { MenuItem, Status } from '../../bindings/hanxi/internal/modules/quickmenu/models'
import { getErrorMessage } from '../utils/errors'
import { useToast } from '../composables/useToast'
import WheelPreview from '../components/quickmenu/WheelPreview.vue'
import TrayItemsEditor from '../components/tray/TrayItemsEditor.vue'

const emit = defineEmits<{
  (e: 'navigate', route: string): void
}>()

const { showToast } = useToast()

const status = shallowRef<Status | null>(null)
const items = shallowRef<MenuItem[]>([])
const loading = ref(true)
const errorMsg = ref('')
// 二级轮盘开关：勾选即存（与"常规偏好"同款热保存语义），保存后静默刷新条目
// 预览——开/关的树形态不同（分组展开 vs 拍平）。
const twoTier = ref(true)
const savingTier = ref(false)

const TYPE_LABEL: Record<string, string> = {
  exe: '程序',
  command: '命令',
  route: '页面',
  group: '分组',
}
const typeLabel = (t: string) => TYPE_LABEL[t] ?? '条目'

async function refresh() {
  loading.value = true
  errorMsg.value = ''
  try {
    const [, list] = await Promise.all([loadState(), QuickMenuAPI.QuickMenuService.ListItems()])
    items.value = list ?? []
  } catch (err) {
    errorMsg.value = getErrorMessage(err)
  } finally {
    loading.value = false
  }
}

// loadState 读取状态并同步开关回显；返回 list 由调用方自行接（组合 Promise）。
async function loadState() {
  const st = await QuickMenuAPI.QuickMenuService.GetStatus()
  status.value = st
  twoTier.value = st.twoTier
  holdMs.value = st.holdMs
  movePx.value = st.moveTol
  holdDraft.value = st.holdMs
  moveDraft.value = st.moveTol
  return st
}

// ---- N5-C2 触发参数（长按时长/位移容差）----
// 有效值口径在后端（钳制 200–1500ms / 4–64px，盘上坏值不武装钩子）；本页只
// 做草稿-保存两段式：改动亮"应用"，保存回显以钳后返回值为准，失败回滚草稿。
const holdMs = ref(450)
const movePx = ref(16)
const holdDraft = ref(450)
const moveDraft = ref(16)
const savingTrigger = ref(false)
const triggerDirty = computed(
  () => holdDraft.value !== holdMs.value || moveDraft.value !== movePx.value,
)

async function saveTrigger() {
  savingTrigger.value = true
  try {
    const [h, m] = await QuickMenuAPI.QuickMenuService.SetTriggerConfig(
      Number(holdDraft.value) || 0, Number(moveDraft.value) || 0,
    )
    holdMs.value = h
    movePx.value = m
    holdDraft.value = h
    moveDraft.value = m
  } catch (err) {
    errorMsg.value = getErrorMessage(err)
    try { await loadState() } catch { /* 回读失败保留错误文案，重试即再拉 */ }
  } finally {
    savingTrigger.value = false
  }
}

async function toggleTwoTier(on: boolean) {
  savingTier.value = true
  try {
    await QuickMenuAPI.QuickMenuService.SetTwoTier(on)
    const [, list] = await Promise.all([loadState(), QuickMenuAPI.QuickMenuService.ListItems()])
    items.value = list ?? []
  } catch (err) {
    errorMsg.value = getErrorMessage(err)
    await refresh() // 保存失败回滚回显，不私留"看起来已生效"的假状态
  } finally {
    savingTier.value = false
  }
}

// N6-C2/C3：只读页也挂同源预览盘（条目一变盘即变）；C1 内嵌编辑面后补写侧闭环
// ——点击盘格经身份（type+hint/组 label）定位下方编辑列行并选中，行上出「⇄ 互换」；
// 选中仅前端高亮，互换落盘仍走既有 SetTrayMenu 保存链（不新增后端写 RPC）。
const selected = ref<number | null>(null)
const editorRef = ref<InstanceType<typeof TrayItemsEditor> | null>(null)
function pickSector(i: number) {
  if (selected.value === i) {
    selected.value = null
    editorRef.value?.clearSelection()
    return
  }
  selected.value = i
  const m = items.value[i]
  if (!m || !editorRef.value?.locate({ type: m.type, hint: m.hint ?? '', label: m.label ?? '' })) {
    // 编辑列可能未加载/该扇区刚被删除或未保存——如实提示不静默
    showToast('已选中扇区，但编辑列表暂未能定位对应行（若条目刚改动请先保存或重试刷新）')
  }
}
// 盘重新拉取后扇区身份可能移位：清扇区选中与行选中，防错指。
async function reloadAfterSave() {
  selected.value = null
  editorRef.value?.clearSelection()
  try {
    items.value = (await QuickMenuAPI.QuickMenuService.ListItems()) ?? []
  } catch {
    await refresh() // 预览重拉失败退回整页刷新（错误态自带重试入口，不留假预览）
  }
}

// N5-C4 容量软警示：主盘推荐 ≤8（不禁止，扇区数=条目数自适应，越多越窄）。
const C4_RECOMMEND_MAX = 8
const capacityWarn = computed(() => {
  const n = items.value.length
  return n > C4_RECOMMEND_MAX
    ? `主盘已 ${n} 格（建议 ≤${C4_RECOMMEND_MAX}）：条目越多格子越窄，可把同类条目收进"分组"，悬停展开盘外子环收纳。`
    : ''
})

onMounted(refresh)
</script>

<template>
  <div class="page">
    <div class="header-row">
      <h1>快捷菜单</h1>
      <span
        v-if="status"
        class="chip"
        :class="status.trapActive ? 'chip-positive' : 'chip-warning'"
      >{{ status.trapActive ? '监听在位' : '钩子未启用' }}</span>
    </div>
    <p class="subtitle">
      在任意界面按住鼠标右键约 <b class="mono">{{ status ? status.holdMs : 450 }}ms</b>
      即刻在光标处弹出圆形快捷启动轮盘（无需松手）；按住中移动超过
      <b class="mono">{{ status ? status.moveTol : 16 }}px</b> 视为拖拽、自动让位给应用原生右键。
      普通右键（提前松开）完全不受影响，任务栏与托盘区亦自动让位。
    </p>

    <div v-if="loading" class="state-box">正在读取快捷菜单状态…</div>
    <div v-else-if="errorMsg" class="state-box state-error">
      加载失败：{{ errorMsg }}
      <button type="button" class="btn btn-small btn-secondary" @click="refresh">重试</button>
    </div>

    <template v-else>
      <section class="panel">
        <h2 class="sec-title">轮盘行为</h2>
        <label class="setting-row setting-row-tappable">
          <span class="setting-main">
            <span class="setting-name">启用二级轮盘</span>
            <span class="setting-desc">分组扇区悬停即在主盘外圈展开子环（点击扇区可钉住）；关闭时分组子条目直接拍平进主盘。修改即时生效。</span>
          </span>
          <input
            type="checkbox"
            class="switch"
            :checked="twoTier"
            :disabled="savingTier"
            @change="toggleTwoTier(($event.target as HTMLInputElement).checked)"
          />
        </label>
        <div class="setting-row">
          <span class="setting-main">
            <span class="setting-name">触发参数</span>
            <span class="setting-desc">
              右键按住多久唤出轮盘（200–1500ms，出厂 450）与抬手前允许的光标漂移（4–64px，出厂 16）。
              调短更跟手但普通右键易误触；调大防误触。保存即热更新全局钩子。
            </span>
            <span class="trigger-form">
              <label class="trigger-field">按住 <input v-model.number="holdDraft" type="number" min="200" max="1500" step="50" class="text-input trigger-num mono" aria-label="长按时长毫秒" /></label>
              <label class="trigger-field">ms · 位移容差 <input v-model.number="moveDraft" type="number" min="4" max="64" step="2" class="text-input trigger-num mono" aria-label="位移容差像素" /></label>
              <label class="trigger-field">px</label>
            </span>
          </span>
          <button type="button" class="btn btn-small btn-secondary" :disabled="!triggerDirty || savingTrigger" @click="saveTrigger">
            {{ savingTrigger ? '应用中…' : '应用' }}
          </button>
        </div>
      </section>

      <section class="panel">
        <h2 class="sec-title">当前条目</h2>
        <p class="sec-note">
          与托盘右键菜单共用同一份配置（本页下方「条目编辑」即可改，无需去设置页），主盘
          <b class="mono">{{ items.length }}</b> 个扇区{{ twoTier ? '（分组可展开子环）' : '（分组已拍平）' }}。
        </p>

        <div class="qm-layout">
        <div class="qm-list-col">
        <div v-if="items.length === 0" class="empty-state">
          <p>尚未配置任何条目。可在下方「条目编辑」中添加要快速启动的程序、托管命令或分组。</p>
        </div>
        <ul v-else class="item-list">
          <li v-for="(item, i) in items" :id="`qm-item-row-${i}`" :key="item.index" class="item-block" :class="{ 'is-picked': selected === i }">
            <div class="item-row">
              <span class="item-main">
                <span class="item-label">{{ item.label }}</span>
                <span v-if="item.hint" class="item-hint mono">{{ item.hint }}</span>
              </span>
              <span class="item-kind">{{ typeLabel(item.type) }}</span>
            </div>
            <ul v-if="item.children?.length" class="item-sub">
              <li v-for="kid in item.children" :key="kid.index" class="item-row item-row-sub">
                <span class="item-main">
                  <span class="item-label">{{ kid.label }}</span>
                  <span v-if="kid.hint" class="item-hint mono">{{ kid.hint }}</span>
                </span>
                <span class="item-kind">{{ typeLabel(kid.type) }}</span>
              </li>
            </ul>
          </li>
        </ul>

        <div class="panel-foot">
          <!-- N5-C1 后本页已内嵌编辑面，此跳设置入口暂保留（同一配置的等效入口，移除与否待后续批次裁决） -->
          <button type="button" class="btn btn-primary btn-small" @click="emit('navigate', '/settings/tray')">
            前往设置页配置
          </button>
        </div>
        </div>
        <aside class="qm-preview-col">
          <WheelPreview :items="items" :active-index="selected" :scale="0.9" @pick="pickSector" />
          <p v-if="capacityWarn" class="qm-cap-warn" role="status">{{ capacityWarn }}</p>
          <p class="qm-preview-hint">与你挂出的轮盘同一几何——点盘格可定位下方条目（再点取消；选中行上出「⇄ 互换」）</p>
        </aside>
        </div>
      </section>

      <!-- N5-C1：与设置页同挂共享编辑面（同一份 settings.TrayMenu 账），保存后托盘与轮盘同时生效 -->
      <section class="panel">
        <h2 class="sec-title">条目编辑</h2>
        <p class="sec-note">与「设置→托盘右键菜单」是同一份配置：此处勾选、排序、分组，保存后托盘菜单与本页面轮盘预览同时生效。</p>
        <TrayItemsEditor ref="editorRef" @saved="reloadAfterSave" />
      </section>

      <section class="panel usage">
        <h2 class="sec-title">使用说明</h2>
        <ul class="usage-list">
          <li>长按触发后菜单贴靠在光标处，屏幕边缘与多显示器下会自动钳位，不会被裁掉。</li>
          <li>点击条目即刻启动；<kbd>Esc</kbd> 或点击菜单外部（失焦）收起，鼠标离开即停不影响后续操作。</li>
          <li>设置页把条目组织进"分组"后，主盘对应扇区悬停即在盘外圈展开子环（点击扇区可钉住）；子环展开时 <kbd>Esc</kbd> 先收子环，再按收起整个轮盘。</li>
          <li>不想选任何条目时，向外甩出盘缘即进入半透明取消态，滑回盘面恢复或 <kbd>Esc</kbd> 收起；点击中心 hub 亦可收起。</li>
          <li>条目"命令"类会先懒初始化对应托管模块；"页面"类会唤出主窗口并导航。</li>
          <li>不需要此能力时，在设置页模块管理中将"快捷菜单"停用即可（全局钩子随停用即时摘除）。</li>
          <li>扇区数=条目数自适应，<b>建议主盘 ≤8</b>（非硬限）：超出盘面仍可用但格子变窄，把同类条目收进"分组"悬停展开子环是最省力的收纳方式。</li>
        </ul>
      </section>
    </template>
  </div>
</template>

<style scoped>
/* N6-C2：列表与同源预览盘并排；窄屏单列堆叠 */
.qm-layout { display: grid; grid-template-columns: minmax(0, 1fr) auto; gap: 16px; align-items: start; }
.qm-list-col { min-width: 0; }
.qm-preview-col { position: sticky; top: 12px; display: flex; flex-direction: column; align-items: center; gap: 6px; }
.qm-preview-hint { margin: 0; font-size: var(--text-xs); color: var(--color-text-subtle); text-align: center; }
.item-block.is-picked { outline: 2px solid var(--color-primary); outline-offset: 1px; border-radius: var(--radius-control); }
@media (max-width: 860px) { .qm-layout { grid-template-columns: minmax(0, 1fr); } .qm-preview-col { position: static; } }
.sec-title {
  font-size: var(--text-md);
  font-weight: 600;
  margin: 0 0 6px;
  color: var(--color-text);
}
.sec-note {
  font-size: var(--text-sm);
  color: var(--color-text-muted);
  margin: 0 0 10px;
  line-height: 1.6;
}

/* 条目预览：与弹窗同构的"名称 + 类型标记"行语法，hint 用 mono 呈现机器值 */
.item-list {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.item-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  align-items: center;
  gap: 10px;
  padding: 8px 10px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-control);
  background: var(--surface-soft);
}
/* 分组块：主行 + 缩进子行同框，视觉上与"分组→子盘"的层级对应 */
.item-block {
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.item-sub {
  list-style: none;
  margin: 0;
  padding: 0 0 0 18px;
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.item-row-sub {
  border-style: dashed;
  background: transparent;
}
.item-main {
  display: flex;
  flex-direction: column;
  min-width: 0;
  gap: 2px;
}
.item-label {
  font-size: var(--text-base);
  font-weight: 500;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.item-hint {
  font-size: var(--text-xs);
  color: var(--color-text-subtle);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.item-kind {
  font-size: var(--text-micro);
  color: var(--color-text-muted);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-pill);
  padding: 1px 7px;
  white-space: nowrap;
}

/* N5-C2 触发参数行：草稿数值对 + 应用钮同行，mono 机器值 */
.trigger-form { display: flex; flex-wrap: wrap; align-items: center; gap: 6px; margin-top: 6px; }
.trigger-field { display: inline-flex; align-items: center; gap: 4px; font-size: var(--text-sm); color: var(--color-text-muted); }
.trigger-num { width: 84px; padding: 3px 8px; }
.panel-foot {
  display: flex;
  justify-content: flex-end;
  margin-top: 12px;
}

/* 二级轮盘开关行原 .tier-* 家族与设置页 .setting-row/.setting-main/.setting-name/.setting-desc
   原子同义（soft/page 底色与 line-height 为 ±2px 级微差），scoped 副本删净、模板改挂全局原子落回 */
.switch {
  width: 18px;
  height: 18px;
  flex-shrink: 0;
  cursor: pointer;
  accent-color: var(--color-primary);
}

.usage {
  margin-top: 16px;
}
/* N5-C4：容量软警示（预览下缘黄字，不禁止保存） */
.qm-cap-warn { margin: 6px 0 0; font-size: var(--text-xs); color: var(--state-warning); line-height: 1.5; }
.usage-list {
  margin: 0;
  padding-left: 18px;
  display: flex;
  flex-direction: column;
  gap: 6px;
  font-size: var(--text-sm);
  color: var(--color-text-muted);
  line-height: 1.65;
}
kbd {
  font-family: var(--font-mono);
  font-size: var(--text-xs);
  border: 1px solid var(--color-border-strong);
  border-bottom-width: 2px;
  border-radius: 4px;
  padding: 0 5px;
  background: var(--surface-soft);
  color: var(--color-text);
}
</style>
