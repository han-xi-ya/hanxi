// 网页应用（WebApp）条目管理与窗态编排的业务状态单一来源。
// 工程范式对标 useWechatBot：bindings 调用序列、useWailsEvent 事件订阅、
// toast 回报（失败统一 showErrorToast 长保活）、危险删除走 useConfirm 均可数对位。
// 表单面板折叠/展开属纯 UI 开关，留在视图本体（与 WechatBotView 同一契约）。
//
// 必须在组件 setup 同步期内调用：内部经 useWailsEvent 订阅 'webapp:windows-changed'
// （1 订阅 / 1 注销配对，onScopeDispose 自动清理）。视图常驻 App.vue 的 KeepAlive，
// 注销仅发生在卸载/驱逐时——停用期窗口态事件照常触发重拉，列表缓存保持温热。
//
// 注意：bindings/hanxi/internal/modules/webapp 目录要等 Go 侧服务定稿后生成绑定，
// 本文件的 import 在生成前属预期中间态红（由主会话绑定落地后统一 typecheck）。
import { ref, computed, onMounted } from 'vue'
import * as WebAppAPI from '../../bindings/hanxi/internal/modules/webapp'
import type { WebAppEntryView } from '../../bindings/hanxi/internal/modules/webapp/models'
import { getErrorMessage } from '../utils/errors'
import { useToast } from './useToast'
import { useWailsEvent } from './useWailsEvent'
import { useConfirm } from './useConfirm'

/** 名称长度闸门前端镜像（与后端 SaveEntry 的 40 字上限同值；仅省一次往返，权威仍在服务端）。 */
export const NAME_MAX = 40

// —— W1 冻结契约（defaultOpen）的前端桥接 ——
// 条目 JSON 字段 defaultOpen ∈ '' | 'window' | 'browser'，''（含存量缺字段）
// 语义即「独立窗口」。bindings 再生前本文件内的类型桥接（as unknown as）属
// 预期过渡态：W1 落盘、绑定重生成后可移除，不改调用形状。
/** 默认打开方式（表单可选值；'' 仅出现在存量数据，UI 归一化为 'window'）。 */
export type WebAppOpenMode = 'window' | 'browser'

/** 归一化条目 defaultOpen：'browser' 以外（含 undefined/''/未知值）一律按 'window' 呈现。 */
export function entryDefaultOpen(entry: WebAppEntryView): WebAppOpenMode {
  const raw = (entry as unknown as { defaultOpen?: string }).defaultOpen
  return raw === 'browser' ? 'browser' : 'window'
}

/** 行级忙态动作：同一行任一异步操作进行中即锁整行钮组，防连发并发的窗口编排。 */
export type RowAction = 'open' | 'collapse' | 'external' | 'delete'

export function useWebApp() {
  const { showToast, showErrorToast } = useToast()
  const { confirm } = useConfirm()

  // —— 条目列表与加载状态（loadError 走内联错误面板 + 重试，不叠 toast 刷屏）——
  const entries = ref<WebAppEntryView[]>([])
  const loading = ref(true)
  const loadError = ref('')

  async function refresh() {
    loading.value = true
    loadError.value = ''
    try {
      const list = await WebAppAPI.WebAppService.ListEntries()
      entries.value = list || []
    } catch (err: unknown) {
      loadError.value = getErrorMessage(err)
    } finally {
      loading.value = false
    }
  }

  // 存在任一可见窗时「收起全部」才有意义（已收起驻留 / 无窗均不计入）。
  const anyWindowShown = computed(() => entries.value.some(e => e.windowOpen))

  const collapsingAll = ref(false)

  // —— 行级忙态表：entryID → 进行中动作（undefined=空闲）——
  const rowBusy = ref<Record<string, RowAction | undefined>>({})

  function isRowBusy(entryID: string): boolean {
    return rowBusy.value[entryID] !== undefined
  }

  async function runRowAction(
    entry: WebAppEntryView,
    action: RowAction,
    failLabel: string,
    task: () => Promise<void>,
  ) {
    if (isRowBusy(entry.id)) return
    rowBusy.value[entry.id] = action
    try {
      await task()
    } catch (err: unknown) {
      showErrorToast(`${failLabel}: ${getErrorMessage(err)}`)
    } finally {
      rowBusy.value[entry.id] = undefined
    }
  }

  // —— 窗口编排：Open/Collapse/OpenExternal/CollapseAll 与后端契约逐一对应 ——
  // Open 幂等三态（可见置顶 / 收起热恢复 / 无窗新建），收起驻留时按钮文案语义仍是「打开」。
  // 开窗类操作「系统窗口即回执」——成功不 toast（窗就在眼前，再报是噪音，同 wechat 本地文件动作纪律）；
  // 窗态翻折由 windows-changed 事件驱动重拉收敛，无需手动 refresh。
  function openEntry(entry: WebAppEntryView) {
    return runRowAction(entry, 'open', `打开「${entry.name}」失败`, () =>
      WebAppAPI.WebAppService.Open(entry.id),
    )
  }

  function collapseEntry(entry: WebAppEntryView) {
    return runRowAction(entry, 'collapse', `收起「${entry.name}」失败`, async () => {
      await WebAppAPI.WebAppService.Collapse(entry.id)
      showToast(`已收起「${entry.name}」网页窗`)
    })
  }

  function openExternal(entry: WebAppEntryView) {
    return runRowAction(entry, 'external', `默认浏览器打开「${entry.name}」失败`, () =>
      WebAppAPI.WebAppService.OpenExternal(entry.id),
    )
  }

  async function collapseAll() {
    if (collapsingAll.value) return
    collapsingAll.value = true
    try {
      await WebAppAPI.WebAppService.CollapseAll()
      showToast('已收起全部网页窗')
      await refresh() // 事件与返回值双通道收敛：末窗若本就不存在，事件可能不触发
    } catch (err: unknown) {
      showErrorToast(`收起全部失败: ${getErrorMessage(err)}`)
    } finally {
      collapsingAll.value = false
    }
  }

  // —— 新增 / 编辑共用表单（editingId 空串=新建，与后端 SaveEntry 口径同构）——
  const formName = ref('')
  const formUrl = ref('')
  const formIcon = ref('')
  // 默认打开方式：新建缺省「独立窗口」；编辑预填存量归一化值（未设值显示为独立窗口）。
  const formDefaultOpen = ref<WebAppOpenMode>('window')
  const editingId = ref('')
  const saving = ref(false)
  const isEditing = computed(() => editingId.value !== '')

  function resetForm() {
    formName.value = ''
    formUrl.value = ''
    formIcon.value = ''
    formDefaultOpen.value = 'window'
    editingId.value = ''
  }

  // 编辑复用新增表单：预填切编辑态；面板展开由视图侧负责（纯 UI 开关不上收）。
  function startEdit(entry: WebAppEntryView) {
    editingId.value = entry.id
    formName.value = entry.name
    formUrl.value = entry.url
    formIcon.value = entry.icon
    formDefaultOpen.value = entryDefaultOpen(entry)
  }

  // 提交保存；成功返回 true（视图据此收起面板）。名称/网址空值前端先闸，
  // 其余校验（validateURL 拒非 http/https、超长等）以服务端中文错误为准直接 toast。
  async function submitForm(): Promise<boolean> {
    if (saving.value) return false
    const name = formName.value.trim()
    const url = formUrl.value.trim()
    if (!name) {
      showToast('请填写网站名称')
      return false
    }
    if (!url) {
      showToast('请填写网址')
      return false
    }
    const wasEditing = isEditing.value
    saving.value = true
    try {
      // 返回值即服务端定稿 ID（新建时由后端生成）；当前列表随后由 refresh 带回，无需本地接线。
      // defaultOpen 双兼容写法（W1 定稿前两侧都接得住，落盘后删桥接不改行为）：
      // ① 全量快照路线——SaveEntry 若扩第 5 参 defaultOpen，此处直传；现行 4 参绑定
      //    生成的 JS 函数按形参截取，多余实参自然丢弃，无副作用。
      // ② 独立导出路线——W1 若以 SetEntryDefaultOpen(entryID, mode) 定稿，绑定再生后
      //    该函数存在，随后补写一次（新建取 SaveEntry 返回的定稿 ID）。
      const savedID = await (WebAppAPI.WebAppService.SaveEntry as unknown as (
        entryID: string, name: string, rawURL: string, icon: string, defaultOpen: string,
      ) => Promise<string>)(editingId.value, name, url, formIcon.value.trim(), formDefaultOpen.value)
      const setDefaultOpen = (WebAppAPI.WebAppService as unknown as {
        SetEntryDefaultOpen?: (entryID: string, mode: WebAppOpenMode) => Promise<void>
      }).SetEntryDefaultOpen
      if (setDefaultOpen) await setDefaultOpen(savedID || editingId.value, formDefaultOpen.value)
      resetForm()
      await refresh()
      showToast(wasEditing ? `「${name}」已保存` : `已添加「${name}」`)
      return true
    } catch (err: unknown) {
      showErrorToast(getErrorMessage(err))
      return false
    } finally {
      saving.value = false
    }
  }

  // 删除走全局确认对话框，文案含条目名；预置条目（如微信文件传输助手）同权可删。
  // 后端删除会连带真销毁存活窗并清理托盘/轮盘引用；条目消失无窗态事件可循，显式重拉。
  async function removeEntry(entry: WebAppEntryView) {
    const accepted = await confirm({
      title: `确定删除「${entry.name}」吗？`,
      description: '其存活网页窗将随删除一并关闭，托盘与快捷菜单中的引用同步清理。',
      tone: 'danger',
    })
    if (!accepted) return
    await runRowAction(entry, 'delete', `删除「${entry.name}」失败`, async () => {
      await WebAppAPI.WebAppService.DeleteEntry(entry.id)
      showToast(`「${entry.name}」已删除`)
      await refresh()
    })
  }

  // 窗口生命周期翻折（开/收/关/TTL 自动销毁）全部收敛到本事件 → 收到即重拉列表。
  useWailsEvent<void>('webapp:windows-changed', () => {
    void refresh()
  })

  onMounted(refresh)

  return {
    entries,
    loading,
    loadError,
    refresh,
    anyWindowShown,
    collapsingAll,
    collapseAll,
    isRowBusy,
    openEntry,
    collapseEntry,
    openExternal,
    removeEntry,
    // 表单
    formName,
    formUrl,
    formIcon,
    formDefaultOpen,
    editingId,
    saving,
    isEditing,
    resetForm,
    startEdit,
    submitForm,
  }
}
