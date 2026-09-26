<script setup lang="ts">
// WSL 本机发行版管理控制台（「💻 本机发行版」页签主体，Phase 6 式自 WSLView 拆分）。
// 状态归一列表 + 行内操作 + 「⋯ 更多」下拉（机主 2026-09-26 起面板常驻：点操作项执行但不收）
// + 复选列与批量启停条（启停可多选串行执行，聚合如实回执）
// + 迁移/导出/克隆/瘦身/wsl.conf/详情六种行内编辑器
// 与导出记录抽屉归本组件；busy 分级登记（activeOps）、发行版列表复采（loadInstances）、
// 安装基目录（installDir）留视图——克隆/瘦身进度（cloneProg/compProg）经 v-model 上抛，
// 供跨页签进度横幅文案与页签 label「·运行中」互锁使用。行为逐字迁出，调用序列未动。
import { computed, nextTick, onMounted, ref, watch, onBeforeUnmount } from 'vue'
import * as WSLAPI from '../../../bindings/hanxi/internal/modules/wsl/wslservice'
import type { Report } from '../../../bindings/hanxi/internal/modules/wsl/readiness/models'
import type {
  CloneProgress,
  CompactProgress,
  DistroForensics,
  DistroInstance,
  DistroOpResult,
  ExportRecord,
  WslConfDoc,
} from '../../../bindings/hanxi/internal/modules/wsl/models'
import { useToast } from '../../composables/useToast'
import { useConfirm } from '../../composables/useConfirm'
import { useClipboard } from '../../composables/useClipboard'
import { useWailsEvent } from '../../composables/useWailsEvent'
import { getErrorMessage } from '../../utils/errors'
import { fmtSize } from '../../utils/format'
import UiBanner from '../ui/UiBanner.vue'
import UiClipboardField from '../ui/UiClipboardField.vue'
import UiStatusChip from '../ui/UiStatusChip.vue'
import UiProgressBar from '../ui/UiProgressBar.vue'

const props = defineProps<{
  report: Report | null
  // 呈现判据（视图计算）：体检确认装了本体即开管理面；复采到实例或如实报错时同样呈现。
  ready: boolean
  instances: DistroInstance[]
  instLoading: boolean
  instError: string
  loadInstances: () => Promise<void>
  // busy 分级机制（activeOps 单一来源在视图，本层只读判定 + 上闸下闸转发）
  busyAny: boolean
  globalBusy: boolean
  busyWith: (op: string) => boolean
  busyDistro: (name: string) => boolean
  rowBusy: (name: string) => boolean
  startOp: (op: string) => void
  finishOp: (op: string) => void
  // 克隆/瘦身进度：视图状态 v-model 双向（跨页签进度可见性互锁）
  cloneProg: CloneProgress | null
  compProg: CompactProgress | null
  cloneBusy: boolean
  compTerm: boolean
  cloneStageText: Record<string, string>
  compactStageText: Record<string, string>
  // 落位与目录选择共享件（视图单一来源）
  installDir: string
  previewSubdir: (base: string, id: string) => string
  pickFolderInto: (fill: (p: string) => void, title: string) => Promise<void>
  modeWord: (m?: string | null) => string
}>()

const emit = defineEmits<{
  'update:cloneProg': [value: CloneProgress | null]
  'update:compProg': [value: CompactProgress | null]
}>()

const { showToast } = useToast()
const { confirm } = useConfirm()
const { copy } = useClipboard()

// 列表与提权通道解耦：独立 loadInstances（视图通道），操作后复采（wsl 状态有滞后，
// 复采即真相——不在前端乐观编状态）。busy 分级：全局互斥类在飞才全站关门；
// 单发行版写操作只锁本行；只读（终端/文件/详情/刷新）任何状态下可用——
// 后端另有每发行版单飞闸与迁移全局闸兜并发，前端不做过度锁。
const movingName = ref('') // 迁移内联表单展开中的发行版（同时只开一行）
const moveTarget = ref('')
// 导出：内联格式选择（同时只开一行），本会话导出工件全量登记展示。
const exportingName = ref('')
const exportFormat = ref<'gz' | 'tar'>('gz')
const exportRecords = ref<ExportRecord[]>([])
// 导出/瘦身成功后自动展开「本会话导出记录」抽屉——工件即刻可见，不用手动翻。
const exportLogOpen = ref(false)

async function loadExportRecords() {
  try {
    exportRecords.value = (await WSLAPI.ListDistroExports()) ?? []
  } catch {
    // 登记面是辅助信息：失败不打扰主列表，保留已有视图
  }
}

// runDistroOp：发行版操作编排（确认链 + busy 分级闸门 + 复采收口）。
// 与视图 runOp 的差别：提权与否由 uac 如实声明（terminate/设默认/导出是用户态命令，
// 谎报"会弹 UAC"反而制造困惑）；完成后只复采列表不重跑全量体检。
// kind:'read'（终端/文件）绕过闸门任何状态可用；写操作仅在"全局互斥在飞 / 本操作重入"时拒绝。
async function runDistroOp(opts: {
  name: string
  title: string
  desc: string
  tone?: 'default' | 'warning' | 'danger'
  confirmLabel?: string
  uac?: boolean
  details?: Array<{ label: string; value: string }>
  confirm?: boolean
  kind?: 'read' | 'write'
  invoke: () => PromiseLike<DistroOpResult>
  then?: (out: DistroOpResult) => void
}) {
  if (opts.kind !== 'read' && (props.globalBusy || props.busyWith(opts.name))) return
  if (opts.confirm !== false) {
    const accepted = await confirm({
      title: opts.title,
      description: opts.desc + (opts.uac
        ? '\n\n该操作需管理员权限：随后会弹出系统 UAC 授权窗口，请在窗口中确认继续。'
        : '\n\n该操作以普通权限执行，不会弹出 UAC。'),
      tone: opts.tone ?? 'warning',
      details: opts.details ?? [],
      ...(opts.confirmLabel ? { confirmLabel: opts.confirmLabel } : {}),
    })
    if (!accepted) return
  }
  props.startOp(opts.name)
  try {
    const out = await opts.invoke()
    showToast(out?.message || '操作已完成')
    opts.then?.(out)
  } catch (e) {
    showToast(`${opts.title}失败: ${getErrorMessage(e)}`, { duration: 8000 })
  } finally {
    props.finishOp(opts.name)
    await props.loadInstances() // 成败都复采：命令可能已部分生效，状态以复采为准
  }
}

const openTerminal = (d: DistroInstance) => runDistroOp({
  name: `terminal:${d.name}`, kind: 'read', title: `启动 ${d.name}`, confirm: false,
  desc: `唤起系统默认终端进入 ${d.name}。说明：WSL 在发行版内无前台进程时会自动停机，"运行中"是会话驱动的自然形态，本工具不做后台保活。`,
  invoke: () => WSLAPI.OpenTerminal(d.name),
})
const terminateDistro = (d: DistroInstance) => runDistroOp({
  name: `stop:${d.name}`, title: `关机 ${d.name}？`, tone: 'default',
  desc: '执行 wsl --terminate——等同拔掉这一个发行版的虚拟机电源（WSL 不提供单发行版的优雅关机）：未保存的前台进程即刻终止，数据盘无损；要停全部请到就绪检测页「🌑 停止全部」。下次访问（唤终端/\\wsl$ 路径）自动再开机。',
  invoke: () => WSLAPI.TerminateDistro(d.name),
})
// 重启（对齐 wsl-dashboard 语义但刻意不做保活）：后端 terminate→确认已停→拉起探针。
const restartDistro = (d: DistroInstance) => runDistroOp({
  name: `restart:${d.name}`, title: `重启 ${d.name}？`, tone: d.running ? 'warning' : 'default',
  desc: (d.running ? '先停止再启动验证：现有终端/前台会话会被中断（数据无损）。' : '当前已停止——重启即拉起并验证可启动。')
    + '之后不进入终端的话，发行版空闲片刻会自动回落为「已停止」，本工具不做后台保活（平台常态）。',
  invoke: () => WSLAPI.RestartDistro(d.name),
})
// 文件管理器：只读外呼免确认（同唤终端）；停止的发行版会被后端顺手拉起。
const openFolder = (d: DistroInstance) => runDistroOp({
  name: `folder:${d.name}`, kind: 'read', title: `打开 ${d.name} 的文件`, confirm: false,
  desc: `在资源管理器打开 \\\\wsl$\\${d.name} 浏览发行版文件系统${d.running ? '' : '（当前已停止，会先拉起发行版）'}。只读浏览入口，不改动任何数据。`,
  invoke: () => WSLAPI.OpenDistroFolder(d.name),
})
const setDefaultDistro = (d: DistroInstance) => runDistroOp({
  name: `setdefault:${d.name}`, title: `把 ${d.name} 设为默认发行版？`, tone: 'default',
  desc: '执行 wsl --set-default：此后不带 -d 的 wsl 命令与控制台默认进入该发行版。',
  invoke: () => WSLAPI.SetDefaultDistro(d.name),
})
const unregisterDistro = (d: DistroInstance) => runDistroOp({
  name: `unreg:${d.name}`, title: `删除发行版 ${d.name}？`, tone: 'danger', confirmLabel: '🗑 删除数据并移除',
  details: [
    { label: '发行版', value: d.name },
    { label: '磁盘占用', value: d.sizeBytes ? fmtSize(d.sizeBytes) : '未知' },
    { label: '数据位置', value: d.basePath || '未知' },
  ],
  desc: '执行 wsl --unregister：先停止该发行版，随后其数据盘（VHDX）连同全部文件被系统删除——不可恢复。'
    + '如需留档，请先「导出」为 tar 再删。商店安装的发行版其启动器将被一并自动卸载'
    + '（仅当启动器被其它发行版共用、或卸载失败时，回执会点名让你到「设置→应用」手动处理）。',
  invoke: () => WSLAPI.UnregisterDistro(d.name),
})
// ---------- 复选批量启停（机主 2026-09-26："启动和停止可以复选，这样批量操作体验会好很多"） ----------
// 零新增后端导出：串行循环复用单行的 TerminateDistro / RestartDistro 调用，逐行以同款
// `stop:<名>` / `restart:<名>` 名目登记 busy（常驻进度横幅与行级闸门天然生效）。
// 启动语义如实取「重启钮对停止实例的拉起验证」路径（wsl 无显式 start 命令，RestartDistro
// 对已停止实例即"拉起并验证可启动"，空闲后自动回落停止是平台常态，与单行钮口径一致）。
// 聚合回执不谎报：成功/失败分桶点名，勾选中状态不符的项先经确认框预告再如实归入跳过数。
const selected = ref<Set<string>>(new Set())
const batchBusy = ref(false)
function toggleSelect(name: string, on: boolean) {
  const next = new Set(selected.value)
  if (on) next.add(name)
  else next.delete(name)
  selected.value = next
}
const allSelected = computed(() =>
  props.instances.length > 0 && props.instances.every(i => selected.value.has(i.name)))
const someSelected = computed(() => selected.value.size > 0 && !allSelected.value)
const selRunning = computed(() => props.instances.filter(i => selected.value.has(i.name) && i.running))
const selStopped = computed(() => props.instances.filter(i => selected.value.has(i.name) && !i.running))
function toggleSelectAll() {
  selected.value = allSelected.value ? new Set() : new Set(props.instances.map(i => i.name))
}
function clearSelection() { selected.value = new Set() }
// 复采后消失的发行版同步出账：选择集不得留幽灵名虚增"已选 N"。
watch(() => props.instances, (list) => {
  if (!selected.value.size) return
  const names = new Set(list.map(i => i.name))
  selected.value = new Set([...selected.value].filter(n => names.has(n)))
})
async function runBatch(kind: 'start' | 'stop') {
  if (batchBusy.value || props.globalBusy) return
  const picked = kind === 'stop' ? selRunning.value : selStopped.value
  // 本行已有其他写操作在飞（导出/克隆/编辑等）的不进队列：后端单飞闸会拒，前端先行避让并点名。
  const targets = picked.filter(d => !props.busyDistro(d.name))
  const inflight = picked.filter(d => props.busyDistro(d.name))
  const skipped = (kind === 'stop' ? selStopped.value : selRunning.value).concat(inflight)
  if (!targets.length) return
  const word = kind === 'stop' ? '停止' : '启动'
  const accepted = await confirm({
    title: `批量${word} ${targets.length} 个发行版？`,
    tone: kind === 'stop' ? 'warning' : 'default',
    description: kind === 'stop'
      ? `逐个执行 wsl --terminate——等同挨个拔掉这些发行版的虚拟机电源（数据盘无损，下次访问自动再开机）；串行执行，失败项点名。`
      : `逐个执行「拉起验证」（同单行「🔄 重启」对停止实例的路径）：拉起后无前台会话时稍后自动回落「已停止」，本工具不做后台保活；串行执行，失败项点名。`
      + '\n\n该操作以普通权限执行，不会弹出 UAC。',
    details: [
      { label: `批量${word}`, value: targets.map(d => d.name).join('、') },
      ...(skipped.length ? [{ label: '自动跳过', value: skipped.map(d => d.name).join('、') }] : []),
    ],
  })
  if (!accepted) return
  batchBusy.value = true
  const okNames: string[] = []
  const fails: string[] = []
  try {
    for (const d of targets) {
      const op = `${kind === 'stop' ? 'stop' : 'restart'}:${d.name}`
      props.startOp(op)
      try {
        const out = await (kind === 'stop' ? WSLAPI.TerminateDistro(d.name) : WSLAPI.RestartDistro(d.name))
        if (out && out.success === false) fails.push(`${d.name}：${out.message || '后端未报成功'}`)
        else okNames.push(d.name)
      } catch (e) {
        fails.push(`${d.name}：${getErrorMessage(e)}`)
      } finally {
        props.finishOp(op)
      }
    }
  } finally {
    batchBusy.value = false
    selected.value = new Set() // 成败都以复采为真相：选择集清空防误按
    await props.loadInstances()
  }
  const tail = skipped.length ? `，跳过 ${skipped.length}（状态不符或在飞）：${skipped.map(d => d.name).join('、')}` : ''
  if (fails.length) {
    showToast(`批量${word}完成：成功 ${okNames.length}，失败 ${fails.length}${tail} —— ${fails.join('；')}`, { duration: 10000 })
  } else {
    showToast(`批量${word}完成：${okNames.length} 个全部成功${tail}`)
  }
}

// 导出两段式：点击展开内联格式选择，「开始导出」才进确认链（对齐迁移的表单先行范式）。
function startExport(d: DistroInstance) {
  exportingName.value = d.name
  exportFormat.value = 'gz'
}
function cancelExport() {
  exportingName.value = ''
}
const confirmExport = (d: DistroInstance) => runDistroOp({
  name: `export:${d.name}`, title: `导出 ${d.name}？`, tone: 'default',
  details: [{ label: '导出格式', value: exportFormat.value === 'gz' ? 'tar.gz（压缩）' : 'tar（未压缩）' }],
  desc: exportFormat.value === 'gz'
    ? '执行 wsl --export --format tar.gz：整个文件系统压缩导出为 tar.gz（需 WSL 2.4.4+），'
      + '落到「下载\\WSL 导出」文件夹（文件名后端拼装、同名不覆盖）。大发行版可达数十 GB 耗时数分钟，期间请勿退出。'
    : '执行 wsl --export：整个文件系统导出为未压缩 tar（速度更快、兼容老版本 WSL，但体积大），'
      + '落到「下载\\WSL 导出」文件夹（文件名后端拼装、同名不覆盖）。大发行版可达数十 GB 耗时数分钟，期间请勿退出。',
  invoke: () => WSLAPI.ExportDistro(d.name, exportFormat.value === 'gz'),
  then: (out) => {
    exportingName.value = ''
    if (out?.success) {
      void loadExportRecords()
      exportLogOpen.value = true // 成功即展开导出记录：工件即刻可见
    }
  },
})

// 迁移：内联表单先行，确认时把目标路径与"会被连带打停的运行中实例"写进确认框。
function startMove(d: DistroInstance) {
  movingName.value = d.name
  // 预填「安装目录\<发行版名>」供修改（同克隆落位规则）；安装目录留空则不猜。
  moveTarget.value = props.previewSubdir(props.installDir, d.name)
}
function cancelMove() {
  movingName.value = ''
  moveTarget.value = ''
}
const moveRunningOthers = computed(() =>
  props.instances.filter(i => i.running && i.name !== movingName.value))
const confirmMove = (d: DistroInstance) => runDistroOp({
  name: `move:${d.name}`, title: `迁移 ${d.name} 到新位置？`, tone: 'warning', uac: true,
  details: [
    { label: '当前安装位置', value: d.basePath || '未知' },
    { label: '目标目录', value: moveTarget.value.trim() },
  ],
  desc: '执行 wsl --manage --move：先把整个 WSL 子系统 wsl --shutdown'
    + (moveRunningOthers.value.length ? `（将连带打停运行中的：${moveRunningOthers.value.map(i => i.name).join('、')}）` : '')
    + '，再移动数据盘到目标目录（须为空目录或不存在的路径）。迁移期间所有发行版不可用。',
  invoke: () => WSLAPI.MoveDistro(d.name, moveTarget.value.trim()),
  then: () => { movingName.value = ''; moveTarget.value = '' },
})

async function revealExport(id: string) {
  try {
    await WSLAPI.RevealDistroExport(id)
  } catch (e) {
    showToast(`打开位置失败: ${getErrorMessage(e)}`)
  }
}

// ---------- 单发行版详情（只读抽屉；历史名"取证"，内部符号沿用 forensics） ----------
// 后端红线：停止的发行版绝不进 guest（wsl -d 会顺手拉起）——抽屉里以 Notes 如实说明缺项。
interface ForensicsState { loading: boolean; error: string; data: DistroForensics | null }
const forensicsName = ref('')
const forensics = ref<Record<string, ForensicsState>>({})
const forensicsOf = computed(() => (forensicsName.value ? forensics.value[forensicsName.value] : undefined))

function toggleForensics(d: DistroInstance) {
  forensicsName.value = forensicsName.value === d.name ? '' : d.name
  if (forensicsName.value && !forensics.value[d.name]) void loadForensics(d.name)
}

async function loadForensics(name: string) {
  const prev = forensics.value[name]
  forensics.value = { ...forensics.value, [name]: { loading: true, error: '', data: prev?.data ?? null } }
  try {
    const data = await WSLAPI.GetDistroForensics(name)
    forensics.value = { ...forensics.value, [name]: { loading: false, error: '', data } }
  } catch (e) {
    forensics.value = { ...forensics.value, [name]: { loading: false, error: getErrorMessage(e), data: prev?.data ?? null } }
  }
}

// df 值以 MB 计，根盘常见数十 GB——fmtSize 上限只到 MB，这里升一档到 GB 呈现。
function mb(v: number): string {
  return v >= 1024 ? `${(v / 1024).toFixed(1)} GB` : `${v} MB`
}

// ---------- 克隆（异步事件驱动）与导入 ----------
// 克隆受理后转后台：wsl:clone 事件推进度，busy 一直挂到终态。
// 克隆是"单发行版重操作"：源发行版行关门，其他发行版照常可操作
//（数十 GB 拷完前不该让全页灰死；后端另有双名单飞闸兜底）。
// cloneProg 状态住视图（跨页签进度互锁），本层经 v-model 读写。
const cloneSrc = ref('')
const cloneNewName = ref('')
const cloneTarget = ref('')

// 请求后端取消在飞克隆：拷贝段半成品自清，挂载段取消走失败清理路径（保留拷贝盘）。
async function requestCancelClone() {
  if (!props.cloneProg) return
  try {
    const out = await WSLAPI.CancelClone(props.cloneProg.source)
    showToast(out.message || '已请求取消')
  } catch (e) {
    showToast(`取消失败: ${getErrorMessage(e)}`)
  }
}

function startClone(d: DistroInstance) {
  cloneSrc.value = d.name
  cloneNewName.value = `${d.name}-Copy`
  // 目标预填「安装目录\<新实例名>」（与后端 underDir 同规则），可任意改动；
  // 安装目录留空时不猜，留占位符引导手填或 📁 选择。
  cloneTarget.value = props.previewSubdir(props.installDir, cloneNewName.value)
  emit('update:cloneProg', null)
}
function cancelClone() {
  if (props.cloneBusy) return // 在飞不收编：进度可见直到终态
  cloneSrc.value = ''
  emit('update:cloneProg', null)
}

async function submitClone(d: DistroInstance) {
  if (props.globalBusy || props.busyWith(`clone:${d.name}`)) return
  const newName = cloneNewName.value.trim()
  const target = cloneTarget.value.trim()
  const accepted = await confirm({
    title: `克隆 ${d.name} → ${newName}？`,
    tone: 'warning',
    description: `克隆会先关机「${d.name}」，再把数据盘整份复制到目标目录，副本以「${newName}」注册为新发行版（原发行版不受影响）。`
      + '数十 GB 时耗时数分钟，进度直接显示在本行。需要 WSL 2.7.3 以上；版本不足请改走「导出 → 导入」。'
      + '\n\n该操作以普通权限执行，不会弹出 UAC。',
    details: [
      { label: '新发行版名', value: newName },
      { label: '目标目录', value: target },
    ],
  })
  if (!accepted) return
  props.startOp(`clone:${d.name}`)
  emit('update:cloneProg', { source: d.name, target: newName, stage: 'copying', done: 0, total: 0 })
  try {
    const out = (await WSLAPI.CloneDistro(d.name, newName, target)) as { message?: string }
    showToast(out.message || '已开始克隆')
  } catch (e) {
    props.finishOp(`clone:${d.name}`)
    // 失败行内保活：错误写进进度态，本行红条驻留可回看——toast 只是补充，不再是唯一线索
    emit('update:cloneProg', { source: d.name, target: newName, stage: 'error', done: 0, total: 0, error: getErrorMessage(e) })
    showToast(`克隆失败: ${getErrorMessage(e)}`, { duration: 8000 })
    await props.loadInstances()
  }
}

useWailsEvent<CloneProgress>('wsl:clone', (p) => {
  if (!p) return
  emit('update:cloneProg', p)
  if (p.stage === 'done') {
    props.finishOp(`clone:${p.source}`)
    cloneSrc.value = ''
    emit('update:cloneProg', null)
    showToast(p.message || '克隆完成')
    void props.loadInstances()
  } else if (p.stage === 'error') {
    props.finishOp(`clone:${p.source}`) // 错误留在表单里可见，不吞
    void props.loadInstances()
  }
})

// ---------- /etc/wsl.conf 编辑器 ----------
// 读时若发行版已停止会被 wsl -d 顺手拉起（点「配置」即默认接受）；
// 写回的语法闸门/引用校验/写前备份全在后端，前端只呈现实情。
const confName = ref('')
const confDoc = ref<WslConfDoc | null>(null)
const confText = ref('')
const confLoading = ref(false)
const confError = ref('')

function openConf(d: DistroInstance) {
  if (confName.value === d.name) {
    closeConf()
    return
  }
  confName.value = d.name
  confDoc.value = null
  confText.value = ''
  void loadConf(d.name)
}
function closeConf() {
  if (confLoading.value || (confName.value && props.busyDistro(confName.value))) return
  confName.value = ''
  confDoc.value = null
  confText.value = ''
  confError.value = ''
}
async function loadConf(name: string) {
  confLoading.value = true
  confError.value = ''
  try {
    const doc = await WSLAPI.GetWslConf(name)
    confDoc.value = doc
    confText.value = doc?.text ?? ''
  } catch (e) {
    confError.value = getErrorMessage(e)
  } finally {
    confLoading.value = false
  }
}
const saveConf = (d: DistroInstance) => runDistroOp({
  name: `confsave:${d.name}`, title: `写回 ${d.name} 的 /etc/wsl.conf？`, tone: 'warning',
  desc: '后端三步防线：语法校验 → 默认用户存在性求证 → root 备份原文件后整文件写回并复验。'
    + '\nwsl.conf 只在发行版启动时读取——保存后需「⏹ 关机」（或「🔄 重启」）该发行版再进入才生效。',
  details: [{ label: '文件大小', value: `${new Blob([confText.value]).size} B` }],
  invoke: () => WSLAPI.SaveWslConf(d.name, confText.value),
  then: (out) => { if (out?.success) void offerTerminateAfterConf(d) },
})

// 保存成功即给"让配置生效"的下一步：终止后下次进入自然读到新 wsl.conf。
async function offerTerminateAfterConf(d: DistroInstance) {
  const yes = await confirm({
    title: `关机 ${d.name} 使新配置生效？`,
    tone: 'default',
    description: 'wsl.conf 只在发行版启动时读取。现在关机（数据无损，下次进入自动再开机）即可立刻生效；也可以稍后自行点「⏹ 关机」或「🔄 重启」。',
  })
  if (!yes) return
  try {
    const r = await WSLAPI.TerminateDistro(d.name)
    showToast(r?.message || '已关机，配置已生效')
  } catch (e) {
    showToast(`关机失败: ${getErrorMessage(e)}`)
  }
  await props.loadInstances()
}

// ---------- 磁盘瘦身（三级工作流，异步事件驱动） ----------
const compSrc = ref('')
const compBackupDir = ref('') // 留空=默认「下载\WSL 导出」；大盘备份常被 C 盘容量卡住
const compCancelable = computed(() => !!props.compProg && ['backup', 'trim', 'waiting'].includes(props.compProg.stage))

async function cancelCompact() {
  try {
    const out = await WSLAPI.CancelCompact()
    showToast(out.message || '已请求取消')
  } catch (e) {
    showToast(`取消被拒: ${getErrorMessage(e)}`) // 进入动盘段后如实拒绝，不假装停了
  }
}

function openCompact(d: DistroInstance) {
  compSrc.value = d.name
  emit('update:compProg', null)
  compBackupDir.value = ''
}
function closeCompact() {
  if (compSrc.value && !props.compTerm && props.busyAny) return // 在飞不收编
  compSrc.value = ''
  emit('update:compProg', null)
}

async function submitCompact(d: DistroInstance) {
  if (props.globalBusy || props.busyWith(`compact:${d.name}`)) return
  const accepted = await confirm({
    title: `为 ${d.name} 瘦身数据盘？`,
    tone: 'danger',
    description: '这是对数据盘的手术，全程分四步：'
      + '\n① 先强制做一份全量 tar 备份（需要约等于数据盘已用大小的额外空间）；'
      + '\n② 在发行版内做 fstrim 归还已删块，然后关闭发行版；'
      + '\n③ 用 Optimize-VHD 压缩数据盘（需要 Hyper-V 模块，会提权；省得不够就不做这步）；'
      + '\n④ 仍省得不够时，注销旧实例、从刚才的备份重建一个新实例（位置会换到新的旁路目录）。'
      + '\n\n任何一步失败都会如实中止并点名备份文件位置；完成后备份仍保留，确认无误后可自行删除。'
      + '\n数十 GB 盘耗时可达数十分钟，期间请勿退出 Hanxi。',
    details: [
      { label: '发行版', value: d.name },
      { label: '当前占用', value: d.sizeBytes ? fmtSize(d.sizeBytes) : '未知' },
    ],
  })
  if (!accepted) return
  props.startOp(`compact:${d.name}`)
  emit('update:compProg', { name: d.name, stage: 'backup', beforeMB: 0, afterMB: 0 })
  try {
    const out = (await WSLAPI.CompactDistro(d.name, compBackupDir.value.trim())) as { message?: string }
    showToast(out.message || '已开始瘦身')
  } catch (e) {
    props.finishOp(`compact:${d.name}`)
    // 失败行内保活：错误进进度态，红条驻留本行（模板有 stage==='error' 分支）
    emit('update:compProg', { name: d.name, stage: 'error', error: getErrorMessage(e), beforeMB: 0, afterMB: 0 })
    showToast(`瘦身未受理: ${getErrorMessage(e)}`, { duration: 8000 })
  }
}

useWailsEvent<CompactProgress>('wsl:compact', (p) => {
  if (!p) return
  emit('update:compProg', p)
  if (p.stage === 'done') {
    props.finishOp(`compact:${p.name}`)
    showToast(p.message || '瘦身完成')
    void props.loadInstances()
    void loadExportRecords() // 备份 tar 也在导出登记里
    exportLogOpen.value = true // 备份工件即刻可见
  } else if (p.stage === 'error') {
    props.finishOp(`compact:${p.name}`) // 错误留在表单里可见（含备份位置）
    void props.loadInstances()
  }
})

// ---------- 行级「⋯ 更多」下拉（Teleport 到 body + fixed 坐标；外点/Esc/滚动/复采关闭） ----------
// 遮挡根因（机主实跑反馈）：旧面板 absolute 挂在行内 .row-menu 下，被两层祖先裁剪——
// .table-container（overflow-x:auto，CSS 规范下 overflow-y 同步失效为 auto，纵向照样裁）
// 与应用主内容区 .content-area（overflow-y:auto）。表格下半屏的行点开菜单即被切掉。
// 修法：面板 Teleport 出 DOM 链直挂 body，以触发钮视口 rect 算 fixed 坐标；下方空间
// 不够且上方放得下则向上翻转；左右向视口内钳制；打开期间任何滚动/缩放、列表复采
// （行位移动、旧坐标失效）都直接收合。零依赖，不引 popper。
// 同时只开一行；菜单项动作**执行但面板常驻**（机主 2026-09-26："点击后它不关闭，
// 因为我有可能需要点多次操作"）——收面板只发生在：外点 / 再点触发钮 / Esc /
// 滚动缩放 / 列表复采（行位移旧坐标失信）五个既有通道；经这些动作间接打开确认框后
// 复采或点空白，面板亦随上述通道自然收合，无专门"动作后自杀"逻辑。Esc 走全局 keydown，
// 外点走 document pointerdown（命中 .row-menu 触发链或 .row-menu-panel 面板本体则忽略
// ——面板已不在 .row-menu DOM 子树内，须单独认账，否则面板内点击在 pointerdown 阶段
// 就被收掉、click 永远到不了菜单项）。监听仅在面板打开期间挂载，卸载时摘除。
const rowMenuName = ref('')
const rowMenuUp = ref(false)
const rowMenuStyle = ref<Record<string, string>>({})
const rowMenuPanelEl = ref<HTMLElement | null>(null)
const MENU_GAP = 4
const MENU_EDGE = 8

const rowMenuDistro = computed(() => props.instances.find(i => i.name === rowMenuName.value) ?? null)

// 面板项单一来源：模板 v-for 消费，行为与原八个手写钮逐字一致。
const rowMenuItems = computed(() => {
  const d = rowMenuDistro.value
  if (!d) return []
  return [
    { key: 'folder', label: '📂 文件', disabled: false, active: false,
      title: '资源管理器打开 \\\\wsl$\\<发行版> 浏览文件系统（停止时会被顺手拉起；只读入口免确认）',
      run: () => openFolder(d) },
    { key: 'default', label: '⭐ 设默认', disabled: props.rowBusy(d.name) || d.default, active: false,
      title: d.default ? '已是默认发行版' : 'wsl --set-default：不带 -d 的 wsl 命令与控制台默认进入它',
      run: () => setDefaultDistro(d) },
    { key: 'export', label: '📤 导出', disabled: props.rowBusy(d.name) || (!!exportingName.value && exportingName.value !== d.name), active: false,
      title: 'wsl --export：选择格式（tar.gz 压缩 / tar 未压缩）导出到「下载」文件夹，可 long-running',
      run: () => startExport(d) },
    { key: 'move', label: '🧭 迁移', disabled: props.rowBusy(d.name) || (!!movingName.value && movingName.value !== d.name), active: false,
      title: 'wsl --manage --move：迁移数据盘到其他盘（UAC 提权，会先停机全部 WSL）',
      run: () => startMove(d) },
    { key: 'clone', label: '🧬 克隆', disabled: props.rowBusy(d.name) || (!!cloneSrc.value && cloneSrc.value !== d.name), active: false,
      title: '克隆：关机源发行版 → 整盘复制数据盘 → 副本就地挂为新发行版（原实例不动；需 WSL 2.7.3+，免 UAC）',
      run: () => startClone(d) },
    { key: 'compact', label: '🗜 瘦身', disabled: props.rowBusy(d.name) || (!!compSrc.value && compSrc.value !== d.name), active: false,
      title: '数据盘瘦身：备份→fstrim→Optimize-VHD 压缩→不足则从备份注销重建（会换落位目录）',
      run: () => openCompact(d) },
    { key: 'forensics', label: '📋 详情', disabled: false, active: forensicsName.value === d.name,
      title: '只读详情：VHDX 逻辑/实占与稀疏、根盘用量、IPv4、网络模式（停止时不进 guest，以免顺手启动它）',
      run: () => toggleForensics(d) },
    { key: 'conf', label: '⚙ wsl.conf', disabled: props.rowBusy(d.name) || (!!confName.value && confName.value !== d.name), active: confName.value === d.name,
      title: '编辑 /etc/wsl.conf（systemd/automount/默认用户等）：读时会启动发行版；写回有语法闸门 + 引用校验 + 写前备份',
      run: () => openConf(d) },
  ]
})

async function toggleRowMenu(name: string, e: MouseEvent) {
  if (rowMenuName.value === name) {
    closeRowMenu()
    return
  }
  const trigger = e.currentTarget as HTMLElement
  if (!rowMenuName.value) {
    rowMenuUp.value = false
    // 先离屏隐藏挂载：量得真实尺寸前不闪错位（nextTick 是微任务，浏览器尚未绘制）
    rowMenuStyle.value = { left: '-9999px', top: '0px', visibility: 'hidden' }
  }
  rowMenuName.value = name
  await nextTick()
  placeRowMenu(trigger)
}

function placeRowMenu(trigger: HTMLElement) {
  const panel = rowMenuPanelEl.value
  if (!panel || !rowMenuName.value) return
  const rect = trigger.getBoundingClientRect()
  const ph = panel.offsetHeight
  const pw = panel.offsetWidth
  const vw = window.innerWidth
  const vh = window.innerHeight
  const up = vh - rect.bottom < ph + MENU_GAP && rect.top > ph + MENU_GAP
  const left = Math.max(MENU_EDGE, Math.min(rect.right - pw, vw - pw - MENU_EDGE))
  rowMenuUp.value = up
  rowMenuStyle.value = up
    ? { left: `${left}px`, bottom: `${vh - rect.top + MENU_GAP}px`, visibility: 'visible' }
    : { left: `${left}px`, top: `${rect.bottom + MENU_GAP}px`, visibility: 'visible' }
}

function closeRowMenu() { rowMenuName.value = '' }
// 菜单项点击：执行动作但**不关面板**（常驻语义见上方注释块）。会复采列表的动作
// （文件/设默认，及后续走完的导出等）经 instances watch 自然收——"数据复采才关"；
// 展开表单类动作（导出/迁移/克隆/瘦身/详情/wsl.conf）面板继续挂着，供连点多项。
function rowMenuAction(fn: () => void) { fn() }
function onRowMenuDocDown(e: Event) {
  const t = e.target as HTMLElement | null
  if (rowMenuName.value && !t?.closest?.('.row-menu') && !t?.closest?.('.row-menu-panel')) closeRowMenu()
}
function onRowMenuDocKey(e: KeyboardEvent) {
  if (e.key === 'Escape') closeRowMenu()
}
// 锚点失效即收：滚动（capture 收全部祖先容器）与缩放后 fixed 坐标不再对准触发钮。
function onRowMenuAnchorLost() { closeRowMenu() }
watch(rowMenuName, (open) => {
  if (open) {
    document.addEventListener('pointerdown', onRowMenuDocDown)
    document.addEventListener('keydown', onRowMenuDocKey)
    window.addEventListener('scroll', onRowMenuAnchorLost, true)
    window.addEventListener('resize', onRowMenuAnchorLost)
  } else {
    document.removeEventListener('pointerdown', onRowMenuDocDown)
    document.removeEventListener('keydown', onRowMenuDocKey)
    window.removeEventListener('scroll', onRowMenuAnchorLost, true)
    window.removeEventListener('resize', onRowMenuAnchorLost)
  }
})
// 列表复采后行可能位移/增删，旧坐标不再可信：收。
watch(() => props.instances, closeRowMenu)

onMounted(() => {
  loadExportRecords()
})

onBeforeUnmount(() => {
  document.removeEventListener('pointerdown', onRowMenuDocDown)
  document.removeEventListener('keydown', onRowMenuDocKey)
  window.removeEventListener('scroll', onRowMenuAnchorLost, true)
  window.removeEventListener('resize', onRowMenuAnchorLost)
})
</script>

<template>
  <template v-if="ready">
    <div class="section-title distro-head">
      <h3>本机发行版 ({{ instances.length }})</h3>
      <div class="btn-group distro-head-actions">
        <button class="btn btn-secondary btn-small" :disabled="instLoading" @click="loadInstances">
          {{ instLoading ? '刷新中…' : '↻ 刷新列表' }}
        </button>
      </div>
    </div>
    <!-- 批量启停条：有勾选才浮现（常态零占位）；启动/停止各自只作用于勾选集中状态相符的子集，
         在飞（batchBusy/globalBusy）一律禁按防重入 -->
    <div v-if="selected.size" class="batch-bar" role="group" aria-label="批量操作">
      <span class="batch-count">已选 <b>{{ selected.size }}</b> 项：</span>
      <button class="btn btn-secondary btn-small" :disabled="batchBusy || globalBusy || !selStopped.length"
        :title="`对勾选中已停止的 ${selStopped.length} 个逐个拉起并验证可启动（同单行「🔄 重启」路径；串行执行，失败点名）`"
        @click="runBatch('start')">▶ 批量启动（{{ selStopped.length }}）</button>
      <button class="btn btn-secondary btn-small" :disabled="batchBusy || globalBusy || !selRunning.length"
        :title="`对勾选中运行中的 ${selRunning.length} 个逐个 wsl --terminate（数据盘无损，下次访问自动再开机；停全部请到就绪检测页）`"
        @click="runBatch('stop')">⏹ 批量停止（{{ selRunning.length }}）</button>
      <button class="link-button" :disabled="batchBusy" @click="clearSelection">清除选择</button>
    </div>
    <div v-if="instError" class="error-box">{{ instError }}
      <button class="btn btn-secondary btn-small retry-inline" @click="loadInstances">↻ 重试</button>
    </div>
    <div v-else-if="instLoading && !instances.length" class="hint-line">正在经本机 wsl.exe 读取发行版现状…</div>
    <div v-else-if="!instances.length" class="empty-state">
      <p>WSL 已安装但还没有发行版 —— 到「➕ 添加实例」页挑一个来源（官方商店 / rootfs / VHDX 盘）。</p>
    </div>
    <div v-else class="table-container">
      <table class="tbl">
        <thead>
          <tr>
            <!-- 复选列：表头全选钮带半态（indeterminate），仅参与批量启停，不承载其他写操作 -->
            <th class="check-th" style="width: 36px;">
              <input type="checkbox" :checked="allSelected" :indeterminate="someSelected"
                :disabled="batchBusy" aria-label="全选发行版" @change="toggleSelectAll" />
            </th>
            <th style="width: 16%;">发行版</th>
            <th style="width: 10%;">状态</th>
            <th class="col-ver" style="width: 8%;">WSL 版本</th>
            <th style="width: 10%;">磁盘占用</th>
            <th>操作</th>
          </tr>
        </thead>
        <tbody>
          <template v-for="d in instances" :key="d.name">
            <tr :class="{ 'row-selected': selected.has(d.name) }">
              <td class="check-cell">
                <input type="checkbox" :checked="selected.has(d.name)" :disabled="batchBusy"
                  :aria-label="`选择发行版 ${d.name}`"
                  @change="toggleSelect(d.name, ($event.target as HTMLInputElement).checked)" />
              </td>
              <td>
                <code class="mono">{{ d.name }}</code>
                <UiStatusChip v-if="d.default" tone="information">★ 默认</UiStatusChip>
              </td>
              <td>
                <!-- 状态归一呈现：原文是本地化文案（跨语言系统不可作判据），仅收进 title 备查 -->
                <UiStatusChip :tone="d.running ? 'positive' : 'neutral'" :title="d.stateText">
                  {{ d.running ? '运行中' : '已停止' }}
                </UiStatusChip>
              </td>
              <td class="mono col-ver">{{ d.version }}</td>
              <td class="mono dim" :title="d.vhdxPath || d.basePath || undefined">{{ d.sizeBytes ? fmtSize(d.sizeBytes) : '—' }}</td>
              <td>
                <!-- 行内常驻 4 钮（终端/重启/关机/删除），次要操作收「⋯ 更多」；
                     只读钮不受 busy 分级锁，写钮只锁"全局在飞或本行正忙" -->
                <div class="distro-actions">
                  <button class="btn btn-secondary btn-small"
                    title="唤起系统默认终端进入该发行版（WSL 无前台进程时会自动停机，本工具不做后台保活）"
                    @click="openTerminal(d)">⌨ 终端</button>
                  <button class="btn btn-secondary btn-small" :disabled="rowBusy(d.name)"
                    title="重启：停止→确认已停→拉起验证（不做后台保活，空闲后自动回落停止；wsl.conf 改动的生效捷径）"
                    @click="restartDistro(d)">🔄 重启</button>
                  <button class="btn btn-secondary btn-small" :disabled="rowBusy(d.name) || !d.running"
                    :title="d.running ? 'wsl --terminate：等同关掉本发行版的虚拟机电源（硬停；数据盘无损，下次访问自动再启动）；停全部请到就绪检测页「🌑 停止全部」' : '当前已停止'"
                    @click="terminateDistro(d)">⏹ 关机</button>
                  <button class="btn btn-danger-outline btn-small" :disabled="rowBusy(d.name)"
                    title="wsl --unregister：数据销毁级删除（连带清理独占的商店启动器），不可恢复"
                    @click="unregisterDistro(d)">🗑 删除</button>
                  <div class="row-menu">
                    <button class="btn btn-secondary btn-small" :aria-expanded="rowMenuName === d.name ? 'true' : 'false'" aria-haspopup="true"
                      title="文件 / 设默认 / 导出 / 迁移 / 克隆 / 瘦身 / 详情 / wsl.conf"
                      @click="toggleRowMenu(d.name, $event)">⋯ 更多</button>
                  </div>
                </div>
              </td>
            </tr>
            <tr v-if="movingName === d.name" class="move-row-editor">
              <td colspan="6">
                <div class="move-editor">
                  <UiBanner tone="warn" class="slim">
                    迁移会先执行 <code class="mono">wsl --shutdown</code> 打停整个 WSL 子系统<template v-if="moveRunningOthers.length">——当前运行中的
                    <b>{{ moveRunningOthers.map(i => i.name).join('、') }}</b> 会被连带终止<template v-if="d.running">（<b>{{ d.name }}</b> 本身也在运行，可先「⏹ 关机」缩小影响面）</template></template>，随后提权移动数据盘（UAC 授权）。目标须为空目录或不存在的路径，路径合法性由后端把关。
                  </UiBanner>
                  <div class="move-input-row">
                    <label class="move-label" for="wsl-move-target">目标目录</label>
                    <input id="wsl-move-target" v-model="moveTarget" class="input mono"
                      :placeholder="`D:\\WSL\\${d.name}`" spellcheck="false" :disabled="rowBusy(d.name)"
                      @keyup.enter="moveTarget.trim() && confirmMove(d)" />
                    <button class="btn btn-secondary btn-small" :disabled="rowBusy(d.name)"
                      title="调系统文件夹选择框：选好即回填，仍可手动修改" @click="pickFolderInto((p) => moveTarget = p, `选择 ${d.name} 的迁移目标目录`)">📁 选目录</button>
                    <button class="btn btn-primary btn-small" :disabled="!moveTarget.trim() || rowBusy(d.name)"
                      @click="confirmMove(d)">{{ busyWith(`move:${d.name}`) ? '迁移中…' : '✔ 确认迁移' }}</button>
                    <button class="btn btn-secondary btn-small" :disabled="busyWith(`move:${d.name}`)" @click="cancelMove">取消</button>
                  </div>
                  <div v-if="!moveTarget.trim()" class="hint-line">目标目录为空，「确认迁移」已置灰——填入或 📁 选一个非空路径即可开始。</div>
                </div>
              </td>
            </tr>
            <tr v-if="exportingName === d.name" class="export-row-editor">
              <td colspan="6">
                <div class="move-editor">
                  <div class="move-input-row">
                    <label class="move-label">导出格式</label>
                    <label class="radio-label"><input v-model="exportFormat" type="radio" value="gz" :disabled="busyWith(`export:${d.name}`)" />
                      tar.gz 压缩（默认，体积小；需 WSL 2.4.4+）</label>
                    <label class="radio-label"><input v-model="exportFormat" type="radio" value="tar" :disabled="busyWith(`export:${d.name}`)" />
                      tar 未压缩（更快、兼容老版本，体积大）</label>
                    <button class="btn btn-primary btn-small" :disabled="rowBusy(d.name)"
                      @click="confirmExport(d)">{{ busyWith(`export:${d.name}`) ? '导出中…' : '✔ 开始导出' }}</button>
                    <button class="btn btn-secondary btn-small" :disabled="busyWith(`export:${d.name}`)" @click="cancelExport">取消</button>
                  </div>
                </div>
              </td>
            </tr>
            <tr v-if="cloneSrc === d.name" class="clone-row-editor">
              <td colspan="6">
                <div class="move-editor">
                  <template v-if="cloneProg && cloneProg.source === d.name">
                    <div class="move-input-row">
                      <UiStatusChip :tone="cloneProg.stage === 'error' ? 'danger' : 'information'">
                        {{ cloneStageText[cloneProg.stage] ?? cloneProg.stage }}
                      </UiStatusChip>
                      <span v-if="(cloneProg.stage === 'copying' || cloneProg.stage === 'importing') && cloneProg.total" class="fx-bar">
                        <UiProgressBar :percent="Math.min(99, Math.round(cloneProg.done / cloneProg.total * 100))" />
                      </span>
                      <span v-if="cloneProg.total" class="mono dim">{{ fmtSize(cloneProg.done) }} / {{ fmtSize(cloneProg.total) }}</span>
                    </div>
                    <div v-if="cloneProg.stage === 'error'" class="error-box">{{ cloneProg.error }}
                      <button class="btn btn-secondary btn-small retry-inline" @click="cancelClone">知道了，收起</button>
                    </div>
                    <div v-if="cloneBusy" class="move-input-row">
                      <button class="btn btn-secondary btn-small" @click="requestCancelClone">✋ 取消克隆</button>
                      <span class="hint-dim">拷贝段取消会清理半成品；挂载段取消则保留已拷盘并点名路径</span>
                    </div>
                  </template>
                  <template v-else>
                    <UiBanner v-if="d.running" tone="warn" class="slim">
                      源发行版 <b>{{ d.name }}</b> 正在运行：克隆开始时会先终止它（数据无损，完成后可再启动）。
                    </UiBanner>
                    <div class="move-input-row">
                      <label class="move-label" for="wsl-clone-name">新发行版名</label>
                      <input id="wsl-clone-name" v-model="cloneNewName" class="input mono" spellcheck="false" :disabled="rowBusy(d.name)" />
                      <label class="move-label" for="wsl-clone-target">目标目录</label>
                      <input id="wsl-clone-target" v-model="cloneTarget" class="input mono" :placeholder="`D:\\WSL\\${d.name}-Copy`"
                        spellcheck="false" :disabled="rowBusy(d.name)"
                        @keyup.enter="cloneNewName.trim() && cloneTarget.trim() && submitClone(d)" />
                      <button class="btn btn-secondary btn-small" :disabled="rowBusy(d.name)"
                        title="调系统文件夹选择框：选好即回填，仍可手动修改" @click="pickFolderInto((p) => cloneTarget = p, `选择 ${d.name} 克隆副本的落位目录`)">📁 选目录</button>
                      <button class="btn btn-primary btn-small" :disabled="!cloneNewName.trim() || !cloneTarget.trim() || rowBusy(d.name)"
                        @click="submitClone(d)">✔ 开始克隆</button>
                      <button class="btn btn-secondary btn-small" :disabled="busyWith(`clone:${d.name}`)" @click="cancelClone">取消</button>
                    </div>
                    <div v-if="!cloneNewName.trim() || !cloneTarget.trim()" class="hint-line">
                      名称或目标目录为空，「开始克隆」已置灰——副本必须有一个明确的落位目录（留空没有意义可猜）。
                    </div>
                  </template>
                </div>
              </td>
            </tr>
            <tr v-if="compSrc === d.name" class="compact-row-editor">
              <td colspan="6">
                <div class="move-editor">
                  <template v-if="compProg">
                    <div class="move-input-row">
                      <UiStatusChip :tone="compProg.stage === 'error' ? 'danger' : compProg.stage === 'done' ? 'positive' : 'information'">
                        {{ compactStageText[compProg.stage] ?? compProg.stage }}
                      </UiStatusChip>
                      <UiStatusChip v-if="compProg.tier" tone="neutral">{{ compProg.tier === 'tier2' ? '已走备份重建' : compProg.tier === 'tier1' ? '就地压缩完成' : compProg.tier }}</UiStatusChip>
                      <span v-if="compProg.stage === 'done' && compProg.afterMB" class="mono dim">
                        {{ mb(compProg.beforeMB) }} → {{ mb(compProg.afterMB) }}</span>
                    </div>
                    <div v-if="compProg.stage === 'error'" class="error-box">{{ compProg.error }}</div>
                    <div v-else-if="compProg.message" class="hint-line">{{ compProg.message }}</div>
                    <div class="move-input-row">
                      <button v-if="compCancelable" class="btn btn-danger-outline btn-small" @click="cancelCompact">✋ 取消瘦身</button>
                      <button class="btn btn-secondary btn-small" :disabled="!compTerm" @click="closeCompact">
                        {{ compTerm ? '收起' : '在飞（终态后可收起）' }}</button>
                      <span v-if="!compCancelable && !compTerm" class="hint-dim">已进入数据盘处理/重建段，取消会被拒绝（有备份兜底，等它跑完）</span>
                    </div>
                  </template>
                  <template v-else>
                    <UiBanner tone="error" class="slim">
                      瘦身是对数据盘的手术：<b>强制先全量备份</b>，再 fstrim/停机、Optimize-VHD 压缩数据盘（需 Hyper-V 模块，UAC），
                      省量不足自动转「从备份重建」——注销旧实例并从备份重导入（落位会换到新旁路目录）。失败如实中止并点名备份位置；完成后备份保留供你自行清理。
                    </UiBanner>
                    <div class="move-input-row">
                      <label class="move-label" for="wsl-compact-bak">备份目录</label>
                      <input id="wsl-compact-bak" v-model="compBackupDir" class="input mono"
                        placeholder="默认「下载\WSL 导出」；大盘备份可指到空闲卷（须绝对路径）" spellcheck="false" :disabled="rowBusy(d.name)" />
                      <button class="btn btn-secondary btn-small" :disabled="rowBusy(d.name)"
                        title="调系统文件夹选择框：选好即回填，仍可手动修改" @click="pickFolderInto((p) => compBackupDir = p, `选择 ${d.name} 瘦身备份的存放目录`)">📁 选目录</button>
                    </div>
                    <div class="move-input-row">
                      <button class="btn btn-danger-outline btn-small" :disabled="rowBusy(d.name)" @click="submitCompact(d)">🗜 确认开始瘦身</button>
                      <button class="btn btn-secondary btn-small" :disabled="busyWith(`compact:${d.name}`)" @click="closeCompact">取消</button>
                    </div>
                  </template>
                </div>
              </td>
            </tr>
            <tr v-if="confName === d.name" class="conf-row-editor">
              <td colspan="6">
                <div class="move-editor">
                  <div v-if="confLoading && !confDoc" class="hint-line">正在读取 /etc/wsl.conf…（发行版未运行时会先被启动，这是读配置的预期动作）</div>
                  <div v-else-if="confError && !confDoc" class="error-box">{{ confError }}
                    <button class="btn btn-secondary btn-small retry-inline" @click="loadConf(d.name)">↻ 重试</button>
                    <button class="btn btn-secondary btn-small" @click="closeConf">收起</button>
                  </div>
                  <template v-else-if="confDoc">
                    <UiBanner v-if="confDoc.missing" tone="info" class="slim">该发行版尚无 /etc/wsl.conf——保存即首建。</UiBanner>
                    <UiBanner v-for="(w, i) in confDoc.warnings ?? []" :key="i" tone="warn" class="slim">{{ w }}</UiBanner>
                    <!-- 换装 UiClipboardField：conf 常被整段从别处复制来改，标配粘贴钮；
                         编辑器语义是"整份替换"，pasteMode=replace -->
                    <UiClipboardField
                      v-model="confText"
                      mono
                      :rows="8"
                      paste-mode="replace"
                      :disabled="rowBusy(d.name)"
                      :placeholder="'[boot]\nsystemd=true'"
                    />
                    <div class="move-input-row">
                      <button class="btn btn-primary btn-small" :disabled="rowBusy(d.name) || confLoading"
                        @click="saveConf(d)">{{ busyWith(`confsave:${d.name}`) ? '保存中…' : '✔ 保存写回' }}</button>
                      <button class="btn btn-secondary btn-small" :disabled="rowBusy(d.name) || confLoading"
                        @click="loadConf(d.name)">↻ 重读</button>
                      <button class="btn btn-secondary btn-small" :disabled="rowBusy(d.name)" @click="closeConf">收起</button>
                      <span class="hint-dim">语法闸门 / 默认用户求证 / 写前备份（{{ '/etc/wsl.conf.hanxi.bak' }}）由后端把关；改完记得「⏹ 关机」或「🔄 重启」再进入</span>
                    </div>
                  </template>
                </div>
              </td>
            </tr>
            <tr v-if="forensicsName === d.name" class="forensics-row">
              <td colspan="6">
                <div class="forensics-panel">
                  <div v-if="!forensicsOf || (forensicsOf.loading && !forensicsOf.data)" class="hint-line">详情采集中：注册表巡查 + 磁盘双口径 + guest 只读探测…</div>
                  <div v-else-if="forensicsOf.error && !forensicsOf.data" class="error-box">{{ forensicsOf.error }}
                    <button class="btn btn-secondary btn-small retry-inline" @click="loadForensics(d.name)">↻ 重试</button>
                  </div>
                  <template v-else-if="forensicsOf?.data">
                    <dl class="fx-grid">
                      <div><dt>VHDX 逻辑大小</dt><dd class="mono">{{ forensicsOf.data.logicalBytes ? fmtSize(forensicsOf.data.logicalBytes) : '—' }}</dd></div>
                      <div><dt>磁盘实占</dt><dd class="mono">{{ forensicsOf.data.allocBytes ? fmtSize(forensicsOf.data.allocBytes) : '—' }}
                        <UiStatusChip v-if="forensicsOf.data.sparse" tone="information">稀疏盘</UiStatusChip></dd></div>
                      <div v-if="forensicsOf.data.dfOk"><dt>根盘用量（guest df）</dt><dd class="mono">
                        <span class="fx-bar"><UiProgressBar :percent="forensicsOf.data.dfUsePct" /></span>
                        {{ mb(forensicsOf.data.dfUsedMB) }} / {{ mb(forensicsOf.data.dfTotalMB) }} · 可用 {{ mb(forensicsOf.data.dfAvailMB) }} · {{ forensicsOf.data.dfUsePct }}%</dd></div>
                      <div><dt>发行版 IPv4</dt><dd class="mono">
                        <template v-if="forensicsOf.data.ipv4">{{ forensicsOf.data.ipv4 }}
                          <button class="link-button" @click="copy(forensicsOf.data.ipv4)">复制</button>
                        </template>
                        <template v-else>—</template></dd></div>
                      <div v-if="forensicsOf.data.pfn"><dt>包家族名 (PFN)</dt><dd class="mono">{{ forensicsOf.data.pfn }}</dd></div>
                      <div v-if="forensicsOf.data.basePath"><dt>安装路径</dt><dd class="mono">{{ forensicsOf.data.basePath }}</dd></div>
                      <div v-if="forensicsOf.data.vhdxPath"><dt>VHDX 路径</dt><dd class="mono">{{ forensicsOf.data.vhdxPath }}</dd></div>
                    </dl>
                    <ul v-if="forensicsOf.data.notes?.length" class="fx-notes">
                      <li v-for="(n, i) in forensicsOf.data.notes" :key="i">ⓘ {{ n }}</li>
                    </ul>
                    <div class="fx-foot">
                      <UiStatusChip :tone="forensicsOf.data.running ? 'positive' : 'neutral'">{{ forensicsOf.data.running ? '运行中' : '已停止' }}</UiStatusChip>
                      <UiStatusChip tone="neutral" title="来自宿主 ~/.wslconfig 的 [networking] networkingMode">网络模式：{{ modeWord(forensicsOf.data.networkMode) }}</UiStatusChip>
                      <button class="btn btn-secondary btn-small" :disabled="forensicsOf.loading"
                        @click="loadForensics(d.name)">{{ forensicsOf.loading ? '刷新中…' : '↻ 刷新' }}</button>
                    </div>
                  </template>
                </div>
              </td>
            </tr>
          </template>
        </tbody>
      </table>
    </div>

    <!-- 本会话导出工件登记：多份全列，逐份直达位置；导出/瘦身成功即自动展开 -->
    <details v-if="exportRecords.length" class="info-details export-log" :open="exportLogOpen">
      <summary class="info-summary">📦 本会话导出记录（{{ exportRecords.length }}）</summary>
      <ul class="export-log-list">
        <li v-for="r in exportRecords" :key="r.id">
          <code class="mono">{{ r.id }}</code>
          <span class="dim mono">{{ r.name }} · {{ fmtSize(r.size) }} · {{ r.at }}</span>
          <button class="link-button" @click="copy(r.path)">复制路径</button>
          <button class="link-button" @click="revealExport(r.id)">📂 打开位置</button>
        </li>
      </ul>
    </details>

    <!-- 「⋯ 更多」面板：单实例 Teleport 直挂 body，fixed 坐标由触发钮 rect 算得
         （遮挡根因与翻转/钳制策略见 script 注释；面板内容单一来源 rowMenuItems） -->
    <Teleport to="body">
      <div v-if="rowMenuItems.length" ref="rowMenuPanelEl" class="row-menu-panel" role="menu"
        :class="{ up: rowMenuUp }" :style="rowMenuStyle" @keydown.esc.stop="closeRowMenu">
        <button v-for="item in rowMenuItems" :key="item.key" class="row-menu-item" role="menuitem"
          :disabled="item.disabled" :class="{ active: item.active }" :title="item.title"
          @click="rowMenuAction(item.run)">{{ item.label }}</button>
      </div>
    </Teleport>
  </template>
  <div v-else-if="report" class="empty-state">
    <p>本机尚未安装 WSL 本体——先到「🐧 就绪检测」页执行「🚀 一键开启」（「虚拟机平台」启用后需重启一次），装好发行版后实例会列在这里。</p>
  </div>
  <div v-else class="hint-line">就绪体检进行中，稍候自动列出本机发行版…</div>
</template>

<style scoped>
/* 全局原子（.btn 家族/.tbl/.chip/.error-box/.empty-state/.mono/.link-button/.hint-dim/.banner*）
   落 components.css；以下为本控制台独有或有意补差（注释注明）。 */

/* 多行诊断文案（换行符保留）——对全局 .error-box 的补差白名单行，非同名副本 */
.error-box { white-space: pre-line; }
/* 基形（字号/颜色/左内距）落回全局 .hint-line；此处仅留 WSL 档补差两行 */
.hint-line { line-height: 1.7; white-space: pre-line; }
/* 全局 .hint-dim 只定义颜色，WSL 注记统一配 --text-sm 小字——补差，非副本 */
.hint-dim { font-size: var(--text-sm); }
/* .banner.slim 等值副本已删净：UiBanner 根元素挂 .banner + .slim，落回全局 :where(.banner.slim) */
/* .dim 非全局原子名，本控制台独有 */
.dim { color: var(--color-text-muted); font-size: var(--text-sm); }
.retry-inline { margin-left: 10px; }
/* display/gap 落回全局 .btn-group；此处仅留换行补差 */
.btn-group { flex-wrap: wrap; }

/* 发行版管理控制台 */
.distro-head { display: flex; align-items: center; justify-content: space-between; gap: 10px; flex-wrap: wrap; }
.distro-head-actions { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }
.distro-actions { display: flex; gap: 6px; flex-wrap: wrap; align-items: center; }
/* 实锤②（窄屏挤行）：操作区常驻 5 钮 + 4 数据列，视口窄时列被压到换行错乱。
   给表体设 760px 地板：不足即由 .table-container 的 overflow-x:auto 横向滚动承接
   （旧形 width:100% 无地板，窄屏只会把发行版名/操作列碾碎）。复选列加入后地板维持 760：
   新增 36px 由发行版列（18%→16%）让渡，滚动兜底机制口径不变。 */
.tbl { min-width: 760px; }

/* 批量复选：表头/行内勾选钮与选中行底色（--surface-selected 四主题皆有定义） */
.check-th, .check-cell { text-align: center; }
.check-th input[type='checkbox'], .check-cell input[type='checkbox'] {
  width: 15px; height: 15px; margin: 0; accent-color: var(--color-primary); cursor: pointer; vertical-align: middle;
}
.check-th input:disabled, .check-cell input:disabled { cursor: not-allowed; opacity: 0.6; }
.check-th input:focus-visible, .check-cell input:focus-visible { outline: 2px solid var(--focus-ring, var(--color-primary)); outline-offset: 1px; }
.tbl tbody tr.row-selected > td { background: var(--surface-selected); }
/* 批量条：勾选非空才出现的浮出工具条，与 .control-bar 同族表面语言，纵向压扁（一行流） */
.batch-bar {
  display: flex; align-items: center; gap: 10px; flex-wrap: wrap; padding: 6px 10px;
  background: var(--surface-panel); border: 1px solid var(--color-border-strong);
  border-radius: var(--radius-control); font-size: var(--text-sm);
}
.batch-count b { font-variant-numeric: tabular-nums; }
.batch-bar .link-button:disabled { opacity: 0.5; cursor: not-allowed; }

/* 窄屏档（机主 2026-09-26 截图反馈"一屏容不下两屏高"）：≤640px 收紧**纵向**节奏——
   单元格竖内距 8→5、行钮组缝隙 6→4、批量条/编辑器内距压扁、导出记录卡展开后限高内滚
   （摘要行语义保留：summary 恒一行，展开不再是无限长卷宗）。横向仍以 .tbl 760px 地板 +
   容器滚动兜底（设计系统口径：宽表格在自身容器内滚，不把每列碾到换行）。
   390px 再降一档：隐去 WSL 版本列（本机几乎恒为 2，全量版本在「详情」抽屉可查），
   缩短横向滚动行程。宽屏（>640px）一切观感零回退——媒体查询外无一字改动。 */
@media (max-width: 640px) {
  .tbl :is(th, td) { padding: 5px 8px; }
  .distro-actions { gap: 4px; }
  .batch-bar { gap: 6px; padding: 4px 8px; }
  .move-editor { gap: 6px; }
  .forensics-panel { gap: 4px; }
  .export-log-list { max-height: 32vh; overflow-y: auto; }
  .export-log-list li { padding: 2px 10px; }
}
@media (max-width: 390px) {
  .col-ver { display: none; }
}

/* 行级「⋯ 更多」下拉：面板 Teleport 到 body + fixed 坐标（挂载/翻转/收合策略见 script），
   .row-menu 只做触发钮的布局盒，不再充当 absolute 定位上下文。 */
.row-menu { display: inline-flex; }
.row-menu-panel {
  /* fixed + 直挂 body：逃离 .table-container(overflow-x:auto) 与 .content-area(overflow-y:auto)
     两层裁剪；left/top|bottom 由 placeRowMenu 写进内联样式。z 低于弹层家族（≥950/1000）
     与 Toast(999999)，压过一切内容层与粘性头。视口极矮时面板自身可滚，不再要求翻转位。 */
  position: fixed; z-index: 900; min-width: 150px; max-height: calc(100vh - 16px); overflow-y: auto;
  display: flex; flex-direction: column; gap: 2px; padding: 4px;
  background: var(--surface-panel); border: 1px solid var(--color-border-strong);
  border-radius: var(--radius-control); box-shadow: var(--shadow-panel);
}
.row-menu-item {
  display: flex; align-items: center; gap: 6px; text-align: left; width: 100%;
  background: none; border: none; border-radius: 6px; padding: 6px 10px;
  font-size: var(--text-sm); color: var(--color-text); cursor: pointer; white-space: nowrap;
}
.row-menu-item:hover:not(:disabled) { background: var(--surface-hover); }
.row-menu-item:focus-visible { outline: 2px solid var(--focus-ring, var(--color-primary)); outline-offset: -2px; }
.row-menu-item:disabled { opacity: 0.5; cursor: not-allowed; }
.row-menu-item.active { color: var(--color-primary); }

.radio-label { display: inline-flex; align-items: center; gap: 5px; font-size: var(--text-sm); color: var(--color-text); cursor: pointer; }
.radio-label input[type='radio'] { accent-color: var(--color-primary); margin: 0; }
/* 行内编辑器统一浅底（六种编辑器行同形，合并声明） */
.move-row-editor td, .export-row-editor td, .clone-row-editor td,
.compact-row-editor td, .conf-row-editor td, .forensics-row td { background: var(--surface-page); }

/* wsl.conf 编辑器与瘦身表单 */

/* 取证抽屉 */
.forensics-panel { display: flex; flex-direction: column; gap: 6px; padding: 4px 0; }
.fx-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(240px, 1fr)); gap: 8px 18px; margin: 0; }
.fx-grid dt { font-size: var(--text-xs); color: var(--color-text-subtle); }
.fx-grid dd { margin: 0; font-size: var(--text-sm); word-break: break-all; }
.fx-bar { width: 90px; display: inline-flex; vertical-align: middle; margin-right: 6px; }
.fx-notes { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 2px; font-size: var(--text-sm); color: var(--color-text-muted); }
.fx-foot { display: flex; align-items: center; gap: 10px; }
.export-log { margin-top: 2px; }
.export-log-list { list-style: none; margin: 0; padding: 4px 0; display: flex; flex-direction: column; }
.export-log-list li { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; padding: 4px 12px; font-size: var(--text-sm); border-bottom: 1px solid var(--color-border); }
.export-log-list li:last-child { border-bottom: none; }
.move-editor { display: flex; flex-direction: column; gap: 8px; padding: 2px 0; }
.move-input-row { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.move-label { font-size: var(--text-sm); font-weight: 600; color: var(--color-text-muted); white-space: nowrap; }
.input {
  background: var(--surface-page); border: 1px solid var(--color-border); border-radius: 6px;
  padding: 6px 10px; font-size: var(--text-sm); color: var(--color-text); font-family: inherit; flex: 1 1 240px; min-width: 0;
}
.input:focus { border-color: var(--color-primary); }
/* 焦点环不再被 outline:none 掐灭：键盘聚焦（:focus-visible）恢复清晰焦点环 */
.input:focus-visible { outline: 2px solid var(--focus-ring, var(--color-primary)); outline-offset: 1px; }
.input:disabled { opacity: 0.6; }

/* 知识卡（导出记录抽屉壳）：info-details/info-summary 全家族（含 ::after/marker/[open] 两条）
   与托管家族上收的全局原子逐字等值，副本已删净落回 components.css。 */
</style>
