<script setup lang="ts">
// 托盘/轮盘条目编辑面（N5-C1 抽自设置页 TraySection）：候选条目勾选 / 外部程序添加 /
// 二级分组编辑 / 排序与移除 / 脏标记保存 / 失败回滚重拉，交互语义逐字搬移不改行为。
// 两宿主同挂：「设置→托盘右键菜单」（TraySection）与「快捷菜单轮盘页」（QuickMenuView）
// 编辑的本来就是同一份 settings.TrayMenu 账，配置一处改两面生效。
// 数据源与保存 RPC 保持现契约（AppService.ListTrayMenuOptions/GetTrayMenu/SetTrayMenu），
// 保存即经 SetTrayMenu 重建原生托盘菜单；成功上抛 saved 供宿主自行重拉（如轮盘页刷新
// ListItems 预览）——跨页事件化 tray:changed 属后续批次决策，本次不新增全局事件。
// 照搬现状的已知病灶：isOptionAdded 只判顶层条目，组内重复引用不判重（后续批次处理）。
// 按钮家族不保留 scoped 副本，直接落回 components.css 全局 :where 标准形。
import { ref, computed, watch, nextTick, onMounted } from 'vue'
import * as AppAPI from '../../../bindings/hanxi/internal/app'
import type { TrayMenuOption } from '../../../bindings/hanxi/internal/app/models'
import type { TrayMenuItem } from '../../../bindings/hanxi/internal/settings/models'
import { getErrorMessage } from '../../utils/errors'
import { useToast } from '../../composables/useToast'
import AppIcon from '../ui/AppIcon.vue'

const emit = defineEmits<{
  (e: 'saved'): void
}>()

// —— N5-C3 点上格互换（读侧：宿主把预览选中扇区的身份喂进来定位行；写侧：行内互换钮）——
// 身份匹配（type+hint / 组按 label）而非下标换算：编辑列里可能存在被后端 wheelView
// 滤掉的停用模块行，相对序会错位；扁平拍平形态下组子条目占多扇区，也一律归位到
// 组行。映射逻辑收在本组件（唯一持有 trayItems 的人），宿主只转发"用户点了哪个扇区"。
const selectedRow = ref<number | null>(null)

const rowRefs = ref<Record<number, HTMLElement | undefined>>({})
function setRowRef(i: number, el: unknown) {
  rowRefs.value[i] = el as HTMLElement | undefined
}

const normHint = (s: string) => s.trim().toLowerCase().replace(/\//g, '\\')
function leafMatches(r: TrayMenuItem, type: string, hint: string): boolean {
  return r.type === type && normHint(r.type === 'exe' ? r.path : r.ref) === normHint(hint)
}
function rowIndexOfSector(type: string, hint: string, label: string): number | null {
  for (let i = 0; i < trayItems.value.length; i++) {
    const r = trayItems.value[i]
    if (r.type === 'group') {
      if (type === 'group' && r.label === label) return i
      if ((r.children ?? []).some((ch) => leafMatches(ch, type, hint))) return i
      continue
    }
    if (leafMatches(r, type, hint)) return i
  }
  return null
}

/** 宿主预览点击入口：定位并高亮对应编辑行（滚到可见）；命中返回 true。 */
function locate(sector: { type: string; hint: string; label: string }): boolean {
  const i = rowIndexOfSector(sector.type, sector.hint ?? '', sector.label ?? '')
  if (i == null) return false
  selectedRow.value = i
  nextTick(() => rowRefs.value[i]?.scrollIntoView({ block: 'nearest', behavior: 'smooth' }))
  return true
}

/** 清除选中（宿主在条目列表刷新后调用，防错位选中残留）。 */
function clearSelection() {
  selectedRow.value = null
}
defineExpose({ locate, clearSelection })

function swapRows(i: number) {
  const j = selectedRow.value
  if (j == null || j < 0 || j >= trayItems.value.length || j === i) return
  const arr = trayItems.value
  ;[arr[i], arr[j]] = [arr[j], arr[i]]
  selectedRow.value = i // 选中跟着"扇区内容"走：互换后被点扇区的内容落在被点击的行 i
  openGroup.value = -1 // 与 moveItem 同纪律：下标错位防护
}

const { showToast } = useToast()

const trayLoading = ref(true)
const trayOptions = ref<TrayMenuOption[]>([])
const trayItems = ref<TrayMenuItem[]>([])
const savingTray = ref(false)
const trayDirty = ref(false)
const pickingExe = ref(false)
const exeForm = ref({ path: '', args: '', label: '' })

// —— 二级分组编辑（group 条目：轮盘外扩子环，托盘呈现原生子菜单）——
const openGroup = ref(-1) // 展开编辑中的分组下标（-1 无；增删/移动行后复位防错位）
const childPick = ref<Record<number, string>>({}) // 各组"从候选添加"下拉的暂存键

function ensureKids(g: TrayMenuItem): TrayMenuItem[] {
  if (!g.children) g.children = []
  return g.children
}

function addGroup() {
  trayItems.value.push({ type: 'group', ref: '', path: '', args: '', label: '', enabled: true, children: [] })
  openGroup.value = trayItems.value.length - 1
}

function addChildFromOption(g: TrayMenuItem, gi: number) {
  const opt = trayOptions.value.find(o => `${o.type}|${o.ref}` === childPick.value[gi])
  if (!opt) return
  ensureKids(g).push({ type: opt.type, ref: opt.ref, path: '', args: '', label: '', enabled: true })
  childPick.value[gi] = ''
}

async function addChildExe(g: TrayMenuItem) {
  pickingExe.value = true
  try {
    const p = await AppAPI.AppService.PickExeFile()
    if (p) ensureKids(g).push({ type: 'exe', ref: '', path: p, args: '', label: '', enabled: true }) // 取消时为空串
  } catch (e: unknown) {
    showToast(`打开文件选择框失败: ${getErrorMessage(e)}`)
  } finally {
    pickingExe.value = false
  }
}

function moveChild(g: TrayMenuItem, j: number, delta: number) {
  const kids = g.children ?? []
  const k = j + delta
  if (k < 0 || k >= kids.length) return
  ;[kids[j], kids[k]] = [kids[k], kids[j]]
}

function removeChild(g: TrayMenuItem, j: number) {
  g.children?.splice(j, 1)
}

// 候选项默认标签索引：type|ref → label（行内自定义名为空时回退展示）
const optionLabels = computed(() => {
  const map = new Map<string, string>()
  for (const opt of trayOptions.value) map.set(`${opt.type}|${opt.ref}`, opt.label)
  return map
})

watch(trayItems, () => { trayDirty.value = true }, { deep: true })

async function refreshTray() {
  trayLoading.value = true
  try {
    const [opts, items] = await Promise.all([
      AppAPI.AppService.ListTrayMenuOptions(),
      AppAPI.AppService.GetTrayMenu(),
    ])
    trayOptions.value = opts ?? []
    trayItems.value = (items ?? []).map(i => ({ ...i }))
    await nextTick() // 等 watcher 冲刷后再复位脏标记，避免加载即"未保存"
    trayDirty.value = false
  } catch (e: unknown) {
    showToast(`获取托盘配置失败: ${getErrorMessage(e)}`)
  } finally {
    trayLoading.value = false
  }
}

function exeBaseName(p: string): string {
  const base = p.split(/[\\/]/).pop() || p
  return base.replace(/\.[^.\\/]+$/, '')
}

function defaultLabelFor(item: TrayMenuItem): string {
  if (item.type === 'exe') return exeBaseName(item.path) || '外部程序'
  return optionLabels.value.get(`${item.type}|${item.ref}`) || item.ref
}

function typeLabel(type: string): string {
  if (type === 'command') return '工具命令'
  if (type === 'route') return '页面'
  if (type === 'group') return '分组'
  return '外部程序'
}

function isOptionAdded(opt: TrayMenuOption): boolean {
  return trayItems.value.some(i => i.type === opt.type && i.ref === opt.ref)
}

function toggleOption(opt: TrayMenuOption) {
  const idx = trayItems.value.findIndex(i => i.type === opt.type && i.ref === opt.ref)
  if (idx >= 0) {
    trayItems.value.splice(idx, 1)
  } else {
    trayItems.value.push({ type: opt.type, ref: opt.ref, path: '', args: '', label: '', enabled: true })
  }
}

function moveItem(i: number, delta: number) {
  const j = i + delta
  const arr = trayItems.value
  if (j < 0 || j >= arr.length) return
  ;[arr[i], arr[j]] = [arr[j], arr[i]]
  openGroup.value = -1 // 下标错位防护：移动后不再认定原展开组
  selectedRow.value = null // C3 选中同样按位置索引持有，移动后不作废即错指
}

function removeItem(i: number) {
  trayItems.value.splice(i, 1)
  openGroup.value = -1
  selectedRow.value = null
}

async function browseExe() {
  pickingExe.value = true
  try {
    const p = await AppAPI.AppService.PickExeFile()
    if (p) exeForm.value.path = p // 取消时为空串，保持现值
  } catch (e: unknown) {
    showToast(`打开文件选择框失败: ${getErrorMessage(e)}`)
  } finally {
    pickingExe.value = false
  }
}

function addExe() {
  const path = exeForm.value.path.trim()
  if (!path) {
    showToast('请先填写或选择程序路径')
    return
  }
  trayItems.value.push({
    type: 'exe',
    ref: '',
    path,
    args: exeForm.value.args.trim(),
    label: exeForm.value.label.trim(),
    enabled: true,
  })
  exeForm.value = { path: '', args: '', label: '' }
}

async function saveTrayMenu() {
  savingTray.value = true
  try {
    await AppAPI.AppService.SetTrayMenu(trayItems.value)
    trayDirty.value = false
    showToast('托盘右键菜单已保存并即时生效')
    emit('saved') // 宿主自行决定重拉（轮盘页刷新 ListItems 预览；设置页宿主可忽略）
  } catch (e: unknown) {
    showToast(`保存托盘菜单失败: ${getErrorMessage(e)}`)
    await refreshTray()
  } finally {
    savingTray.value = false
  }
}

onMounted(refreshTray)
</script>

<template>
  <div class="tray-editor">
    <div v-if="trayLoading" class="state-box">正在加载托盘配置…</div>

    <template v-else>
      <!-- 当前菜单条目（顺序即右键菜单/轮盘显示顺序；group 行可展开子条目编辑） -->
      <div v-if="trayItems.length > 0" class="tray-list">
        <template v-for="(item, i) in trayItems" :key="`${item.type}|${item.ref}|${item.path}|${i}`">
          <!-- 分组行：轮盘二级扇区 / 托盘子菜单 -->
          <div v-if="item.type === 'group'" class="tray-row tray-row-group" :class="{ 'tray-selected': selectedRow === i }" :ref="el => setRowRef(i, el)">
            <div class="tray-row-main">
              <span class="tray-tag tray-tag-group">分组</span>
              <input
                class="tray-input tray-name"
                v-model="item.label"
                placeholder="分组名（必填）"
                maxlength="30"
                title="分组显示名：轮盘外扩子环标题与托盘子菜单标题"
              />
              <code class="tray-ref">{{ (item.children?.length ?? 0) }} 个子条目 · 轮盘外扩子环 / 托盘子菜单</code>
            </div>
            <div class="setting-actions">
              <button class="btn btn-secondary btn-small" :class="{ 'tray-active': openGroup === i }" @click="openGroup = openGroup === i ? -1 : i">
                {{ openGroup === i ? '收起子条目' : '子条目' }}
              </button>
              <button v-if="selectedRow != null && selectedRow !== i" class="btn btn-secondary btn-small" @click="swapRows(i)" :title="`与选中的第 ${selectedRow + 1} 行互换位置（预览盘点对应扇区即选中）`">⇄ 互换</button>
              <button class="btn btn-secondary btn-small" :disabled="i === 0" @click="moveItem(i, -1)" title="上移" aria-label="上移"><AppIcon name="chevron-up" :size="14" /></button>
              <button class="btn btn-secondary btn-small" :disabled="i === trayItems.length - 1" @click="moveItem(i, 1)" title="下移" aria-label="下移"><AppIcon name="chevron-down" :size="14" /></button>
              <button class="btn btn-secondary btn-small tray-remove" @click="removeItem(i)" title="删除分组及其子条目"><AppIcon name="x" :size="14" /> 移除</button>
            </div>
          </div>
          <div v-if="item.type === 'group' && openGroup === i" class="tray-children">
            <div
              v-for="(ch, j) in item.children ?? []"
              :key="`child-${ch.type}|${ch.ref}|${ch.path}|${j}`"
              class="tray-child-row"
            >
              <span class="tray-tag" :class="`tray-tag-${ch.type}`">{{ typeLabel(ch.type) }}</span>
              <input
                class="tray-input tray-name"
                v-model="ch.label"
                :placeholder="defaultLabelFor(ch)"
                maxlength="30"
                title="自定义显示名，留空使用默认名称"
              />
              <code class="tray-ref" :title="ch.type === 'exe' ? ch.path : ch.ref">
                {{ ch.type === 'exe' ? ch.path : ch.ref }}<template v-if="ch.type === 'exe' && ch.args"> {{ ch.args }}</template>
              </code>
              <div class="setting-actions">
                <button class="btn btn-secondary btn-small" :disabled="j === 0" @click="moveChild(item, j, -1)" title="子条目上移" aria-label="子条目上移"><AppIcon name="chevron-up" :size="14" /></button>
                <button class="btn btn-secondary btn-small" :disabled="j >= (item.children?.length ?? 0) - 1" @click="moveChild(item, j, 1)" title="子条目下移" aria-label="子条目下移"><AppIcon name="chevron-down" :size="14" /></button>
                <button class="btn btn-secondary btn-small tray-remove" @click="removeChild(item, j)" title="移除子条目" aria-label="移除子条目"><AppIcon name="x" :size="14" /></button>
              </div>
            </div>
            <div v-if="!(item.children?.length)" class="tray-child-empty">分组还没有子条目（保存会被拒绝）：从下方添加。</div>
            <div class="tray-child-add">
              <select class="tray-input tray-child-select" v-model="childPick[i]">
                <option value="" disabled>从候选条目选择…</option>
                <option v-for="opt in trayOptions" :key="`${opt.type}|${opt.ref}`" :value="`${opt.type}|${opt.ref}`">
                  {{ opt.label }}{{ opt.moduleName ? `（${opt.moduleName}）` : '' }}
                </option>
              </select>
              <button class="btn btn-secondary btn-small" :disabled="!childPick[i]" @click="addChildFromOption(item, i)"><AppIcon name="plus" :size="14" /> 子条目</button>
              <button class="btn btn-secondary btn-small" :disabled="pickingExe" @click="addChildExe(item)"><AppIcon name="folder" :size="14" /> 外部程序…</button>
            </div>
          </div>
          <!-- 普通叶子行（勿用 v-else：与分组行之间隔着子条目编辑面板，会断链误渲染） -->
          <div v-if="item.type !== 'group'" class="tray-row" :class="{ 'tray-selected': selectedRow === i }" :ref="el => setRowRef(i, el)">
            <div class="tray-row-main">
              <span class="tray-tag" :class="`tray-tag-${item.type}`">{{ typeLabel(item.type) }}</span>
              <input
                class="tray-input tray-name"
                v-model="item.label"
                :placeholder="defaultLabelFor(item)"
                maxlength="30"
                title="自定义菜单显示名，留空使用默认名称"
              />
              <code class="tray-ref" :title="item.type === 'exe' ? item.path : item.ref">
                {{ item.type === 'exe' ? item.path : item.ref }}<template v-if="item.type === 'exe' && item.args"> {{ item.args }}</template>
              </code>
            </div>
            <div class="setting-actions">
              <button v-if="selectedRow != null && selectedRow !== i" class="btn btn-secondary btn-small" @click="swapRows(i)" :title="`与选中的第 ${selectedRow + 1} 行互换位置（预览盘点对应扇区即选中）`">⇄ 互换</button>
              <button class="btn btn-secondary btn-small" :disabled="i === 0" @click="moveItem(i, -1)" title="上移" aria-label="上移"><AppIcon name="chevron-up" :size="14" /></button>
              <button class="btn btn-secondary btn-small" :disabled="i === trayItems.length - 1" @click="moveItem(i, 1)" title="下移" aria-label="下移"><AppIcon name="chevron-down" :size="14" /></button>
              <button class="btn btn-secondary btn-small tray-remove" @click="removeItem(i)" title="从托盘菜单移除"><AppIcon name="x" :size="14" /> 移除</button>
            </div>
          </div>
        </template>
      </div>
      <div v-else class="state-box">尚未配置托盘条目：在下方「可选条目」中勾选、添加外部程序，或新建分组。</div>

      <div class="tray-tools">
        <button class="btn btn-secondary btn-small" @click="addGroup"><AppIcon name="plus" :size="14" /> 新建分组（轮盘二级扇区）</button>
        <span class="hint">分组在轮盘中悬停展开外扩子环（点击可钉住；可在快捷菜单页关闭二级、改为拍平），在托盘中呈现子菜单。子条目建议 ≤4，超过 8 轮盘仅显示前 8 项。</span>
      </div>

      <!-- 外部程序添加 -->
      <div class="exe-form">
        <span class="exe-form-title">添加外部程序</span>
        <div class="exe-form-row">
          <input class="tray-input tray-exe-path" v-model="exeForm.path" placeholder="程序绝对路径，如 C:\Tools\app.exe 或桌面快捷方式 .lnk" />
          <button class="btn btn-secondary btn-small" :disabled="pickingExe" @click="browseExe"><AppIcon name="folder" :size="14" /> 浏览…</button>
        </div>
        <div class="exe-form-row">
          <input class="tray-input tray-exe-args" v-model="exeForm.args" placeholder="启动参数（可选，空格分隔，引号可包空格）" />
          <input class="tray-input tray-exe-label" v-model="exeForm.label" placeholder="菜单名称（可选）" maxlength="30" />
          <button class="btn btn-secondary btn-small" @click="addExe"><AppIcon name="plus" :size="14" /> 添加</button>
        </div>
      </div>

      <!-- 候选条目目录 -->
      <div class="option-block">
        <span class="exe-form-title">可选条目</span>
        <div class="option-grid">
          <label v-for="opt in trayOptions" :key="`${opt.type}|${opt.ref}`" class="option-item" :title="opt.moduleName ? `来自模块：${opt.moduleName}` : opt.ref">
            <input type="checkbox" class="switch" :checked="isOptionAdded(opt)" @change="toggleOption(opt)" />
            <span class="option-label">{{ opt.label }}</span>
          </label>
        </div>
        <div v-if="trayOptions.length === 0" class="state-box">暂无候选条目（托管工具命令与模块页面导航均为空）。</div>
      </div>

      <div class="tray-footer">
        <span class="hint">修改后需点击保存；条目对应模块被禁用时，点击菜单项会收到错误提示。</span>
        <button class="btn btn-primary" :disabled="savingTray || !trayDirty" @click="saveTrayMenu">
          {{ savingTray ? '保存中…' : '保存托盘菜单' }}
        </button>
      </div>
    </template>
  </div>
</template>

<style scoped>
.tray-editor { display: flex; flex-direction: column; gap: 12px; }

.tray-list { display: flex; flex-direction: column; gap: 8px; }
.tray-row {
  background: var(--surface-page); border: 1px solid var(--color-border); border-radius: var(--radius-control);
  padding: 8px 12px; display: flex; justify-content: space-between; align-items: center; gap: 12px; flex-wrap: wrap;
}
.tray-row-group { border-color: var(--color-border-strong); background: var(--surface-panel); }
/* N5-C3：预览盘选中扇区对应行的双向定位高亮 */
.tray-selected { border-color: var(--color-primary); box-shadow: inset 3px 0 0 var(--color-primary); }
.tray-row-main { display: flex; align-items: center; gap: 8px; min-width: 0; flex: 1; }
.tray-tag {
  font-size: var(--text-micro); color: var(--color-text-muted); background: var(--surface-hover);
  padding: 1px 6px; border-radius: 4px; white-space: nowrap; border: 1px solid var(--color-border);
}
.tray-tag-command { color: var(--color-primary); border-color: var(--color-primary); }
.tray-tag-group { color: var(--color-primary); border-color: var(--color-primary); }
.tray-active { color: var(--color-primary); border-color: var(--color-primary); }
.tray-ref {
  font-family: var(--font-mono); font-size: var(--text-xs); color: var(--color-text-subtle);
  white-space: nowrap; overflow: hidden; text-overflow: ellipsis; min-width: 0;
}
.tray-input {
  padding: 5px 8px; border: 1px solid var(--color-border); border-radius: 6px;
  background: var(--surface-panel); color: var(--color-text); font-size: var(--text-sm);
}
.tray-name { width: 170px; flex-shrink: 0; }
.tray-remove:hover:not(:disabled) { border-color: var(--state-danger); color: var(--state-danger); }

/* 分组子条目编辑面板 */
.tray-children {
  margin: -4px 0 4px 22px; padding: 8px 10px; display: flex; flex-direction: column; gap: 6px;
  border: 1px dashed var(--color-border-strong); border-radius: var(--radius-control); background: var(--surface-page);
}
.tray-child-row { display: flex; align-items: center; gap: 8px; min-width: 0; flex-wrap: wrap; }
.tray-child-empty { font-size: var(--text-xs); color: var(--state-danger); }
.tray-child-add { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.tray-child-select { min-width: 200px; }

.tray-tools { display: flex; align-items: center; gap: 10px; flex-wrap: wrap; }

.exe-form {
  background: var(--surface-page); border: 1px dashed var(--color-border); border-radius: var(--radius-control);
  padding: 10px 12px; display: flex; flex-direction: column; gap: 8px;
}
.exe-form-title { font-size: var(--text-sm); font-weight: 600; color: var(--color-text); }
.exe-form-row { display: flex; gap: 8px; align-items: center; flex-wrap: wrap; }
.tray-exe-path { flex: 1; min-width: 0; }
.tray-exe-args { flex: 1; min-width: 0; }
.tray-exe-label { width: 170px; flex-shrink: 0; }

.option-block { display: flex; flex-direction: column; gap: 8px; }
.option-grid {
  display: grid; grid-template-columns: repeat(auto-fill, minmax(260px, 1fr));
  gap: 6px 12px;
}
.option-item {
  background: var(--surface-page); border: 1px solid var(--color-border); border-radius: var(--radius-control);
  padding: 7px 10px; display: flex; align-items: center; gap: 8px; cursor: pointer; min-width: 0;
}
.option-item:hover { background: var(--surface-hover); }
.option-label { font-size: var(--text-sm); color: var(--color-text); white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.switch { width: 18px; height: 18px; cursor: pointer; accent-color: var(--color-primary); flex: none; }

.tray-footer { display: flex; justify-content: space-between; align-items: center; gap: 16px; flex-wrap: wrap; }
.hint { font-size: var(--text-sm); color: var(--color-text-muted); }
</style>
