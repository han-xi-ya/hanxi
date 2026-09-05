// Everything 结果表列宽：表头拖拽调宽 + localStorage 记忆（仅结果面板 EverythingResultsPanel 消费）。
// 语义与原 EverythingView 内实现逐字一致：拖拽挂 document 级 move/up，松手防抖 300ms 落盘；
// 装载即读取记忆值；卸载清理防抖计时器与半途拖拽的 document 监听，防句柄泄漏。
// 记忆值经合法性校验（60~900px）后覆盖默认值；存储损坏/不可用时静默回退默认。
import { computed, onMounted, onUnmounted, reactive } from 'vue'
import type { ComputedRef } from 'vue'

export type EverythingColKey = 'name' | 'path' | 'size' | 'time' | 'action'

const DEFAULT_COLS: Record<EverythingColKey, number> = { name: 320, path: 420, size: 70, time: 130, action: 118 }
const COL_STORAGE_KEY = 'hanxi-everything-result-cols'

export interface UseEverythingColumnsReturn {
  colWidths: Record<EverythingColKey, number>
  totalColWidth: ComputedRef<number>
  startResize: (key: EverythingColKey, e: MouseEvent) => void
}

export function useEverythingColumns(): UseEverythingColumnsReturn {
  const colWidths = reactive<Record<EverythingColKey, number>>({ ...DEFAULT_COLS })
  const totalColWidth = computed(() => Object.values(colWidths).reduce((a, b) => a + b, 0))

  let saveColTimer: number | null = null
  let dragCleanup: (() => void) | null = null

  function loadColWidths() {
    try {
      const raw = localStorage.getItem(COL_STORAGE_KEY)
      if (!raw) return
      const saved = JSON.parse(raw) as Partial<Record<EverythingColKey, number>>
      for (const k of Object.keys(DEFAULT_COLS) as EverythingColKey[]) {
        const v = saved[k]
        if (typeof v === 'number' && v >= 60 && v <= 900) colWidths[k] = v
      }
    } catch {
      // 存储损坏/不可用：静默回退默认值
    }
  }

  function saveColWidths() {
    if (saveColTimer) clearTimeout(saveColTimer)
    saveColTimer = window.setTimeout(() => {
      try { localStorage.setItem(COL_STORAGE_KEY, JSON.stringify(colWidths)) } catch { /* 忽略写失败 */ }
    }, 300)
  }

  // 表头拖拽调宽：mousedown 挂 document 级 move/up，松手防抖落盘
  function startResize(key: EverythingColKey, e: MouseEvent) {
    e.preventDefault()
    const startX = e.clientX
    const startW = colWidths[key]
    const onMove = (ev: MouseEvent) => {
      colWidths[key] = Math.min(900, Math.max(60, Math.round(startW + (ev.clientX - startX))))
    }
    const onUp = () => {
      document.removeEventListener('mousemove', onMove)
      document.removeEventListener('mouseup', onUp)
      dragCleanup = null
      saveColWidths()
    }
    dragCleanup = onUp
    document.addEventListener('mousemove', onMove)
    document.addEventListener('mouseup', onUp)
  }

  onMounted(loadColWidths)
  onUnmounted(() => {
    if (saveColTimer) { clearTimeout(saveColTimer); saveColTimer = null }
    if (dragCleanup) dragCleanup() // 拖拽中途卸载：移除 document 级监听，防句柄泄漏
  })

  return { colWidths, totalColWidth, startResize }
}
