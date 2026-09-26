<script setup lang="ts">
// 快捷菜单模块页：全局右键长按唤出能力的状态、条目预览与就地编辑。
// 重设计批次（机主授权整页重排，功能零增删）：
//   页头状态区（钩子 chip + 关键参数 chip 化一览）→ 左右双栏主区
//   （左=条目编辑 TrayItemsEditor 主场，右=轮盘预览+当前条目常驻 sticky）
//   → 行为设置与使用说明降为底部并排次级折叠。
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
// ——点击盘格经身份（type+hint/组 label）定位编辑列行并选中，行上出「⇄ 互换」；
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
  <div class="page qm-page">
    <!-- ① 页头状态区：钩子是否在位 + 关键参数 chip 化一览（数值不再散进长句） -->
    <header class="panel qm-state">
      <div class="header-row">
        <h1>快捷菜单</h1>
        <span
          v-if="status"
          class="chip"
          :class="status.trapActive ? 'chip-positive' : 'chip-warning'"
        >{{ status.trapActive ? '监听在位' : '钩子未启用' }}</span>
      </div>
      <p class="subtitle">
        在任意界面按住鼠标右键即刻在光标处弹出圆形快捷启动轮盘（无需松手）；提前松手的
        普通右键完全不受影响，任务栏与托盘区亦自动让位。
      </p>
      <div v-if="status" class="qm-params" aria-label="关键参数一览">
        <span class="chip chip-neutral qm-param">
          <span class="qm-param-k">触发时长</span><b class="mono qm-param-v">{{ status.holdMs }}ms</b>
        </span>
        <span class="chip chip-neutral qm-param">
          <span class="qm-param-k">位移容差</span><b class="mono qm-param-v">{{ status.moveTol }}px</b>
        </span>
        <span class="chip chip-neutral qm-param">
          <span class="qm-param-k">二级轮盘</span><b class="qm-param-v">{{ twoTier ? '开' : '关' }}</b>
        </span>
        <span class="chip chip-neutral qm-param">
          <span class="qm-param-k">条目</span><b class="mono qm-param-v">{{ items.length }}</b>
        </span>
      </div>
    </header>

    <div v-if="loading" class="state-box">正在读取快捷菜单状态…</div>
    <div v-else-if="errorMsg" class="state-box state-error">
      加载失败：{{ errorMsg }}
      <button type="button" class="btn btn-small btn-secondary" @click="refresh">重试</button>
    </div>

    <template v-else>
      <!-- ② 双栏主区：左=条目编辑主场，右=轮盘预览+当前条目（宽屏 sticky 常驻） -->
      <div class="qm-main">
        <section class="panel qm-edit-col">
          <h2 class="sec-title">条目编辑</h2>
          <p class="sec-note">
            与「设置→托盘右键菜单」是同一份配置：此处勾选、排序、分组，保存后托盘菜单与右侧轮盘预览同时生效。
          </p>
          <!-- N5-C1：与设置页同挂共享编辑面（同一份 settings.TrayMenu 账），保存后托盘与轮盘同时生效 -->
          <TrayItemsEditor ref="editorRef" @saved="reloadAfterSave" />
          <div class="qm-edit-foot">
            <!-- 重设计：编辑已内嵌后"前往设置页配置"大钮属冗余入口，降为页脚等效链接（navigate 契约保留） -->
            <button
              type="button"
              class="link-button qm-settings-link"
              @click="emit('navigate', '/settings/tray')"
            >同一份配置也可在「设置 → 托盘右键菜单」分区里改</button>
          </div>
        </section>

        <aside class="qm-side-col">
          <section class="panel qm-preview-panel">
            <h2 class="sec-title">轮盘预览</h2>
            <div class="qm-preview-box">
              <WheelPreview :items="items" :active-index="selected" :scale="0.9" @pick="pickSector" />
            </div>
            <p v-if="capacityWarn" class="qm-cap-warn" role="status">{{ capacityWarn }}</p>
            <p class="qm-preview-hint">与你挂出的轮盘同一几何——点盘格可定位编辑列对应行（再点取消；选中行上出「⇄ 互换」）</p>
          </section>

          <section class="panel qm-items-panel">
            <h2 class="sec-title">当前条目</h2>
            <p class="sec-note">
              主盘 <b class="mono">{{ items.length }}</b> 个扇区{{ twoTier ? '（分组可展开子环）' : '（分组已拍平）' }}。
            </p>
            <div class="qm-items-scroll">
              <div v-if="items.length === 0" class="empty-state">
                <p>尚未配置任何条目。可在「条目编辑」中添加要快速启动的程序、托管命令或分组。</p>
              </div>
              <ul v-else class="item-list">
                <li v-for="(item, i) in items" :key="item.index" class="item-block" :class="{ 'is-picked': selected === i }">
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
            </div>
          </section>
        </aside>
      </div>

      <!-- ③ 次级区：行为设置（含触发参数草稿-应用两段式）与使用说明并排折叠，默认收起 -->
      <div class="qm-secondary">
        <details class="info-details qm-behave">
          <summary class="info-summary">轮盘行为与触发参数</summary>
          <div class="info-body">
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
          </div>
        </details>

        <details class="info-details qm-usage">
          <summary class="info-summary">使用说明</summary>
          <div class="info-body">
            <ul class="usage-list">
              <li>长按触发后菜单贴靠在光标处，屏幕边缘与多显示器下会自动钳位，不会被裁掉。</li>
              <li>按住中移动超过「位移容差」（见页头参数一览）即视为拖拽，自动让位给应用原生右键，不误弹轮盘。</li>
              <li>点击条目即刻启动；<kbd>Esc</kbd> 或点击菜单外部（失焦）收起，鼠标离开即停不影响后续操作。</li>
              <li>按住时长与位移容差在上方「轮盘行为与触发参数」里可调：调短更跟手、普通右键易误触，调大反之。</li>
              <li>把条目组织进"分组"后，主盘对应扇区悬停即在盘外圈展开子环（点击扇区可钉住）；子环展开时 <kbd>Esc</kbd> 先收子环，再按收起整个轮盘。</li>
              <li>不想选任何条目时，向外甩出盘缘即进入半透明取消态，滑回盘面恢复或 <kbd>Esc</kbd> 收起；点击中心 hub 亦可收起。</li>
              <li>条目"命令"类会先懒初始化对应托管模块；"页面"类会唤出主窗口并导航。</li>
              <li>不需要此能力时，在设置页模块管理中将"快捷菜单"停用即可（全局钩子随停用即时摘除）。</li>
              <li>扇区数=条目数自适应，<b>建议主盘 ≤8</b>（非硬限）：超出盘面仍可用但格子变窄，把同类条目收进"分组"悬停展开子环是最省力的收纳方式。</li>
            </ul>
          </div>
        </details>
      </div>
    </template>
  </div>
</template>

<style scoped>
/* 页级纵堆节奏：状态区 → 双栏主区 → 次级折叠区 */
.qm-page { display: flex; flex-direction: column; gap: 12px; }

/* ① 状态区参数 chip 行：键 muted、机器值 mono，全部可换行不撑横滚 */
.qm-params { display: flex; flex-wrap: wrap; gap: 6px; margin-top: 4px; }
.qm-param { gap: 4px; }
.qm-param-k { color: var(--color-text-muted); }
.qm-param-v { font-size: var(--text-xs); color: var(--color-text); }

/* ② 双栏主区：左编辑主场吃满剩余宽，右侧栏 sticky 常驻；390px 门禁靠 minmax(0,…) */
.qm-main { display: grid; grid-template-columns: minmax(0, 1fr) auto; gap: 12px; align-items: start; }
.qm-edit-col { min-width: 0; }
.qm-side-col {
  position: sticky; top: 12px;
  display: flex; flex-direction: column; gap: 12px;
  min-width: 0; max-width: min(340px, 100%);
}
.qm-preview-box { display: flex; justify-content: center; min-width: 0; }
.qm-preview-hint { margin: 0; font-size: var(--text-xs); color: var(--color-text-subtle); text-align: center; }
/* 条目数远超推荐值时右侧栏不顶破视口：列表区内滚，页面本身不横滚 */
.qm-items-scroll { max-height: 46vh; overflow-y: auto; overflow-x: hidden; }

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
.item-block.is-picked { outline: 2px solid var(--color-primary); outline-offset: 1px; border-radius: var(--radius-control); }
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

/* N5-C1 后编辑面已内嵌，设置页跳钮降为编辑面板页脚等效链接 */
.qm-edit-foot { display: flex; justify-content: flex-end; margin-top: 12px; }

/* N5-C4：容量软警示（预览下缘黄字，不禁止保存） */
.qm-cap-warn { margin: 6px 0 0; font-size: var(--text-xs); color: var(--state-warning); line-height: 1.5; }

/* ③ 次级折叠区：宽屏两列并排，窄屏自然落单列（min() 防 320 轨道撑破窄容器） */
.qm-secondary { display: grid; grid-template-columns: repeat(auto-fit, minmax(min(320px, 100%), 1fr)); gap: 12px; }
.qm-behave .info-body { gap: 10px; }
.qm-behave .setting-row { flex-wrap: wrap; }

/* N5-C2 触发参数行：草稿数值对 + 应用钮同行，mono 机器值 */
.trigger-form { display: flex; flex-wrap: wrap; align-items: center; gap: 6px; margin-top: 6px; }
.trigger-field { display: inline-flex; align-items: center; gap: 4px; font-size: var(--text-sm); color: var(--color-text-muted); }
.trigger-num { width: 84px; padding: 3px 8px; }

/* 二级轮盘开关行挂全局 .setting-row/.setting-main/.setting-name/.setting-desc 原子；
   switch 视觉尺寸沿用本页原 18px 档 */
.switch {
  width: 18px;
  height: 18px;
  flex-shrink: 0;
  cursor: pointer;
  accent-color: var(--color-primary);
}

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
/* 窄屏单列纵堆：状态→预览(含当前条目)→编辑→次级设置；sticky 与内滚随之解除 */
@media (max-width: 760px) {
  .qm-main { grid-template-columns: minmax(0, 1fr); }
  .qm-side-col { position: static; max-width: 100%; order: -1; }
  .qm-items-scroll { max-height: none; }
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

/* ≤760px：单列纵堆，DOM 序（编辑在前）经 order 调整为 状态→预览(含条目列表)→编辑→设置折叠 */
@media (max-width: 760px) {
  .qm-main { grid-template-columns: minmax(0, 1fr); }
  .qm-side-col { position: static; order: -1; max-width: none; align-items: stretch; }
  .qm-preview-box { justify-content: center; }
}
</style>
