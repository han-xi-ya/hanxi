import { useToast } from './useToast'

export interface UseClipboardReturn {
  /** 复制文本，返回是否成功；纯行为不弹 toast（需要回执请用 copyWithToast）。 */
  copy: (text: string) => Promise<boolean>
  /**
   * 读取剪贴板文本（用户主动触发：点"粘贴"按钮等）。主路
   * navigator.clipboard.readText（需安全上下文 + WebView2 权限）；
   * 不可用/无权限/非文本一律返回 null，由调用方降级提示手动 Ctrl+V。
   * 刻意不引入 document.execCommand('paste')（多数内核已禁）。
   */
  paste: () => Promise<string | null>
  /**
   * 复制并给统一回执：成功 toast(okTip，缺省「已复制」)，失败
   * showErrorToast('复制失败')。全仓复制入口的默认话术出口——
   * 视图不再自写"剪贴板不可用 / execCommand 不可用"等三帮失败文案
   *（PLAN_CLIPBOARD §3.2A）。返回是否成功，供调用方追加动作。
   */
  copyWithToast: (text: string, okTip?: string) => Promise<boolean>
}

/**
 * 剪贴板统一入口（复制/粘贴/回执话术）：
 * ① 复制两级策略（对齐 MarkerOnView.copyRepo 等存量实现）——安全上下文优先
 *    navigator.clipboard；回退隐藏 textarea + execCommand；两路皆败返回 false。
 * ② 粘贴仅 readText 一路，失败即 null（不报错刷屏，降级提示归调用方）。
 */
export function useClipboard(): UseClipboardReturn {
  const { showToast, showErrorToast } = useToast()

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

  async function paste(): Promise<string | null> {
    try {
      if (navigator.clipboard?.readText && window.isSecureContext) {
        const text = await navigator.clipboard.readText()
        return text ?? null
      }
    } catch {
      // 权限拒绝 / 无文本格式：一律视为不可用，交由调用方提示手动 Ctrl+V
    }
    return null
  }

  async function copyWithToast(text: string, okTip = '已复制'): Promise<boolean> {
    const ok = await copy(text)
    if (ok) {
      showToast(okTip)
    } else {
      showErrorToast('复制失败')
    }
    return ok
  }

  return { copy, paste, copyWithToast }
}
