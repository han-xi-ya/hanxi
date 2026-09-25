<script setup lang="ts">
// 「文字识别」：本地 hanxi-ocr 服务的托管启停 + 识别工作台（双引擎并存，计划 §5.5：
// PP-OCR 开源引擎 / 微信引擎组件，单端口单活，切换即重启识别服务）。
// 三输入通道（对话框选图 / 拖拽 / 粘贴）汇流为 ImageRef 后统一转发 path 模式识别；
// 状态以事件为主、5s 轮询兜底。
// 边界：识别能力全部在上游服务，本视图不做任何本地推理（与后端口径一致）。
import { computed, onMounted, ref, watch } from 'vue'
import * as OcrAPI from '../../bindings/hanxi/internal/modules/ocr/ocrservice'
import type { DropResult as GeneratedDropResult, EngineInfo, HostedVersion, ImageRef, OcrOutcome, ServiceState, SnipHotkeyState } from '../../bindings/hanxi/internal/modules/ocr/models'
import type { Record as HistoryRecord } from '../../bindings/hanxi/internal/history/models'
import { useToast } from '../composables/useToast'
import { useClipboard } from '../composables/useClipboard'
import { useConfirm } from '../composables/useConfirm'
import { useWailsEvent } from '../composables/useWailsEvent'
import { usePolling } from '../composables/usePolling'
import { useAsyncAction } from '../composables/useAsyncAction'
import { getErrorMessage } from '../utils/errors'
import { fmtSize } from '../utils/format'
import { parsePaste } from '../utils/paste'
import { toolStateMeta } from '../constants/status'
import PageHeader from '../components/ui/PageHeader.vue'
import UiStatusChip from '../components/ui/UiStatusChip.vue'
import UiBanner from '../components/ui/UiBanner.vue'
import HistoryPanel from '../components/tool/HistoryPanel.vue'
import UiHistoryDialog from '../components/ui/UiHistoryDialog.vue'

const { showToast } = useToast()
const { copyWithToast } = useClipboard()
const { confirm } = useConfirm()

// DropResult 的新增字段由本次 Go DTO 提供；绑定产物按协作约定由协调者统一生成。
type DropResult = GeneratedDropResult & {
  engine: string
  activated: boolean
  shouldStart: boolean
}

// ---------- 状态 ----------
const state = ref<ServiceState | null>(null) // null = 首帧尚未取得
const engines = ref<EngineInfo[]>([]) // 引擎注册表（GetEngines）；空=首帧未取得
const enginesReady = ref(false) // 已取得过一次（区分"加载中"与"真的没有"）
const hosted = ref<HostedVersion[]>([]) // 托管版本树清单（F7 ListHostedVersions）
const showSettings = ref(false)
const portInput = ref('')
const followOnExit = ref(true)
const autoCopy = ref(true)
const hotkey = ref<SnipHotkeyState | null>(null) // 剪贴板识图热键配置（null=未取得）

const image = ref<ImageRef | null>(null)
const outcome = ref<OcrOutcome | null>(null)
const dragOver = ref(false)
const readingFile = ref(false)

const { busy: recBusy, run: runRec } = useAsyncAction()
const { busy: ctrlBusy, run: runCtrl } = useAsyncAction()
const { busy: snipBusy, run: runSnip } = useAsyncAction()
const { busy: clipBusy, run: runClip } = useAsyncAction()
const { busy: switchBusy, run: runSwitch } = useAsyncAction()
const { busy: manageBusy, run: runManage } = useAsyncAction()
const switchingId = ref('') // 切换进行中的目标引擎（行内按钮态）
const uninstallingKey = ref('') // 卸载进行中的版本（engine-version 行内按钮态）

const chip = computed(() => (state.value ? toolStateMeta(state.value.state) : null))
const online = computed(() => state.value?.online === true)
const canRecognize = computed(() => online.value && !!image.value && !recBusy.value)

// 引擎中文名（表头/确认框展示用）：身份判定恒以 engineID（后端 store 登记），
// 上游 engine 型号串只作次要信息直出不解析（计划 §3/§5.1 口径）。
const ENGINE_LABELS: Record<string, string> = { wechat: '微信引擎', paddle: 'PP-OCR 开源引擎' }
const activeEngineLabel = computed(() => ENGINE_LABELS[state.value?.engineID || ''] || '')
// 引擎带病（服务在线但上游自报中文错误）→ 仅命中当前活跃行时给警示态
const engineSick = computed(() => online.value && !!state.value?.engineError)

// 服务掉线但已有上次结果 → stale 提示（保留数据，诚实标记）
const stale = computed(() => !!outcome.value?.ok && !!state.value && !state.value.online)


async function refreshStatus() {
  try {
    state.value = await OcrAPI.GetStatus()
  } catch (e) {
    console.warn('ocr GetStatus failed:', getErrorMessage(e))
  }
}

// 引擎注册表视图：与状态同频刷新（导入/切换后必须即时反映安装态与路径）。
async function refreshEngines() {
  try {
    engines.value = (await OcrAPI.GetEngines()) || [] // Go 空切片到达为 null，归一为空数组
    enginesReady.value = true
  } catch (e) {
    console.warn('ocr GetEngines failed:', getErrorMessage(e))
  }
}

// 托管版本树清单（F7）：与状态/注册表同频刷新（安装/卸载后生效标记须即时跟随）。
async function refreshHosted() {
  try {
    hosted.value = (await OcrAPI.ListHostedVersions()) || [] // Go 空切片到达为 null，归一为空数组
  } catch (e) {
    console.warn('ocr ListHostedVersions failed:', getErrorMessage(e))
  }
}

async function refreshAll() {
  await Promise.all([refreshStatus(), refreshEngines(), refreshHosted()])
}
usePolling(refreshAll, 5000)
useWailsEvent<ServiceState>('ocr:service-state', (st) => {
  if (st && typeof st.state === 'string') state.value = st
})

// ---------- 启停 ----------
async function startService() {
  const res = await runCtrl(() => OcrAPI.StartService())
  if (res.ok) {
    showToast(res.data.message || '服务已启动')
  } else {
    showToast(`启动失败: ${getErrorMessage(res.error)}`)
  }
  await refreshStatus()
}

async function stopService() {
  const res = await runCtrl(() => OcrAPI.StopService())
  if (res.ok) {
    showToast(res.data.message || '服务已停止')
  } else {
    showToast(`停止失败: ${getErrorMessage(res.error)}`)
  }
  await refreshStatus()
}

// ---------- 组件导入（拖放/对话框；回执统一走 ocr:file-drop-result 事件） ----------
// 后端回执是安装后激活/启动语义的唯一真相；前端不得再从事件到达时的旧 state 推断。
// 微信引擎：单文件 exe（后端按形态分流，拖文件走原校验链）。
async function importViaDialog() {
  try {
    await OcrAPI.ImportServiceExeDialog() // 成功/失败提示与自动启动由事件统一处理，此处只兜程序性错误
  } catch (e) {
    showToast(getErrorMessage(e))
  }
}

// PP-OCR 开源引擎：目录件（后端原生文件夹框选目录 + 导入一体）。成功/失败回执
// （含校验拒绝的中文指引）均由后端广播 ocr:file-drop-result 统一处理，此处
// 只兜程序性错误；登记即激活与停旧起新收口在后端，前端不再补启停。
async function importPaddleViaDialog() {
  try {
    await OcrAPI.ImportPaddleDirDialog()
  } catch (e) {
    showToast(getErrorMessage(e))
  }
}

// ---------- F7 托管版本（zip 安装 / 列表 / 卸载） ----------
// 安装=对话框选 zip（与拖放同一后端校验链，回执统一走 ocr:file-drop-result）；
// 卸载=删版本目录（在跑生效版本由后端拒卸，中文指引原样直出不吞）。

function hostedOf(engineId: string): HostedVersion[] {
  return hosted.value.filter((v) => v.engine === engineId)
}

async function installZipViaDialog() {
  try {
    await OcrAPI.InstallHostedZipDialog() // 校验/成功/失败提示由事件回执统一处理，此处只兜程序性错误
  } catch (e) {
    showToast(getErrorMessage(e))
  }
}

async function requestUninstall(v: HostedVersion) {
  const accepted = await confirm({
    title: `卸载 ${ENGINE_LABELS[v.engine] || v.engine} v${v.version}？`,
    description: v.effective
      ? '该版本当前生效：卸载后本引擎自动改用托管树内其余版本；无其余版本则回退旧自动发现。正在运行的服务不受影响，下次启动起新件。'
      : '仅删除该托管版本目录；安装包原件保留在 installers/，随时可重拖装回。',
    confirmLabel: '卸载',
    tone: 'danger',
    details: [
      { label: '引擎', value: ENGINE_LABELS[v.engine] || v.engine },
      { label: '版本', value: v.version },
      { label: '大小', value: fmtSize(v.size || 0) },
    ],
  })
  if (!accepted) return
  uninstallingKey.value = `${v.engine}-${v.version}`
  const res = await runManage(() => OcrAPI.UninstallHostedVersion(v.engine, v.version))
  uninstallingKey.value = ''
  if (!res.ok) {
    showToast(`卸载失败: ${getErrorMessage(res.error)}`)
  } else {
    showToast(res.data.message || '已卸载') // 在用拒卸（refused-in-use）的中文指引同样直出
  }
  await refreshAll()
}

// ---------- 引擎切换（单活语义：在跑则停旧起新，须显式确认） ----------
async function requestSwitch(id: string) {
  const eng = engines.value.find((e) => e.id === id)
  if (!eng || eng.active || !eng.installed || switchBusy.value) return
  const st = state.value
  // 托管实例在跑 → 切换必然重启识别服务：先确认；未运行/external 直发后端
  // （external 由后端按"不越权"拒绝并给中文指引，前端原样呈现）
  if (st && !st.external && (st.state === 'running' || st.state === 'starting')) {
    const accepted = await confirm({
      title: `切换为${eng.label}？`,
      description: '当前识别服务正在运行：切换引擎将重启识别服务（先停止现有实例、再启动新引擎），期间短暂中断，进行中的识别请求会失败。',
      confirmLabel: '重启并切换',
      tone: 'warning',
      details: [
        { label: '当前引擎', value: activeEngineLabel.value || '—' },
        { label: '切换为', value: eng.label },
      ],
    })
    if (!accepted) return
  }
  switchingId.value = id
  const res = await runSwitch(() => OcrAPI.SetActiveEngine(id))
  switchingId.value = ''
  if (!res.ok) {
    showToast(`切换引擎失败: ${getErrorMessage(res.error)}`)
  } else if (res.data.action === 'external-unmanaged') {
    showToast(res.data.message || '外部实例正在服务，暂不接管切换') // 拒绝原因必须直出，不能吞
  } else {
    showToast(res.data.message || '引擎切换完成')
  }
  await refreshAll()
}

useWailsEvent<DropResult>('ocr:file-drop-result', (r) => {
  if (!r || !r.kind) return
  if (r.kind === 'image') {
    if (r.ok && r.image) {
      image.value = r.image // 原生通道真实路径：免 dataURL 全量 IPC
      outcome.value = null
    } else if (r.message) {
      showToast(r.message)
    }
    return
  }
  // import：取消对话框回执静默（无 message）
  if (!r.ok) {
    if (r.message) showToast(r.message)
    void refreshAll()
    return
  }
  showToast(r.message || (r.activated ? '引擎已安装并设为当前' : '引擎已安装'))
  void refreshAll()
  if (r.shouldStart) void startService()
})

// 从未发现组件的首启场景：自动展开设置面板，让导入区直达视线（只提示一次）
const importHinted = ref(false)
watch(state, (st) => {
  if (!importHinted.value && st && st.state === 'stopped' && !st.exePath) {
    importHinted.value = true
    showSettings.value = true
  }
})

// ---------- 设置面板 ----------
async function loadSettings() {
  try {
    portInput.value = String(await OcrAPI.GetListenPort())
    followOnExit.value = await OcrAPI.GetFollowOnExit()
    autoCopy.value = await OcrAPI.GetAutoCopy()
  } catch (e) {
    console.warn('ocr settings load failed:', getErrorMessage(e))
  }
  try {
    hotkey.value = await OcrAPI.GetSnipHotkey()
  } catch (e) {
    console.warn('ocr hotkey settings load failed:', getErrorMessage(e))
  }
}

// ---------- 剪贴板识图热键（键位录入 + 冲突即时报错，PLAN_CLIPBOARD §3.3） ----------
const hotkeyError = ref('')
const recording = ref(false)
const recordPreview = ref('')

// 键位框回显：录入中显示实时预览/引导语，静默时显示实际键位
const hotkeyDisplay = computed(() => {
  if (recording.value) return recordPreview.value || '按下新组合键…'
  return hotkey.value?.accel || '…'
})

function startRecord() {
  recording.value = true
  recordPreview.value = ''
  hotkeyError.value = ''
}

function cancelRecord() {
  // 焦点离开即退出录入；组合键录入途中失焦只复位不改配置
  recording.value = false
  recordPreview.value = ''
}

// e.key → 规范化主键：单字符大写；F 区直取；DOM 专有名映射到 Wails 具名键。
const NAMED_KEYS: Record<string, string> = {
  ' ': 'Space', Escape: 'Escape', Enter: 'Enter', Tab: 'Tab', Backspace: 'Backspace',
  Delete: 'Delete', Insert: 'Insert', Home: 'Home', End: 'End',
  PageUp: 'Page Up', PageDown: 'Page Down', ArrowLeft: 'Left', ArrowUp: 'Up',
  ArrowRight: 'Right', ArrowDown: 'Down',
}

function recordKeyName(key: string): string {
  if (NAMED_KEYS[key]) return NAMED_KEYS[key]
  if (/^F\d{1,2}$/.test(key)) return key
  if (key.length === 1) return key.toUpperCase()
  return key
}

async function onRecordKey(e: KeyboardEvent) {
  if (!recording.value) return
  e.preventDefault()
  e.stopPropagation()
  if (['Control', 'Alt', 'Shift', 'Meta', 'OS'].includes(e.key)) {
    // 修饰键按下途中：实时预览组合前缀（Meta/OS → Win）
    const mods = [e.ctrlKey && 'Ctrl', e.altKey && 'Alt', e.shiftKey && 'Shift', (e.metaKey || e.key === 'OS') && 'Win'].filter(Boolean)
    recordPreview.value = mods.length ? mods.join('+') + '+' : ''
    return
  }
  const mods = [e.ctrlKey && 'Ctrl', e.altKey && 'Alt', e.shiftKey && 'Shift', e.metaKey && 'Win'].filter(Boolean)
  const accel = [...mods, recordKeyName(e.key)].join('+')
  recording.value = false
  recordPreview.value = ''
  await commitHotkey(accel)
}

async function commitHotkey(accel: string) {
  hotkeyError.value = ''
  try {
    await OcrAPI.SetSnipHotkey(accel)
    hotkey.value = await OcrAPI.GetSnipHotkey()
    showToast(`剪贴板识图热键已设为 ${accel}`)
  } catch (e) {
    // 注册冲突：后端已回滚不落账——红字直出占用原因，回显恢复实际键位
    hotkeyError.value = getErrorMessage(e)
    try {
      hotkey.value = await OcrAPI.GetSnipHotkey()
    } catch { /* 拉取失败保持现回显 */ }
  }
}

async function toggleHotkey(v: boolean) {
  if (!hotkey.value) return
  const prev = hotkey.value
  hotkey.value = { ...prev, enabled: v }
  hotkeyError.value = ''
  try {
    await OcrAPI.SetSnipHotkeyEnabled(v)
    hotkey.value = await OcrAPI.GetSnipHotkey()
    showToast(v ? '热键已启用：复制图片后按键即识别' : '热键已停用（页内「剪贴板识图」按钮仍可用）')
  } catch (e) {
    hotkey.value = prev // 占用失败后端未落账，回滚回显
    hotkeyError.value = getErrorMessage(e)
  }
}

function resetHotkey() {
  void commitHotkey('Ctrl+Alt+T')
}

// 框选截屏识别：与轮盘命令同链路（系统截屏 → 识别 → 悬浮卡+自动复制）。
async function snipRecognize() {
  const res = await runSnip(() => OcrAPI.SnipAndRecognize())
  if (!res.ok) {
    showToast(`截屏识别失败: ${getErrorMessage(res.error)}`)
    return
  }
  if (res.data.cancelled) return // 用户放弃选区：静默
  if (res.data.ok) showToast('识别完成，结果已在悬浮卡中')
}

// 剪贴板识图：与全局热键（默认 Ctrl+Alt+T）/轮盘命令同链路——剪贴板已有图
// 直接识别，不弹截屏覆盖层、不清用户剪贴板。
async function snipClipboard() {
  const res = await runClip(() => OcrAPI.RecognizeClipboardImage())
  if (!res.ok) {
    showToast(`剪贴板识图失败: ${getErrorMessage(res.error)}`)
    return
  }
  if (res.data.ok) showToast('识别完成，结果已在悬浮卡中')
}

async function toggleAutoCopy(v: boolean) {
  autoCopy.value = v
  try {
    await OcrAPI.SetAutoCopy(v)
    showToast(v ? '截屏识别后将自动复制文字' : '不再自动复制，可在卡片内手动选取')
  } catch (e) {
    autoCopy.value = !v
    showToast(getErrorMessage(e))
  }
}

async function applyPort() {
  const port = Number(portInput.value)
  if (!Number.isInteger(port)) {
    showToast('端口需为整数')
    return
  }
  try {
    const res = await OcrAPI.SetListenPort(port)
    showToast(res === 'pending' ? '端口已保存，下次启动服务生效' : '端口已应用')
    await refreshStatus()
  } catch (e) {
    showToast(getErrorMessage(e))
  }
}

async function toggleFollow(v: boolean) {
  followOnExit.value = v
  try {
    await OcrAPI.SetFollowOnExit(v)
    showToast(v ? 'Hanxi 退出时将一起关闭服务' : '服务独立驻留，不随 Hanxi 退出')
  } catch (e) {
    followOnExit.value = !v
    showToast(getErrorMessage(e))
  }
}

// ---------- 图片三通道 → ImageRef 汇流 ----------
function dataURLof(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => resolve(String(reader.result))
    reader.onerror = () => reject(new Error('读取剪贴板/拖拽图片失败'))
    reader.readAsDataURL(file)
  })
}

async function acceptFile(file: File) {
  if (!file.type.startsWith('image/')) {
    showToast('只支持图片文件（PNG / JPG / WEBP / BMP / GIF / TIFF）')
    return
  }
  readingFile.value = true
  try {
    const ref = await OcrAPI.SavePastedImage(file.name || '粘贴图片', await dataURLof(file))
    image.value = ref
    outcome.value = null
  } catch (e) {
    showToast(getErrorMessage(e))
  } finally {
    readingFile.value = false
  }
}

// Wails/WebView2 环境下文件拖放走原生通道（带真实磁盘路径，由后端回报
// ocr:file-drop-result），DOM File 通道仅作纯浏览器开发的降级回退，
// 二者互斥防双处理。
function nativeDropActive() {
  const w = window as unknown as { chrome?: { webview?: { postMessageWithAdditionalObjects?: unknown } } }
  return !!w.chrome?.webview?.postMessageWithAdditionalObjects
}

function onDrop(e: DragEvent) {
  dragOver.value = false
  if (nativeDropActive()) return
  const file = e.dataTransfer?.files?.[0]
  if (file) void acceptFile(file)
}

function onPaste(e: ClipboardEvent) {
  // 三态分流走 utils/paste 纯函数：图片接管；文本/非图文件放行（dropzone 无文本语义）
  const payload = parsePaste(e)
  if (payload.kind !== 'image') return
  e.preventDefault()
  void acceptFile(payload.file)
}

async function chooseByDialog() {
  try {
    const path = await OcrAPI.PickImageDialog()
    if (!path) return // 用户取消
    image.value = await OcrAPI.InspectImage(path)
    outcome.value = null
  } catch (e) {
    showToast(getErrorMessage(e))
  }
}

function clearImage() {
  image.value = null
  outcome.value = null
}

// ---------- 识别与复制 ----------
async function recognize() {
  if (!image.value) return
  const res = await runRec(() => OcrAPI.RecognizeImage(image.value!.path))
  if (!res.ok) {
    outcome.value = { ok: false, text: '', lines: [], elapsedMs: 0, error: getErrorMessage(res.error) }
    return
  }
  outcome.value = res.data
  if (!res.data.ok) showToast('识别失败，详见结果区提示')
}

async function copyAll() {
  await copyWithToast(outcome.value?.text || '', '已复制全部文本')
}

async function copyLine(text: string) {
  await copyWithToast(text, '已复制该行')
}

// ---------- 历史记录（Teleport 弹窗；双击行经 InspectImage 回填图片，Q6 行内数据直用） ----------
// 面板本体走公共 HistoryPanel（自取数），本视图只管遮罩壳与回填出口。N20（2026-09-25）
// 对"弹窗里再弹确认"场景做了两处契约适配（ConfirmDialog 是同层 1000 的 App 单例）：
//   1) Esc 在 confirmState.open 时让位——「清空本桶」确认盖在本弹窗上，一次 Esc
//      不得同时关掉两层（document 级监听与 ConfirmDialog 自身监听同场竞走）；
//   2) 本弹窗 z-index 低于 ConfirmDialog——视图经 KeepAlive 懒挂载，本 Teleport 的
//      DOM 位置天然晚于 App 单例确认框，同层拼 DOM 序不能保证确认框反而在上。
const showHistory = ref(false)

// 历史行→识别区回填（Q6 行内数据直用；InspectImage 重读本机图片，失败中文 toast）
async function applyHistoryImage(rec: HistoryRecord) {
  try {
    image.value = await OcrAPI.InspectImage(rec.input)
    outcome.value = null
    showHistory.value = false
  } catch (e) {
    showToast(getErrorMessage(e))
  }
}

onMounted(() => {
  void refreshAll()
  void loadSettings()
})
</script>

<template>
  <section class="page ocr-view">
    <PageHeader
      title="文字识别"
      subtitle="本地 hanxi-ocr 服务，双引擎并存：PP-OCR 开源引擎（公开可获取，自行导入）或微信引擎组件（私发）。拖入、粘贴或选择图片即可识别，全程离线不联网。"
    >
      <template #actions>
        <div class="ocr-head-actions">
          <span v-if="!state" class="ocr-checking live-pulse">探测服务中…</span>
          <UiStatusChip v-else :tone="chip!.tone">{{ chip!.text }}</UiStatusChip>
          <span v-if="online && state!.version" class="ocr-ver" :title="`上游型号 ${state!.engine}`">
            <template v-if="activeEngineLabel">{{ activeEngineLabel }} · </template>{{ state!.version }}<template v-if="!state!.engineRunning"> · 引擎预热中</template>
          </span>
          <button class="btn btn-secondary btn-small" :disabled="snipBusy" title="唤起系统截屏，框选区域即识别（服务未运行时自动拉起）" @click="snipRecognize">
            {{ snipBusy ? '截屏识别中…' : '📷 框选识别' }}
          </button>
          <button class="btn btn-secondary btn-small" :aria-expanded="showHistory" @click="showHistory = true" title="最近识别留档：图片路径与文本可一键回填">
            🕘 历史
          </button>
          <button class="btn btn-secondary btn-small" :disabled="clipBusy" title="识别剪贴板中已有的图片，不重新截屏（与全局热键 Ctrl+Alt+T 同链路）" @click="snipClipboard">
            {{ clipBusy ? '识图中…' : '📋 剪贴板识图' }}
          </button>
          <button class="btn btn-secondary btn-small" :aria-expanded="showSettings" @click="showSettings = !showSettings">
            {{ showSettings ? '收起设置' : '服务设置' }}
          </button>
        </div>
      </template>
    </PageHeader>

    <!-- 服务引导：按状态给下一步，绝不裸报错 -->
    <UiBanner v-if="state && state.state === 'stopped'" tone="info">
      识别服务未运行：
      <button class="link-button" @click="showSettings = true">打开引擎列表</button>
      安装其一——<b>引擎安装包（.zip，旁挂 .sha256）</b>拖入即自动校验落位，
      也可旧式导入 <b>PP-OCR 解压目录</b>或<b>微信引擎</b>单文件 hanxi-ocr.exe（私发渠道获取）；
      引擎已就绪则直接启动。外部自行启动的实例会被自动接管识别。
      <button class="btn btn-primary btn-small ocr-banner-btn" :disabled="ctrlBusy" @click="startService">
        {{ ctrlBusy ? '启动中…' : '▶ 启动服务' }}
      </button>
    </UiBanner>
    <UiBanner v-else-if="state && state.state === 'starting'" tone="info">
      服务启动中（冷启动约 1 秒），就绪前输入区暂不可用…
    </UiBanner>
    <UiBanner v-else-if="state && state.external" tone="warn">
      检测到外部自行启动的 hanxi-ocr（{{ state.listenAddr }}）：识别照常，但启停不接管（进程归属不在 Hanxi）。
    </UiBanner>
    <UiBanner v-else-if="state && state.state === 'failed'" tone="error">
      {{ state.error || '服务异常退出' }}
      <button class="btn btn-primary btn-small ocr-banner-btn" :disabled="ctrlBusy" @click="startService">
        {{ ctrlBusy ? '启动中…' : '重试启动' }}
      </button>
    </UiBanner>
    <UiBanner v-else-if="state && state.hung" tone="warn">
      引擎挂起无响应（识别 Call 未返回）。重启服务可恢复：
      <button class="btn btn-secondary btn-small ocr-banner-btn" :disabled="ctrlBusy" @click="stopService">停止服务</button>
    </UiBanner>
    <!-- 引擎带病：服务在线但上游自报中文错误——独立警示（可与上行横幅叠加），绝不吞 -->
    <UiBanner v-if="engineSick" tone="warn">
      服务在线但当前引擎异常：{{ state!.engineError }}。可停止并重新启动服务，或在「服务设置 → 识别引擎」切换引擎尝试恢复。
    </UiBanner>

    <div v-if="showSettings" class="ocr-card ocr-settings">
      <div class="section-title"><h3>识别引擎</h3></div>
      <p class="ocr-set-note">单端口单活：同一时刻仅一个引擎在服务，切换即重启识别；两者对识别链路完全等价。</p>

      <!-- 引擎列表：GetEngines 注册表驱动，两行卡（微信 / PP-OCR 开源） -->
      <div v-if="!enginesReady" class="state-box ocr-eng-loading">读取引擎列表…<span class="live-pulse">…</span></div>
      <div v-else class="ocr-engine-list">
        <div v-for="eng in engines" :key="eng.id" class="ocr-engine-row"
          :class="{ 'ocr-engine-current': eng.active, 'ocr-engine-sick': eng.active && engineSick }">
          <div class="ocr-engine-top">
            <span class="ocr-engine-name">{{ eng.label }}</span>
            <span class="ocr-engine-badges">
              <UiStatusChip v-if="eng.active" :tone="engineSick ? 'warning' : 'positive'">
                {{ engineSick ? '当前 · 引擎异常' : '当前引擎' }}
              </UiStatusChip>
              <UiStatusChip :tone="eng.installed ? 'positive' : 'neutral'">{{ eng.installed ? '已安装' : '未安装' }}</UiStatusChip>
              <span v-if="eng.auto" class="ocr-set-tag">自动发现</span>
            </span>
          </div>
          <div v-if="eng.installed" class="ocr-engine-meta">
            <span v-if="eng.version" class="ocr-engine-ver mono">{{ eng.version }}</span>
            <code class="ocr-engine-path mono" :title="eng.path">{{ eng.path }}</code>
            <span v-if="eng.active && online && state!.engine" class="ocr-engine-model">
              上游型号 {{ state!.engine }}<template v-if="state!.engineMode"> · {{ state!.engineMode }}</template>
            </span>
          </div>
          <!-- 警示态优先：当前引擎带病直出中文原因；未安装给注册表指引 -->
          <p v-if="eng.active && engineSick" class="ocr-engine-warn">{{ state!.engineError }}</p>
          <p v-else-if="eng.error" class="ocr-engine-err">{{ eng.error }}</p>
          <div class="ocr-engine-actions">
            <button v-if="eng.installed && !eng.active" class="btn btn-primary btn-small"
              :disabled="switchBusy" @click="requestSwitch(eng.id)">
              {{ switchBusy && switchingId === eng.id ? '切换中…' : '设为当前' }}
            </button>
            <button v-if="eng.id === 'paddle'" class="btn btn-secondary btn-small" @click="importPaddleViaDialog">
              {{ eng.installed ? '更换目录…' : '导入目录…' }}
            </button>
            <button v-else class="btn btn-secondary btn-small" @click="importViaDialog">
              {{ eng.installed ? '更换组件…' : '导入组件…' }}
            </button>
          </div>
          <!-- F7 托管版本：该引擎已装进 versions/hanxi-ocr 的落位件（标记生效版，可卸载） -->
          <div v-if="hostedOf(eng.id).length" class="ocr-hosted">
            <div v-for="v in hostedOf(eng.id)" :key="v.version" class="ocr-hosted-row"
              :class="{ 'ocr-hosted-effective': v.effective }">
              <span class="ocr-hosted-ver mono" :title="v.note || v.dir">{{ v.version }}</span>
              <UiStatusChip v-if="v.effective" tone="positive">生效</UiStatusChip>
              <UiStatusChip v-if="v.state !== 'ready'" tone="danger">损坏</UiStatusChip>
              <span class="ocr-hosted-meta">
                {{ fmtSize(v.size) }}<template v-if="v.installedAt"> · {{ v.installedAt }}</template>
              </span>
              <button class="btn btn-secondary btn-small ocr-hosted-uninstall" :disabled="manageBusy"
                :aria-label="`卸载 ${eng.label} ${v.version}`" @click="requestUninstall(v)">
                {{ uninstallingKey === eng.id + '-' + v.version ? '卸载中…' : '卸载' }}
              </button>
            </div>
          </div>
        </div>
      </div>

      <div class="ocr-set-row ocr-import-row ocr-zip-row">
        <span class="ocr-set-k">引擎包</span>
        <button class="btn btn-primary btn-small" @click="installZipViaDialog">安装引擎包…</button>
        <div id="ocr-import-target" class="ocr-import-drop" data-file-drop-target="true"
          role="button" tabindex="0" aria-label="拖入引擎安装包 zip 托管安装；点击可选取 zip"
          @click="installZipViaDialog" @keydown.enter.prevent="installZipViaDialog">
          📦 推荐：把引擎安装包 <b>hanxi-ocr-&lt;engine&gt;-&lt;version&gt;.zip</b>（旁挂同名 .sha256）
          拖到这里或点「安装引擎包…」——自动识别引擎并校验落位；非当前引擎只登记，是否启动以后端安装回执为准
        </div>
      </div>
      <div class="ocr-set-row">
        <span class="ocr-set-k">旧式导入</span>
        <span class="ocr-set-note">
          <b>PP-OCR</b> 解压目录（含 hanxi-ocr.exe 与 manifest.json）或 <b>微信引擎</b>单文件
          hanxi-ocr.exe 仍走上方引擎行「导入目录/导入组件」——引用式指向，不复制落位
        </span>
      </div>
      <div class="ocr-set-row">
        <span class="ocr-set-k">端口</span>
        <input v-model="portInput" class="ocr-set-port" inputmode="numeric" aria-label="服务端口" />
        <button class="btn btn-secondary btn-small" @click="applyPort">应用</button>
        <label class="ocr-set-follow">
          <input type="checkbox" :checked="followOnExit" @change="toggleFollow(($event.target as HTMLInputElement).checked)" />
          随 Hanxi 退出一起关闭
        </label>
      </div>
      <div class="ocr-set-row">
        <span class="ocr-set-k">截屏识别</span>
        <label class="ocr-set-follow">
          <input type="checkbox" :checked="autoCopy" @change="toggleAutoCopy(($event.target as HTMLInputElement).checked)" />
          识别后自动把文字复制到剪贴板（关闭后可在结果卡内手动选字）
        </label>
      </div>
      <div class="ocr-set-row ocr-hotkey-row">
        <span class="ocr-set-k">识图热键</span>
        <input
          class="ocr-hotkey-input mono" :class="{ 'ocr-hotkey-recording': recording }" readonly
          :value="hotkeyDisplay" :disabled="!hotkey"
          aria-label="剪贴板识图全局热键：点击后按下新组合键"
          @click="startRecord" @keydown="onRecordKey" @blur="cancelRecord"
        />
        <button class="btn btn-secondary btn-small" :disabled="!hotkey || hotkey.accel === 'Ctrl+Alt+T'" @click="resetHotkey">恢复默认</button>
        <label class="ocr-set-follow">
          <input type="checkbox" :checked="hotkey?.enabled" :disabled="!hotkey" @change="toggleHotkey(($event.target as HTMLInputElement).checked)" />
          全局生效：任意软件里复制图片后按键即识别（纯键位组合不开，须带修饰键）
        </label>
      </div>
      <p v-if="hotkeyError" class="ocr-hotkey-err" role="alert">{{ hotkeyError }}</p>
      <p v-else-if="hotkey && hotkey.enabled && !hotkey.registered" class="ocr-hotkey-err" role="alert">
        热键当前未注册成功（可能被其他软件抢占）：点击键位框改键重试。
      </p>
      <p class="ocr-set-note">改端口对已运行的实例下次启动生效；外部自行启动的实例端口以其自身为准。</p>
    </div>

    <div class="ocr-grid">
      <!-- 输入卡 -->
      <div class="ocr-card" :class="{ 'ocr-card-off': !online }">
        <div class="ocr-card-head">
          <h2>选择图片</h2>
          <button v-if="online" class="btn btn-secondary btn-small" :disabled="readingFile" @click="chooseByDialog">
            {{ readingFile ? '读取中…' : '选择图片' }}
          </button>
        </div>

        <div v-if="!image" id="ocr-image-dropzone" class="ocr-dropzone" :class="{ 'ocr-dropzone-hot': dragOver }"
          data-file-drop-target="true"
          tabindex="0" aria-label="拖入图片、Ctrl+V 粘贴图片或按回车选择图片"
          :aria-disabled="!online" @dragover.prevent="dragOver = true"
          @dragleave="dragOver = false" @drop.prevent="onDrop"
          @paste="onPaste" @keydown.enter.prevent="online && chooseByDialog()">
          <p v-if="dragOver" class="ocr-drop-hot-text">松开即载入图片</p>
          <template v-else>
            <p class="ocr-drop-title">拖入图片 · Ctrl+V 粘贴截图</p>
            <p class="ocr-drop-sub">支持 PNG / JPG / WEBP / BMP / GIF / TIFF，单张不超过 64 MB；也可直接「选择图片」</p>
          </template>
        </div>

        <div v-else class="ocr-preview" aria-live="polite">
          <img v-if="image.previewUrl" :src="image.previewUrl" class="ocr-thumb" alt="待识别图片预览" />
          <div v-else class="ocr-thumb ocr-thumb-na" aria-hidden="true">图</div>
          <div class="ocr-preview-meta">
            <strong :title="image.path">{{ image.name }}</strong>
            <span>{{ fmtSize(image.size) }}<template v-if="image.temporary"> · 临时件</template></span>
          </div>
          <div class="ocr-preview-actions">
            <button class="btn btn-primary btn-small" :disabled="!canRecognize" @click="recognize">
              <span v-if="recBusy" class="live-pulse">识别中…</span>
              <span v-else>识别文字</span>
            </button>
            <button class="btn btn-secondary btn-small" :disabled="recBusy" @click="clearImage">移除</button>
          </div>
        </div>

        <p class="ocr-card-foot">识别引擎冷启动约 1 秒；超时会自动复位，重试即可。</p>
      </div>

      <!-- 结果卡 -->
      <div class="ocr-card ocr-result">
        <div class="ocr-card-head">
          <h2>识别结果</h2>
          <button v-if="outcome?.ok" class="btn btn-secondary btn-small" @click="copyAll">复制全文</button>
        </div>

        <div v-if="recBusy" class="state-box ocr-box">识别中<span class="live-pulse">…</span><p>复杂大图可能需要若干秒（最长 30 秒）</p></div>

        <template v-else-if="outcome?.ok">
          <!-- stale：保留最后有效结果并显式标记（设计规范：不得只降透明度或用横幅顶掉数据） -->
          <div v-if="stale" class="banner banner-warn ocr-stale">服务连接已断开，以下为上次识别内容</div>
          <div class="ocr-meta">{{ outcome.lines?.length ?? 0 }} 行 · 耗时 {{ outcome.elapsedMs }} ms</div>
          <pre class="ocr-text" aria-label="识别全文">{{ outcome.text }}</pre>
          <ul class="ocr-lines">
            <li v-for="(ln, i) in outcome.lines || []" :key="i">
              <span class="ocr-coord">{{ ln.x }},{{ ln.y }}</span>
              <span class="ocr-line-text">{{ ln.text }}</span>
              <button class="link-button" :aria-label="`复制第 ${i + 1} 行`" @click="copyLine(ln.text)">复制</button>
            </li>
          </ul>
        </template>

        <div v-else-if="outcome" class="error-box ocr-box" role="alert">
          {{ outcome.error }}
          <button class="btn btn-secondary btn-small ocr-retry" :disabled="!canRecognize" @click="recognize">重试识别</button>
        </div>

        <div v-else class="empty-state ocr-box">
          <p>暂无识别结果 —— 拖入、粘贴或选择图片后点击「识别文字」</p>
        </div>
      </div>
    </div>

    <!-- 历史记录弹窗（公共 HistoryPanel 自取数；Esc/遮罩/关闭 + 焦点入窗/回位，N20 适配弹窗内确认框，见脚本注释） -->
    <UiHistoryDialog :open="showHistory" title="识别历史" @close="showHistory = false"
      note="本页「识别文字」、框选识别、剪贴板识图每次识别各留一条（成败同记，最多保留最近 200 条；关闭设置「识别历史收录 OCR 全文」时只记图片与摘要）。双击记录行或选中后点「应用」，原图回填上方识别区。">
      <HistoryPanel func-type="ocr" max-height="min(46vh, 420px)" @apply="applyHistoryImage" />
    </UiHistoryDialog>
  </section>
</template>

<style scoped>
.ocr-view { display: flex; flex-direction: column; gap: 10px; }
.ocr-head-actions { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.ocr-ver { font-size: var(--text-xs); color: var(--color-text-subtle); font-variant-numeric: tabular-nums; }
.ocr-checking { font-size: var(--text-sm); color: var(--color-text-muted); }
.ocr-banner-btn { margin-left: 8px; vertical-align: middle; }

/* 双面任务卡：输入 5 / 结果 7，窄屏塌单列（结构变换而非缩小文字） */
.ocr-grid { display: grid; grid-template-columns: minmax(0, 5fr) minmax(0, 7fr); gap: 12px; align-items: stretch; }
@media (max-width: 860px) { .ocr-grid { grid-template-columns: minmax(0, 1fr); } }

.ocr-card {
  background: var(--surface-panel); border: 1px solid var(--color-border); border-radius: var(--radius-control);
  padding: 14px 16px; display: flex; flex-direction: column; gap: 10px; min-width: 0;
}
.ocr-card-off { opacity: 0.75; }
.ocr-card-off .ocr-dropzone { pointer-events: none; }
.ocr-card-head { display: flex; align-items: center; justify-content: space-between; gap: 8px; }
.ocr-card-head h2 { font-size: var(--text-md); font-weight: 600; color: var(--color-text); margin: 0; }
.ocr-card-foot { font-size: var(--text-xs); color: var(--color-text-subtle); margin: 0; }

/* 拖区：虚线面板（state-box 语系），焦点/悬停只升一级 */
.ocr-dropzone {
  flex: 1; min-height: 170px; display: flex; flex-direction: column; align-items: center; justify-content: center;
  gap: 6px; text-align: center; padding: 16px; cursor: pointer;
  border: 1px dashed var(--color-border); border-radius: var(--radius-element); background: var(--surface-soft);
  transition: border-color var(--motion-base) ease, background var(--motion-base) ease;
}
.ocr-dropzone:hover, .ocr-dropzone:focus-visible { border-color: var(--color-primary); background: var(--surface-hover); }
.ocr-dropzone:focus-visible { outline: 2px solid var(--color-primary); outline-offset: 2px; }
.ocr-dropzone-hot { border-color: var(--color-primary); background: var(--primary-soft, var(--surface-hover)); }
.ocr-drop-title { font-size: var(--text-base); font-weight: 600; color: var(--color-text); margin: 0; }
.ocr-drop-sub { font-size: var(--text-xs); color: var(--color-text-muted); margin: 0; }
.ocr-drop-hot-text { font-size: var(--text-md); font-weight: 600; color: var(--color-primary); margin: 0; }

/* 预览卡（wechatbot 附件舞台卡形制） */
.ocr-preview { display: grid; grid-template-columns: 56px minmax(0, 1fr) auto; gap: 10px; align-items: center; padding: 8px; background: var(--surface-soft); border: 1px solid var(--color-border); border-radius: var(--radius-element); }
.ocr-thumb { width: 56px; height: 56px; object-fit: contain; border-radius: var(--radius-control); background: var(--surface-hover); }
.ocr-thumb-na { display: flex; align-items: center; justify-content: center; font-size: var(--text-lg); color: var(--color-text-subtle); }
.ocr-preview-meta { display: flex; flex-direction: column; gap: 2px; min-width: 0; font-size: var(--text-sm); color: var(--color-text-muted); }
.ocr-preview-meta strong { color: var(--color-text); font-weight: 600; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.ocr-preview-actions { display: flex; gap: 8px; flex-wrap: wrap; }

/* 结果区 */
.ocr-box { flex: 1; }
.ocr-meta { font-size: var(--text-xs); color: var(--color-text-subtle); font-variant-numeric: tabular-nums; }
.ocr-text {
  margin: 0; padding: 10px 12px; font-size: var(--text-base); line-height: 1.55; color: var(--color-text);
  white-space: pre-wrap; word-break: break-word; background: var(--surface-soft);
  border: 1px solid var(--color-border); border-radius: var(--radius-element);
  max-height: 240px; overflow-y: auto;
}
.ocr-lines { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 2px; max-height: 300px; overflow-y: auto; }
.ocr-lines li { display: grid; grid-template-columns: 72px minmax(0, 1fr) auto; gap: 8px; align-items: baseline; padding: 4px 8px; border-radius: 6px; font-size: var(--text-sm); }
.ocr-lines li:hover { background: var(--surface-hover); }
.ocr-coord { font-family: var(--font-mono); font-size: var(--text-micro); color: var(--color-text-subtle); font-variant-numeric: tabular-nums; }
.ocr-line-text { color: var(--color-text); word-break: break-word; }
.ocr-lines .link-button { opacity: 0; transition: opacity var(--motion-base) ease; }
.ocr-lines li:hover .link-button, .ocr-lines li:focus-within .link-button { opacity: 1; }
.ocr-stale { margin-bottom: 0; }
.ocr-retry { margin-left: 10px; }

/* 组件导入区：原生通道在文件拖入悬停时会自动加 file-drop-target-active */
.ocr-import-row { align-items: stretch; }
.ocr-zip-row .ocr-import-drop { flex: 1; }

/* F7 托管版本清单：引擎行内缩进子表（版本 mono + 生效/损坏徽标 + 卸载动作） */
.ocr-hosted {
  display: flex; flex-direction: column; gap: 4px; margin-top: 2px; padding-top: 8px;
  border-top: 1px dashed var(--color-border);
}
.ocr-hosted-row {
  display: flex; align-items: center; gap: 8px; flex-wrap: wrap;
  padding: 4px 8px; border-radius: 6px; background: var(--surface-panel);
  border: 1px solid var(--color-border); font-size: var(--text-xs);
}
.ocr-hosted-effective { border-color: var(--color-primary); }
.ocr-hosted-ver { color: var(--color-text); font-weight: 600; flex-shrink: 0; }
.ocr-hosted-meta { color: var(--color-text-subtle); min-width: 0; overflow-wrap: anywhere; }
.ocr-hosted-uninstall { margin-left: auto; }
.ocr-import-drop {
  flex: 1; min-width: 0; padding: 12px 14px; text-align: center;
  border: 1px dashed var(--color-border); border-radius: var(--radius-control);
  font-size: var(--text-sm); line-height: 1.6; color: var(--color-text-muted); cursor: pointer;
}
.ocr-import-drop:hover, .ocr-import-drop:focus-visible, .ocr-import-drop.file-drop-target-active {
  border-color: var(--color-primary); color: var(--color-primary); background: var(--surface-hover);
}
.ocr-dropzone.file-drop-target-active { border-color: var(--color-primary); }

/* 设置面板 */
.ocr-settings { gap: 8px; }
.ocr-set-row { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; font-size: var(--text-sm); }
.ocr-set-k { color: var(--color-text-subtle); flex-shrink: 0; width: 56px; }
.ocr-set-tag { font-size: var(--text-micro); padding: 1px 7px; border-radius: var(--radius-pill); background: var(--state-information-soft, var(--surface-hover)); color: var(--state-information); }

/* 引擎列表：installed-card 语系的两行卡（纵排；窄屏自然换行不缩字号） */
.ocr-eng-loading { font-size: var(--text-sm); }
.ocr-engine-list { display: flex; flex-direction: column; gap: 8px; }
.ocr-engine-row {
  background: var(--surface-soft); border: 1px solid var(--color-border); border-radius: var(--radius-control);
  padding: 10px 12px; display: flex; flex-direction: column; gap: 6px; min-width: 0;
  transition: border-color var(--motion-base) ease;
}
.ocr-engine-current { border-color: var(--color-primary); background: var(--surface-panel); }
.ocr-engine-sick { border-color: var(--state-warning); }
.ocr-engine-top { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.ocr-engine-name { font-size: var(--text-base); font-weight: 600; color: var(--color-text); }
.ocr-engine-badges { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; margin-left: auto; }
.ocr-engine-meta { display: flex; align-items: baseline; gap: 8px; flex-wrap: wrap; font-size: var(--text-xs); color: var(--color-text-muted); min-width: 0; }
.ocr-engine-ver { color: var(--color-text); flex-shrink: 0; }
.ocr-engine-path { flex: 1 1 200px; min-width: 0; font-size: var(--text-xs); color: var(--color-text-muted); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.ocr-engine-model { font-size: var(--text-xs); color: var(--color-text-subtle); flex-shrink: 0; }
.ocr-engine-err { margin: 0; font-size: var(--text-xs); color: var(--state-warning); overflow-wrap: anywhere; }
.ocr-engine-warn { margin: 0; font-size: var(--text-sm); color: var(--state-warning); overflow-wrap: anywhere; }
.ocr-engine-actions { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; justify-content: flex-end; }
.ocr-set-port { width: 84px; }

/* 热键键位框：只读录入面（点击进录制态），占用/未注册错误红字直出 */
.ocr-hotkey-row { align-items: center; }
.ocr-hotkey-input {
  width: 148px; padding: 4px 8px; text-align: center; cursor: pointer;
  font-size: var(--text-sm); color: var(--color-text);
  background: var(--surface-soft); border: 1px solid var(--color-border); border-radius: var(--radius-control);
}
.ocr-hotkey-input:hover { border-color: var(--color-primary); }
.ocr-hotkey-recording { border-color: var(--color-primary); color: var(--color-primary); background: var(--surface-panel); }
.ocr-hotkey-err { margin: 0; font-size: var(--text-xs); color: var(--state-danger); overflow-wrap: anywhere; }
.ocr-set-follow { display: flex; align-items: center; gap: 6px; color: var(--color-text-muted); margin-left: auto; }
.ocr-set-note { font-size: var(--text-xs); color: var(--color-text-subtle); margin: 0; }

@media (prefers-reduced-motion: reduce) {
  .ocr-dropzone, .ocr-lines .link-button, .ocr-engine-row { transition: none; }
  .ocr-view :deep(.live-pulse) { animation: none; }
}

</style>
