// ClipDetail 独立契约测试（页面板件、零后端桩）：三分支呈现与四态齐活——
//   text → 等宽正文原样（不猜 Markdown 结构）；image → blobData data URL 预览
//   + 尺寸/体积机器值（blob 未回填如实占位不放空 <img>）；file → 路径逐行可
//   逐条复制（浏览器剪贴板走 useClipboard 统一回执）；loading/error/close/
//   retry/copy/togglePin/delete 全部只发意图或自持浏览器侧动作。
import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import ClipDetail from '../ClipDetail.vue'
import { useToast } from '../../../composables/useToast'
import type { ClipEntry } from '../../../types/clipboard'

const NOW = Date.now()
const detail = (props: Record<string, unknown>) =>
  mount(ClipDetail, { props: { entry: null, loading: false, error: '', ...props } })
const entry = (over: Partial<ClipEntry> = {}): ClipEntry => ({
  id: 'c1',
  hash: 'h1',
  kind: 'text',
  preview: '摘要',
  text: '完整正文\n第二行',
  byteSize: 40,
  createdAt: NOW - 2 * 60_000,
  ...over,
})

afterEach(() => {
  useToast().clearToast()
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

describe('四态分流', () => {
  it('loading 优先：等宽页收起，只给在途状态框（点名动作不只转圈）', () => {
    const w = detail({ entry: entry(), loading: true })
    expect(w.find('.state-box').text()).toContain('正在取回完整内容')
    expect(w.find('.cd-plain').exists()).toBe(false)
    expect(w.find('.cd-head').exists()).toBe(false) // 没拿到条目不摆空头
  })

  it('error 可见可重试：横幅 + retry 意图', async () => {
    const w = detail({ error: '读取详情失败: 磁盘忙' })
    expect(w.find('.cd-error').text()).toContain('磁盘忙')
    await w.find('.cd-error button').trigger('click')
    expect(w.emitted('retry')).toHaveLength(1)
  })

  it('entry=null 且无 loading/error：整页静默（宿主保证此组合罕见）', () => {
    expect(detail({}).text()).toBe('')
  })
})

describe('文本分支', () => {
  it('等宽正文原样换行呈现；头标「文本」；页脚动作发意图', () => {
    const w = detail({ entry: entry() })
    expect(w.find('.cd-plain').classes()).toContain('mono')
    expect(w.find('.cd-plain').text()).toContain('第二行')
    expect(w.find('.cd-kind').text()).toContain('文本')
    expect(w.find('.cd-image').exists()).toBe(false)
    expect(w.find('.cd-files').exists()).toBe(false)
    expect(w.findAll('.cd-foot button').length).toBe(3) // 复制/置顶/删除三钮（spacer 非钮）
  })

  it('页脚三动作：复制=Set 意图 / 置顶随态翻面 / 删除意图；关闭钮收头行', async () => {
    const w = detail({ entry: entry({ pinned: true }) })
    await w.find('.cd-copy-all').trigger('click')
    expect(w.emitted('copy')).toHaveLength(1)
    expect(w.find('.cd-foot button:nth-child(2)').text()).toContain('取消置顶')
    await w.find('.cd-foot button:nth-child(2)').trigger('click')
    await w.find('.cd-foot .text-danger').trigger('click')
    expect(w.emitted('togglePin')).toHaveLength(1)
    expect(w.emitted('delete')).toHaveLength(1)
    await w.find('.cd-close').trigger('click')
    expect(w.emitted('close')).toHaveLength(1)
    expect(w.find('.cd-kind').text()).toContain('已置顶')
  })

  it('空正文给占位；使用计数与来源/体积机器值上脚注', () => {
    const w = detail({ entry: entry({ text: '', useCount: 4, sourceApp: 'Terminal' }) })
    expect(w.find('.cd-plain').text()).toBe('（空文本）')
    expect(w.find('.cd-meta').text()).toContain('已使用 4 次')
    expect(w.find('.cd-meta').text()).toContain('Terminal')
    const bare = detail({ entry: entry() })
    expect(bare.find('.cd-meta').text()).toContain('来源 未知窗口')
    expect(bare.find('.cd-meta').text()).not.toContain('已使用')
  })

  it('manual 片段在头行亮身份', () => {
    expect(detail({ entry: entry({ manual: true }) }).find('.cd-kind').text()).toContain('固定片段')
  })
})

describe('图片分支', () => {
  it('blobData → PNG data URL 预览 + 尺寸/体积机器值', () => {
    const w = detail({
      entry: entry({ kind: 'image', blobData: 'iVBORw0KG', width: 800, height: 600, byteSize: 20480 }),
    })
    const img = w.find('.cd-img')
    expect(img.attributes('src')).toBe('data:image/png;base64,iVBORw0KG')
    expect(img.attributes('alt')).toContain('800×600')
    expect(w.find('.cd-img-meta').text()).toBe('800×600 px · 20 KB')
    expect(w.find('.cd-plain').exists()).toBe(false)
  })

  it('blob 未回填：如实占位，不放 src 为空的 <img>', () => {
    const w = detail({ entry: entry({ kind: 'image', width: 10, height: 10 }) })
    expect(w.find('.cd-img').exists()).toBe(false)
    expect(w.find('.state-box').text()).toContain('图片数据未回填')
  })
})

describe('文件分支', () => {
  it('路径逐行列出；逐条复制走浏览器剪贴板统一回执；不碰宿主 Set', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    vi.stubGlobal('isSecureContext', true)
    vi.stubGlobal('navigator', { ...navigator, clipboard: { writeText } })
    const w = detail({
      entry: entry({ kind: 'file', files: ['C:\\tmp\\甲.txt', 'D:\\乙.pdf'], text: undefined }),
    })
    const rows = w.findAll('.cd-file')
    expect(rows).toHaveLength(2)
    expect(rows[0].find('.cd-path').text()).toBe('C:\\tmp\\甲.txt')
    await rows[1].find('.cd-file-copy').trigger('click')
    expect(writeText).toHaveBeenCalledWith('D:\\乙.pdf')
    expect(useToast().toastMsg.value).toBe('路径已复制到剪贴板')
    expect(w.emitted('copy')).toBeUndefined() // 单行复制不发整表回填意图
    expect(w.find('.cd-plain').exists()).toBe(false)
  })

  it('空清单给占位行', () => {
    const w = detail({ entry: entry({ kind: 'file', files: [] }) })
    expect(w.find('.cd-file').exists()).toBe(false)
    expect(w.find('.state-box').text()).toContain('文件列表为空')
  })
})

describe('敏感警示', () => {
  it('sensitive 条目顶部恒挂警示条（不进 AI 检索通道的话术）', () => {
    const w = detail({ entry: entry({ sensitive: true }) })
    expect(w.find('.cd-sensitive').text()).toContain('不进 AI（MCP 检索）通道')
    expect(detail({ entry: entry() }).find('.cd-sensitive').exists()).toBe(false)
  })
})
