<script setup lang="ts">
// 快捷菜单模块页：全局右键长按唤出能力的状态、条目预览与就地编辑。
// v3 布局批（机主授权整页重排，功能零增删）：主角是「条目编辑 ⟷ 轮盘舱」闭环——
//   页头一行状态条（chip 全绑确认值 refs）→ 双栏主区（左=TrayItemsEditor 编辑主场，
//   自动保存状态条 sticky 沉底；右=轮盘舱 sticky：预览 + 皮肤，「当前条目」镜像面板已删）
//   → 行为设置与使用说明沉底并排折叠原位。
//   断点一律容器查询（页容器实测宽：宽档 C/D 双栏，<840 单列编辑在前+舱紧凑横条，
//   机主 628 截图批复），页面容器升 wide 1440 档。
// N5-C1：条目编辑面与托盘右键菜单同挂共享组件 TrayItemsEditor；轮盘独立批后本页
// 传 scope="wheel" 打轮盘专属账（settings.WheelMenu ⇄ Get/SetWheelMenu），与设置页
// 托盘账各改各的、互不影响，改动实时自动保存无需按钮。
import { computed, ref, shallowRef, onMounted, onUnmounted } from 'vue'
import * as QuickMenuAPI from '../../bindings/hanxi/internal/modules/quickmenu'
import type { MenuItem } from '../../bindings/hanxi/internal/modules/quickmenu/models'
import { getErrorMessage } from '../utils/errors'
import { useToast } from '../composables/useToast'
import WheelPreview from '../components/quickmenu/WheelPreview.vue'
import TrayItemsEditor from '../components/tray/TrayItemsEditor.vue'
import {
  DEFAULT_WHEEL_SKIN, FACE_ALPHA_MIN, fetchWheelSkin, pushWheelSkin, readWheelSkin, saveWheelSkin,
  WHEEL_SKIN_PRESETS, WHEEL_SKIN_PRESET_LABEL,
  type WheelSkin, type WheelSkinPreset,
} from '../components/quickmenu/wheelSkin'

const emit = defineEmits<{
  (e: 'navigate', route: string): void
}>()

const { showToast } = useToast()

// 页头 chip 一律绑"确认值 refs"（holdMs/movePx/twoTier/items/trapActive），不再直读
// 状态快照：快照只在 loadState 刷新，saveTrigger 钳后回写不进快照——曾致"应用后
// 页头参数章不回显"的说谎 bug（v3 T1 收口，status.* 退出模板）。
const trapActive = ref<boolean | null>(null) // null=首刷未回，状态章不猜
const items = shallowRef<MenuItem[]>([])
const loading = ref(true)
const errorMsg = ref('')
// 二级轮盘开关：勾选即存（与"常规偏好"同款热保存语义），保存后静默刷新条目
// 预览——开/关的树形态不同（分组展开 vs 拍平）。
const twoTier = ref(true)
const savingTier = ref(false)

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
  trapActive.value = st.trapActive
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
// 选中仅前端高亮，互换落盘仍走编辑面既有实时保存链（scope=wheel 时即 SetWheelMenu）。
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
    showToast('已选中扇区，但编辑列表暂未能定位对应行（若条目刚改动请稍候自动保存完成或重试刷新）')
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

// v3 轮盘放量分档（与 CSS @container 换形同一实测源，禁各自为政）：页容器宽
// <840 窄档=紧凑横条——盘面 trim 满格窗 + scale 0.56（盒 179px ≤180 上限）；
// ≥1360（D 档）升 1.25；其余 0.9。驱动走容器实测（ResizeObserver）而非
// useMediaQuery——断点按页容器宽切，scale 若跟视口走，1440 上限先于视口收窄
// 的宽窗里会"舱按 C 档、盘按 D 档"错配。回落口径=窄档（qmWidth 0：happy-dom
// RO 空实现与真实首帧未测得时，与窄端降级形态同向，宁紧勿滥）。
const CABIN_STRIP_BREAK = 840
const WHEEL_SCALE_STRIP = 0.56
const WHEEL_SCALE_BASE = 0.9
const WHEEL_SCALE_WIDE = 1.25
const WHEEL_SCALE_BREAK = 1360
const pageEl = ref<HTMLElement | null>(null)
const qmWidth = ref(0)
const cabinStrip = computed(() => qmWidth.value < CABIN_STRIP_BREAK)
const wheelScale = computed(() =>
  cabinStrip.value ? WHEEL_SCALE_STRIP : qmWidth.value >= WHEEL_SCALE_BREAK ? WHEEL_SCALE_WIDE : WHEEL_SCALE_BASE,
)
let pageScaleRO: ResizeObserver | null = null
onMounted(() => {
  // 老内核（@container 缺失）探测：CSS 侧换不了紧凑横条形，script 侧也别缩盘——
  // 强制宽档口径（trim 关、scale 0.9），布局自然落基架单列大卡，不出现"轨大盘小"。
  if (typeof CSS !== 'undefined' && !CSS.supports?.('container-type', 'inline-size')) {
    qmWidth.value = CABIN_STRIP_BREAK + 1
    return
  }
  if (!pageEl.value || typeof ResizeObserver === 'undefined') return
  pageScaleRO = new ResizeObserver((entries) => {
    const w = entries[entries.length - 1]?.contentRect.width ?? 0
    if (w > 0) qmWidth.value = w
  })
  pageScaleRO.observe(pageEl.value)
})
onUnmounted(() => { pageScaleRO?.disconnect(); pageScaleRO = null })

// N5-C4 容量软警示：主盘推荐 ≤8（不禁止，扇区数=条目数自适应，越多越窄）。
const C4_RECOMMEND_MAX = 8
const capacityWarn = computed(() => {
  const n = items.value.length
  return n > C4_RECOMMEND_MAX
    ? `主盘已 ${n} 格（建议 ≤${C4_RECOMMEND_MAX}）：条目越多格子越窄，可把同类条目收进"分组"，悬停展开盘外子环收纳。`
    : ''
})

// ---- 轮盘皮肤（机主拍板 2026-09-26"轮盘皮肤做"；后端化收权批）----
// 真相账在后端 quickmenu（GetSkin/SetSkin，随 hanxidata 数据目录走 NAS），
// 读写封装在 wheelSkin 模块双源函数；localStorage 退为镜像缓存（降级兜底 +
// 跨窗即时）。拨杆/勾选即时生效——本页预览靠响应式 skin 换皮，挂出的轮盘经
// 同源 storage 事件 + 每次唤出重读跟皮，故无"保存"按钮（与二级轮盘开关同语义）。
const skin = ref<WheelSkin>(readWheelSkin())
const savingSkin = ref(false)
/** 透纱滑杆量程（%）：alpha 域 [FACE_ALPHA_MIN,1] 反翻成 0..(1-MIN) 正向"越透越大" */
const SKIN_VEIL_MAX = Math.round((1 - FACE_ALPHA_MIN) * 100)
// 两段式（对齐滑杆手感）：input（连拖）只写本地镜像即时预览，change（松手/落定）
// 才上后端真相；回显按"最后写者胜"应用（seq 门闩，与取色侧同款），防旧往返迟归覆新。
function setSkinLocal(patch: Partial<WheelSkin>) {
  skin.value = saveWheelSkin(patch, skin.value)
}
let skinWriteSeq = 0
async function commitSkin() {
  const target = skin.value
  const mine = ++skinWriteSeq
  savingSkin.value = true
  try {
    const eff = await pushWheelSkin(target) // 后端为真相：回显（钳域归一后）覆本地
    if (mine !== skinWriteSeq) return // 更新一笔已在途：本轮回显作废
    skin.value = saveWheelSkin(eff, eff) // 回显落镜像（弹窗下次唤出读到归正后的真相）
  } catch (err) {
    if (mine !== skinWriteSeq) return
    // 后端不可达：皮肤是纯视觉账，如实 toast（不劫持页面级加载错误态把编辑器
    // 整片换脸）——本地镜像先行值保留，下次落定/唤出重读后端自然纠正。
    showToast(`轮盘皮肤未能存入设置账本，先按本机生效：${getErrorMessage(err)}`)
  } finally {
    if (mine === skinWriteSeq) savingSkin.value = false
  }
}
function setSkinPreset(preset: WheelSkinPreset) {
  setSkinLocal({ preset })
  void commitSkin()
}
function resetSkin() {
  setSkinLocal(DEFAULT_WHEEL_SKIN)
  void commitSkin()
}
const veilPercent = computed(() => Math.round((1 - skin.value.faceAlpha) * 100))
const strokePercent = computed(() => Math.round(skin.value.stroke * 100))

// 皮肤初值以真相为准：后端可达则覆盖镜像（含 NAS 换机后镜像陈旧的场景），
// 不可达保留本地镜像兜底——两条路径都已在 onMounted 首刷一次。
onMounted(async () => {
  await refresh()
  try {
    skin.value = saveWheelSkin(await fetchWheelSkin(), skin.value)
  } catch {
    /* 后端不可达：镜像已是最新可读态，保留 */
  }
})
</script>

<template>
  <div ref="pageEl" class="page-wide qm-page">
    <!-- ① 页头一行状态条：标题、钩子章与参数 chip 同轨（chip 绑确认值 refs，应用即回显） -->
    <header class="panel qm-state">
      <div class="header-row">
        <h1>快捷菜单</h1>
        <span
          v-if="trapActive !== null"
          class="chip"
          :class="trapActive ? 'chip-positive' : 'chip-warning'"
        >{{ trapActive ? '监听在位' : '钩子未启用' }}</span>
        <div class="qm-params" aria-label="关键参数一览">
          <span class="chip chip-neutral qm-param">
            <span class="qm-param-k">触发时长</span><b class="mono qm-param-v">{{ holdMs }}ms</b>
          </span>
          <span class="chip chip-neutral qm-param">
            <span class="qm-param-k">位移容差</span><b class="mono qm-param-v">{{ movePx }}px</b>
          </span>
          <span class="chip chip-neutral qm-param">
            <span class="qm-param-k">二级轮盘</span><b class="qm-param-v">{{ twoTier ? '开' : '关' }}</b>
          </span>
          <span class="chip chip-neutral qm-param">
            <span class="qm-param-k">条目</span><b class="mono qm-param-v">{{ items.length }}</b>
          </span>
        </div>
      </div>
      <p class="subtitle">按住右键即在光标处弹出快捷轮盘，提前松手的普通右键不受影响；任务栏与托盘区自动让位。</p>
    </header>

    <div v-if="loading" class="state-box">正在读取快捷菜单状态…</div>
    <div v-else-if="errorMsg" class="state-box state-error">
      加载失败：{{ errorMsg }}
      <button type="button" class="btn btn-small btn-secondary" @click="refresh">重试</button>
    </div>

    <template v-else>
      <!-- ② 双栏主区：左=条目编辑主场（自动保存状态条 sticky 沉底），右=轮盘舱（预览+皮肤，宽栏 sticky 常驻） -->
      <div class="qm-main">
        <section class="panel qm-edit-col">
          <h2 class="sec-title">条目编辑</h2>
          <p class="sec-note">
            轮盘专属配置，与「设置→托盘右键菜单」各记各账、互不影响：此处勾选、排序、分组只改轮盘。改动实时自动保存，无需按保存钮；分组未补全（无名或零子条目）时该笔暂缓落盘，补全或移除后自动续存。
          </p>
          <!-- 轮盘独立批：同挂共享编辑面但 scope='wheel' 打轮盘专属账（Get/SetWheelMenu），每次落盘上抛 saved 刷预览 -->
          <TrayItemsEditor ref="editorRef" scope="wheel" @saved="reloadAfterSave" />
          <div class="qm-edit-foot">
            <!-- 重设计：编辑已内嵌后"前往设置页配置"大钮属冗余入口，降为页脚等效链接（navigate 契约保留） -->
            <button
              type="button"
              class="link-button qm-settings-link"
              @click="emit('navigate', '/settings/tray')"
            >任务栏托盘入口请在「设置 → 托盘右键菜单」分区里单独配置</button>
          </div>
        </section>

        <aside class="qm-side-col">
          <section class="panel qm-preview-panel">
            <h2 class="sec-title">轮盘预览</h2>
            <div class="qm-preview-box" title="与你挂出的轮盘同一几何同一皮肤——点盘格可定位编辑列对应行（再点取消；选中行上出「⇄ 互换」）">
              <WheelPreview :items="items" :active-index="selected" :scale="wheelScale" :trim="cabinStrip" :skin="skin" @pick="pickSector" />
            </div>
            <p v-if="capacityWarn" class="qm-cap-warn" role="status">{{ capacityWarn }}</p>
            <p class="qm-preview-hint">与你挂出的轮盘同一几何同一皮肤——点盘格可定位编辑列对应行（再点取消；选中行上出「⇄ 互换」）</p>
          </section>

          <section class="panel qm-skin-panel">
            <h2 class="sec-title">轮盘皮肤</h2>
            <p class="sec-note">
              纯视觉偏好，拨存即生效：上方预览与挂出的轮盘同步换皮。随设置账本存入数据目录（NAS 同步随身），不进条目配置。
            </p>
            <div class="qm-skin-row" role="radiogroup" aria-label="盘面色预设">
              <span class="qm-skin-k">盘面色</span>
              <span class="qm-skin-presets">
                <button
                  v-for="p in WHEEL_SKIN_PRESETS"
                  :key="p"
                  type="button"
                  class="chip qm-skin-chip"
                  :class="{ 'is-on': skin.preset === p }"
                  role="radio"
                  :aria-checked="skin.preset === p"
                  :disabled="savingSkin"
                  @click="setSkinPreset(p)"
                ><span class="qm-skin-dot" :class="`dot-${p}`" aria-hidden="true"></span>{{ WHEEL_SKIN_PRESET_LABEL[p] }}</button>
              </span>
            </div>
            <label class="qm-skin-row">
              <span class="qm-skin-k">盘面透明</span>
              <input
                type="range"
                class="qm-skin-range"
                min="0"
                :max="String(SKIN_VEIL_MAX)"
                step="5"
                :value="veilPercent"
                aria-label="盘面透明度百分比"
                @input="setSkinLocal({ faceAlpha: 1 - Number(($event.target as HTMLInputElement).value) / 100 })"
                @change="commitSkin"
              />
              <b class="mono qm-param-v qm-skin-v">{{ veilPercent }}%</b>
            </label>
            <label class="qm-skin-row">
              <span class="qm-skin-k">描边强度</span>
              <input
                type="range"
                class="qm-skin-range"
                min="0"
                max="100"
                step="5"
                :value="strokePercent"
                aria-label="描边强度百分比"
                @input="setSkinLocal({ stroke: Number(($event.target as HTMLInputElement).value) / 100 })"
                @change="commitSkin"
              />
              <b class="mono qm-param-v qm-skin-v">{{ strokePercent }}%</b>
            </label>
            <label class="setting-row setting-row-tappable qm-skin-row">
              <span class="setting-main">
                <span class="setting-name">跟随模块色</span>
                <span class="setting-desc">扇区描边改用条目真图标的主色调；灰色图标与矢量图标回落类型色。</span>
              </span>
              <input
                type="checkbox"
                class="switch"
                :checked="skin.followModuleColor"
                aria-label="跟随模块色"
                :disabled="savingSkin"
                @change="setSkinLocal({ followModuleColor: ($event.target as HTMLInputElement).checked }); commitSkin()"
              />
            </label>
            <div class="qm-skin-foot">
              <button type="button" class="link-button" :disabled="savingSkin" @click="resetSkin">恢复默认皮肤</button>
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
              <li>条目改动实时自动保存，无需按保存钮；唯一例外是<b>不完整的分组</b>（没起名或还没有子条目）——这类草稿暂缓落盘，补全或把分组移除后自动续存，页脚状态条会如实说明。</li>
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
/* 页级纵堆节奏：状态条 → 双栏主区 → 次级折叠区。
   v3 换形枢纽：本元素即容器查询的容器（查询名 qm，容器回调 scale 也测它），
   断点全部按页容器实测宽走，与 wide 1440 上限自洽；老内核 @container 失效时
   自动落回下方单列基架，即窄端降级形态。 */
.qm-page {
  display: flex; flex-direction: column; gap: 12px;
  container: qm / inline-size;
  min-width: 0;
}

/* ① 页头一行状态条：参数 chip 与标题、钩子章同轨右推；键 muted、机器值 mono */
.qm-state .header-row { align-items: center; }
.qm-state h1 { margin: 0; }
.qm-params { display: flex; flex-wrap: wrap; gap: 6px; margin: 0 0 0 auto; justify-content: flex-end; }
.qm-param { gap: 4px; }
.qm-param-k { color: var(--color-text-muted); }
.qm-param-v { font-size: var(--text-xs); color: var(--color-text); }

/* ② 主区基架=窄档（<840 及老内核 @container 失效降级）：单列，DOM 序编辑主场
   在前、轮盘舱在后——机主 628 批复：工作台首屏主角是编辑列表，舱不再置顶。
   窄档的紧凑横条换形与 C/D 双栏见文末容器组；390px 门禁靠 minmax(0,…)。 */
.qm-main { display: grid; grid-template-columns: minmax(0, 1fr); gap: 12px; align-items: start; }
.qm-edit-col { min-width: 0; }
.qm-side-col { display: flex; flex-direction: column; gap: 12px; min-width: 0; }
.qm-preview-box { display: flex; justify-content: center; min-width: 0; }
/* 盘体宽由 scale 定死（calc 320×scale），窄档 trim 已把 512 窗死边收零、盘面
   满格；轨仍窄于盘时按轨收方（slot 锚点为百分比，随盒收缩不失真）兜极窄窗 */
.qm-preview-box :deep(.wp) { max-width: 100%; }
.qm-preview-hint { margin: 0; font-size: var(--text-xs); color: var(--color-text-subtle); text-align: center; }

/* 分区标题/副注标准形（与 Softver/MsgBoard 同款 scoped 副本）：本页此前未定义，
   h2 吃 UA 1.5em——"大标题大留白"的业余感根子之一，一并收进 --text-md 档 */
.sec-title { font-size: var(--text-md); font-weight: 600; margin: 0; color: var(--color-text); }
.sec-note { font-size: var(--text-sm); color: var(--color-text-muted); margin: 0; line-height: 1.6; }
/* 卡内纵堆节奏收口：标题/副注/内容与 6–8px 微距对齐既有档位（此前无节奏全靠块流） */
.qm-edit-col, .qm-state { display: flex; flex-direction: column; gap: 8px; }
.qm-preview-panel { display: flex; flex-direction: column; gap: 6px; }

/* v3 状态条跟手：编辑列长流时自动保存状态条吸底常驻（宿主 scoped :deep 覆层，
   TrayItemsEditor 本体零改动；设置页宿主不受波及） */
.qm-edit-col :deep(.tray-footer) {
  position: sticky;
  bottom: 10px;
  z-index: 2;
  padding: 10px 12px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-control);
  background: var(--surface-panel);
  box-shadow: var(--shadow-small);
}

/* 皮肤面板：键左控件右的紧凑行；预设 chip 带色点，行距吃页级 gap 节奏 */
.qm-skin-panel { display: flex; flex-direction: column; gap: 8px; }
.qm-skin-row { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.qm-skin-k { flex: none; font-size: var(--text-xs); color: var(--color-text-muted); min-width: 4em; }
.qm-skin-presets { display: inline-flex; gap: 4px; flex-wrap: wrap; }
.qm-skin-chip { gap: 5px; cursor: pointer; border: 1px solid transparent; }
.qm-skin-chip.is-on { border-color: var(--color-primary); color: var(--color-primary); background: var(--color-primary-soft); }
.qm-skin-dot { width: 10px; height: 10px; border-radius: 50%; border: 1px solid var(--color-border-strong); flex: none; }
/* 色点 = 各预设盘面色口径的缩影（与盘面配方同源 color-mix，不引入新裸色） */
.qm-skin-dot.dot-frost { background: var(--surface-panel); }
.qm-skin-dot.dot-veil { background: color-mix(in srgb, var(--color-primary) 12%, var(--surface-panel)); }
.qm-skin-dot.dot-ink { background: color-mix(in srgb, var(--color-text) 15%, var(--surface-panel)); }
.qm-skin-range { flex: 1; min-width: 90px; accent-color: var(--color-primary); }
.qm-skin-v { min-width: 3.2em; text-align: right; font-size: var(--text-xs); }
.qm-skin-foot { display: flex; justify-content: flex-end; }
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

/* ---------- v3 换形容器组（查询名 qm = .qm-page 实测宽） ----------
   窄档 <840：舱降级为一条紧凑横条——条身自披卡面（padding/border/底一律用
   既有档位值），两块 section 卸卡面只留内容；盘居左一格（180px 格吃 scale
   0.56 的 179px trim 满格盘，机主"≤180 居左"批复），皮肤控件挤右侧纵列收进
   ~6 行；一切长说明退流（同文案挂 .qm-preview-box 与盘格 title 悬浮可达，宽档
   全量在场）。编辑主场按 DOM 序稳在条前，首屏主角=条目列表。 */
@container qm (max-width: 839.98px) {
  .qm-side-col {
    flex-direction: row; align-items: flex-start;
    padding: 10px 12px;
    border: 1px solid var(--color-border);
    border-radius: var(--radius-element);
    background: var(--surface-panel);
    box-shadow: var(--shadow-small);
  }
  .qm-side-col > .panel { padding: 0; border: none; background: transparent; box-shadow: none; }
  .qm-preview-panel { flex: 0 0 180px; min-width: 0; }
  .qm-skin-panel { flex: 1; min-width: 0; gap: 4px; }
  .qm-side-col .sec-note, .qm-side-col .qm-preview-hint, .qm-side-col .setting-desc { display: none; }
}

/* C 档 840–1359：双栏——编辑主场 min 460 吃剩余，舱固定 340 右列 sticky 常驻，
   恢复双卡全量形态（盘 0.9 窗 288 落 340−32 轨内） */
@container qm (min-width: 840px) {
  .qm-main { grid-template-columns: minmax(460px, 1fr) 340px; }
  .qm-side-col { position: sticky; top: 12px; }
}

/* D 档 ≥1360：舱随页宽 30% 放粗（380–440 封顶），盘 scale 1.25 由 script 侧
   容器回调同线升档（同一实测宽、同一分档线，换形与放量必同步） */
@container qm (min-width: 1360px) {
  .qm-main { grid-template-columns: minmax(0, 1fr) clamp(380px, 30%, 440px); }
}
</style>
