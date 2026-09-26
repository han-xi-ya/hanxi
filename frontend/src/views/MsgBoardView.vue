<script setup lang="ts">
// 桌面留言板模块页（重设计 v4「所见即所得的牌桌」）：v1/v2/v3 的共同病根是
// "页面里堆卡片"——v3 流体三柱虽然治好了死白与错位，但配重仍然反着：草稿
// textarea 与配置项合计吃掉大半屏幕，而真正决定桌面的东西（牌）被压在 0.34
// 基准缩放的预览小样里，只占视口不到 5%。机主诉点很直白：这页本质是一块摆在
// 桌面上的牌，页面却长得像表单合集。
//
// v4 换成"牌桌"骨架——一屏工作台，左右两区，不是卡片堆：
//   左 60%+ 牌面舞台（.mb-hero）：顶部 40px 工具条收纳状态字/异常 chip/挂撤
//     主操作/全屏预览；主体是桌面模拟底（tokens 派生灰，color-mix 随主题联动，
//     零新色值）上的大牌 1:1 预览——缩放由 ResizeObserver 实测"舞台盒宽"与
//     "卡片实际布局宽"共同决定，封顶 1.0：字少屏宽时就是原大真牌，字号/文案
//     改动即时落在大牌上；只有牌宽越过舞台才等比收缩（防裁切契约保留，兜底
//     线仍是字号×13）；底部一条回显栏说"正在挂出的牌面"（已存版本真话）+ 脏态。
//   右 ≤40% 窄长操作栏（.mb-rail）：一行一控件的属性面板形制，全走 setting-row
//     原子——正文（textarea 收 3 行、可展 8 行，正面回应"草稿太大"）、类型速挂
//     一排微钮、字号滑杆、多屏/目标屏、热键、保存行、契约折叠。
//   ≤840（容器查询看主区实际宽度）：舞台在上（高度压缩档）、操作栏在下。
// 老内核/首帧/happy-dom 拿不到实测值时回落 300px 基准盒 + 字号×13 兜底线，
// 数值与 v3 测试断言口径逐位可推（scale(0.34615…)/scale(0.18461…)）。
//
// 语义金标准零删减：挂/撤主动作与 Toggle 翻转纪律（已挂态双击只热更不盲调
// Toggle）、类型单击填词/双击挂出、草稿↔大牌同源联动、字号滑杆 24–200 钳位、
// 多屏 everyScreen+目标屏不在位警示、热键录入与占用回滚、正文 401 字本地拦截、
// 脏态 chip/提示、异常汇总旗标、N30 全屏预览浮层全部保留；数据闸口与后端契约
// 零改动：pullAll 并行拉取、脏判定以服务端回读为准、热键占用失败只回滚热键字段
// 保留草稿。全页牌面画法仍只有舞台一块 BoardCard（回显是纯文本），全屏浮层
// 打开前 DOM 里不存在第二块牌。
import { computed, onActivated, onBeforeUnmount, onDeactivated, onMounted, ref, shallowRef, watch } from 'vue'
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

// 空牌占位文案（舞台大牌与全屏预览共用，别各写一份）
const PREVIEW_EMPTY = '（牌面还是空的——挑一张类型或直接输入文字补上）'

// 类型速挂微钮：表情进正文首行（版式契约见 BoardCard），副行自带回时预期。
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
// 异常汇总旗标：热键未在位/屏不在位/防休眠登记失败任一命中都在操作栏头部亮警示
// （内容再多也先看见病）——v3 挂在"参数配置"折叠标题上，v4 折叠没了，旗标上移到头部。
const advAlert = computed(() => hotkeyMissing.value || screenMissing.value || (shown.value && !!status.value && !status.value.keepAwake))

function screenLabel(s: ScreenInfo): string {
  return `${s.isPrimary ? '主屏' : '副屏'} ${s.device} · ${s.width}×${s.height}`
}

// 舞台回显栏报"已存版本"——Toggle 挂出的真身就是服务端配置，
// 草稿的实时形态交给大牌预览，两个位点各说各的真话，互不冒充。
const serverText = computed(() => server.value?.text ?? '')
const echoTitle = computed(() => serverText.value.split('\n', 1)[0]?.trim() ?? '')
const echoSub = computed(() => serverText.value.split('\n').slice(1).map((l) => l.trim()).filter(Boolean).join(' / '))
const dirtyNote = computed(() => (shown.value
  ? '草稿有改动未保存：眼前的牌面还是旧版，保存即热更。'
  : '草稿有改动未保存：挂牌认的是已保存版本，要改请先保存。'))

// ---- 舞台缩放（v4 牌桌：上限 1.0 的"能多大多大"）----
// v3 病灶复盘：缩放基准钉死 0.34、增长系数再封顶 0.8——舞台再宽牌也只是
// "更大的小样"，1:1 永远缺席。v4 的口径是"能 1:1 就 1:1"：
//   scale = min(1, (舞台实测宽 − 2×PAD) / 卡片实际布局宽)
// 卡片实际宽由 ResizeObserver 实测 .mbp-scaled（transform 不影响布局盒，
// 测得的就是未缩放真实宽）——短文案在宽舞台上直接原大呈现；只有牌宽越过
// 舞台才等比收缩，防裁切契约从"猜 ×13"升级为"量实测"，兜底仍留 字号×13 线。
// 超高裁剪沿用 v3 反算注入：--bc-max-h = (舞台高−12)/scale，大牌"只裁不滚"
// 收在舞台框内（与真牌同纪律），胶带条不再被居中裁切吃掉。
// happy-dom（测试环境）里 RO 是空实现、真实浏览器首帧前也测不到——双端回落
// 300px 基准盒 + 字号×13 兜底宽：64px 时 scale=288/832=0.34615…，120px 时
// scale=288/1560=0.18461…，数值口径可精确断言（v3 同款 0.18461… 逐位一致）。
const STAGE_FALLBACK = 300
const STAGE_PAD = 12
const SCALE_CAP = 1
const stageEl = ref<HTMLElement | null>(null)
const cardEl = ref<HTMLElement | null>(null)
const boxW = ref(STAGE_FALLBACK)
const boxH = ref(STAGE_FALLBACK)
const cardW = ref(0)
let stageRO: ResizeObserver | null = null
function unhookStageRO() {
  stageRO?.disconnect()
  stageRO = null
}
watch([stageEl, cardEl], ([box, card]) => {
  unhookStageRO()
  if (!box || !card || typeof ResizeObserver === 'undefined') return
  stageRO = new ResizeObserver((entries) => {
    for (const entry of entries) {
      const rect = entry.contentRect
      if (!rect || rect.width <= 0 || rect.height <= 0) continue
      if (entry.target === box) {
        boxW.value = rect.width
        boxH.value = rect.height
      } else if (entry.target === card) {
        cardW.value = rect.width
      }
    }
  })
  stageRO.observe(box)
  stageRO.observe(card)
}, { flush: 'post' })

const effFontSize = computed(() =>
  Math.min(FONT_MAX, Math.max(FONT_MIN, form.value.fontSize || 64)),
)
const previewScale = computed(() => {
  const w = boxW.value > 0 ? boxW.value : STAGE_FALLBACK
  // 未实测到卡片宽（首帧/老内核）时按防裁切兜底线 字号×13 保守收缩
  const natural = cardW.value > 0 ? cardW.value : effFontSize.value * 13
  return Math.min(SCALE_CAP, (w - STAGE_PAD) / natural)
})
const previewCardMaxH = computed(() => Math.max(48, Math.floor((boxH.value - STAGE_PAD) / previewScale.value)))
const previewZoom = computed(() => Math.max(1, Math.round(1 / previewScale.value)))
// 机主反馈（2026-09-26）：旧口径「实际挂出约 N 倍大」要人拿倍数心算原图多大，看不懂。
// 改一句大白话：先报预览缩到百分之几（所见直接可验），再报真牌相对预览大的倍数，
// 数字直给、不叠「等效/约…倍大」连环修饰；真实大小的查看引导交给「全屏预览」。
const previewPct = computed(() => Math.max(1, Math.round(previewScale.value * 100)))
const previewZoomNote = computed(() => (
  previewZoom.value <= 1
    ? `预览缩到 ${previewPct.value}%，和真牌几乎一样大`
    : `预览缩到 ${previewPct.value}%，真牌大 ${previewZoom.value} 倍`
))

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
function unhookFullPreviewKey() {
  window.removeEventListener('keydown', onFullPreviewKey)
}
onBeforeUnmount(() => {
  unhookFullPreviewKey()
  unhookStageRO()
})
// 审查 #20（与 UiHistoryDialog #5 同族）：KeepAlive 切页时浮层可能仍开着，
// window 级监听在场会让异页按 Esc 幽灵收起隐藏预览——deactivate 摘、
// activate 按浮层现态补挂。
onDeactivated(unhookFullPreviewKey)
onActivated(() => {
  if (fullPreview.value) window.addEventListener('keydown', onFullPreviewKey)
})

// 草稿框收展：默认 3 行（属性面板一控件一行），展开 8 行写长草稿——
// "草稿太大"的正面回应：常态只占一小行，要写再撑开。
const draftExpanded = ref(false)

onMounted(refresh)
</script>

<template>
  <!-- page-fluid：不吃 .page 阅读档夹持；v4 的宽度治理交给 .mb-desk 容器查询
       ——左右两区按主区实际宽度换形，≤840 上下堆叠 -->
  <div class="page-fluid mb-page">
    <PageHeader
      title="桌面留言板"
      subtitle="离开工位在屏幕中央贴一张大字便利贴；挂出期间自动阻止休眠，撤牌即恢复。"
    />

    <div v-if="loading" class="state-box">正在读取留言板配置…</div>
    <div v-else-if="errorMsg && !status" class="state-box state-error">
      加载失败：{{ errorMsg }}
      <button type="button" class="btn btn-small btn-secondary" @click="refresh">重试</button>
    </div>

    <template v-else>
      <div class="mb-desk">
        <!-- ============ 左区：牌面舞台（工具条 + 1:1 大牌 + 回显栏） ============ -->
        <section class="panel mb-hero" aria-label="牌面舞台">
          <div class="mb-toolbar">
            <span class="hero-dot" :class="{ on: shown }" aria-hidden="true"></span>
            <span class="status-word">{{ shown ? '已挂牌' : '未挂牌' }}</span>
            <UiStatusChip v-if="hotkeyMissing" tone="warning">热键未在位</UiStatusChip>
            <UiStatusChip v-if="shown && status && !status.keepAwake" tone="warning">防休眠登记失败</UiStatusChip>
            <span class="tb-spacer" aria-hidden="true"></span>
            <UiButton class="hero-cta" :variant="shown ? 'danger' : 'primary'" @click="toggle">
              {{ shown ? '撤下留言牌' : '立即挂牌' }}
            </UiButton>
            <UiButton variant="secondary" small @click="fullPreview = true">全屏预览</UiButton>
          </div>

          <!-- 桌面模拟底：color-mix 从 --color-text/--surface-page 派生桌面灰，
               随主题与色板联动，零新色值；大牌居中说"这就是桌面上那张牌" -->
          <div ref="stageEl" class="mbp-box" :style="{ '--bc-max-h': `${previewCardMaxH}px` }">
            <div ref="cardEl" class="mbp-scaled" :style="{ transform: `scale(${previewScale})` }">
              <BoardCard :text="form.text || PREVIEW_EMPTY" :font-size="effFontSize" />
            </div>
          </div>

          <div class="mb-echo">
            <span class="hero-echo-k">{{ shown ? '正在挂出的牌面' : '下次挂出的牌面' }}</span>
            <template v-if="echoTitle">
              <span class="hero-echo-main">{{ echoTitle }}</span>
              <span v-if="echoSub" class="hero-echo-sub">{{ echoSub }}</span>
            </template>
            <span v-else class="hero-echo-sub">（空牌——在右侧挑一张牌面，或写草稿后保存）</span>
            <p v-if="dirty" class="hero-dirty">{{ dirtyNote }}</p>
            <p class="mbp-caption">{{ previewZoomNote }} · 看真实大小点「全屏预览」· 超高只裁不滚</p>
          </div>
        </section>

        <!-- ============ 右区：窄长操作栏（一行一控件的属性面板） ============ -->
        <aside class="mb-rail" aria-label="牌面设置">
          <div class="mb-rail-head">
            <h2 class="sec-title">牌面设置</h2>
            <span v-if="dirty" class="chip chip-warning">未保存</span>
            <span v-else-if="savedTip" class="chip chip-positive">已保存</span>
            <span v-if="advAlert" class="chip chip-warning adv-flag">有异常待处理</span>
          </div>

          <!-- 正文：常态 3 行，可展 8 行 -->
          <div class="setting-row mb-draft-row">
            <label class="sr-only" for="mb-text">留言正文</label>
            <span class="setting-main">
              <span class="setting-name">正文</span>
              <span class="setting-desc">第一行＝大字主题，第二行起＝小字副行（{{ textLen }}/{{ TEXT_LIMIT }} 字）</span>
            </span>
            <div class="mb-draft-ctl">
              <textarea
                id="mb-text"
                v-model="form.text"
                class="text-input mb-text"
                :rows="draftExpanded ? 8 : 3"
                placeholder="例如：☕ 去茶水间了&#10;20 分钟内回来"
                @input="onFormInput"
              ></textarea>
              <div class="mb-draft-tools">
                <button type="button" class="btn btn-ghost btn-small" @click="draftExpanded = !draftExpanded">
                  {{ draftExpanded ? '收起' : '展开' }}
                </button>
                <span class="work-hint">开头放表情更醒目</span>
              </div>
              <p v-if="textOver" class="field-error">正文已超 {{ TEXT_LIMIT }} 字，保存会被拒绝，请精简。</p>
              <!-- 保存失败就地可见（动作失败归正文行；加载失败走上方 state-error 闸口） -->
              <div v-if="errorMsg && status" class="banner banner-error slim" role="alert">保存失败：{{ errorMsg }}</div>
            </div>
          </div>

          <!-- 类型速挂：一排微钮 -->
          <div class="setting-row mb-types">
            <span class="setting-main">
              <span class="setting-name">挑一张牌面</span>
              <span class="setting-desc">点一下填词，<b>双击直接挂出</b>；挂出后文字仍可随改随存</span>
            </span>
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
          </div>

          <!-- 字号滑杆：改一下直接落在左边大牌上（24–200 钳位契约不变） -->
          <div class="setting-row mb-fontrow">
            <span class="setting-main">
              <span class="setting-name">字号</span>
              <span class="setting-desc">24–200 px，拖动即时反映到大牌</span>
            </span>
            <label class="mb-font">
              <input
                v-model.number="form.fontSize"
                type="range"
                class="mb-range"
                :min="String(FONT_MIN)"
                :max="String(FONT_MAX)"
                step="2"
                aria-label="字号"
                @input="onFormInput"
              />
              <b class="mono mb-font-v">{{ effFontSize }} px</b>
            </label>
          </div>

          <label class="setting-row setting-row-tappable">
            <span class="setting-main">
              <span class="setting-name">多屏同时挂牌</span>
              <span class="setting-desc">每块在位显示器各挂一窗，挂撤整组生效</span>
            </span>
            <input v-model="form.everyScreen" type="checkbox" aria-label="多屏同时挂牌" class="switch" @change="onFormInput" />
          </label>

          <div v-if="!form.everyScreen" class="setting-row">
            <span class="setting-main">
              <span class="setting-name">目标显示器</span>
              <span class="setting-desc">只挂一块屏时生效；默认跟随主屏，可指名副屏</span>
            </span>
            <select v-model="form.screen" class="select-input mb-select" aria-label="目标显示器" @change="onFormInput">
              <option value="">主屏（默认）</option>
              <option v-if="screenMissing" :value="form.screen">{{ form.screen }}（不在位）</option>
              <option v-for="s in secondaryScreens" :key="s.device" :value="s.device">{{ screenLabel(s) }}</option>
            </select>
          </div>
          <p v-if="!form.everyScreen && screenMissing" class="field-warn">所选显示器当前不在位（可能已拔掉）——挂牌将自动回落主屏。</p>

          <div class="setting-row">
            <span class="setting-main">
              <span class="setting-name">全局热键</span>
              <span class="setting-desc">加速器写法如 <b class="mono">Ctrl+Alt+B</b>；留空=停用（托盘/轮盘不受影响），被占用会报错并回滚旧键</span>
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
          <div v-if="hotkeyMissing && status" class="banner banner-warn slim" role="note">
            热键「{{ status.hotkey }}」未在位：多半已被其它程序抢占。换个组合保存，或留空停用——期间可用托盘/轮盘唤起。
          </div>

          <div class="setting-row work-actions">
            <UiButton variant="secondary" :disabled="!dirty || saving" @click="save">
              {{ saving ? '保存中…' : '保存设置' }}
            </UiButton>
            <span class="work-hint">保存只写配置；挂出与热更走左上方挂撤钮</span>
          </div>

          <!-- 契约：一行折叠 chip，不占舞台 -->
          <section class="panel mb-usage">
            <details>
              <summary class="sec-title usage-summary">使用说明与行为契约</summary>
              <ul class="usage-list">
                <li>牌体全屏覆盖在位显示器（含任务栏区域）上的<b>压暗层</b>，便利贴居中央；多屏默认同时挂出、挂撤整组生效；<kbd>Esc</kbd> 或点击任意处即整组撤牌；窗口不进任务栏与 Alt+Tab。</li>
                <li>舞台可点「<b>全屏预览</b>」：在主窗内以浮层按真实大小渲染牌面观感（与真牌同一 BoardCard、同一压暗层），<kbd>Esc</kbd> 或点击即返回——纯预览动作，绝不写配置，与挂牌链路零交互。</li>
                <li>挂出期间系统不休眠、显示器不息屏（平台层引用计数聚合器，撤牌/停用/退出即释放）。</li>
                <li>撤牌即真销毁窗口、再唤即重建——不留隐藏窗占内存，也不得白边残影（踩坑 #50）。</li>
                <li>三条唤起通道：本页挂牌钮、全局热键、托盘右键/快捷轮盘命令。</li>
                <li>不需要此能力时在设置页模块管理停用「桌面留言板」，热键随停用即摘。</li>
              </ul>
            </details>
          </section>

          <div class="panel-foot">
            <button type="button" class="btn btn-ghost btn-small" @click="emit('navigate', '/settings/tray')">
              前往设置页配置托盘/轮盘条目
            </button>
          </div>
        </aside>
      </div>

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
/* 页体骨架：页头 + 牌桌两区；换形看主区实际宽度（容器查询），不猜视口 */
.mb-page { display: flex; flex-direction: column; gap: 12px; min-width: 0; }
.mb-desk {
  container: mb / inline-size;
  display: grid;
  align-items: start;
  gap: 12px;
  grid-template-columns: minmax(0, 1fr);
  min-width: 0;
}
/* ≥840：左舞台 60%+（1fr 吃余量）｜右操作栏 ≤40%（300–440px 档，越宽不越拉） */
@container mb (min-width: 840px) {
  .mb-desk {
    grid-template-columns: minmax(0, 1fr) clamp(300px, 34cqi, 440px);
    align-items: stretch;
  }
}

/* ---- 左区：牌面舞台 ---- */
.mb-hero {
  display: flex; flex-direction: column; gap: 10px;
  padding: 10px 12px; min-width: 0;
}
/* 40px 工具条：状态字+异常 chip 在左，挂撤主操作与全屏预览在右 */
.mb-toolbar { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; min-height: 40px; }
.tb-spacer { flex: 1; min-width: 0; }
.hero-dot { width: 10px; height: 10px; border-radius: 50%; background: var(--color-border-strong); flex: none; }
.hero-dot.on { background: var(--state-positive); }
.hero-cta { min-height: 36px; padding: 6px 16px; font-size: var(--text-md); font-weight: 600; }

/* 桌面模拟底：两档 color-mix 灰随主题联动；牌超高只裁不滚（--bc-max-h 注入反算） */
.mbp-box {
  height: clamp(200px, 40vh, 520px); overflow: hidden;
  display: grid; place-items: center;
  background:
    radial-gradient(140% 110% at 50% 30%,
      color-mix(in srgb, var(--color-text) 5%, var(--surface-page)) 0%,
      color-mix(in srgb, var(--color-text) 16%, var(--surface-page)) 100%);
  border: 1px solid var(--color-border); border-radius: var(--radius-element);
}
@container mb (min-width: 840px) {
  /* 宽窗：舞台吃满"页头+工具条+回显栏之外的视口余量"，零滚动优先 */
  .mbp-box { height: clamp(300px, calc(100vh - 330px), 900px); }
}
.mbp-scaled { transform-origin: center; width: max-content; }

/* 回显栏：已存牌面真话 + 脏态 + 缩放大白话，一行流式排布 */
.mb-echo {
  display: flex; flex-wrap: wrap; align-items: baseline; gap: 4px 10px;
  padding-top: 8px; border-top: 1px solid var(--color-border); min-width: 0;
}
.hero-echo-k { font-size: var(--text-xs); color: var(--color-text-subtle); white-space: nowrap; }
.hero-echo-main { font-size: var(--text-md); font-weight: 650; color: var(--color-text); overflow-wrap: anywhere; min-width: 0; }
.hero-echo-sub { font-size: var(--text-sm); color: var(--color-text-muted); overflow-wrap: anywhere; min-width: 0; }
.hero-dirty { flex-basis: 100%; margin: 0; font-size: var(--text-sm); color: var(--state-warning); }
.mbp-caption { flex-basis: 100%; margin: 0; font-size: var(--text-xs); color: var(--color-text-subtle); }

/* ---- 右区：窄长操作栏 ---- */
.mb-rail { display: flex; flex-direction: column; gap: 8px; min-width: 0; }
.mb-rail-head { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; padding: 2px 2px 0; }
.sec-title { font-size: var(--text-md); font-weight: 600; margin: 0; color: var(--color-text); }

/* 正文行：控件在下铺满栏宽，常态 3 行 */
.mb-draft-row { flex-direction: column; align-items: stretch; gap: 8px; }
.mb-draft-ctl { display: flex; flex-direction: column; gap: 6px; min-width: 0; }
.mb-text { width: 100%; resize: vertical; min-height: 60px; line-height: 1.6; font-family: inherit; }
.mb-draft-tools { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.work-hint { font-size: var(--text-xs); color: var(--color-text-subtle); }

/* 类型速挂：一排微钮（表情+词并排，随栏宽换行数） */
.mb-types { flex-direction: column; align-items: stretch; gap: 8px; }
.type-grid { display: flex; flex-wrap: wrap; gap: 6px; }
.type-btn {
  display: flex; align-items: center; gap: 5px;
  padding: 4px 9px;
  border: 1px solid var(--color-border); border-radius: var(--radius-pill);
  background: var(--surface-soft); color: var(--color-text-muted);
  font-size: var(--text-sm); cursor: pointer;
  transition: background var(--motion-fast) ease, border-color var(--motion-fast) ease, color var(--motion-fast) ease;
}
.type-btn:hover { background: var(--surface-hover); color: var(--color-text); }
.type-btn.active { border-color: var(--color-primary); background: var(--color-primary-soft); color: var(--color-primary); }
.type-emoji { font-size: 14px; line-height: 1; }
.type-label { line-height: 1.3; }

/* 字号行：滑杆吃余宽 */
.mb-fontrow .mb-font { display: flex; align-items: center; gap: 10px; flex: 1; min-width: 160px; }
.mb-range { flex: 1; min-width: 90px; accent-color: var(--color-primary); }
.mb-font-v { min-width: 52px; text-align: right; font-size: var(--text-sm); color: var(--color-text); }
.switch { width: 18px; height: 18px; cursor: pointer; accent-color: var(--color-primary); flex: none; }
.mb-select { flex: 1; min-width: 0; max-width: 240px; }
.mb-rail .mb-hotkey { flex: 1; min-width: 0; max-width: 200px; }

/* 保存行 */
.work-actions { flex-wrap: wrap; }

.field-error { margin: 0; font-size: var(--text-sm); color: var(--state-danger); }
.field-warn { margin: 0; padding-left: 1.2em; font-size: var(--text-sm); color: var(--state-warning); }

/* 契约折叠 */
.usage-summary { cursor: pointer; list-style: none; }
.usage-summary::-webkit-details-marker { display: none; }
.usage-list { margin: 8px 0 0; padding-left: 18px; display: flex; flex-direction: column; gap: 6px; font-size: var(--text-sm); color: var(--color-text-muted); line-height: 1.65; }
kbd { font-family: var(--font-mono); font-size: var(--text-xs); border: 1px solid var(--color-border-strong); border-bottom-width: 2px; border-radius: 4px; padding: 0 5px; background: var(--surface-soft); color: var(--color-text); }
.panel-foot { display: flex; justify-content: flex-end; }

/* 正文框可视标签是"正文"行名（setting-name 非 for 关联），隐藏标签兜底
   （与 ModuleCenterView 同形，全站暂无全局档） */
.sr-only {
  position: absolute; width: 1px; height: 1px; margin: -1px; padding: 0;
  overflow: hidden; clip: rect(0 0 0 0); white-space: nowrap; border: 0;
}
</style>

<style scoped>
/* 全屏预览浮层：与 MsgBoardPopup 真牌同源观感——同值压暗层、同 74vh 牌高
   标定、同 150ms 入场淡入。差别只有两处：预览角标常驻（操作者要随时知道
   自己在预览），底部提示不做限时淡出（真牌淡出是给旁观者，这里没旁观者）。
   层级：高于通知抽屉(10002)，低于命令面板(100000)与 Toast(999999)——
   预览不该劫持全局快捷键 UI。独立成块：不与牌桌骨架混排，Teleport
   落体后也不在 .mb-desk 树内。 */
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
</style>
