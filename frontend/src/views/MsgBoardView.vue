<script setup lang="ts">
// 桌面留言板模块页（N6 重设计）：左侧"类型速挂 → 正文编辑 → 挂/撤动作"
// 高频动线，右侧与挂牌弹窗同源的便利贴实时预览（所见即所得由 BoardCard
// 单组件保证）；字号/目标屏/热键等低频配置收进折叠区，异常时汇总警示。
// N30 补口：右侧预览可放大为「全屏预览」纯前端浮层（同一 BoardCard 全尺寸
// 呈现，Esc/点击即退，不走挂牌链路）；小预览缩放比按字号动态收缩防横向裁切。
// 数据闸口与后端契约零改动：pullAll 并行拉取、脏判定以服务端回读为准、
// 热键占用失败只回滚热键字段保留草稿——原纪律原样保留。
import { computed, onBeforeUnmount, onMounted, ref, shallowRef, watch } from 'vue'
import * as MsgBoardAPI from '../../bindings/hanxi/internal/modules/msgboard'
import type { Config, ScreenInfo, Status } from '../../bindings/hanxi/internal/modules/msgboard/models'
import { useWailsEvent } from '../composables/useWailsEvent'
import { getErrorMessage } from '../utils/errors'
import PageHeader from '../components/ui/PageHeader.vue'
import UiStatusChip from '../components/ui/UiStatusChip.vue'
import UiButton from '../components/ui/UiButton.vue'
import BoardCard from '../components/msgboard/BoardCard.vue'

const emit = defineEmits<{
  (e: 'navigate', route: string): void
}>()

// 与后端 store 同源的上限（校验先行免往返，后端仍是最终闸口）
const TEXT_LIMIT = 400
// 字号后端钳位区间（预览换算按钳位后的真实观感走，防手滑输入把预览缩没）
const FONT_MIN = 24
const FONT_MAX = 200

// 空牌占位文案（小预览与全屏预览共用，别各写一份）
const PREVIEW_EMPTY = '（牌面还是空的——点左侧类型或输入文字）'

// 类型速挂钮：表情进正文首行（版式契约见 BoardCard），副行自带回时预期。
// 这是"预设文案 chip 行"的升格形态——旧 4 条预设语义全部保留在内。
interface BoardType { id: string; emoji: string; label: string; text: string }
const TYPES: BoardType[] = [
  { id: 'meeting', emoji: '🤝', label: '开会', text: '🤝 开会中\n请勿打扰' },
  { id: 'lunch', emoji: '🍜', label: '吃饭', text: '🍜 干饭去了\n预计 1 小时后回来' },
  { id: 'tea', emoji: '☕', label: '茶水', text: '☕ 去茶水间了\n20 分钟内回来' },
  { id: 'walk', emoji: '🚶', label: '小憩', text: '🚶 遛弯回血\n马上回来' },
  { id: 'wc', emoji: '🚻', label: '卫生间', text: '🚻 马上回来' },
  { id: 'off', emoji: '🏠', label: '下班', text: '🏠 已下班\n有事留言，明天回复' },
  { id: 'warn', emoji: '⚠️', label: '勿动电脑', text: '⚠️ 请勿动我电脑' },
]

const status = shallowRef<Status | null>(null)
const screens = shallowRef<ScreenInfo[]>([])
const form = ref<Config>({ text: '', fontSize: 64, screen: '', hotkey: '', everyScreen: true })
const loading = ref(true)
const saving = ref(false)
const errorMsg = ref('')
const savedTip = ref(false)
// 服务器快照：任何"表单 vs 已存配置"的脏判定都以它为准（SetConfig 落库后回读）。
const server = ref<Config | null>(null)
const dirty = ref(false)

const textLen = computed(() => Array.from(form.value.text.trim()).length)
const textOver = computed(() => textLen.value > TEXT_LIMIT)

function sameConfig(a: Config, b: Config): boolean {
  return a.text === b.text && a.fontSize === b.fontSize && a.screen === b.screen
    && a.hotkey === b.hotkey && !!a.everyScreen === !!b.everyScreen
}

async function pullAll() {
  const [st, cfg, scr] = await Promise.all([
    MsgBoardAPI.MsgBoardService.GetStatus(),
    MsgBoardAPI.MsgBoardService.GetConfig(),
    MsgBoardAPI.MsgBoardService.ListScreens(),
  ])
  applyServer(st, cfg, scr)
}

function applyServer(st: Status, cfg: Config, scr: ScreenInfo[] | null) {
  status.value = st
  screens.value = scr ?? []
  server.value = cfg
  form.value = { ...cfg }
  dirty.value = false
}

async function refreshStatus() {
  try {
    status.value = await MsgBoardAPI.MsgBoardService.GetStatus()
  } catch { /* 事件驱动的静默刷新，失败留给下一次显式动作兜底 */ }
}

useWailsEvent<void>('msgboard:changed', () => { void refreshStatus() })

async function refresh() {
  loading.value = true
  errorMsg.value = ''
  try {
    await pullAll()
  } catch (err) {
    errorMsg.value = getErrorMessage(err)
  } finally {
    loading.value = false
  }
}

function onFormInput() {
  dirty.value = !server.value || !sameConfig(form.value, server.value)
  savedTip.value = false
}

// saveCore 返回是否保存成功；失败语义与旧版逐字一致：错误可见、热键字段以
// 服务端回滚为准、其余草稿保留。
async function saveCore(): Promise<boolean> {
  if (textOver.value) {
    errorMsg.value = `留言文案 ${textLen.value} 字，超出上限 ${TEXT_LIMIT} 字——便利贴也贴不下整页作文，请精简`
    return false
  }
  saving.value = true
  errorMsg.value = ''
  try {
    await MsgBoardAPI.MsgBoardService.SetConfig({ ...form.value, text: form.value.text.trim() })
    const [st, cfg] = await Promise.all([
      MsgBoardAPI.MsgBoardService.GetStatus(),
      MsgBoardAPI.MsgBoardService.GetConfig(),
    ])
    status.value = st
    server.value = cfg
    form.value = { ...cfg }
    dirty.value = false
    savedTip.value = true
    return true
  } catch (err) {
    errorMsg.value = getErrorMessage(err)
    try {
      const cfg = await MsgBoardAPI.MsgBoardService.GetConfig()
      server.value = cfg
      form.value = { ...form.value, hotkey: cfg.hotkey }
      dirty.value = !sameConfig(form.value, cfg)
    } catch { /* 回读失败保留错误文案，用户重试即再拉 */ }
    return false
  } finally {
    saving.value = false
  }
}

async function save() {
  await saveCore()
}

async function toggle() {
  errorMsg.value = ''
  try {
    await MsgBoardAPI.MsgBoardService.Toggle()
  } catch (err) {
    errorMsg.value = getErrorMessage(err)
  } finally {
    await refreshStatus()
  }
}

// applyType 类型钮：单击=填词（继续可编辑）；双击=填词+保存+挂出一步到位
// （牌已挂着时只热更内容不再 Toggle——Toggle 是翻转语义，盲调会把牌撤掉）。
async function applyType(t: BoardType, showNow: boolean) {
  form.value.text = t.text
  onFormInput()
  if (!showNow) return
  if (!(await saveCore())) return
  if (!shown.value) await toggle()
}

const shown = computed(() => status.value?.shown ?? false)
const screenMissing = computed(() => {
  const cur = form.value.screen
  if (!cur || screens.value.length === 0) return false
  return !screens.value.some((s) => s.device === cur)
})
const secondaryScreens = computed(() => screens.value.filter((s) => !s.isPrimary))
const hotkeyMissing = computed(() => !!status.value && status.value.hotkey !== '' && !status.value.hotkeyActive)
// 折叠区异常汇总：任何一项异常都让"高级设置"标题亮警示（内容再多也先看见病）
const advAlert = computed(() => hotkeyMissing.value || screenMissing.value || (shown.value && !!status.value && !status.value.keepAwake))

function screenLabel(s: ScreenInfo): string {
  return `${s.isPrimary ? '主屏' : '副屏'} ${s.device} · ${s.width}×${s.height}`
}

// ---- 预览（N30）----
// 小预览防裁切：BoardCard 定标链里卡宽上限＝字号×13（88vw 只会收紧不会放大），
// 固定 0.34 缩放一旦遇到大字号，卡的自然宽乘完就横向溢出 300px 预览盒被裁
// （机主报"右侧预览有点问题"的读码复现点之一）。字号超过默认档后把缩放降到
// 刚好容纳（288＝盒宽 300 − 双侧 6px 呼吸），默认 64 字号观感与旧版完全一致。
const PREVIEW_BASE_SCALE = 0.34
const effFontSize = computed(() =>
  Math.min(FONT_MAX, Math.max(FONT_MIN, form.value.fontSize || 64)),
)
const previewScale = computed(() =>
  Math.min(PREVIEW_BASE_SCALE, 288 / (effFontSize.value * 13)),
)
const previewZoom = computed(() => Math.max(3, Math.round(1 / previewScale.value)))

// 全屏预览：纯前端 Teleport 浮层，渲染与真牌同一 BoardCard、同一压暗层与
// --bc-max-h:74vh 标定——"所看即所挂"（主窗最大化且与目标屏同规格时几乎 1:1）。
// 刻意不走真挂牌链路：Toggle 是翻转语义，牌已挂着时"预览"会把真牌撤掉；瞬时
// 挂撤还会惊动 KeepAwake 登记与 N29 窗组账——预览这种只读动作不该有副作用。
const fullPreview = ref(false)
function onFullPreviewKey(e: KeyboardEvent) {
  if (e.key === 'Escape') fullPreview.value = false
}
watch(fullPreview, (on) => {
  if (on) window.addEventListener('keydown', onFullPreviewKey)
  else window.removeEventListener('keydown', onFullPreviewKey)
})
onBeforeUnmount(() => window.removeEventListener('keydown', onFullPreviewKey))

onMounted(refresh)
</script>

<template>
  <div class="page mb-page">
    <PageHeader
      title="桌面留言板"
      subtitle="离开工位在屏幕中央贴一张大字便利贴；挂出期间自动阻止休眠，撤牌即恢复。"
    >
      <template #actions>
        <div class="status-group">
          <UiStatusChip :tone="shown ? 'positive' : 'neutral'">{{ shown ? '已挂牌' : '未挂牌' }}</UiStatusChip>
          <UiStatusChip v-if="hotkeyMissing" tone="warning">热键未在位</UiStatusChip>
          <UiStatusChip v-if="shown && status && !status.keepAwake" tone="warning">防休眠登记失败</UiStatusChip>
        </div>
      </template>
    </PageHeader>

    <div v-if="loading" class="state-box">正在读取留言板配置…</div>
    <div v-else-if="errorMsg && !status" class="state-box state-error">
      加载失败：{{ errorMsg }}
      <button type="button" class="btn btn-small btn-secondary" @click="refresh">重试</button>
    </div>

    <template v-else>
      <div class="mb-layout">
        <!-- 左：高频动线（选类型 → 改正文 → 挂/撤） -->
        <div class="mb-col">
          <section class="panel">
            <h2 class="sec-title">类型速挂</h2>
            <p class="sec-note">点一下填词，<b>双击直接挂出</b>；牌面文字挂出后还能随时改（保存即热更）。</p>
            <div class="type-grid" role="group" aria-label="留言类型">
              <button
                v-for="t in TYPES"
                :key="t.id"
                type="button"
                class="type-btn"
                :class="{ active: form.text.trim() === t.text }"
                :title="`单击填入 · 双击直接挂出\n${t.text.replace('\n', ' / ')}`"
                @click="applyType(t, false)"
                @dblclick="applyType(t, true)"
              >
                <span class="type-emoji" aria-hidden="true">{{ t.emoji }}</span>
                <span class="type-label">{{ t.label }}</span>
              </button>
            </div>
          </section>

          <section class="panel">
            <div class="panel-top">
              <h2 class="sec-title">牌面正文</h2>
              <span v-if="dirty" class="chip chip-warning">未保存</span>
              <span v-else-if="savedTip" class="chip chip-positive">已保存</span>
            </div>
            <p class="sec-note">
              <b>第一行＝大字主题，第二行起＝小字副行</b>；开头放表情更醒目（{{ textLen }}/{{ TEXT_LIMIT }} 字）。
            </p>
            <label class="sr-only" for="mb-text">留言正文</label>
            <textarea
              id="mb-text"
              v-model="form.text"
              class="text-input mb-text"
              rows="3"
              placeholder="例如：☕ 去茶水间了&#10;20 分钟内回来"
              @input="onFormInput"
            ></textarea>
            <p v-if="textOver" class="field-error">正文已超 {{ TEXT_LIMIT }} 字，保存会被拒绝，请精简。</p>

            <div class="action-row">
              <UiButton :variant="shown ? 'danger' : 'primary'" @click="toggle">
                {{ shown ? '⏹ 撤下留言牌' : '📌 立即挂牌' }}
              </UiButton>
              <UiButton variant="secondary" small :disabled="!dirty || saving" @click="save">
                {{ saving ? '保存中…' : '保存设置' }}
              </UiButton>
            </div>
          </section>

          <section class="panel">
            <details class="adv">
              <summary class="adv-summary">
                高级设置<span v-if="advAlert" class="chip chip-warning adv-flag">有异常待处理</span>
              </summary>
              <div class="setting-row">
                <span class="setting-main">
                  <span class="setting-name">字号</span>
                  <span class="setting-desc">主题行字号（DIP px，24–200，越界自动钳位；副行按比例 0.42）</span>
                </span>
                <input
                  v-model.number="form.fontSize"
                  class="text-input mb-num"
                  type="number"
                  min="24"
                  max="200"
                  step="4"
                  aria-label="字号"
                  @input="onFormInput"
                />
              </div>
              <label class="setting-row setting-row-tappable">
                <span class="setting-main">
                  <span class="setting-name">多屏同时挂牌</span>
                  <span class="setting-desc">每块在位显示器各挂一窗，挂撤整组生效；拔掉副屏时窗组自动收回</span>
                </span>
                <input v-model="form.everyScreen" type="checkbox" aria-label="多屏同时挂牌" class="switch" @change="onFormInput" />
              </label>
              <div v-if="!form.everyScreen" class="setting-row">
                <span class="setting-main">
                  <span class="setting-name">目标显示器</span>
                  <span class="setting-desc">只挂一块屏时生效；默认跟随主屏，可指名副屏</span>
                </span>
                <select v-model="form.screen" class="select-input" aria-label="目标显示器" @change="onFormInput">
                  <option value="">主屏（默认）</option>
                  <option v-if="screenMissing" :value="form.screen">{{ form.screen }}（不在位）</option>
                  <option v-for="s in secondaryScreens" :key="s.device" :value="s.device">{{ screenLabel(s) }}</option>
                </select>
              </div>
              <p v-if="!form.everyScreen && screenMissing" class="field-warn">所选显示器当前不在位（可能已拔掉）——挂牌将自动回落主屏。</p>
              <div class="setting-row">
                <span class="setting-main">
                  <span class="setting-name">全局热键</span>
                  <span class="setting-desc">加速器写法如 <b class="mono">Ctrl+Alt+B</b>；留空 = 停用热键（托盘/轮盘不受影响）。改键即时注册，被占用会报错并回滚旧键。</span>
                </span>
                <input
                  v-model="form.hotkey"
                  class="text-input mb-hotkey mono"
                  type="text"
                  placeholder="Ctrl+Alt+B"
                  aria-label="全局热键"
                  @input="onFormInput"
                />
              </div>
              <div v-if="hotkeyMissing && status" class="banner banner-warn" role="note">
                热键「{{ status.hotkey }}」未在位：多半已被其它程序抢占。换个组合保存，或留空停用——期间可用托盘/轮盘唤起。
              </div>
              <div class="panel-foot">
                <button type="button" class="btn btn-ghost btn-small" @click="emit('navigate', '/settings/tray')">
                  前往设置页配置托盘/轮盘条目
                </button>
              </div>
            </details>
          </section>
        </div>

        <!-- 右：实时预览（与挂牌弹窗同一 BoardCard——所见即所得由组件同源保证） -->
        <aside class="mb-preview" aria-label="牌面预览">
          <div class="mbp-box">
            <div class="mbp-scaled" :style="{ transform: `scale(${previewScale})` }">
              <BoardCard :text="form.text || PREVIEW_EMPTY" :font-size="effFontSize" />
            </div>
          </div>
          <p class="mbp-caption">牌面预览（实际挂出约 {{ previewZoom }} 倍大）· 超高只裁不滚</p>
          <div class="mbp-actions">
            <UiButton variant="secondary" small @click="fullPreview = true">🔍 全屏预览</UiButton>
          </div>
        </aside>
      </div>

      <section v-if="errorMsg && status" class="banner banner-error" role="alert">保存失败：{{ errorMsg }}</section>

      <section class="panel usage">
        <details>
          <summary class="sec-title usage-summary">使用说明与行为契约</summary>
          <ul class="usage-list">
            <li>牌体全屏覆盖在位显示器（含任务栏区域）上的<b>压暗层</b>，便利贴居中央；多屏默认同时挂出、挂撤整组生效；<kbd>Esc</kbd> 或点击任意处即整组撤牌；窗口不进任务栏与 Alt+Tab。</li>
            <li>右侧预览可点「<b>全屏预览</b>」：在主窗内以浮层按真实大小渲染牌面观感（与真牌同一 BoardCard、同一压暗层），<kbd>Esc</kbd> 或点击即返回——纯预览动作，绝不写配置，与挂牌链路零交互。</li>
            <li>挂出期间系统不休眠、显示器不息屏（平台层引用计数聚合器，撤牌/停用/退出即释放）。</li>
            <li>撤牌即真销毁窗口、再唤即重建——不留隐藏窗占内存，也不得白边残影（踩坑 #50）。</li>
            <li>三条唤起通道：本页按钮、全局热键（高级设置）、托盘右键/快捷轮盘命令。</li>
            <li>不需要此能力时在设置页模块管理停用「桌面留言板」，热键随停用即摘。</li>
          </ul>
        </details>
      </section>

      <!-- 全屏预览浮层（N30）：Teleport 到 body 躲开页面滚动容器与层叠上下文，
           纯前端渲染同源 BoardCard——不挂牌、不动窗组、不进任何后端链路 -->
      <Teleport to="body">
        <div
          v-if="fullPreview"
          class="mbp-full"
          role="dialog"
          aria-label="牌面全屏预览"
          @click="fullPreview = false"
        >
          <span class="mbp-full-badge" aria-hidden="true">预览浮层 · 非真实挂牌</span>
          <BoardCard class="mbp-full-card" :text="form.text || PREVIEW_EMPTY" :font-size="effFontSize" />
          <div class="mbp-full-hint" aria-hidden="true">
            这是全屏预览，不改变挂牌状态 · 点击任意处或按 <kbd class="mbp-full-kbd">Esc</kbd> 返回
          </div>
        </div>
      </Teleport>
    </template>
  </div>
</template>

<style scoped>
.status-group { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; justify-content: flex-end; }

.mb-layout {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 300px;
  gap: 16px;
  align-items: start;
}
.mb-col { display: flex; flex-direction: column; gap: 12px; min-width: 0; }
@media (max-width: 900px) {
  .mb-layout { grid-template-columns: minmax(0, 1fr); }
  .mb-preview { position: static; }
}

.sec-title { font-size: var(--text-md); font-weight: 600; margin: 0; color: var(--color-text); }
.sec-note { font-size: var(--text-sm); color: var(--color-text-muted); margin: 6px 0 12px; line-height: 1.6; }
.panel-top { display: flex; align-items: center; justify-content: space-between; gap: 12px; flex-wrap: wrap; }
.panel-foot { display: flex; justify-content: flex-end; margin-top: 10px; }

/* 类型速挂钮：表情为主体、文字为副，双击手势在 title 与说明行双入口披露 */
.type-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(96px, 1fr)); gap: 8px; }
.type-btn {
  display: flex; flex-direction: column; align-items: center; gap: 2px;
  padding: 10px 6px 8px;
  border: 1px solid var(--color-border); border-radius: var(--radius-card, 10px);
  background: var(--surface-soft); color: var(--color-text-muted);
  font-size: var(--text-sm); cursor: pointer;
  transition: background var(--motion-fast) ease, border-color var(--motion-fast) ease, color var(--motion-fast) ease;
}
.type-btn:hover { background: var(--surface-hover); color: var(--color-text); }
.type-btn.active { border-color: var(--color-primary); background: var(--color-primary-soft); color: var(--color-primary); }
.type-emoji { font-size: 24px; line-height: 1.2; }
.type-label { line-height: 1.3; }

.mb-text { resize: vertical; min-height: 72px; line-height: 1.6; font-family: inherit; }
.action-row { display: flex; align-items: center; gap: 10px; margin-top: 12px; flex-wrap: wrap; }

.adv-summary { cursor: pointer; list-style: none; }
.adv-summary::-webkit-details-marker { display: none; }
.adv-summary::before { content: '▸'; display: inline-block; width: 1.2em; color: var(--color-text-subtle); transition: transform var(--motion-fast) ease; }
details[open] > .adv-summary::before { transform: rotate(90deg); }
.adv { display: flex; flex-direction: column; }
.adv-flag { margin-left: 8px; }
.setting-row { display: flex; align-items: center; justify-content: space-between; gap: 14px; padding: 10px 0 10px 1.2em; border-top: 1px solid var(--color-border); flex-wrap: wrap; }
.setting-main { display: flex; flex-direction: column; gap: 2px; min-width: 0; }
.setting-name { font-size: var(--text-sm); font-weight: 600; color: var(--color-text); }
.setting-desc { font-size: var(--text-xs); color: var(--color-text-subtle); line-height: 1.55; }
.mb-num { width: 92px; flex: none; font-variant-numeric: tabular-nums; }
.mb-hotkey { width: 168px; flex: none; }
.field-error { margin: 4px 0 0; font-size: var(--text-sm); color: var(--state-danger); }
.field-warn { margin: 6px 0 0; padding-left: 1.2em; font-size: var(--text-sm); color: var(--state-warning); }

/* 预览列：sticky 便签柱。缩放比只影响预览呈现——牌本体（popup）不吃这里的
   任何尺寸，同源的是 BoardCard 画法而非像素。 */
.mb-preview { position: sticky; top: 16px; }
.mbp-box {
  height: 300px; overflow: hidden;
  display: grid; place-items: center;
  background:
    radial-gradient(120% 90% at 50% 40%, var(--surface-hover) 0%, var(--surface-soft) 100%);
  border: 1px dashed var(--color-border); border-radius: var(--radius-card, 10px);
}
/* 缩放比由脚本按字号动态给出（防大字号横向裁切，见 previewScale），
   width:max-content 钉死"按卡的自然宽排版"——不受 300px 盒宽挤压换行 */
.mbp-scaled { transform-origin: center; width: max-content; }
.mbp-caption { margin: 6px 2px 0; font-size: var(--text-xs); color: var(--color-text-subtle); text-align: center; line-height: 1.5; }
.mbp-actions { display: flex; justify-content: center; margin-top: 8px; }

/* 全屏预览浮层：与 MsgBoardPopup 真牌同源观感——同值压暗层、同 74vh 牌高
   标定、同 150ms 入场淡入。差别只有两处：预览角标常驻（操作者要随时知道
   自己在预览），底部提示不做限时淡出（真牌淡出是给旁观者，这里没旁观者）。
   层级：高于通知抽屉(10002)，低于命令面板(100000)与 Toast(999999)——
   预览不该劫持全局快捷键 UI。 */
.mbp-full {
  position: fixed;
  inset: 0;
  z-index: 99998;
  display: grid;
  place-items: center;
  background: rgba(6, 10, 14, 0.38);
  cursor: pointer;
  user-select: none;
  animation: mbp-full-in 150ms ease-out;
}
.mbp-full-card { --bc-max-h: 74vh; }
@keyframes mbp-full-in {
  from { opacity: 0; }
  to { opacity: 1; }
}
.mbp-full-badge {
  position: absolute;
  top: 14px;
  left: 16px;
  padding: 2px 10px;
  border: 1px solid rgba(238, 246, 247, 0.25);
  border-radius: 999px;
  background: rgba(6, 10, 14, 0.55);
  color: rgba(238, 246, 247, 0.8);
  font-size: var(--text-sm);
}
.mbp-full-hint {
  position: absolute;
  bottom: 26px;
  left: 0;
  right: 0;
  text-align: center;
  font-size: var(--text-md);
  color: rgba(238, 246, 247, 0.62);
  text-shadow: 0 1px 3px rgba(0, 0, 0, 0.6);
  pointer-events: none;
}
.mbp-full-kbd {
  font-family: var(--font-mono);
  font-size: var(--text-sm);
  border: 1px solid rgba(238, 246, 247, 0.4);
  border-bottom-width: 2px;
  border-radius: 4px;
  padding: 0 6px;
}
@media (prefers-reduced-motion: reduce) {
  .mbp-full { animation: none; }
}

.usage { margin-top: 4px; }
.usage-summary { cursor: pointer; list-style: none; }
.usage-summary::-webkit-details-marker { display: none; }
.usage-list { margin: 8px 0 0; padding-left: 18px; display: flex; flex-direction: column; gap: 6px; font-size: var(--text-sm); color: var(--color-text-muted); line-height: 1.65; }
kbd { font-family: var(--font-mono); font-size: var(--text-xs); border: 1px solid var(--color-border-strong); border-bottom-width: 2px; border-radius: 4px; padding: 0 5px; background: var(--surface-soft); color: var(--color-text); }
</style>
