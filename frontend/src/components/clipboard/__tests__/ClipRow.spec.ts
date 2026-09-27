// ClipRow 独立契约测试（呈现件、零后端桩）：行结构吃齐契约呈现面——kind 图标、
// preview（空值按类别兜底）、autoTags 丸、置顶/片段/敏感徽标、来源窗口、相对
// 时间与悬浮全量、使用计数；动作只发意图事件（Set/TogglePin/Delete 归视图）。
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import ClipRow from '../ClipRow.vue'
import type { ClipEntry } from '../../../types/clipboard'

const NOW = Date.now()
const row = (over: Partial<ClipEntry> = {}) => {
  const entry: ClipEntry = {
    id: 'c1',
    hash: 'h1',
    kind: 'text',
    preview: 'git commit -m 提交规范',
    byteSize: 42,
    createdAt: NOW - 5 * 60_000,
    ...over,
  }
  return mount(ClipRow, { props: { entry } })
}

const toolBtn = (w: ReturnType<typeof row>, label: string) =>
  w.findAll('.cr-tools button').find((b) => b.attributes('aria-label') === label)

describe('主区呈现', () => {
  it('preview 原样上屏；摘要空时按类别兜底（图片报尺寸/文件报个数/文本给占位）', () => {
    expect(row().find('.cr-preview').text()).toBe('git commit -m 提交规范')
    expect(
      row({ kind: 'image', preview: '', width: 1920, height: 1080 }).find('.cr-preview').text(),
    ).toBe('图片 1920×1080')
    expect(
      row({ kind: 'file', preview: '', files: ['a.txt', 'b.txt'] }).find('.cr-preview').text(),
    ).toBe('2 个文件')
    expect(row({ preview: '   ' }).find('.cr-preview').text()).toBe('（空文本）')
  })

  it('autoTags 小丸逐个渲染；无标签整段缺席', () => {
    const w = row({ autoTags: ['url', 'cjk'] })
    expect(w.findAll('.cr-tag').map((t) => t.text())).toEqual(['url', 'cjk'])
    expect(row().findAll('.cr-tag')).toHaveLength(0)
  })

  it('徽标族：置顶文字徽标、片段芯片、敏感 ⚠ 芯片（title 明写不进 AI 通道）', () => {
    const w = row({ pinned: true, manual: true, sensitive: true })
    expect(w.find('.cr-pin').text()).toContain('置顶')
    expect(w.findAll('.cr-badge').map((b) => b.text()).join(' ')).toContain('片段')
    const danger = w.findAll('.cr-badge').find((b) => b.classes().includes('chip-danger'))!
    expect(danger.text()).toContain('敏感')
    expect(danger.attributes('title')).toContain('不进 AI（MCP 检索）通道')
    expect(row().find('.cr-badge').exists()).toBe(false)
  })

  it('来源窗口与相对时间/悬浮全量时间/使用计数', () => {
    const w = row({ sourceApp: 'Google Chrome — 项目页', useCount: 3 })
    expect(w.find('.cr-src').text()).toBe('Google Chrome — 项目页')
    expect(w.find('.cr-time').text()).toBe('5 分钟前')
    expect(w.find('.cr-time').attributes('title')).toContain('复制于')
    expect(w.find('.cr-count').text()).toBe('用 3 次')
    expect(row({ useCount: 0 }).find('.cr-count').exists()).toBe(false)
    expect(row().find('.cr-src').exists()).toBe(false) // 脏数据无来源不给空串占位
  })

  it('宿主差分：active 挂 is-active；置顶数据挂 is-pinned（左沿主色描边）', () => {
    const w = row({ pinned: true })
    expect(w.classes()).toContain('is-pinned')
    expect(w.classes()).not.toContain('is-active')
    const w2 = mount(ClipRow, {
      props: {
        entry: { id: 'x', hash: 'h', kind: 'text', preview: 'p', byteSize: 1, createdAt: NOW } as ClipEntry,
        active: true,
      },
    })
    expect(w2.classes()).toContain('is-active')
  })
})

describe('动作只发意图', () => {
  it('preview 点击/回车/空格 → select；工具钮三族各发各的', async () => {
    const w = row()
    await w.find('.cr-preview').trigger('click')
    await w.find('.cr-preview').trigger('keydown', { key: 'Enter' })
    await w.find('.cr-preview').trigger('keydown', { key: ' ' })
    expect(w.emitted('select')).toHaveLength(3)
    await toolBtn(w, '复制到剪贴板')!.trigger('click')
    await toolBtn(w, '固定置顶')!.trigger('click')
    await toolBtn(w, '删除此条')!.trigger('click')
    expect(w.emitted('copy')).toHaveLength(1)
    expect(w.emitted('togglePin')).toHaveLength(1)
    expect(w.emitted('delete')).toHaveLength(1)
  })

  it('已置顶行的钮位翻面：aria-label 与 title 同步「取消置顶」', () => {
    const w = row({ pinned: true })
    const pin = w.findAll('.cr-tools button').find((b) => b.attributes('aria-label') === '取消置顶')
    expect(pin).toBeDefined()
    expect(pin!.classes()).toContain('cr-on')
  })

  it('行摘要键盘可达（role=button + tabindex），图标装饰性 aria-hidden', () => {
    const w = row()
    expect(w.find('.cr-preview').attributes('role')).toBe('button')
    expect(w.find('.cr-preview').attributes('tabindex')).toBe('0')
    expect(w.find('.cr-icon svg').attributes('aria-hidden')).toBe('true')
  })
})
