<script setup lang="ts">
// 桌面留言板模块页（N6 重设计）：左侧"类型速挂 → 正文编辑 → 挂/撤动作"
// 高频动线，右侧与挂牌弹窗同源的便利贴实时预览（所见即所得由 BoardCard
// 单组件保证）；字号/目标屏/热键等低频配置收进折叠区，异常时汇总警示。
// 数据闸口与后端契约零改动：pullAll 并行拉取、脏判定以服务端回读为准、
// 热键占用失败只回滚热键字段保留草稿——原纪律原样保留。
import { computed, onMounted, ref, shallowRef } from 'vue'
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
const form = ref<Config>({ text: '', fontSize: 64, screen: '', hotkey: '' })
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
  return a.text === b.text && a.fontSize === b.fontSize && a.screen === b.screen && a.hotkey === b.hotkey
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
              <div class="setting-row">
                <span class="setting-main">
                  <span class="setting-name">目标显示器</span>
                  <span class="setting-desc">牌子挂在哪块屏；默认跟随主屏，可指名副屏</span>
                </span>
                <select v-model="form.screen" class="select-input" aria-label="目标显示器" @change="onFormInput">
                  <option value="">主屏（默认）</option>
                  <option v-if="screenMissing" :value="form.screen">{{ form.screen }}（不在位）</option>
                  <option v-for="s in secondaryScreens" :key="s.device" :value="s.device">{{ screenLabel(s) }}</option>
                </select>
              </div>
              <p v-if="screenMissing" class="field-warn">所选显示器当前不在位（可能已拔掉）——挂牌将自动回落主屏。</p>
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
            <div class="mbp-scaled">
              <BoardCard :text="form.text || '（牌面还是空的——点左侧类型或输入文字）'" :font-size="form.fontSize || 64" />
            </div>
          </div>
          <p class="mbp-caption">牌面预览（实际挂出约 3 倍大）· 超高只裁不滚</p>
        </aside>
      </div>

      <section v-if="errorMsg && status" class="banner banner-error" role="alert">保存失败：{{ errorMsg }}</section>

      <section class="panel usage">
        <details>
          <summary class="sec-title usage-summary">使用说明与行为契约</summary>
          <ul class="usage-list">
            <li>牌体全屏覆盖目标显示器（含任务栏区域）上的<b>压暗层</b>，便利贴居中央；<kbd>Esc</kbd> 或点击任意处即撤；窗口不进任务栏与 Alt+Tab。</li>
            <li>挂出期间系统不休眠、显示器不息屏（平台层引用计数聚合器，撤牌/停用/退出即释放）。</li>
            <li>撤牌即真销毁窗口、再唤即重建——不留隐藏窗占内存，也不得白边残影（踩坑 #50）。</li>
            <li>三条唤起通道：本页按钮、全局热键（高级设置）、托盘右键/快捷轮盘命令。</li>
            <li>不需要此能力时在设置页模块管理停用「桌面留言板」，热键随停用即摘。</li>
          </ul>
        </details>
      </section>
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
.mbp-scaled { transform: scale(0.34); transform-origin: center; }
.mbp-caption { margin: 6px 2px 0; font-size: var(--text-xs); color: var(--color-text-subtle); text-align: center; line-height: 1.5; }

.usage { margin-top: 4px; }
.usage-summary { cursor: pointer; list-style: none; }
.usage-summary::-webkit-details-marker { display: none; }
.usage-list { margin: 8px 0 0; padding-left: 18px; display: flex; flex-direction: column; gap: 6px; font-size: var(--text-sm); color: var(--color-text-muted); line-height: 1.65; }
kbd { font-family: var(--font-mono); font-size: var(--text-xs); border: 1px solid var(--color-border-strong); border-bottom-width: 2px; border-radius: 4px; padding: 0 5px; background: var(--surface-soft); color: var(--color-text); }
</style>
