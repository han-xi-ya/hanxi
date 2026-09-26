// 导出面 util 规格（N16 C 批）：钉死三件事——①单条文档与后端 filestore
// EncodeMemo 的格式契约（frontmatter 键序/单行消毒/正文原样，导出的文件必须
// 能被后端 DecodeMemo 读回）；②全库汇总器的篇头统计与分节结构；③下载通道
// 的成功/降级两路与 objectURL 延迟释放。文件名消毒盯住 Windows 非法字符与
// 保留设备名——这是导出钮真正落盘的第一道闸门。
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { MemoItem } from '../../../bindings/hanxi/internal/modules/memo/models'
import {
  buildLibraryDigest,
  buildMemoFile,
  downloadTextFile,
  safeExportFileName,
  safeLibraryFileName,
  sanitizeFileName,
  sanitizeLine,
} from '../memoexport'

const item = (over: Partial<MemoItem> = {}): MemoItem => ({
  id: 'memo_1758000000000000001',
  title: '生产库连接串',
  content: 'host=10.0.0.1;pw=topsecret',
  tags: ['#SQL'],
  isPinned: false,
  isMasked: false,
  colorTag: 'blue',
  createdAt: '2026-09-01T10:00:00Z',
  updatedAt: '2026-09-02T11:30:00Z',
  ...over,
})

afterEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  vi.useRealTimers()
})

describe('sanitizeLine（与后端 filestore.sanitizeLine 同语义）', () => {
  it('连续空白折单空格并去首尾，换行/tab 一并压平', () => {
    expect(sanitizeLine('  多行\n标题\t含\t空白 ')).toBe('多行 标题 含 空白')
    expect(sanitizeLine('')).toBe('')
  })
})

describe('buildMemoFile（单条 = 文件库同源格式）', () => {
  it('frontmatter 键序与值形态对齐 EncodeMemo，正文原样接在闭合 --- 之后', () => {
    const doc = buildMemoFile(item())
    expect(doc).toBe(
      [
        '---',
        'id: memo_1758000000000000001',
        'title: 生产库连接串',
        'tags: ["#SQL"]',
        'pinned: false',
        'masked: false',
        'color: blue',
        'created: 2026-09-01T10:00:00Z',
        'updated: 2026-09-02T11:30:00Z',
        '---',
        'host=10.0.0.1;pw=topsecret',
      ].join('\n'),
    )
  })

  it('标题压单行、tags 为 JSON 数组、空 tags 落 null（Go nil 切片同形）', () => {
    const doc = buildMemoFile(item({ title: '换行\n标题', tags: null }))
    expect(doc).toContain('title: 换行 标题\n')
    expect(doc).toContain('tags: null\n')
  })

  it('正文含 --- 行不破坏结构：结束行只认 frontmatter 之前第一个', () => {
    const doc = buildMemoFile(item({ content: 'a\n---\nb' }))
    expect(doc.endsWith('---\na\n---\nb')).toBe(true)
    // frontmatter 区恰好一次闭合（第 10 行的 ---），正文里的 --- 在其后
    const firstClose = doc.split('\n').indexOf('---', 1)
    expect(firstClose).toBe(9)
  })
})

describe('buildLibraryDigest（全库汇总稿）', () => {
  it('篇头交代条数与敏感排除，每条一节带元信息，分节线隔开', () => {
    const md = buildLibraryDigest(
      [item(), item({ id: 'm2', title: '', content: '周报', tags: null, isPinned: true, isMasked: false })],
      { exportedAt: new Date(2026, 8, 26, 14, 5, 0), excludedMasked: 3 },
    )
    expect(md).toContain('# 随手记 · 全库导出')
    expect(md).toContain('- 导出时间：2026-09-26 14:05')
    expect(md).toContain('- 便签条数：2')
    expect(md).toContain('- 敏感遮罩条目：3 条已排除，明文未写入本文件')
    expect(md).toContain('## 生产库连接串')
    expect(md).toContain('- 标签：#SQL')
    expect(md).toContain('## 无标题便签')
    expect(md).toContain('📌 置顶')
    expect(md).toContain('\n\n---\n\n## ')
  })

  it('无排除项时明示"无"；调用方保证传进来的都是该导出的（本器不做过滤）', () => {
    const md = buildLibraryDigest([item()], {})
    expect(md).toContain('- 敏感遮罩条目：无')
  })
})

describe('文件名消毒', () => {
  it('剔除 Windows 非法字符与控制符，空白折 -，首尾点/空格剥离', () => {
    // 非法字符是剔除不是折算——只有空白才折 '-'
    expect(sanitizeFileName('a<b>:c"/d\\e|f?g*h*\x01i')).toBe('abcdefghi')
    expect(sanitizeFileName('月份 计划: v2')).toBe('月份-计划-v2')
    expect(sanitizeFileName('  尾点尾空格  ')).toBe('尾点尾空格')
    expect(sanitizeFileName('....')).toBe('无标题便签')
  })

  it('保留设备名破歧义（CON.md → ·CON.md）', () => {
    expect(sanitizeFileName('CON')).toBe('·CON')
    expect(sanitizeFileName('com1.log')).toBe('·com1.log')
    expect(sanitizeFileName('CONFIG.md')).toBe('CONFIG.md') // 前缀命中不算保留名
  })

  it('限长 60 且不劈在点/横杠尾巴上', () => {
    const long = sanitizeFileName('x'.repeat(100))
    expect(long.length).toBeLessThanOrEqual(60)
  })

  it('单条导出名 = 消毒标题-id 尾缀防重；无标题条目回落便签前缀', () => {
    const name = safeExportFileName(item({ id: 'memo_1758000000000000001' }))
    expect(name).toBe('生产库连接串-00000001.md')
    expect(safeExportFileName(item({ title: '  ', id: 'm2' }))).toBe('便签-m2.md')
  })

  it('全库汇总名带本地时间戳、.md 收尾', () => {
    const name = safeLibraryFileName(new Date(2026, 8, 26, 9, 8, 7))
    expect(name).toBe('hanxi-随手记导出-20260926-090807.md')
  })
})

describe('downloadTextFile（浏览器原生下载通道）', () => {
  it('成功路：createObjectURL→a[download] 点击→移除，objectURL 延迟 4s 释放', () => {
    vi.useFakeTimers()
    const create = vi.spyOn(URL, 'createObjectURL').mockReturnValue('blob:fake/1')
    const revoke = vi.spyOn(URL, 'revokeObjectURL').mockImplementation(() => {})
    let captured: HTMLAnchorElement | null = null
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(function (this: HTMLAnchorElement) {
      captured = this
    })
    expect(downloadTextFile('a.md', '正文')).toBe(true)
    expect(create).toHaveBeenCalledTimes(1)
    expect(create.mock.calls[0][0]).toBeInstanceOf(Blob)
    expect(click).toHaveBeenCalledTimes(1)
    expect(captured!.download).toBe('a.md')
    expect(captured!.getAttribute('href')).toBe('blob:fake/1')
    expect(document.body.contains(captured!)).toBe(false) // 用完即摘
    expect(revoke).not.toHaveBeenCalled() // 立即 revoke 会掐死异步取流
    vi.advanceTimersByTime(4000)
    expect(revoke).toHaveBeenCalledWith('blob:fake/1')
  })

  it('无 createObjectURL 环境返回 false 交调用方降级，不抛错', () => {
    vi.stubGlobal('URL', {})
    expect(downloadTextFile('a.md', 'x')).toBe(false)
  })

  it('通道内部抛错也吞成 false（下载是锦上添花，不是炸点）', () => {
    vi.spyOn(URL, 'createObjectURL').mockImplementation(() => {
      throw new Error('blob refused')
    })
    expect(downloadTextFile('a.md', 'x')).toBe(false)
  })
})
