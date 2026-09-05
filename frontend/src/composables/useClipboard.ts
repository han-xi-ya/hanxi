export interface UseClipboardReturn {
  /** 复制文本，返回是否成功；纯行为不弹 toast（提示归调用方）。 */
  copy: (text: string) => Promise<boolean>
}

/**
 * 剪贴板复制两级策略（对齐 MarkerOnView.copyRepo 等存量实现）：
 * ① 安全上下文优先 navigator.clipboard；② 回退隐藏 textarea + execCommand。
 * 两路皆败返回 false。
 */
export function useClipboard(): UseClipboardReturn {
  async function copy(text: string): Promise<boolean> {
    try {
      if (navigator.clipboard && window.isSecureContext) {
        await navigator.clipboard.writeText(text)
        return true
      }
    } catch {
      // 落入 execCommand 降级
    }
    try {
      const ta = document.createElement('textarea')
      ta.value = text
      ta.style.position = 'fixed'
      ta.style.opacity = '0'
      document.body.appendChild(ta)
      ta.select()
      const ok = document.execCommand('copy')
      document.body.removeChild(ta)
      return ok
    } catch {
      return false
    }
  }

  return { copy }
}
