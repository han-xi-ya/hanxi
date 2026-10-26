// trailing 防抖：HistoryPanel（keyword 350ms 防抖重排）与 useEverythingSearch
//（输入停顿 350ms，回车/卸载 cancel 清待触发）两处手写副本抽出的公共正源。
// 刻意不带立即执行档/参数透传/wait 返回值等投机特性；components/tray/TrayItemsEditor.vue
// 的自管定时器属有意差异，不走本件。

/** debounce 返回的调用体：每次调用重置倒计时；cancel 清除未触发的待执行（幂等）。 */
export interface Debounced {
  (): void
  cancel(): void
}

/** 生成 trailing 防抖调用：静默 wait 毫秒后触发一次；期间重复调用只算最后一次。 */
export function debounce(fn: () => void, wait: number): Debounced {
  let timer: ReturnType<typeof setTimeout> | undefined
  const debounced = () => {
    clearTimeout(timer)
    timer = setTimeout(() => {
      timer = undefined
      fn()
    }, wait)
  }
  debounced.cancel = () => {
    clearTimeout(timer)
    timer = undefined
  }
  return debounced
}
