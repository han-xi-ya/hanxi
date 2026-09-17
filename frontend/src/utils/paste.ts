// 粘贴事件解析（纯函数，可单测）：ClipboardEvent → 图像 / 文本 / 无 三态分流。
// 图像优先于文本（截图常同时携带位图与文件路径文本，图片通道是各粘贴挂点的主语义）；
// 纯空白文本视为无。调用方（OcrView 等）决定 preventDefault 与后续动作。

export type PastePayload =
  | { kind: 'image'; file: File }
  | { kind: 'text'; text: string }
  | { kind: 'none' }

/** 从粘贴事件取第一张图片文件（无剪贴板数据/无 file 项返回 null）。 */
export function pasteImage(e: ClipboardEvent): File | null {
  const items = e.clipboardData?.items
  if (!items) return null
  for (const item of Array.from(items)) {
    if (item.kind === 'file') {
      const file = item.getAsFile()
      if (file && file.type.startsWith('image/')) return file
    }
  }
  return null
}

/** 解析粘贴事件：图优先，其次非空文本，都无则 none。 */
export function parsePaste(e: ClipboardEvent): PastePayload {
  const image = pasteImage(e)
  if (image) return { kind: 'image', file: image }
  const text = e.clipboardData?.getData('text/plain')
  if (text && text.trim() !== '') return { kind: 'text', text }
  return { kind: 'none' }
}
