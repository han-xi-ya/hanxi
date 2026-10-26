<script setup lang="ts">
// SSH 主机清单面板（账本 + 导出对账带）。
//
// 这个面板的灵魂是对账带，不是表格：清单只是账本，ssh 真正读的是导出的
// ~/.ssh/hanxi.conf——两者关系（一致 / 已改动 / 还没导出过 / 缺 Include 接线 /
// 缺钥匙）必须常驻摊开，"改了没同步"不允许静默。写用户主目录只能由人显式按下
// 「同步给 ssh」触发（useHosts 头注的同一条后端纪律）。
//
// 全部列/按钮/表单壳走全局原子（.card/.tbl/.table-container/.empty-state/.empty-hint/
// .error-box/.hint-line/.state-warning/.mono/.chip/.btn-group/.btn-*/.link-button/
// .toggle-label/.form-item-block/.form-label/.select-input）；scoped 只补布局，
// 不自造任何彩色按钮档——buttonColorContract 在扫这里。所有按钮容器挂 .btn-group
// （全局档含 flex-wrap），窄窗整颗换行而不是溢出（"刷新网卡被挤出窗口"的实测事故条款）。
import { computed, onMounted, ref } from 'vue'
import UiBanner from '../ui/UiBanner.vue'
import UiStatusChip from '../ui/UiStatusChip.vue'
import { aliasIssue, baseOfKeyPath, useHosts } from './useHosts'
import type { HostEntry, KeyEntry } from '../../../bindings/hanxi/internal/modules/sshkeys/models'
import { useConfirm } from '../../composables/useConfirm'
import { useClipboard } from '../../composables/useClipboard'

const { confirm } = useConfirm()
const { copyWithToast } = useClipboard()

const {
  hosts, summary, listError, exportError, exportNote, checkedAt,
  keyMissing,
  ledgerKeys, ledgerReadFailed, refreshKeyOptions,
  previewAlias,
  listBusy, exportBusy,
  refreshAll, saveHost, toggleEnabled, removeHost, syncToSSH, undoExport,
} = useHosts()

onMounted(refreshAll)

// —— 清单展示辅助 ——
// 端口 0 在后端按 22 处理；对账要账目清楚，缺省值也显式写出来，不留"没填？"的歧义。
function addrText(h: HostEntry): string {
  return `${h.host}:${h.port || 22}`
}

// 密钥列只上屏基名（id_ed25519_1panel 这种天天见的可读音），完整路径退到 title。
// "这把钥匙在本机找不到"的帽子**只**由后端对账逐条 stat 的名单（missingKeyAliases）
// 来戴——本面板下方表单虽然也调 ListKeys，但那只做下拉**选项来源**，两路用途严禁
// 合并（旧事故：拿台账基名猜在不在，把放别处的真钥匙标成"找不到"，帽子正压在
// 删除决策上；判"缺不缺"永远不消费台账名单，见 useHosts.refreshKeyOptions 头注）。
function keyName(h: HostEntry): string {
  return baseOfKeyPath(h.keyPath)
}

// —— 新增/编辑表单 ——
// 表单草稿存这里（composable 之上、面板之内）：SshKeysView 用 v-show 切页，
// 组件不销毁，填了一半的内容不会因切页蒸发。
type Draft = { alias: string; host: string; port: string; user: string; keyPath: string; note: string; enabled: boolean }
const blankDraft = (): Draft => ({ alias: '', host: '', port: '', user: '', keyPath: '', note: '', enabled: true })

const formOpen = ref(false)
const draft = ref<Draft>(blankDraft())
// 编辑态锁别名：后端 Update 只按别名整条覆盖（改了就是另一把钥匙的账，见
// ledger.go）。界面上**如实反映**这一点——输入框禁用但内容可见，并给出
// "要改名就新增一条、删旧的"的出路，而不是把主键藏起来。
const editingAlias = ref('')
const aliasTouched = ref(false)
const hostTouched = ref(false)
const portTouched = ref(false)
const attempted = ref(false)
const formError = ref('')

// —— 新增态别名：默认由后端从地址派生，前端零算法 ——
// 派生与去重（重名自动加 -2）的权威都在后端（sshconf + ledger.add：别名留空即走
// 这条路）。这里**不实现第二套派生**：预览调 SuggestAlias RPC 只为"将生成成什么"
// 给人一眼；保存时自动模式发**空别名**交给后端落账，界面再以返回的整表为准——
// 预览没过去重，绝不能拿预览值当正式值发出去（规则复制=第二套真相，本仓为别名
// 规则已单独写过镜像注释（aliasIssue），不再增处）。
const customAlias = ref(false)        // 「我想自己起别名」勾选（原生 checkbox，全仓无 switch 原子）
const suggestedAlias = ref('')        // SuggestAlias 的预览返回值
const aliasPreviewError = ref('')     // 派生失败的后端原文（''=无失败）
const previewedHost = ref('')          // 预览值对应的地址；地址没变就不重发 RPC（是 computed 的依赖，必须是响应式的）
const aliasAuto = computed(() => !editingAlias.value && !customAlias.value)

// 一个输入框两种形态：自动态只读、显示预览（地址变了预览就作废，显示占位文案）；
// 自填/编辑态显示并编辑草稿值。自动态的 set 是空操作——只读框本就挡真人输入，
// 这里再挡一道"程序塞值绕过只读"的假路径。
const aliasField = computed({
  get: () => (aliasAuto.value
    ? (previewedHost.value === draft.value.host.trim() ? suggestedAlias.value : '')
    : draft.value.alias),
  set: (v: string) => { if (!aliasAuto.value) draft.value.alias = v },
})

async function refreshAliasPreview() {
  if (editingAlias.value) return
  const host = draft.value.host.trim()
  if (!host) {
    suggestedAlias.value = ''
    aliasPreviewError.value = ''
    previewedHost.value = ''
    return
  }
  // 同一地址已预览成功就不重发；上次是失败的，交还自动时再试一次（结果多半
  // 还是失败，但勾选框弹回+提示在，用户看得见为什么自动模式给不出东西）。
  if (host === previewedHost.value && !aliasPreviewError.value) return
  const res = await previewAlias(host)
  // 迟到响应丢弃：等待期间地址又改了，旧地址的结果既不作预览也不判失败。
  if (draft.value.host.trim() !== host) return
  previewedHost.value = host
  if (res.ok) {
    suggestedAlias.value = res.alias
    aliasPreviewError.value = ''
  } else {
    // 派生不出（纯中文/纯符号地址）：把自定义框**就地弹开**并给一句人话——
    // 表单不卡进看不见原因的地方；不自作主张替用户编一个别名。
    suggestedAlias.value = ''
    aliasPreviewError.value = res.error
    customAlias.value = true
  }
}

function onHostBlur() {
  hostTouched.value = true
  // 自填/编辑态不重算预览：框里没人看，别拿「弹开自定义」的提示去惊动正在手写的人。
  if (aliasAuto.value) void refreshAliasPreview()
}

// 勾上=接管手写（草稿清空，别拿预览值冒充用户起的名字）；取消=交还自动并补齐
// 当前地址的预览（自动态下用户改过地址又切回来时，不能让框停在陈旧预览上）。
function onCustomAliasToggle() {
  if (customAlias.value) draft.value.alias = ''
  else void refreshAliasPreview()
}

const aliasProblem = computed(() => {
  // 自动态不校验别名：框里是后端预览，正式值由后端派生+去重后定，前端挑不了。
  if (aliasAuto.value) return ''
  const base = aliasIssue(draft.value.alias)
  if (base) return base
  // 重名在前端先拦（保存前拦，非仅后端拒）：别名是账本主键，也是导出配置的
  // 匹配名，重名会让渲染层的段数自证被破坏。
  if (!editingAlias.value && draft.value.alias && hosts.value.some(h => h.alias === draft.value.alias)) {
    return `账本里已有一台「${draft.value.alias}」：别名是唯一键，重名时后写的会撞掉先写的——要改这台就用列表里的「编辑」。`
  }
  return ''
})
const hostProblem = computed(() => {
  // 只轻拦"没填"；IP/域名的成套判据在后端（sshconf/sshprobe），UI 不造第二判据。
  return draft.value.host.trim() ? '' : '地址必填：填 IP 或域名；协议前缀、端口、user@ 都不写在这里（各有栏位）。'
})
const portProblem = computed(() => {
  const v = draft.value.port
  if (v === '') return ''
  const n = Number(v)
  if (!Number.isInteger(n) || n < 0 || n > 65535) {
    return `端口需要是 0–65535 的整数（当前「${v}」）；留空或 0 都按 22 记。`
  }
  return ''
})

// 反馈时机：即时但不出声——字段 blur 过或按过一次保存之后，错误才着色显示；
// pristine 字段不提前报红。保存钮**不因校验红而禁用**：点了要能看见为什么存不了。
const aliasShown = computed(() => (aliasTouched.value || attempted.value) ? aliasProblem.value : '')
const hostShown = computed(() => (hostTouched.value || attempted.value) ? hostProblem.value : '')
const portShown = computed(() => (portTouched.value || attempted.value) ? portProblem.value : '')

// —— 「私钥路径」下拉：台账当选项来源 ——
// 只列「有私钥、且后端给了 suggestPath」的行（孤儿 .pub 不是私钥路径的合法答案；
// suggestPath 为空的有私钥行按后端契约不存在，真出现宁可不列，不拿别的字段顶）。
// **值一律用 suggestPath（`~/<名字>`），不取 privatePath、前端也不自己拼**：
// `~/名字` 形态跨机器稳定（换用户名不漂），而台账只扫 ~/.ssh——凡是列出来的私钥
// 必然写得成 `~/` 形态，这个判断的依据在扫描侧才完整（models KeyEntry.SuggestPath
// 契约）；前端拼路径就是第二次猜测，清单里已经因"拿台账猜"误标过一次，不再猜第二处。
const keySelectOptions = computed(() => ledgerKeys.value.filter(k => k.hasPrivate && !!k.suggestPath))

// select 与输入框共用一个值（draft.keyPath），两个方向都同步：
// 选下拉→写入输入框；手输任意路径→select 落到这颗禁用的「自定义」哨兵上
// （disabled 只拦用户点击、仍可被程序选中回显），把"只能选不能输"的死路堵死——
// 存在先登记机器、回头才生成钥匙的顺序，输入框任何时候都必须能纯手输保存。
const CUSTOM_KEYPATH = '__custom_keypath__'
const keySelectValue = computed(() => {
  const v = draft.value.keyPath
  if (!v) return ''
  return keySelectOptions.value.some(k => k.suggestPath === v) ? v : CUSTOM_KEYPATH
})

// label 用人能读的：`名字（算法 · 指纹前 8 位）`。完整路径不进 label——选中后
// 路径就在输入框里，label 再挂一遍长路径只会把选项挤得没法读。
// 指纹剥掉 `SHA256:` 前缀再取前 8 位：前缀是格式名不是身份，整串照搬等于没截。
function keyOptionLabel(k: KeyEntry): string {
  const tags = [k.type, (k.fingerprint ?? '').replace(/^SHA256:/, '').slice(0, 8)].filter(Boolean)
  return tags.length ? `${k.name}（${tags.join(' · ')}）` : k.name
}

function onKeyPathSelect(ev: Event) {
  const v = (ev.target as HTMLSelectElement).value
  // 禁用项（读取失败/空台账/自定义哨兵）不可能被用户选中，到这里的只有
  // ''（第一项：清空为"不指定"）或某条 suggestPath。
  if (v === CUSTOM_KEYPATH) return
  draft.value.keyPath = v
}

// 联动提示（本下拉的主要价值之一）：选中的这把 hasPublic=false，「部署公钥」对它
// 必然不可用——当场就地说明，不等人跑去部署时撞墙才发现。只陈述台账事实，
// 不推断钥匙在不在（"缺不缺钥匙"的帽子仍只由后端名单戴，见 keyName 头注）。
const selectedKeyLacksPub = computed(() => {
  const k = keySelectOptions.value.find(o => o.suggestPath === draft.value.keyPath)
  return !!k && !k.hasPublic
})

// —— 裸文件名纠正提示（防"本机找不到这把钥匙"那类坑再发生）——
// 机主踩过的坑：把密钥路径打成光秃秃的文件名（无 ~ 、无目录分隔符）。后端 expandHome
// 对这种值原样 stat 必然找不到，ExportSummary.missingKeyAliases 点名"本机找不到这把
// 钥匙"——**那是事实不是误报**（ssh 同样按相对路径解析不到），要防的是它再发生。
// 判据苛刻：只有「值既不含 / 与 \ 也不以 ~ 开头」且「台账确有同名私钥」时才提示——
// 修复目标取台账那行的 suggestPath（后端契约给的 `~/<名字>`），所以这一改是**确定性
// 纠正**而非猜路径：与已废弃的"拿台账基名猜钥匙在不在"性质不同（那才是推断，见
// useHosts.refreshKeyOptions 头注）。不硬拦保存：相对路径可能是有意为之（别的执行
// 环境），但提示常驻值旁，不在提交瞬间闪现。
const bareKeyFix = computed(() => {
  const v = draft.value.keyPath.trim()
  if (!v || v.startsWith('~') || v.includes('/') || v.includes('\\')) return ''
  return keySelectOptions.value.find(o => o.name === v)?.suggestPath ?? ''
})

function openAdd() {
  draft.value = blankDraft()
  editingAlias.value = ''
  aliasTouched.value = hostTouched.value = portTouched.value = false
  attempted.value = false
  formError.value = ''
  customAlias.value = false
  suggestedAlias.value = ''
  aliasPreviewError.value = ''
  previewedHost.value = ''
  formOpen.value = true
  // 每次开表单重拉一次台账：刚在「密钥台账」生成的钥匙要当场选得到，
  // 不该逼人重开面板（挂载时另有 refreshAll 拉第一遍）。失败静默降级。
  void refreshKeyOptions()
}

function openEdit(h: HostEntry) {
  draft.value = {
    alias: h.alias, host: h.host, port: h.port ? String(h.port) : '',
    user: h.user, keyPath: h.keyPath, note: h.note, enabled: h.enabled,
  }
  editingAlias.value = h.alias
  aliasTouched.value = hostTouched.value = portTouched.value = false
  attempted.value = false
  formError.value = ''
  customAlias.value = false
  suggestedAlias.value = ''
  aliasPreviewError.value = ''
  previewedHost.value = ''
  formOpen.value = true
  void refreshKeyOptions()
}

function closeForm() {
  formOpen.value = false
  formError.value = ''
}

async function onSave() {
  attempted.value = true
  if (aliasProblem.value || hostProblem.value || portProblem.value) return
  const entry: HostEntry = {
    // 自动态发**空别名**：派生与去重（重名加 -2）由后端在落账时一并做，界面随后
    // 以返回的整表为准——预览只是预览，不当正式值发出去（见上方别名注释）。
    alias: aliasAuto.value ? '' : draft.value.alias.trim(),
    host: draft.value.host.trim(),
    port: Number(draft.value.port) || 0,
    user: draft.value.user.trim(),
    keyPath: draft.value.keyPath.trim(),
    note: draft.value.note.trim(),
    enabled: draft.value.enabled,
  }
  const res = await saveHost(entry, !editingAlias.value)
  if (res.ok) {
    closeForm()
  } else {
    // 后端拒绝的原文（sshconf 的字段级中文）落回表单现场，草稿保留——
    // "改对了才能走"不该以丢掉输入为代价。
    formError.value = res.error
  }
}

// 启停走原生 checkbox（全仓没有 switch 原子，不私造）。失败时把 DOM 的 checked
// 拨回账本真实值：vdom 里的 h.enabled 没变过，patch 不会重写这个属性，必须手动复位。
async function onRowToggle(h: HostEntry, ev: Event) {
  const el = ev.target as HTMLInputElement
  const ok = await toggleEnabled(h, el.checked)
  if (!ok) el.checked = h.enabled
}

async function onRemove(h: HostEntry) {
  const ok = await confirm({
    title: `从清单移除 ${h.alias}？`,
    description: '从清单移除不会删除磁盘上的私钥，也不会自动改动已经导出的产物——ssh 那边这台机器还在，直到你下一次点「同步给 ssh」。',
    details: [
      { label: '地址', value: addrText(h) },
      { label: '用户', value: h.user || '（未填，走 ssh 默认）' },
      { label: '密钥路径', value: h.keyPath || '（未设）' },
    ],
    confirmLabel: '移除',
    tone: 'danger',
  })
  if (!ok) return
  await removeHost(h.alias)
}

async function onUndoExport() {
  const ok = await confirm({
    title: '撤销导出？',
    description: '删除产物、摘掉顶层 config 里自己那段 Include；你自己的 config 不删。清单账本原样保留，再点「同步给 ssh」随时可重建产物。',
    confirmLabel: '撤销',
    details: summary.value?.confPath ? [{ label: '将删除', value: summary.value.confPath }] : [],
  })
  if (!ok) return
  await undoExport()
}

// —— 对账带呈现（操作面板语法：左一句状态，右一组动作）——
// 状态一句（产物与账本一致 / 账本已改动 / 还没导出过…）直接用后端 ExportStatus
// 的 message 原文，前端不重推一套话；色彩档只看 drift/included 布尔位，
// 不解析文本决定行为——"产物是新的但线没接上"也不绿给假安心。
const bandTone = computed<'positive' | 'warning' | 'neutral'>(() => {
  if (!summary.value) return 'neutral'
  return summary.value.drift || !summary.value.included ? 'warning' : 'positive'
})
const bandLabel = computed(() => {
  if (!summary.value) return exportBusy.value ? '正在核对账本与 ssh 产物…' : '对账状态未取到（见下方错误）'
  return summary.value.message
})
// 待落盘：账本走在了产物前面（漂移或从未导出），等人点「同步给 ssh」。
// 只看布尔位；导出写失败时不满足此位，界面不会把失败塌成"还没导出过"。
const exportPending = computed(() => {
  const s = summary.value
  return !!s && s.drift && !s.written
})

// —— 行内「启用」的语义讲全（它是导出开关，不是模块开关）——
const ROW_TOGGLE_TITLE =
  '「启用」= 是否导出给 ssh。关掉：账本保留、这行还在，只是导出的产物里不再写这一段，' +
  '点「同步给 ssh」后这个别名就不再认得。这不是模块开关——模块开关在 设置→模块管理。'
function rowToggleAria(h: HostEntry): string {
  return h.enabled
    ? `主机 ${h.alias}：现在导出给 ssh（关掉不删账本，只是不再导出）`
    : `主机 ${h.alias}：现在不导出给 ssh（账本保留；下次「同步给 ssh」后 ssh 不再认得这个别名）`
}
</script>

<template>
  <section class="card">
    <div class="card-top">
      <div>
        <h2>主机清单</h2>
        <p class="hint-line">
          登记的每台机器 = 以后 <code>ssh 别名</code> 用的名字。清单只是账本；
          要让本机的 ssh 真正认得它们，需要显式点下方「同步给 ssh」。
        </p>
      </div>
      <div class="btn-group">
        <button class="btn btn-secondary" :disabled="listBusy" @click="openAdd">新增主机</button>
        <button class="btn btn-secondary" :disabled="listBusy" @click="refreshAll">
          {{ listBusy ? '读取中…' : '刷新' }}
        </button>
      </div>
    </div>

    <!-- 导出对账带：账本 ↔ ssh 实际读到的那份文件的关系，常驻摊开。
         操作面板语法：头行左一句状态、右一组动作（同步=主、撤销=次，同档同高），
         细节（接线/缺钥匙/产物路径/回执/失败）在次级摊开。
         清单为空时不显示（避免与列表空态双空态；没有账本就没有可对的账）。 -->
    <div v-if="hosts.length" class="export-band">
      <div class="band-head">
        <div class="band-status">
          <UiStatusChip :tone="bandTone">{{ bandLabel }}</UiStatusChip>
          <span v-if="checkedAt" class="hint-line">上次对账 {{ checkedAt }}</span>
        </div>
        <div class="btn-group">
          <!-- 主操作与旁边次级钮同档同高（都是 .btn 基座 36px），不留"只升一颗"的高低差。 -->
          <button class="btn btn-primary" :disabled="exportBusy" @click="syncToSSH">
            {{ exportBusy ? '进行中…' : '同步给 ssh' }}
          </button>
          <button class="btn btn-secondary" :disabled="exportBusy" @click="onUndoExport">撤销导出</button>
        </div>
      </div>

      <UiBanner v-if="summary && !summary.included" tone="warn">
        顶层 config 还缺 Include 行——ssh 现在不会去读 <span class="mono">{{ summary.confPath }}</span>。
        点「同步给 ssh」时会补上这一段接线（只加自己那段，不碰别的内容）。
      </UiBanner>

      <UiBanner v-if="summary && summary.keyMissing > 0" tone="warn">
        ⚠ 有 {{ summary.keyMissing }} 条的私钥文件在本机不存在。
        这很正常，不是账本坏了：账本随 hanxi 数据根同步搬家，私钥文件**不在数据根里**——
        换机后清单必然先"找不到钥匙"。把私钥文件放回原路径后，重新对账本数字自动归零。
      </UiBanner>

      <!-- 产物与备份路径成功后常驻可见：这是"写到我的用户目录了"的凭据，可复制。 -->
      <div v-if="summary" class="hint-line">
        产物：<span class="mono">{{ summary.confPath }}</span>
        <button class="link-button" @click="copyWithToast(summary.confPath, '已复制产物路径')">复制</button>
        <span v-if="summary.rendered">· 导出 {{ summary.rendered }} 段</span>
      </div>
      <div v-if="summary?.backupPath" class="hint-line">
        上次覆盖前的备份：<span class="mono">{{ summary.backupPath }}</span>
        <button class="link-button" @click="copyWithToast(summary.backupPath, '已复制备份路径')">复制</button>
      </div>
      <div v-if="exportNote" class="hint-line" role="status">{{ exportNote }}</div>

      <UiBanner v-if="exportError" tone="error">⚠ {{ exportError }}</UiBanner>

      <!-- 待落盘 = 空态语法的下一步（不写"暂无数据"，也不塌成新状态）；
           只看 written/drift 布尔位，不解析 message 文本。 -->
      <div v-if="exportPending" class="hint-line">
        下一步：点右上「同步给 ssh」——ssh 现在读到的还不是这份账本。
      </div>
      <div class="hint-line">
        账本的增删改不会自己碰你的用户主目录——只有点「同步给 ssh」才会写文件；
        改了账本还没同步时，上面会一直亮着「产物还是旧的」等你处理。
      </div>
    </div>

    <!-- 新增/编辑表单：面板内展开（与连接测试面板的展开块同形制），
         后端拒绝的原文落回这里，草稿不丢。 -->
    <div v-show="formOpen" class="hosts-form">
      <div class="card-top">
        <h3>{{ editingAlias ? `编辑主机 ${editingAlias}` : '新增主机' }}</h3>
        <button type="button" class="btn btn-ghost btn-small" @click="closeForm">收起</button>
      </div>

      <div class="form-fields">
        <div class="form-item-block">
          <label class="form-label" for="hosts-alias">别名</label>
          <input
            id="hosts-alias" v-model="aliasField" class="input input-mono"
            :disabled="!!editingAlias" :readonly="aliasAuto"
            :placeholder="aliasAuto ? '将由地址自动生成' : 'nas01'"
            autocomplete="off" spellcheck="false"
            :aria-invalid="aliasShown ? 'true' : 'false'" aria-describedby="hosts-alias-msg"
            @blur="aliasTouched = true"
          />
          <label v-if="!editingAlias" class="toggle-label alias-custom-toggle">
            <input id="hosts-alias-custom" v-model="customAlias" type="checkbox" @change="onCustomAliasToggle" />
            我想自己起别名
          </label>
          <!-- 派生失败：自动弹开自定义框（上方勾选已亮），原因就地摊开——
               表单不卡进看不见理由的地方。文案讲机制：后端确实造不出合法名。 -->
          <p v-if="aliasPreviewError" id="hosts-alias-preview-fail" class="hint-line state-warning" role="status">
            ⚠ 这台机器的地址派生不出合法别名，请手工起一个。后端原话：{{ aliasPreviewError }}
          </p>
          <p id="hosts-alias-msg" class="field-msg" :class="{ 'field-error': !!aliasShown }">
            {{ aliasShown || (aliasAuto
              ? '不用填：后端从地址派生并去重（撞名自动加序号）——这就是 ssh 命令里敲的名字。想自己定就勾上「我想自己起别名」'
              : '小写字母、数字、_、-，最长 32；这就是 ssh 命令里敲的名字') }}
          </p>
        </div>

        <div class="form-item-block">
          <label class="form-label" for="hosts-host">地址</label>
          <input
            id="hosts-host" v-model="draft.host" class="input input-mono"
            placeholder="192.168.1.10 或 nas.example.com" autocomplete="off" spellcheck="false"
            :aria-invalid="hostShown ? 'true' : 'false'" aria-describedby="hosts-host-msg"
            @blur="onHostBlur"
          />
          <p id="hosts-host-msg" class="field-msg" :class="{ 'field-error': !!hostShown }">
            {{ hostShown || 'IP 或域名；更细的合法性由后端把关，存不进的话原话退回给你' }}
          </p>
        </div>

        <div class="form-item-block field-narrow">
          <label class="form-label" for="hosts-port">端口</label>
          <input
            id="hosts-port" v-model="draft.port" class="input input-mono"
            type="number" min="1" max="65535" placeholder="22"
            :aria-invalid="portShown ? 'true' : 'false'" aria-describedby="hosts-port-msg"
            @blur="portTouched = true"
          />
          <p id="hosts-port-msg" class="field-msg" :class="{ 'field-error': !!portShown }">
            {{ portShown || '留空按 22 记' }}
          </p>
        </div>

        <div class="form-item-block field-narrow">
          <label class="form-label" for="hosts-user">用户</label>
          <input
            id="hosts-user" v-model="draft.user" class="input input-mono"
            placeholder="deploy" autocomplete="off" spellcheck="false"
          />
          <p class="field-msg">
            留空将以<strong>本机</strong>用户名登录远端；账本随同步搬家后别的电脑本机名不同，建议显式填。
          </p>
        </div>

        <div class="form-item-block field-wide">
          <label class="form-label" for="hosts-keypath-select">私钥路径</label>
          <div class="keypath-row">
            <!-- 下拉值=suggestPath（`~/名字`，跨机器稳定；依据在后端扫描侧，勿改取
                 privatePath 或前端拼），输入框始终可纯手输——见脚本区用途注释。 -->
            <select
              id="hosts-keypath-select" class="select-input" :value="keySelectValue"
              @change="onKeyPathSelect($event)"
            >
              <option value="">（未指定——留空，走 agent/默认身份）</option>
              <option
                v-for="k in keySelectOptions" :key="k.name" :value="k.suggestPath"
              >{{ keyOptionLabel(k) }}</option>
              <option
                v-if="!keySelectOptions.length && ledgerReadFailed" disabled value="__read_failed__"
              >（本机密钥列表读取失败，可直接输入路径）</option>
              <option
                v-else-if="!keySelectOptions.length" disabled value="__no_keys__"
              >本机还没有密钥，可先去台账生成（也可直接输入路径）</option>
              <option :value="CUSTOM_KEYPATH" disabled>自定义（手动输入的路径，不在台账选项中）</option>
            </select>
            <input
              id="hosts-keypath" v-model="draft.keyPath" class="input input-mono"
              aria-label="私钥路径（可直接输入任意路径）"
              placeholder="C:\Users\你\.ssh\id_ed25519 或 ~/id_ed25519"
              autocomplete="off" spellcheck="false"
            />
          </div>
          <p class="field-msg">
            从下拉挑就是本机台账里的钥匙（`~/名字` 写法换机不漂）；也可以直接输入任意路径。
            只存路径，不读内容；留空表示这台不指定密钥。路径里的空格会让配置行只认首个 token，后端会拒。
          </p>
          <!-- 联动提示走既有原子 .hint-line + .state-warning 字色，不新造样式；
               只陈述台账事实 hasPublic=false，不是"钥匙缺失"判据（那是后端名单的事）。 -->
          <p v-if="selectedKeyLacksPub" id="hosts-keypath-hint" class="hint-line state-warning" role="status">
            ⚠ 这把还没有配套公钥，「部署公钥」对它不可用——请先去「密钥台账」点「补出公钥」。
          </p>
          <!-- 裸文件名（无 ~、无目录分隔）且台账确有同名私钥：这是确定性纠正，不是猜路径
               （修复值就是台账那行后端给的 suggestPath）；不硬拦保存，提示随值常驻。 -->
          <p v-if="bareKeyFix" id="hosts-keypath-bare-warn" class="hint-line state-warning" role="status">
            ⚠ 只写了文件名，ssh 会按当前目录解析、找不到这把钥匙。
            <button type="button" id="hosts-keypath-bare-fix" class="link-button" @click="draft.keyPath = bareKeyFix">改成 {{ bareKeyFix }}</button>
          </p>
        </div>

        <div class="form-item-block field-wide">
          <label class="form-label" for="hosts-note">备注</label>
          <input
            id="hosts-note" v-model="draft.note" class="input"
            placeholder="NAS 上的黑群晖" autocomplete="off"
          />
          <p class="field-msg">备注会渲染成导出配置里的注释行；换行不允许（那就是往配置里塞新行）。</p>
        </div>

        <label class="toggle-label">
          <input id="hosts-enabled" v-model="draft.enabled" type="checkbox" />
          启用（关掉即不导出给 ssh，账本保留）
        </label>
      </div>

      <p v-if="editingAlias" class="hint-line">
        别名是账本主键：后端的更新按别名整条覆盖，改别名等于换一把钥匙的账——所以编辑态这里锁死；
        要换名字，请新增一条再删掉旧的，两步都看得见。
      </p>

      <div v-if="formError" class="error-box">⚠ {{ formError }}</div>

      <div class="btn-group">
        <button class="btn btn-primary" :disabled="listBusy" @click="onSave">
          {{ listBusy ? '保存中…' : (editingAlias ? '保存' : '加入清单') }}
        </button>
        <button class="btn btn-secondary" @click="closeForm">取消</button>
      </div>
      <div class="hint-line">保存只改账本；要让 ssh 用上它，请再点上方「同步给 ssh」。</div>
    </div>

    <!-- 清单表格区 -->
    <div v-if="listError" class="error-box">{{ listError }}</div>

    <div v-else-if="listBusy && !hosts.length" class="empty-hint">正在读取主机清单…</div>

    <div v-else-if="!hosts.length" class="empty-state">
      <p>账本还是空的。</p>
      <p class="hint-line">
        在这里登记 SSH 机器：别名就是你以后 <code>ssh nas01</code> 用的名字。
        登记后还要显式导出（点「同步给 ssh」），才会写进本机的 <code>~/.ssh/hanxi.conf</code> 让 ssh 认得它们。
      </p>
      <p class="hint-line">
        清单存在 hanxi 数据根里、随同步搬家；私钥文件不跟着走——到新机器后，
        对账带会数出有几台"钥匙不在本机"，那是常态，跟着提示放好文件即可。
      </p>
      <button class="btn btn-primary" @click="openAdd">新增第一台</button>
    </div>

    <div v-else class="table-container">
      <table class="tbl">
        <thead>
          <tr>
            <th scope="col">
              启用
              <div class="th-sub">= 是否导出给 ssh</div>
            </th>
            <th scope="col">别名</th>
            <th scope="col">地址:端口</th>
            <th scope="col">用户</th>
            <th scope="col">密钥</th>
            <th scope="col">备注</th>
            <th scope="col">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="h in hosts" :key="h.alias">
            <td>
              <!-- 原生 checkbox：全仓没有 switch 原子，不私造。
                   title/aria-label 把"启用"说成它真正管的事（导出与否），不是模块开关。 -->
              <label class="toggle-label">
                <input
                  type="checkbox" class="row-toggle" :checked="h.enabled" :disabled="listBusy"
                  :title="ROW_TOGGLE_TITLE" :aria-label="rowToggleAria(h)" @change="onRowToggle(h, $event)"
                />
              </label>
              <!-- 停用行与对账带呼应：drift 亮着说明旧产物里这台还没被摘掉（复用 summary.drift，
                   不另造第二套状态判断）。 -->
              <div v-if="!h.enabled && summary && summary.drift" class="hint-dim cell-sub">
                还没重新导出：旧产物里这台仍在，ssh 暂时还认得它
              </div>
            </td>
            <td>
              <span class="mono cell-alias">{{ h.alias }}</span>
              <UiStatusChip v-if="!h.enabled" tone="neutral">已停用</UiStatusChip>
            </td>
            <td><span class="mono">{{ addrText(h) }}</span></td>
            <td>
              <span class="mono">{{ h.user || '—' }}</span>
              <div v-if="!h.user" class="hint-dim">留空 = ssh 用本机用户名</div>
            </td>
            <td>
              <template v-if="h.keyPath">
                <!-- 只显示基名：路径整列上屏占宽又读不出信息；完整路径进 title。 -->
                <span class="mono cell-keyname" :title="h.keyPath">{{ keyName(h) }}</span>
                <!-- 判据是后端逐条 stat 出来的名单（ExportSummary.missingKeyAliases），
                     不再由前端拿台账基名去猜——猜会把放在别处的真钥匙标成"找不到"，
                     而这顶帽子正挂在"能不能删这把"的判断现场。未对账过一律不标。
                     tone 走 warning 不走 danger：换机后钥匙不跟账本走是常态，不是故障。 -->
                <div v-if="keyMissing(h.alias)" class="cell-keyflag">
                  <UiStatusChip tone="warning">本机找不到这把钥匙</UiStatusChip>
                </div>
              </template>
              <span v-else class="hint-dim">未设</span>
            </td>
            <td><span class="cell-note" :title="h.note || undefined">{{ h.note || '—' }}</span></td>
            <td>
              <div class="btn-group">
                <button class="btn btn-secondary btn-micro" @click="openEdit(h)">编辑</button>
                <button class="btn btn-secondary btn-micro" @click="onRemove(h)">删除</button>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
      <p class="hint-line">
        共 {{ hosts.length }} 条 · 「启用」只管导出与否：「已停用」保留在账本里，但不会进入导出的 ssh 产物。
      </p>
    </div>
  </section>
</template>

<style scoped>
/* 只补布局，一切色彩/形制走全局原子。
   .card-top 与 KeyringPanel 的 scoped 副本同形，上收 components.css 未在册——两家各留
   一份本地档，不做跨组件私有样式引用。 */
.card-top {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 10px;
  flex-wrap: wrap;
}
.card-top h2, .card-top h3 { font-size: var(--text-md); font-weight: 600; margin: 0; }

/* 对账带：整块与表格区分层；中文长句行高放宽，不裁 CJK 笔画 */
.export-band {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 10px 12px;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-control);
  background: var(--surface-soft);
}
.export-band .hint-line, .hosts-form .hint-line { line-height: 1.7; }

/* 操作面板头行：左一句状态、右一组动作同层排；窄窗（<760）转竖排，不挤列。 */
.band-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  flex-wrap: wrap;
}
.band-status { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; min-width: 0; }

/* 表头小注与行内呼应只降字重/字号——色彩仍走全局原子，不在此造档。 */
.th-sub { font-weight: 400; font-size: var(--text-xs); color: var(--color-text-subtle); line-height: 1.5; }
.cell-sub { font-size: var(--text-xs); line-height: 1.6; max-width: 160px; }
.cell-keyflag { margin-top: 2px; }

@media (max-width: 760px) {
  .band-head { flex-direction: column; align-items: stretch; }
  .band-head .btn-group { justify-content: flex-start; }
}

/* 表单：面板内展开块；栅格列 minmax(0,1fr) 防长路径撑破 */
.hosts-form {
  display: flex;
  flex-direction: column;
  gap: 10px;
  padding: 12px 14px;
  border: 1px dashed var(--color-border);
  border-radius: var(--radius-control);
}
.form-fields { display: flex; flex-wrap: wrap; gap: 10px 14px; }
.form-fields .form-item-block { flex: 1 1 200px; min-width: 0; }
.form-fields .field-narrow { flex-grow: 0; flex-basis: 110px; }
.form-fields .field-wide { flex-basis: 100%; }

/* 输入档取仓内既有形状（WSL 面板家族同源值）；仓里没有全局 .input 原子，
   刻意不复制 portscan 家族的跨目录私有表，本地补一份最小等档。 */
.input {
  width: 100%;
  min-width: 0;
  background: var(--surface-page);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-chip);
  padding: 6px 10px;
  font-size: var(--text-sm);
  color: var(--color-text);
  font-family: inherit;
}
.input:focus { border-color: var(--color-primary); }
.input:focus-visible { outline: 2px solid var(--focus-ring); outline-offset: 1px; }
.input:disabled { background: var(--surface-hover); color: var(--color-text-subtle); cursor: not-allowed; }
.input-mono { font-family: var(--font-mono); }

.field-msg { margin: 0; font-size: var(--text-xs); color: var(--color-text-subtle); line-height: 1.6; }
.field-error { color: var(--state-danger); }

/* 「私钥路径」行：下拉+输入框同行，窄窗各自行内换行不挤压（minmax 0 防长路径撑破，
   与 .form-fields 同一手法）。只补布局与字档对齐——色彩档仍在全局 .select-input 与
   本页 .input 里，warning 字色走全局 .state-warning 原子，此处零彩色底。 */
.keypath-row { display: flex; flex-wrap: wrap; gap: 8px; }
.keypath-row .input { flex: 1 1 220px; }
/* 全局 select 档未管到字号/字色/圆角，这里对齐本页 .input 的最小等档（同用中性 token）。 */
.keypath-row .select-input {
  flex: 1 1 220px;
  min-width: 0;
  padding: 6px 10px;
  border-radius: var(--radius-chip);
  font-size: var(--text-sm);
  color: var(--color-text);
  font-family: inherit;
  background: var(--surface-page);
}
.keypath-row .select-input:focus { border-color: var(--color-primary); }
.keypath-row .select-input:focus-visible { outline: 2px solid var(--focus-ring); outline-offset: 1px; }

.cell-alias { font-weight: 600; }
.cell-note {
  display: inline-block;
  max-width: 240px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  vertical-align: bottom;
}
</style>
