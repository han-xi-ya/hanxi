// Everything 内嵌搜索：输入即搜防抖（350ms）+ 中文组合输入守卫 + 过期响应丢弃（searchSeq）、
// 300 条截断警示、结果打开/定位/复制，以及 ES 搜索组件就绪（EnsureSearchTool）与独立下载进度态。
// 由 EverythingView 在 setup 期实例化并接线到子组件（props/emits），bindings 调用与文案逐字保持；
// 事件分发（'everything:download' 的 es/app 双分支）仍由视图统一订阅后调用 handleEsTicket。
import { onUnmounted, ref } from 'vue'
import type { Ref } from 'vue'
import * as EverythingAPI from '../../bindings/hanxi/internal/modules/everything/everythingservice'
import type { DownloadTicket } from '../../bindings/hanxi/internal/modules/everything/models'
import type { Result } from '../../bindings/hanxi/internal/modules/everything/search/models'
import { useToast } from './useToast'
import { useClipboard } from './useClipboard'
import { getErrorMessage } from '../utils/errors'

// 结果完整路径拼接：目录 path 自带尾部分隔符则直接相接，否则补 '\'
export function resultFullPath(r: Result): string {
  const sep = '\\'
  if (!r.path) return r.name
  return r.path.endsWith(sep) ? r.path + r.name : r.path + sep + r.name
}

export function useEverythingSearch(busy: Ref<boolean>) {
  const keyword = ref('')
  const searched = ref('')
  const results = ref<Result[]>([])
  const searching = ref(false)
  const searchError = ref('')
  const truncated = ref(false)
  const esReady = ref(false)
  const esBusy = ref(false)
  const esProgress = ref<DownloadTicket | null>(null)
  const composing = ref(false)

  const { showToast } = useToast()
  const { copy } = useClipboard()

  let debounceTimer: number | null = null
  let searchSeq = 0 // 过期响应丢弃：最新一次搜索的序号才允许落盘结果

  async function ensureTool(): Promise<boolean> {
    if (esReady.value) return true
    esBusy.value = true
    try {
      await EverythingAPI.EnsureSearchTool()
      esReady.value = true
      return true
    } catch (e) {
      showToast(`搜索组件就绪失败: ${getErrorMessage(e)}`)
      return false
    } finally {
      esBusy.value = false
    }
  }

  // 实时搜索：输入停顿 350ms 自动触发；中文输入法组合期间不触发（composition 守卫）
  function onKeywordInput() {
    if (composing.value) return
    const q = keyword.value.trim()
    if (!q) {
      if (debounceTimer) { clearTimeout(debounceTimer); debounceTimer = null }
      results.value = []
      searched.value = ''
      searchError.value = ''
      truncated.value = false
      return
    }
    if (debounceTimer) clearTimeout(debounceTimer)
    debounceTimer = window.setTimeout(() => { doSearch() }, 350)
  }

  function onKeywordEnter() {
    if (debounceTimer) { clearTimeout(debounceTimer); debounceTimer = null }
    doSearch()
  }

  function onCompositionEnd() {
    composing.value = false
    onKeywordInput() // 中文上屏后补触发一次
  }

  async function doSearch() {
    const q = keyword.value.trim()
    if (!q || searching.value || busy.value) return
    const seq = ++searchSeq
    searching.value = true
    searchError.value = ''
    truncated.value = false
    try {
      if (!(await ensureTool())) return // 组件缺失且安装失败时给出 toast 后中断（由 finally 复位状态）
      const list = await EverythingAPI.Search(q, 300)
      if (seq !== searchSeq) return // 已有更新的关键词在途，丢弃过期响应
      results.value = list ?? []
      searched.value = q
      truncated.value = results.value.length >= 300
      if (results.value.length === 0) showToast(`「${q}」无匹配结果`)
    } catch (e) {
      if (seq !== searchSeq) return
      searchError.value = getErrorMessage(e)
    } finally {
      if (seq === searchSeq) searching.value = false
    }
  }

  async function openResult(r: Result) {
    try {
      await EverythingAPI.OpenTarget(resultFullPath(r))
    } catch (e) {
      showToast(`打开失败: ${getErrorMessage(e)}`)
    }
  }

  async function revealResult(r: Result) {
    try {
      await EverythingAPI.RevealTarget(resultFullPath(r))
    } catch (e) {
      showToast(`定位失败: ${getErrorMessage(e)}`)
    }
  }

  // 点击名称/路径单元格 = 复制完整路径（两级剪贴板策略经 useClipboard 收编）
  async function copyResult(r: Result) {
    if (await copy(resultFullPath(r))) {
      showToast('已复制完整路径')
    } else {
      showToast('复制失败')
    }
  }

  // 'everything:download' 事件的 es 组件分支：独立进度态，不进版本 map（视图统一订阅后分发至此）
  function handleEsTicket(t: DownloadTicket) {
    esProgress.value = t
    if (t.stage === 'done') {
      esReady.value = true
      setTimeout(() => { esProgress.value = null }, 800)
    }
    if (t.stage === 'error') esProgress.value = null
  }

  onUnmounted(() => {
    if (debounceTimer) { clearTimeout(debounceTimer); debounceTimer = null }
  })

  return {
    keyword, searched, results, searching, searchError, truncated,
    esReady, esBusy, esProgress, composing,
    ensureTool, onKeywordInput, onKeywordEnter, onCompositionEnd, doSearch,
    openResult, revealResult, copyResult, handleEsTicket,
  }
}
