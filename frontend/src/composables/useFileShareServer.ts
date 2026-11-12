// 局域网文件快传（FileShare）业务状态与操作单一来源（拆分期自 FileShareView.vue 抽出）。
// bindings 调用序列、事件名、toast 文案、轮询参数与停止态空转保护均属契约面，
// 由特征测试 FileShareView.spec.ts 锁定。
//
// 必须在组件 setup 同步期内调用：内部经 useWailsEvent 注册三个事件订阅、经 usePolling
// 注册运行态轮询（onMounted/onActivated/onDeactivated/onUnmounted 与 KeepAlive 耦合的
// 生命周期契约，docs/FRONTEND.md §6 铁律 3），并在 onMounted 触发首屏 loadData。
import { computed, onMounted, ref, shallowRef } from 'vue'
import { makeQrSvg } from '../utils/qr'
import * as AppAPI from '../../bindings/hanxi/internal/app'
import * as FileShareAPI from '../../bindings/hanxi/internal/modules/fileshare'
import type {
  ShareConfig,
  ServerStatus,
  NetworkEndpoint,
  DropItem,
  TransferEvent
} from '../../bindings/hanxi/internal/modules/fileshare/models'
import { getErrorMessage } from '../utils/errors'
import { useToast } from './useToast'
import { useConfirm } from './useConfirm'
import { useWailsEvent } from './useWailsEvent'
import { usePolling } from './usePolling'
import { useClipboard } from './useClipboard'

export function useFileShareServer() {
  const { showToast, showErrorToast } = useToast()
  const { confirm } = useConfirm()
  const { copyWithToast } = useClipboard()

  // 状态定义
  const status = ref<ServerStatus>({
    isRunning: false,
    port: 80,
    sharePath: '',
    activeConnections: 0,
    uploadCount: 0,
    downloadCount: 0,
    uploadBytes: 0,
    downloadBytes: 0,
    uploadRate: 0,
    downloadRate: 0,
    allowUpload: true,
    allowTextDrop: true,
    autoSaveToMemo: true,
    activeUrls: [],
    startedAt: '',
  })

  const configForm = ref<ShareConfig>({
    port: 80,
    sharePath: '',
    allowUpload: true,
    allowTextDrop: true,
    autoSaveToMemo: true,
    maxUploadSizeMB: 0,
    authToken: '',
  })

  const endpoints = shallowRef<NetworkEndpoint[]>([])
  const dropInbox = shallowRef<DropItem[]>([])
  const transferLogs = ref<TransferEvent[]>([])
  const loading = ref(false)
  // 首屏数据落地前置真：hero/概览出读取态，启停与保存禁用——慢后端下不得
  // 拿默认「已停止·端口 80」假概览抢跑启动
  const initialLoading = ref(true)
  // 「保存共享规则」单飞位：连点不得并发重复 SaveConfig
  const saving = ref(false)
  const errorMsg = ref('')
  const savedSharePath = ref('')

  const normalizedSharePath = computed(() => configForm.value.sharePath.trim())
  const canOpenShareDirectory = computed(() => {
    return Boolean(savedSharePath.value) && normalizedSharePath.value === savedSharePath.value
  })
  // 「当前路径尚未保存」提示位：路径非空且与已保存值不一致时为真。
  const unsavedPathHint = computed(
    () => Boolean(normalizedSharePath.value) && !canOpenShareDirectory.value,
  )

  // 二维码 SVG 缓存 Map (url -> svg)；生成失败另记错误 Map (url -> 错误文本)，
  // 卡片呈现「生成失败·重试」而非永久「生成中」假加载
  const qrMap = ref<Record<string, string>>({})
  const qrErrors = ref<Record<string, string>>({})

  async function loadData() {
    try {
      const [srvStatus, cfg, eps, inbox] = await Promise.all([
        FileShareAPI.FileShareService.GetServerStatus(),
        FileShareAPI.FileShareService.GetConfig(),
        FileShareAPI.FileShareService.GetNetworkEndpoints(),
        FileShareAPI.FileShareService.GetDropInbox(),
      ])

      status.value = srvStatus
      configForm.value = { ...cfg }
      savedSharePath.value = cfg.sharePath.trim()
      endpoints.value = eps ?? []
      dropInbox.value = inbox ?? []

      await generateQRCodes()
    } catch (err: unknown) {
      errorMsg.value = `加载文件快传数据失败: ${getErrorMessage(err)}`
    } finally {
      initialLoading.value = false
    }
  }

  async function generateQRCodes() {
    const map: Record<string, string> = {}
    const errors: Record<string, string> = {}
    for (const ep of endpoints.value) {
      try {
        // 深墨+纯白硬对比是功能参数、不 token 化（原裁决随 utils/qr.ts 头注保留）
        map[ep.url] = await makeQrSvg(ep.url, 140)
      } catch (e: unknown) {
        errors[ep.url] = getErrorMessage(e)
      }
    }
    qrMap.value = map
    qrErrors.value = errors
  }

  /** 二维码重试：只重跑生成，不重拉服务状态/端点。 */
  async function retryQRCodes() {
    await generateQRCodes()
  }

  async function handleToggleServer() {
    if (initialLoading.value || loading.value) return
    loading.value = true
    errorMsg.value = ''
    try {
      if (status.value.isRunning) {
        await FileShareAPI.FileShareService.StopServer()
        status.value.isRunning = false
        showToast('局域网文件快传服务已停止')
      } else {
        // 先保存最新配置再启动
        await FileShareAPI.FileShareService.SaveConfig(configForm.value)
        const res = await FileShareAPI.FileShareService.StartServer()
        status.value = res
        showToast(`局域网快传服务已成功启动于端口 :${res.port}`)
      }
      await loadData()
    } catch (err: unknown) {
      errorMsg.value = `服务操作失败: ${getErrorMessage(err)}`
      showErrorToast(`操作失败: ${getErrorMessage(err)}`)
    } finally {
      loading.value = false
    }
  }

  async function handleSaveConfig() {
    if (saving.value) return
    saving.value = true
    try {
      await FileShareAPI.FileShareService.SaveConfig(configForm.value)
      savedSharePath.value = normalizedSharePath.value
      // 运行中保存=保存+热应用两件事一条回执，不再连发两条 toast 互相顶掉
      showToast(status.value.isRunning
        ? '快传共享设置已保存并热应用到运行中的快传服务'
        : '快传共享设置已保存')
    } catch (err: unknown) {
      showErrorToast(`保存失败: ${getErrorMessage(err)}`)
    } finally {
      saving.value = false
    }
  }

  async function handleChooseDirectory() {
    try {
      const selected = await FileShareAPI.FileShareService.ChooseDirectory()
      if (selected) {
        configForm.value.sharePath = selected
        showToast(`已选择共享目录: ${selected}`)
        // 若服务已在运行，自动热同步生效
        if (status.value.isRunning) {
          await handleSaveConfig()
        }
      }
    } catch (err: unknown) {
      showErrorToast(`选择目录失败: ${getErrorMessage(err)}`)
    }
  }

  async function handleOpenShareDirectory() {
    if (!canOpenShareDirectory.value) return
    try {
      await AppAPI.AppService.OpenPath(savedSharePath.value)
    } catch (err: unknown) {
      showErrorToast(`打开目录失败: ${getErrorMessage(err)}`)
    }
  }

  async function copyToClipboard(text: string, tip = '已复制访问链接') {
    // 两级剪贴板策略与统一回执话术全部收进 useClipboard.copyWithToast
    //（PLAN_CLIPBOARD §3.2C）；本函数只保留"默认成功话术"的薄封装职责。
    await copyWithToast(text, tip)
  }

  async function handleDeleteDrop(id: string) {
    // 删除不可恢复：单条也走确认（内容摘要进 details，删什么看得见）
    const item = dropInbox.value.find(d => d.id === id)
    const accepted = await confirm({
      title: '删除这条投递内容？',
      description: '删除后不可恢复。',
      confirmLabel: '删除',
      tone: 'danger',
      details: item ? [{ label: '内容', value: item.content.slice(0, 60) }] : [],
    })
    if (!accepted) return
    try {
      await FileShareAPI.FileShareService.DeleteDropItem(id)
      dropInbox.value = dropInbox.value.filter(d => d.id !== id)
      showToast('已删除投递内容')
    } catch (err: unknown) {
      showErrorToast(`删除失败: ${getErrorMessage(err)}`)
    }
  }

  async function handleClearInbox() {
    if (dropInbox.value.length === 0) return
    // 一键批量删除全部跨端投递且不可恢复：danger 确认明示条数
    const count = dropInbox.value.length
    const accepted = await confirm({
      title: '清空投递箱？',
      description: `将删除全部 ${count} 条跨端投递内容，删除后不可恢复。`,
      confirmLabel: `清空 ${count} 条`,
      tone: 'danger',
      details: [{ label: '条数', value: `${count} 条` }],
    })
    if (!accepted) return
    try {
      await FileShareAPI.FileShareService.ClearDropInbox()
      dropInbox.value = []
      showToast('投递箱已清空')
    } catch (err: unknown) {
      showErrorToast(`清空失败: ${getErrorMessage(err)}`)
    }
  }

  // ---------- 订阅与轮询（useWailsEvent/usePolling 契约：setup 期注册、KeepAlive 停用即停） ----------
  useWailsEvent<ServerStatus>('fileshare:status', (s) => {
    if (s) status.value = s
  })

  useWailsEvent<TransferEvent>('fileshare:transfer', (t) => {
    if (t) transferLogs.value = [t, ...transferLogs.value.slice(0, 49)]
  })

  useWailsEvent<DropItem>('fileshare:text-dropped', (item) => {
    if (!item) return
    dropInbox.value = [item, ...dropInbox.value]
    showToast(`收到来自移动端的文本投递: ${item.content.slice(0, 24)}...`)
  })

  // 运行中才拉状态（停止态空转保护）；不立即首跑——进页数据由 loadData 负责
  usePolling(async () => {
    if (!status.value.isRunning) return
    status.value = await FileShareAPI.FileShareService.GetServerStatus()
  }, 2000, { immediateFirstRun: false })

  onMounted(() => {
    loadData()
  })

  return {
    status,
    configForm,
    endpoints,
    dropInbox,
    transferLogs,
    loading,
    initialLoading,
    saving,
    errorMsg,
    qrMap,
    qrErrors,
    canOpenShareDirectory,
    unsavedPathHint,
    handleToggleServer,
    handleSaveConfig,
    handleChooseDirectory,
    handleOpenShareDirectory,
    copyToClipboard,
    handleDeleteDrop,
    handleClearInbox,
    retryQRCodes,
  }
}
