// 局域网文件快传（FileShare）业务状态与操作单一来源。
// Phase 6 巨型孤本结构拆分时自 FileShareView.vue 逐字抽出：bindings 调用序列、
// 事件名、toast 文案、轮询参数与停止态空转保护等业务契约均未改动。
//
// 必须在组件 setup 同步期内调用：内部经 useWailsEvent 注册三个事件订阅、经 usePolling
// 注册运行态轮询（onMounted/onActivated/onDeactivated/onUnmounted 与 KeepAlive 耦合的
// 生命周期契约，docs/FRONTEND.md §6 铁律 3），并在 onMounted 触发首屏 loadData。
import { computed, onMounted, ref, shallowRef } from 'vue'
import QRCode from 'qrcode'
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
import { useWailsEvent } from './useWailsEvent'
import { usePolling } from './usePolling'
import { useClipboard } from './useClipboard'

export function useFileShareServer() {
  const { showToast } = useToast()
  const { copy } = useClipboard()

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
  const errorMsg = ref('')
  const savedSharePath = ref('')

  const normalizedSharePath = computed(() => configForm.value.sharePath.trim())
  const canOpenShareDirectory = computed(() => {
    return Boolean(savedSharePath.value) && normalizedSharePath.value === savedSharePath.value
  })
  // 「当前路径尚未保存」提示位——原模板表达式 normalizedSharePath && !canOpenShareDirectory 的等价收拢。
  const unsavedPathHint = computed(
    () => Boolean(normalizedSharePath.value) && !canOpenShareDirectory.value,
  )

  // 二维码 SVG 缓存 Map (url -> svg)
  const qrMap = ref<Record<string, string>>({})

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
    }
  }

  async function generateQRCodes() {
    const map: Record<string, string> = {}
    for (const ep of endpoints.value) {
      try {
        map[ep.url] = await QRCode.toString(ep.url, {
          type: 'svg',
          margin: 1,
          width: 140,
          // 二维码必须保持深墨+纯白硬对比——扫码器不认主题色，这两个 hex 是功能参数而非装饰色，
          // 刻意不参与 token 化（全文件仅此处允许硬编码色值）。
          color: {
            dark: '#1e293b',
            light: '#ffffff',
          },
        })
      } catch (e) {
        console.warn('QR code generate error:', e)
      }
    }
    qrMap.value = map
  }

  async function handleToggleServer() {
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
      showToast(`操作失败: ${getErrorMessage(err)}`)
    } finally {
      loading.value = false
    }
  }

  async function handleSaveConfig() {
    try {
      await FileShareAPI.FileShareService.SaveConfig(configForm.value)
      savedSharePath.value = normalizedSharePath.value
      showToast('快传共享设置已保存')
      if (status.value.isRunning) {
        showToast('设置已动态热应用到运行中的快传服务')
      }
    } catch (err: unknown) {
      showToast(`保存失败: ${getErrorMessage(err)}`)
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
      showToast(`选择目录失败: ${getErrorMessage(err)}`)
    }
  }

  async function handleOpenShareDirectory() {
    if (!canOpenShareDirectory.value) return
    try {
      await AppAPI.AppService.OpenPath(savedSharePath.value)
    } catch (err: unknown) {
      showToast(`打开目录失败: ${getErrorMessage(err)}`)
    }
  }

  async function copyToClipboard(text: string, tip = '已复制访问链接') {
    // 两级剪贴板策略收编进 useClipboard；成功/失败文案逐字保留
    const ok = await copy(text)
    showToast(ok ? tip : '复制到剪贴板失败')
  }

  async function handleDeleteDrop(id: string) {
    try {
      await FileShareAPI.FileShareService.DeleteDropItem(id)
      dropInbox.value = dropInbox.value.filter(item => item.id !== id)
      showToast('已删除投递内容')
    } catch (err: unknown) {
      showToast(`删除失败: ${getErrorMessage(err)}`)
    }
  }

  async function handleClearInbox() {
    if (dropInbox.value.length === 0) return
    try {
      await FileShareAPI.FileShareService.ClearDropInbox()
      dropInbox.value = []
      showToast('投递箱已清空')
    } catch (err: unknown) {
      showToast(`清空失败: ${getErrorMessage(err)}`)
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

  // 运行中才拉状态（停止态空转保护逐字保留）；不立即首跑——进页数据由 loadData 负责
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
    errorMsg,
    qrMap,
    canOpenShareDirectory,
    unsavedPathHint,
    handleToggleServer,
    handleSaveConfig,
    handleChooseDirectory,
    handleOpenShareDirectory,
    copyToClipboard,
    handleDeleteDrop,
    handleClearInbox,
  }
}
