<script setup lang="ts">
// 桌面留言板模块页：牌面文案（预设填词 + 自定义正文/字号）、目标显示器与
// 全局热键配置的单一编辑面；挂/撤主按钮与实时状态回显（msgboard:changed 事件
// 驱动，配置改动由后端热更到在挂的牌）。防休眠登记结果如实回显，不承诺暗效果。
import { computed, onMounted, ref, shallowRef } from 'vue'
import * as MsgBoardAPI from '../../bindings/hanxi/internal/modules/msgboard'
import type { Config, ScreenInfo, Status } from '../../bindings/hanxi/internal/modules/msgboard/models'
import { useWailsEvent } from '../composables/useWailsEvent'
import { getErrorMessage } from '../utils/errors'
import PageHeader from '../components/ui/PageHeader.vue'
import UiStatusChip from '../components/ui/UiStatusChip.vue'
import UiButton from '../components/ui/UiButton.vue'

const emit = defineEmits<{
  (e: 'navigate', route: string): void
}>()

// 与后端 store 同源的上限（校验先行免往返，后端仍是最终闸口）
const TEXT_LIMIT = 400

const status = shallowRef<Status | null>(null)
const screens = shallowRef<ScreenInfo[]>([])
const presets = shallowRef<string[]>([])
const form = ref<Config>({ text: '', fontSize: 64, screen: '', hotkey: '' })
const loading = ref(true)
const saving = ref(false)
const errorMsg = ref('')
const savedTip = ref(false)
// 服务器快照：任何"表单 vs 已存配置"的脏判定都以它为准（SetConfig 落库后回读）。
const server = ref<Config | null>(null)

const textLen = computed(() => Array.from(form.value.text.trim()).length)
const textOver = computed(() => textLen.value > TEXT_LIMIT)

function sameConfig(a: Config, b: Config): boolean {
  return a.text === b.text && a.fontSize === b.fontSize && a.screen === b.screen && a.hotkey === b.hotkey
}
const dirty = ref(false)

async function pullAll() {
  const [st, cfg, list, scr] = await Promise.all([
    MsgBoardAPI.MsgBoardService.GetStatus(),
    MsgBoardAPI.MsgBoardService.GetConfig(),
    MsgBoardAPI.MsgBoardService.ListPresets(),
    MsgBoardAPI.MsgBoardService.ListScreens(),
  ])
  applyServer(st, cfg, list, scr)
}

function applyServer(st: Status, cfg: Config, list: string[] | null, scr: ScreenInfo[] | null) {
  status.value = st
  presets.value = list ?? []
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

async function save() {
  if (textOver.value) {
    errorMsg.value = `留言文案 ${textLen.value} 字，超出上限 ${TEXT_LIMIT} 字——全屏大字也放不下一页纸，请精简`
    return
  }
  saving.value = true
  errorMsg.value = ''
  try {
    await MsgBoardAPI.MsgBoardService.SetConfig({ ...form.value, text: form.value.text.trim() })
    // 热键占用等失败走 catch；成功（含后端字段回滚）都以回读为准，不留假状态
    const [st, cfg] = await Promise.all([
      MsgBoardAPI.MsgBoardService.GetStatus(),
      MsgBoardAPI.MsgBoardService.GetConfig(),
    ])
    status.value = st
    server.value = cfg
    form.value = { ...cfg }
    dirty.value = false
    savedTip.value = true
  } catch (err) {
    errorMsg.value = getErrorMessage(err)
    // 只采纳服务端的热键字段（后端占用失败会回滚热键、其余字段照常已落盘）；
    // 其它字段的本地草稿保留，避免"报错把用户没保存的编辑一并抹掉"。
    try {
      const cfg = await MsgBoardAPI.MsgBoardService.GetConfig()
      server.value = cfg
      form.value = { ...form.value, hotkey: cfg.hotkey }
      dirty.value = !sameConfig(form.value, cfg)
    } catch { /* 回读失败保留错误文案，用户重试即再拉 */ }
  } finally {
    saving.value = false
  }
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

function applyPreset(p: string) {
  form.value.text = p
  onFormInput()
}

const shown = computed(() => status.value?.shown ?? false)
const screenMissing = computed(() => {
  const cur = form.value.screen
  if (!cur || screens.value.length === 0) return false
  return !screens.value.some((s) => s.device === cur)
})
const secondaryScreens = computed(() => screens.value.filter((s) => !s.isPrimary))

function screenLabel(s: ScreenInfo): string {
  return `${s.isPrimary ? '主屏' : '副屏'} ${s.device} · ${s.width}×${s.height}`
}

onMounted(refresh)
</script>

<template>
  <div class="page">
    <PageHeader title="桌面留言板" subtitle="离开工位一键在屏幕全屏挂出离岗告示牌；挂出期间自动阻止系统/显示器休眠，撤牌即恢复。">
      <template #actions>
        <div class="status-group">
          <UiStatusChip :tone="shown ? 'positive' : 'neutral'">{{ shown ? '已挂牌' : '未挂牌' }}</UiStatusChip>
          <UiStatusChip
            v-if="status"
            :tone="status.hotkey === '' ? 'neutral' : status.hotkeyActive ? 'positive' : 'warning'"
          >
            {{ status.hotkey === '' ? '热键已停用' : status.hotkeyActive ? `热键在位 ${status.hotkey}` : '热键未在位' }}
          </UiStatusChip>
          <UiStatusChip v-if="shown && status" :tone="status.keepAwake ? 'information' : 'warning'">
            {{ status.keepAwake ? '防休眠生效' : '防休眠登记失败' }}
          </UiStatusChip>
        </div>
      </template>
    </PageHeader>

    <div v-if="loading" class="state-box">正在读取留言板配置…</div>
    <div v-else-if="errorMsg && !status" class="state-box state-error">
      加载失败：{{ errorMsg }}
      <button type="button" class="btn btn-small btn-secondary" @click="refresh">重试</button>
    </div>

    <template v-else>
      <section class="panel">
        <div class="panel-top">
          <h2 class="sec-title">牌面内容</h2>
          <div class="panel-actions">
            <span v-if="dirty" class="chip chip-warning">未保存</span>
            <span v-else-if="savedTip" class="chip chip-positive">已保存</span>
            <UiButton variant="primary" small :disabled="!dirty || saving" @click="save">
              {{ saving ? '保存中…' : '保存设置' }}
            </UiButton>
            <UiButton :variant="shown ? 'danger' : 'primary'" small @click="toggle">
              {{ shown ? '撤下留言牌' : '立即挂牌' }}
            </UiButton>
          </div>
        </div>
        <p class="sec-note">
          牌面 = 半透深色底 + 大字正文；自定义留空时展示第一条预设。保存即热更——
          牌正挂着时改正文/字号会立刻反映到屏上，换目标显示器则拆牌重挂。
        </p>

        <label class="form-label" for="mb-text">留言正文（{{ textLen }}/{{ TEXT_LIMIT }} 字，支持换行）</label>
        <textarea
          id="mb-text"
          v-model="form.text"
          class="text-input mb-text"
          rows="3"
          placeholder="例如：去开会了，11 点后回来"
          @input="onFormInput"
        ></textarea>

        <div class="preset-row" role="group" aria-label="预设文案模板">
          <button
            v-for="p in presets"
            :key="p"
            type="button"
            class="preset-chip"
            :class="{ active: form.text.trim() === p }"
            @click="applyPreset(p)"
          >{{ p }}</button>
        </div>
        <p v-if="textOver" class="field-error">正文已超 {{ TEXT_LIMIT }} 字，保存会被拒绝，请精简。</p>

        <div class="setting-row">
          <span class="setting-main">
            <span class="setting-name">字号</span>
            <span class="setting-desc">正文字号（DIP px，24–200，越界自动钳位）</span>
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
      </section>

      <section class="panel">
        <h2 class="sec-title">唤起方式</h2>
        <p class="sec-note">三条通道随叫随到：全局热键（本页配置）、托盘右键菜单项、快捷轮盘条目——后两者在设置页「托盘菜单」添加"挂出/撤下留言牌"命令。</p>
        <div class="setting-row">
          <span class="setting-main">
            <span class="setting-name">全局热键</span>
            <span class="setting-desc">加速器写法如 <b class="mono">Ctrl+Alt+B</b>；留空 = 停用热键（托盘/轮盘不受影响）。改键即时注册，被其它程序占用会报错并回滚旧键。</span>
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
        <div v-if="status && status.hotkey !== '' && !status.hotkeyActive" class="banner banner-warn" role="note">
          热键「{{ status.hotkey }}」未在位：多半已被其它程序抢占。换个组合保存，或留空停用——期间可用托盘/轮盘唤起。
        </div>
        <div class="panel-foot">
          <button type="button" class="btn btn-ghost btn-small" @click="emit('navigate', '/settings/tray')">
            前往设置页配置托盘/轮盘条目
          </button>
        </div>
      </section>

      <section v-if="errorMsg && status" class="banner banner-error" role="alert">保存失败：{{ errorMsg }}</section>

      <section class="panel usage">
        <h2 class="sec-title">使用说明</h2>
        <ul class="usage-list">
          <li>牌体全屏覆盖目标显示器（含任务栏区域），<kbd>Esc</kbd> 或点击牌面任意处即撤；窗口不进任务栏与 Alt+Tab。</li>
          <li>挂出期间系统不休眠、显示器不息屏（平台层引用计数聚合器，撤牌/停用/退出即释放）。</li>
          <li>撤牌即真销毁窗口、再唤即重建——不留隐藏窗占内存，也不得白边残影。</li>
          <li>预设模板是一键填词候选，点选后进编辑框可继续改成自定义正文。</li>
          <li>不需要此能力时在设置页模块管理停用"桌面留言板"，热键随停用即摘。</li>
        </ul>
      </section>
    </template>
  </div>
</template>

<style scoped>
.status-group { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; justify-content: flex-end; }

.sec-title {
  font-size: var(--text-md);
  font-weight: 600;
  margin: 0;
  color: var(--color-text);
}
.sec-note {
  font-size: var(--text-sm);
  color: var(--color-text-muted);
  margin: 6px 0 12px;
  line-height: 1.6;
}
.panel-top {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
}
.panel-actions { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.panel-foot { display: flex; justify-content: flex-end; margin-top: 10px; }

.mb-text {
  resize: vertical;
  min-height: 64px;
  line-height: 1.6;
  font-family: inherit;
}
.preset-row { display: flex; flex-wrap: wrap; gap: 6px; margin: 8px 0 4px; }
.preset-chip {
  border: 1px solid var(--color-border);
  border-radius: var(--radius-pill);
  background: var(--surface-soft);
  color: var(--color-text-muted);
  font-size: var(--text-sm);
  padding: 3px 10px;
  cursor: pointer;
  transition: background var(--motion-fast) ease, color var(--motion-fast) ease, border-color var(--motion-fast) ease;
}
.preset-chip:hover { background: var(--surface-hover); color: var(--color-text); }
.preset-chip.active {
  border-color: var(--color-primary);
  background: var(--color-primary-soft);
  color: var(--color-primary);
}
.mb-num { width: 92px; flex: none; font-variant-numeric: tabular-nums; }
.mb-hotkey { width: 168px; flex: none; }
.field-error { margin: 4px 0 0; font-size: var(--text-sm); color: var(--state-danger); }
.field-warn { margin: 6px 0 0; font-size: var(--text-sm); color: var(--state-warning); }

.usage { margin-top: 16px; }
.usage-list {
  margin: 8px 0 0;
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
