<script setup lang="ts">
// WSL「➕ 添加实例」页签：三源统一新增入口（官方商店清单 / 本地 rootfs / 现有 VHDX 盘，
// Phase 6 式自 WSLView 拆分）。镜像站源刻意不做：第三方 rootfs 的信任链无法在本工具内把关。
// 商店清单（online）懒加载经 ensureOnlineLoaded() 由视图页签 watch 触发；
// 安装基目录（installDir）住视图（跨页签落位预填共享），经 v-model 回填。
// 行为逐字迁出：bindings 调用、确认文案与 busy 闸门的执行序列未做任何改动。
import { computed, ref, watch } from 'vue'
import * as WSLAPI from '../../../bindings/hanxi/internal/modules/wsl/wslservice'
import type { DistroOption, Report } from '../../../bindings/hanxi/internal/modules/wsl/readiness/models'
import { useToast } from '../../composables/useToast'
import { useConfirm } from '../../composables/useConfirm'
import { getErrorMessage } from '../../utils/errors'
import UiBanner from '../ui/UiBanner.vue'

const props = defineProps<{
  report: Report | null
  busyAny: boolean
  globalBusy: boolean
  busyWith: (op: string) => boolean
  // 全局互斥类操作编排（import 走 startOp/finishOp；商店安装走 runOp 白名单）
  startOp: (op: string) => void
  finishOp: (op: string) => void
  runOp: (opts: {
    name: string
    title: string
    desc: string
    tone?: 'default' | 'warning' | 'danger'
    confirmLabel?: string
    invoke: () => PromiseLike<unknown>
  }) => Promise<void>
  loadInstances: () => Promise<void>
  installDir: string
  previewSubdir: (base: string, id: string) => string
  pickFolderInto: (fill: (p: string) => void, title: string) => Promise<void>
}>()

const emit = defineEmits<{ 'update:installDir': [value: string] }>()

const { showToast } = useToast()
const { confirm } = useConfirm()

const installDirModel = computed({
  get: () => props.installDir,
  set: (value: string) => emit('update:installDir', value),
})

// 发行版安装被拦的预告（点之前就说清，别等 UAC 弹了再失败）：
// 虚拟机平台未启用，或已启用但 CBS 台账欠重启——WSL2 此刻都起不了虚拟机。
// 与后端 virtualizationGate 同源判据；体检未出结果时不猜测（返回空不显示）。
const distroBlockedReason = computed(() => {
  const r = props.report
  if (!r || !r.wslVersion) return ''
  if (!r.vmPlatformEnabled) return '「虚拟机平台」尚未启用，WSL2 承载不了发行版——现在点安装注定失败。请先回「🐧 就绪检测」页执行「🚀 一键开启」或「▶️ 开启虚拟机平台」。'
  if (r.rebootPending) return '「虚拟机平台」已启用但还没重启生效，WSL2 此刻起不了虚拟机——现在装发行版注定失败。重启一次再回来挑系统即可（注意：Task Manager 显示的"虚拟化已启用"是 BIOS 硬件位，与这个 Windows 功能开关是两回事）。'
  return ''
})

const addSource = ref<'store' | 'rootfs' | 'vhdx'>('store')
const addName = ref('')
const addFile = ref('')
// VHDX 两形态：false=就地注册（--import-in-place 零拷贝）；true=盘副本落位（--import … --vhd）。
const addVhdCopy = ref(false)
// 三源共用一个 addFile 输入：切源必清空——防止 tar 路径串进 VHDX 框（反之亦然）。
watch(addSource, () => { addFile.value = '' })
// 创建钮可用性：tar 必带落位目录；VHDX 就地挂载可免目录（盘留在原处）。
const canAddRootfs = computed(() =>
  !!addName.value.trim() && !!addFile.value.trim() && !!props.installDir.trim())
const canAddVhd = computed(() =>
  !!addName.value.trim() && !!addFile.value.trim() && (!addVhdCopy.value || !!props.installDir.trim()))

async function pickAddFile(kind: 'rootfs' | 'vhdx') {
  try {
    const p = await WSLAPI.PickDistroImageDialog(kind)
    if (p) addFile.value = p // 取消返回空串：静默，保留手填通道
  } catch (e) {
    showToast(`打开系统文件选择框失败: ${getErrorMessage(e)}——可直接在输入框手动填写路径`)
  }
}

// rootfs tar → wsl --import（免 UAC）；目录走全局安装基目录。导入是全局互斥类（整链落盘）。
async function submitImport() {
  if (props.busyAny) return
  const name = addName.value.trim()
  const dir = props.installDir.trim()
  const tar = addFile.value.trim()
  const accepted = await confirm({
    title: `导入新发行版 ${name}？`,
    tone: 'warning',
    description: '执行 wsl --import：解包 tar 建立新发行版实例（数十 GB 时耗时数分钟，请勿退出）。'
      + 'tar 须为本工具导出产物或可信来源 rootfs；落位子目录须为空或不存在；名称与本机名单防撞。'
      + '\n\n该操作以普通权限执行，不会弹出 UAC。',
    details: [
      { label: 'tar 源', value: tar },
      { label: '落位目录', value: props.previewSubdir(dir, name) },
    ],
  })
  if (!accepted) return
  props.startOp('import')
  try {
    const out = await WSLAPI.ImportDistro(name, dir, tar)
    showToast(out?.message || '导入完成')
    addName.value = ''
    addFile.value = ''
  } catch (e) {
    showToast(`导入失败: ${getErrorMessage(e)}`, { duration: 8000 })
  } finally {
    props.finishOp('import')
    await props.loadInstances() // 导入可能部分生效：复采为准
  }
}

// 现有 VHDX 盘 → 新实例：就地注册（零拷贝，盘留在原处）或复制落位。
async function submitImportVhd() {
  if (props.busyAny) return
  const name = addName.value.trim()
  const dir = props.installDir.trim()
  const vhdx = addFile.value.trim()
  const copy = addVhdCopy.value
  const accepted = await confirm({
    title: `挂载 VHDX 为新发行版 ${name}？`,
    tone: 'warning',
    description: copy
      ? '执行 wsl --import … --vhd：微软语义即把数据盘【复制】到安装位置下的同名子目录（数十 GB 时耗时较长）。'
        + '适合接管备份盘且想换位置的场合；原盘保持不动。'
      : '执行 wsl --import-in-place：就地注册，零拷贝——该 VHDX 文件从此就是发行版的数据盘，'
        + '移动/删除它会直接伤及实例（想换位置请用挂载后的「🧭 迁移」）。',
    details: [
      { label: 'VHDX 盘', value: vhdx },
      ...(copy ? [{ label: '副本落位', value: props.previewSubdir(dir, name) }] : []),
    ],
  })
  if (!accepted) return
  props.startOp('import')
  try {
    const out = await WSLAPI.ImportDistroVhd(name, dir, vhdx, copy)
    showToast(out?.message || '挂载完成')
    addName.value = ''
    addFile.value = ''
  } catch (e) {
    showToast(`挂载失败: ${getErrorMessage(e)}`, { duration: 8000 })
  } finally {
    props.finishOp('import')
    await props.loadInstances()
  }
}

// ---------- 官方商店清单（wsl --list --online；懒加载由视图页签 watch 驱动） ----------
const online = ref<DistroOption[]>([])
const onlineLoading = ref(false)
const onlineError = ref('')

async function loadOnline() {
  onlineLoading.value = true
  onlineError.value = ''
  try {
    online.value = (await WSLAPI.ListOnlineDistros()) ?? []
  } catch (e) {
    online.value = []
    onlineError.value = `获取在线发行版清单失败: ${getErrorMessage(e)}\n该清单由本机 wsl.exe 提供：未装 WSL 或版本过旧时，请先在「就绪检测」页完成一键开启并重启。`
  } finally {
    onlineLoading.value = false
  }
}

async function installDistro(opt: DistroOption) {
  const dir = props.installDir.trim()
  await props.runOp({
    name: `distro-${opt.id}`,
    title: `安装发行版 ${opt.id}？`,
    tone: 'default',
    desc: `执行 wsl --install -d ${opt.id}：下载安装后首次进入该发行版需创建 Linux 用户名与密码。`
      + (dir
          ? `\n\n装完将自动迁移落位到：${props.previewSubdir(dir, opt.id)}\n（基目录 + 同名子目录；同一条提权链一次 UAC 完成——wsl --install 本身不支持指定目录，"装完即迁"是唯一正规通道。）`
          : '\n\n当前安装基目录留空——将装到系统默认位置（通常在 C 盘）。想避开 C 盘，请在上方「安装基目录」行填写（默认 D:\\wsl），改动会自动记住。'),
    invoke: () => WSLAPI.InstallDistroTo(opt.id, dir),
  })
}

defineExpose({
  // 首次切到本页才拉数据（在线清单依赖本机命令，冷页避免无谓探测）——判据与拆分前逐字一致。
  ensureOnlineLoaded() {
    if (online.value.length === 0 && !onlineLoading.value && !onlineError.value) {
      loadOnline()
    }
  },
})
</script>

<template>
  <div class="install-panel">
    <div class="move-input-row">
      <label class="move-label">来源类型</label>
      <div class="btn-group">
        <button class="btn btn-secondary btn-small" :class="{ active: addSource === 'store' }" :disabled="busyAny" @click="addSource = 'store'">🛒 官方商店发行版</button>
        <button class="btn btn-secondary btn-small" :class="{ active: addSource === 'rootfs' }" :disabled="busyAny" @click="addSource = 'rootfs'">📄 本地 rootfs（tar）</button>
        <button class="btn btn-secondary btn-small" :class="{ active: addSource === 'vhdx' }" :disabled="busyAny" @click="addSource = 'vhdx'">💽 现有 VHDX 发行盘</button>
      </div>
    </div>
    <!-- 安装基目录三源共用；VHDX 就地挂载不动盘，故隐藏该行 -->
    <div v-if="addSource !== 'vhdx' || addVhdCopy" class="move-input-row">
      <label class="move-label" for="wsl-install-dir">安装基目录</label>
      <input id="wsl-install-dir" v-model="installDirModel" class="input mono"
        placeholder="例：D:\wsl" spellcheck="false" :disabled="globalBusy" />
      <button class="btn btn-secondary btn-small" :disabled="globalBusy"
        title="调系统文件夹选择框：选好即回填，仍可手动修改" @click="pickFolderInto((p) => installDirModel = p, '选择安装基目录')">📁 选目录</button>
    </div>
    <UiBanner v-if="!installDir.trim() && (addSource !== 'vhdx' || addVhdCopy)" tone="warn" class="slim">
      ⚠ 留空 = 用系统默认位置，新实例通常落在 C 盘
    </UiBanner>
    <div v-if="addSource !== 'vhdx' || addVhdCopy" class="hint-line">安装基目录按「基目录」使用：每个实例自动落在其下<b>同名子目录</b>（如 D:\wsl\Ubuntu；末级已是实例名则不重复追加）。须为本机绝对路径且所在盘存在；改动自动记住。</div>
  </div>

  <!-- 源①：官方商店清单（wsl --install 白名单 + 装完即迁落位，同一条提权链一次 UAC）。
       表格逐行「⬇ 安装」是唯一动线；空态自带「↻ 重新查询」（刷新钮不再只活在成功分支）。 -->
  <div v-if="addSource === 'store'" class="install-panel">
    <UiBanner v-if="distroBlockedReason" tone="warn" class="slim distro-block-banner">{{ distroBlockedReason }}</UiBanner>
    <div class="hint-line">安装落位：<b>{{ installDir.trim() || '系统默认（通常在 C 盘）' }}</b> 下的同名子目录——改基目录见上方「安装基目录」行，改动自动记住。</div>
    <div class="section-title distro-head">
      <h3>可安装的官方发行版 ({{ online.length }})</h3>
      <div class="btn-group distro-head-actions">
        <button class="btn btn-secondary btn-small" :disabled="onlineLoading" @click="loadOnline">
          {{ onlineLoading ? '查询中…' : '↻ 刷新清单' }}
        </button>
      </div>
    </div>
    <div v-if="onlineLoading" class="hint-line">正在向本机 wsl.exe 查询在线清单…</div>
    <div v-else-if="onlineError" class="error-box">{{ onlineError }}
      <button class="btn btn-secondary btn-small retry-inline" @click="loadOnline">↻ 重试</button>
    </div>
    <div v-else-if="!online.length" class="empty-state">
      <p>清单未加载——它由本机 wsl.exe 提供（需 WSL 本体已装好）。
        <button class="btn btn-secondary btn-small retry-inline" @click="loadOnline">↻ 重新查询</button></p>
    </div>
    <div v-else class="table-container">
      <table class="tbl">
        <thead>
          <tr>
            <th style="width: 220px;">ID</th>
            <th>名称</th>
            <th style="width: 110px;">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="opt in online" :key="opt.id">
            <td><code class="mono">{{ opt.id }}</code></td>
            <td>{{ opt.label }}</td>
            <td>
              <button class="btn btn-secondary btn-small" :disabled="busyAny || !!distroBlockedReason"
                :title="distroBlockedReason || `wsl --install -d ${opt.id}（UAC 提权）`"
                @click="installDistro(opt)">⬇ 安装</button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
    <div class="hint-line">下载体量较大多半要几分钟；首次进入发行版需创建 Linux 用户名与密码。</div>
  </div>

  <!-- 源②：本地 rootfs tar（wsl --import，免 UAC） -->
  <div v-else-if="addSource === 'rootfs'" class="install-panel">
    <UiBanner tone="info" class="slim">
      导入 = <code class="mono">wsl --import</code>：把本工具导出产物或可信 rootfs tar 解包落成新增实例（免 UAC）。
      落位子目录须为空或不存在；名称与本机名单防撞；大 tar 解包耗时数分钟，期间请勿退出。
    </UiBanner>
    <div class="move-input-row">
      <label class="move-label" for="wsl-add-name">实例名称</label>
      <input id="wsl-add-name" v-model="addName" class="input mono" placeholder="MyDistro" spellcheck="false" :disabled="busyAny" />
    </div>
    <div class="move-input-row">
      <label class="move-label" for="wsl-add-file">tar 文件路径</label>
      <input id="wsl-add-file" v-model="addFile" class="input mono" placeholder="导出工件或 rootfs tar 的完整路径（「本会话导出记录」处可复制）" spellcheck="false" :disabled="busyAny"
        @keyup.enter="canAddRootfs && submitImport()" />
      <button class="btn btn-secondary btn-small" :disabled="busyAny" title="调系统文件选择框挑选 tar（也可手动填写路径）" @click="pickAddFile('rootfs')">📁 浏览</button>
    </div>
    <div class="move-input-row">
      <button class="btn btn-primary btn-small" :disabled="!canAddRootfs || busyAny" @click="submitImport">
        {{ busyWith('import') ? '导入中…' : '✔ 创建（解包落位）' }}
      </button>
      <span v-if="addName.trim()" class="hint-dim">落位：{{ previewSubdir(installDir.trim(), addName.trim()) || '请先填写安装基目录' }}</span>
    </div>
  </div>

  <!-- 源③：现有 VHDX 发行盘（--import-in-place 零拷贝 / --import … --vhd 复制落位，免 UAC） -->
  <div v-else class="install-panel">
    <UiBanner tone="info" class="slim">
      挂载 = 把现成的 ext4 发行盘（「🗜 瘦身」备份盘、别机带来的 ext4.vhdx 等）落成新增实例（免 UAC；要求 WSL 2.7.3+）。
      <b>就地挂载零拷贝</b>——该文件从此就是实例的数据盘，移动/删除它即伤及实例；换位置请用挂载后的「🧭 迁移」。
    </UiBanner>
    <div class="move-input-row">
      <label class="move-label" for="wsl-add-name">实例名称</label>
      <input id="wsl-add-name" v-model="addName" class="input mono" placeholder="MyDistro" spellcheck="false" :disabled="busyAny" />
    </div>
    <div class="move-input-row">
      <label class="move-label" for="wsl-add-file">VHDX 盘路径</label>
      <input id="wsl-add-file" v-model="addFile" class="input mono" placeholder="ext4.vhdx 等发行盘的完整路径" spellcheck="false" :disabled="busyAny"
        @keyup.enter="canAddVhd && submitImportVhd()" />
      <button class="btn btn-secondary btn-small" :disabled="busyAny" title="调系统文件选择框挑选 VHDX（也可手动填写路径）" @click="pickAddFile('vhdx')">📁 浏览</button>
    </div>
    <div class="move-input-row">
      <label class="move-label">挂载方式</label>
      <label class="radio-label"><input v-model="addVhdCopy" type="radio" :value="false" :disabled="busyAny" /> 就地挂载（零拷贝，推荐）</label>
      <label class="radio-label"><input v-model="addVhdCopy" type="radio" :value="true" :disabled="busyAny" /> 复制盘到安装目录（原盘不动）</label>
    </div>
    <div class="move-input-row">
      <button class="btn btn-primary btn-small" :disabled="!canAddVhd || busyAny" @click="submitImportVhd">
        {{ busyWith('import') ? '挂载中…' : '✔ 创建实例' }}
      </button>
      <span v-if="addVhdCopy && addName.trim()" class="hint-dim">副本落位：{{ previewSubdir(installDir.trim(), addName.trim()) || '请先填写安装基目录' }}</span>
    </div>
  </div>
</template>

<style scoped>
/* 全局原子落 components.css；以下为本页签独有或有意补差（注释注明）。 */

/* 多行诊断文案（换行符保留）——对全局 .error-box 的补差白名单行，非同名副本 */
.error-box { white-space: pre-line; }
/* 基形（字号/颜色/左内距）落回全局 .hint-line；此处仅留 WSL 档补差两行 */
.hint-line { line-height: 1.7; white-space: pre-line; }
/* 全局 .hint-dim 只定义颜色，WSL 注记统一配 --text-sm 小字——补差，非副本 */
.hint-dim { font-size: var(--text-sm); }
/* .banner.slim 等值副本已删净：UiBanner 根元素挂 .banner + .slim，落回全局 :where(.banner.slim) */
.retry-inline { margin-left: 10px; }
/* display/gap 落回全局 .btn-group；此处仅留换行补差 */
.btn-group { flex-wrap: wrap; }

/* 表头行（与本机发行版控制台同形——§③ 上收候选） */
.distro-head { display: flex; align-items: center; justify-content: space-between; gap: 10px; flex-wrap: wrap; }
.distro-head-actions { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }

.radio-label { display: inline-flex; align-items: center; gap: 5px; font-size: var(--text-sm); color: var(--color-text); cursor: pointer; }
.radio-label input[type='radio'] { accent-color: var(--color-primary); margin: 0; }

/* 安装落位目录面板 */
.install-panel {
  display: flex; flex-direction: column; gap: 8px; padding: 10px 12px;
  background: var(--surface-panel); border: 1px solid var(--color-border); border-radius: var(--radius-control);
}
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
/* .btn.active 为"开关钮选中态"补差（全局 .btn 家族无此态）——非副本 */
.btn.active { border-color: var(--color-primary); color: var(--color-primary); }
</style>
