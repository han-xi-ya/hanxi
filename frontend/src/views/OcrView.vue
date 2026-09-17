<script setup lang="ts">
// 「文字识别」：本地 hanxi-ocr 服务（微信 4.0 OCR 引擎封装的私有组件）的
// 托管启停 + 识别工作台。三输入通道（对话框选图 / 拖拽 / 粘贴）汇流为
// ImageRef 后统一转发 path 模式识别；状态以事件为主、5s 轮询兜底。
// 边界：识别能力全部在上游服务，本视图不做任何本地推理（与后端口径一致）。
import { computed, onMounted, ref, watch } from 'vue'
import * as OcrAPI from '../../bindings/hanxi/internal/modules/ocr/ocrservice'
import type { DropResult, ImageRef, OcrOutcome, ServiceState } from '../../bindings/hanxi/internal/modules/ocr/models'
import { useToast } from '../composables/useToast'
import { useClipboard } from '../composables/useClipboard'
import { useWailsEvent } from '../composables/useWailsEvent'
import { usePolling } from '../composables/usePolling'
import { useAsyncAction } from '../composables/useAsyncAction'
import { getErrorMessage } from '../utils/errors'
import { fmtSize } from '../utils/format'
import { toolStateMeta } from '../constants/status'
import PageHeader from '../components/ui/PageHeader.vue'
import UiStatusChip from '../components/ui/UiStatusChip.vue'
import UiBanner from '../components/ui/UiBanner.vue'

const { showToast } = useToast()
const { copy } = useClipboard()

// ---------- 状态 ----------
const state = ref<ServiceState | null>(null) // null = 首帧尚未取得
const showSettings = ref(false)
const portInput = ref('')
const followOnExit = ref(true)
const autoCopy = ref(true)

const image = ref<ImageRef | null>(null)
const outcome = ref<OcrOutcome | null>(null)
const dragOver = ref(false)
const readingFile = ref(false)

const { busy: recBusy, run: runRec } = useAsyncAction()
const { busy: ctrlBusy, run: runCtrl } = useAsyncAction()
const { busy: snipBusy, run: runSnip } = useAsyncAction()

const chip = computed(() => (state.value ? toolStateMeta(state.value.state) : null))
const online = computed(() => state.value?.online === true)
const canRecognize = computed(() => online.value && !!image.value && !recBusy.value)

// 服务掉线但已有上次结果 → stale 提示（保留数据，诚实标记）
const stale = computed(() => !!outcome.value?.ok && !!state.value && !state.value.online)

async function refreshStatus() {
  try {
    state.value = await OcrAPI.GetStatus()
  } catch (e) {
    console.warn('ocr GetStatus failed:', getErrorMessage(e))
  }
}
usePolling(refreshStatus, 5000)
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
async function importViaDialog() {
  try {
    await OcrAPI.ImportServiceExeDialog() // 成功/失败提示与自动启动由事件统一处理，此处只兜程序性错误
  } catch (e) {
    showToast(getErrorMessage(e))
  }
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
    void refreshStatus()
    return
  }
  showToast(r.message || '组件已导入')
  void refreshStatus()
  const st = state.value?.state
  if (st === 'stopped' || st === 'failed') void startService() // 导入即托管：拖进来就能跑
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

async function browseExe() {
  try {
    const path = await OcrAPI.BrowseServiceExeDialog()
    if (!path) return
    await OcrAPI.SetServiceExePath(path)
    showToast('服务路径已更新')
    await refreshStatus()
  } catch (e) {
    showToast(getErrorMessage(e))
  }
}

async function resetExeAuto() {
  try {
    await OcrAPI.SetServiceExePath('')
    showToast('已恢复自动发现（Hanxi 同级 ../hanxi-ocr）')
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
  const file = Array.from(e.clipboardData?.items || [])
    .find((item) => item.kind === 'file')
    ?.getAsFile()
  if (!file) return
  e.preventDefault()
  void acceptFile(file)
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
  const ok = await copy(outcome.value?.text || '')
  showToast(ok ? '已复制全部文本' : '复制失败')
}

async function copyLine(text: string) {
  const ok = await copy(text)
  showToast(ok ? '已复制该行' : '复制失败')
}

onMounted(() => {
  void refreshStatus()
  void loadSettings()
})
</script>

<template>
  <section class="page ocr-view">
    <PageHeader
      title="文字识别"
      subtitle="本地 hanxi-ocr 服务（微信 4.0 识别引擎封装）：拖入、粘贴或选择图片即可识别，全程离线不联网。"
    >
      <template #actions>
        <div class="ocr-head-actions">
          <span v-if="!state" class="ocr-checking live-pulse">探测服务中…</span>
          <UiStatusChip v-else :tone="chip!.tone">{{ chip!.text }}</UiStatusChip>
          <span v-if="online && state!.version" class="ocr-ver" :title="`引擎 ${state!.engine}`">
            {{ state!.version }}<template v-if="!state!.engineRunning"> · 引擎预热中</template>
          </span>
          <button class="btn btn-secondary btn-small" :disabled="snipBusy" title="唤起系统截屏，框选区域即识别（服务未运行时自动拉起）" @click="snipRecognize">
            {{ snipBusy ? '截屏识别中…' : '📷 框选识别' }}
          </button>
          <button class="btn btn-secondary btn-small" :aria-expanded="showSettings" @click="showSettings = !showSettings">
            {{ showSettings ? '收起设置' : '服务设置' }}
          </button>
        </div>
      </template>
    </PageHeader>

    <!-- 服务引导：按状态给下一步，绝不裸报错 -->
    <UiBanner v-if="state && state.state === 'stopped'" tone="info">
      识别服务未运行。把单文件版 hanxi-ocr.exe
      <button class="link-button" @click="showSettings = true">拖入下方导入区</button>
      或解压组件到 <code class="mono">{{ state.exePath || 'Hanxi 同级目录 ../hanxi-ocr' }}</code>，
      再点击「启动服务」；外部自行启动的实例会被自动接管识别。
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

    <div v-if="showSettings" class="ocr-card ocr-settings">
      <div class="ocr-set-row">
        <span class="ocr-set-k">服务程序</span>
        <code class="mono ocr-set-path" :title="state?.exePath || ''">{{ state?.exePath || '未发现（预期 Hanxi 同级 ../hanxi-ocr/hanxi-ocr.exe）' }}</code>
        <span v-if="state?.exeAuto" class="ocr-set-tag">自动发现</span>
        <button class="btn btn-secondary btn-small" @click="browseExe">浏览…</button>
        <button v-if="state && !state.exeAuto" class="link-button" @click="resetExeAuto">恢复自动</button>
      </div>
      <div class="ocr-set-row ocr-import-row">
        <span class="ocr-set-k">导入组件</span>
        <div id="ocr-import-target" class="ocr-import-drop" data-file-drop-target="true"
          role="button" tabindex="0" aria-label="拖入或点击选择 hanxi-ocr.exe 导入"
          @click="importViaDialog" @keydown.enter.prevent="importViaDialog">
          将单文件版 <b>hanxi-ocr.exe</b>（约 48 MB）拖到这里，或点击选择文件；
          校验通过即指向它并自动启动。升级组件：拖入新版覆盖旧路径即可
        </div>
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
.ocr-set-path { flex: 1; min-width: 200px; font-size: var(--text-xs); color: var(--color-text-muted); overflow-wrap: anywhere; }
.ocr-set-tag { font-size: var(--text-micro); padding: 1px 7px; border-radius: var(--radius-pill); background: var(--state-information-soft, var(--surface-hover)); color: var(--state-information); }
.ocr-set-port { width: 84px; }
.ocr-set-follow { display: flex; align-items: center; gap: 6px; color: var(--color-text-muted); margin-left: auto; }
.ocr-set-note { font-size: var(--text-xs); color: var(--color-text-subtle); margin: 0; }

@media (prefers-reduced-motion: reduce) {
  .ocr-dropzone, .ocr-lines .link-button { transition: none; }
  .ocr-view :deep(.live-pulse) { animation: none; }
}
</style>
