// parsePaste / pasteImage：粘贴事件的 图→文→无 三态分流纯函数。
import { describe, expect, it } from 'vitest'
import { parsePaste, pasteImage } from '../paste'

interface ItemSpec {
  kind: 'file' | 'string'
  type?: string
  file?: File | null
}

// 构造最小可测的 ClipboardEvent 替身（happy-dom 不支持 new ClipboardEvent 带数据）
function fakePaste(items: ItemSpec[] = [], text = ''): ClipboardEvent {
  const clipboardData = {
    items: items.map((i) => ({
      kind: i.kind,
      type: i.type ?? '',
      getAsFile: () => (i.kind === 'file' ? (i.file ?? null) : null),
    })),
    getData: (fmt: string) => (fmt === 'text/plain' ? text : ''),
  }
  return { clipboardData } as unknown as ClipboardEvent
}

function imageFile(): File {
  return new File([new Uint8Array([1])], 'shot.png', { type: 'image/png' })
}

describe('pasteImage', () => {
  it('取 image/* 文件项', () => {
    const f = imageFile()
    expect(pasteImage(fakePaste([{ kind: 'file', type: 'image/png', file: f }]))).toBe(f)
  })

  it('非图片文件不接管（Explorer 复制的文件项放行给浏览器默认）', () => {
    const doc = new File(['x'], 'a.docx', { type: 'application/vnd.openxmlformats-officedocument.wordprocessingml.document' })
    expect(pasteImage(fakePaste([{ kind: 'file', type: doc.type, file: doc }]))).toBeNull()
  })

  it('无 clipboardData：null 不抛', () => {
    expect(pasteImage({ clipboardData: null } as unknown as ClipboardEvent)).toBeNull()
  })
})

describe('parsePaste', () => {
  it('图片优先于文本（截图常同时携带位图与路径文本）', () => {
    const f = imageFile()
    const e = fakePaste([{ kind: 'file', type: 'image/png', file: f }], 'C:\\tmp\\shot.png')
    expect(parsePaste(e)).toEqual({ kind: 'image', file: f })
  })

  it('纯文本 → text 原样（不 trim 内容本体）', () => {
    const e = fakePaste([], 'curl -X GET http://x ')
    expect(parsePaste(e)).toEqual({ kind: 'text', text: 'curl -X GET http://x ' })
  })

  it('空白文本与无数据都归 none', () => {
    expect(parsePaste(fakePaste([], '   \n'))).toEqual({ kind: 'none' })
    expect(parsePaste(fakePaste([], ''))).toEqual({ kind: 'none' })
    expect(parsePaste({ clipboardData: undefined } as unknown as ClipboardEvent)).toEqual({ kind: 'none' })
  })

  it('file 项 getAsFile 为 null 时继续找文本', () => {
    const e = fakePaste([{ kind: 'file', type: 'image/png', file: null }], 'fallback')
    expect(parsePaste(e)).toEqual({ kind: 'text', text: 'fallback' })
  })
})

