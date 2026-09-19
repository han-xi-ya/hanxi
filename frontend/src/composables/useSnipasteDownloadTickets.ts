import { onUnmounted, ref } from 'vue'
import type { NormalizedProgress } from '../components/managed/adapter'

export interface SnipasteDownloadTicket extends NormalizedProgress {
  version: string
}

const DONE_TICKET_RETIRE_MS = 900

export function useSnipasteDownloadTickets() {
  const downloading = ref<Record<string, SnipasteDownloadTicket>>({})
  const rowErrors = ref<Record<string, string>>({})
  const cleanupTimers = new Map<string, ReturnType<typeof setTimeout>>()

  function ticketOf(version: string): SnipasteDownloadTicket | undefined {
    return downloading.value[version]
  }

  function setTicket(progress: NormalizedProgress): void {
    downloading.value = {
      ...downloading.value,
      [progress.key]: { ...progress, version: progress.key },
    }
  }

  function clearTicket(version: string): void {
    const next = { ...downloading.value }
    delete next[version]
    downloading.value = next
  }

  function cancelCleanup(version: string): void {
    const timer = cleanupTimers.get(version)
    if (timer) clearTimeout(timer)
    cleanupTimers.delete(version)
  }

  function setRowError(version: string, message: string): void {
    rowErrors.value = { ...rowErrors.value, [version]: message }
  }

  function clearRowError(version: string): void {
    setRowError(version, '')
  }

  function begin(version: string): void {
    cancelCleanup(version)
    clearRowError(version)
    setTicket({
      key: version,
      stage: 'pending',
      done: 0,
      total: 0,
      message: '正在创建下载任务',
    })
  }

  function fail(version: string, message: string): void {
    cancelCleanup(version)
    setTicket({ key: version, stage: 'error', done: 0, total: 0, message })
    setRowError(version, message)
  }

  function markInProgress(version: string): void {
    cancelCleanup(version)
    clearRowError(version)
    setTicket({
      key: version,
      stage: 'pending',
      done: 0,
      total: 0,
      message: '已有下载任务正在进行',
    })
  }

  async function handleProgress(
    progress: NormalizedProgress,
    refreshLocal: () => Promise<boolean>,
    isInstalled: (version: string) => boolean,
  ): Promise<void> {
    const version = progress.key
    cancelCleanup(version)
    setTicket(progress)

    if (progress.stage === 'error') {
      setRowError(version, progress.message || '安装失败')
      return
    }

    clearRowError(version)
    if (progress.stage !== 'done') return

    setTicket({ ...progress, message: '安装完成，正在同步本地版本' })
    const refreshed = await refreshLocal()
    if (!refreshed || !isInstalled(version)) {
      setRowError(version, '安装已完成，但本地版本列表刷新失败，请重试读取本地版本')
      return
    }

    const timer = setTimeout(() => {
      if (ticketOf(version)?.stage === 'done' && isInstalled(version)) clearTicket(version)
      cleanupTimers.delete(version)
    }, DONE_TICKET_RETIRE_MS)
    cleanupTimers.set(version, timer)
  }

  function isBusy(version: string): boolean {
    const ticket = ticketOf(version)
    return !!ticket && ticket.stage !== 'error'
  }

  onUnmounted(() => {
    cleanupTimers.forEach(clearTimeout)
    cleanupTimers.clear()
  })

  return {
    downloading,
    rowErrors,
    ticketOf,
    setTicket,
    clearTicket,
    cancelCleanup,
    setRowError,
    clearRowError,
    begin,
    fail,
    markInProgress,
    handleProgress,
    isBusy,
  }
}
