// ClipRow 独立契约测试（呈现件、零后端桩）：R-F2 行结构三区分离——44px
// 缩略图井（image 吃 List 直供 thumb，缺图/脏值回落图标井；非 image 身份
// 即使脏挂 thumb 也不出图）、主区（preview 截断 + 分级元数据行：徽标→标签
// →尾部机器值簇）、行尾常驻快操作。动作只发意图事件（Set/TogglePin/Delete
// 归视图）；主区整体是键盘可达的 select 入口。
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

describe('缩略图井（R-G2 消费面）', () => {
  const THUMB = 'data:image/jpeg;base64,/9j/4AAQ'

  it('image 条目有 thumb → 渲染 <img>（装饰件：alt 空 + 井位 aria-hidden），不再出图标', () => {
    const w = row({ kind: 'image', preview: '', thumb: THUMB })
    const img = w.find('.cr-thumb')
    expect(img.attributes('src')).toBe(THUMB)
    expect(img.attributes('alt')).toBe('')
    expect(w.find('.cr-well').attributes('aria-hidden')).toBe('true')
    expect(w.find('.cr-well svg').exists()).toBe(false)
  })

  it('image 缺 thumb → 井位回落图标（tile 井底给图条目身份留占位语义）', () => {
    const w = row({ kind: 'image', preview: '', width: 800, height: 600 })
    expect(w.find('.cr-thumb').exists()).toBe(false)
    expect(w.find('.cr-well-tile').exists()).toBe(true)
    expect(w.find('.cr-well svg').exists()).toBe(true)
  })

  it('安全闸：非 data:image/ 脏值不进出图；非 image 身份即使挂 thumb 也不出图', () => {
    expect(row({ kind: 'image', thumb: 'javascript:alert(1)' }).find('.cr-thumb').exists()).toBe(false)
    expect(row({ kind: 'text', thumb: 'https://evil/x.png' }).find('.cr-thumb').exists()).toBe(false)
    expect(row({ kind: 'text', thumb: 'data:image/jpeg;base64,AAA' }).find('.cr-thumb').exists()).toBe(false)
  })

  it('非 image 类别：井位裸图标无井底（左轨对齐但不给文本账刷底噪）', () => {
    const w = row()
    expect(w.find('.cr-well-tile').exists()).toBe(false)
    expect(w.find('.cr-well svg').exists()).toBe(true)
  })
})

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

  it('元数据分级：敏感→置顶→片段徽标在前、autoTags 丸次之、尾部簇收来源/计数/时间', () => {
    const w = row({
      autoTags: ['url', 'cjk'],
      pinned: true,
      manual: true,
      sensitive: true,
      sourceApp: 'Google Chrome — 项目页',
      useCount: 3,
    })
    const badges = w.findAll('.cr-badge').map((b) => b.text())
    expect(badges[0]).toContain('敏感')
    expect(badges[1]).toContain('置顶')
    expect(badges[2]).toContain('片段')
    const danger = w.findAll('.cr-badge')[0]
    expect(danger.classes()).toContain('chip-danger')
    expect(danger.attributes('title')).toContain('不进 AI（MCP 检索）通道')
    expect(w.findAll('.cr-tag').map((t) => t.text())).toEqual(['url', 'cjk'])
    const tail = w.find('.cr-tail')
    expect(tail.find('.cr-src').text()).toBe('Google Chrome — 项目页')
    expect(tail.find('.cr-count').text()).toBe('用 3 次')
    expect(tail.find('.cr-time').text()).toBe('5 分钟前')
    expect(tail.find('.cr-time').attributes('title')).toContain('复制于')
  })

  it('无标签/无来源/零计数各归各的静默：脏数据不给空串占位', () => {
    const w = row()
    expect(w.findAll('.cr-tag')).toHaveLength(0)
    expect(w.findAll('.cr-badge')).toHaveLength(0)
    expect(w.find('.cr-src').exists()).toBe(false)
    expect(w.find('.cr-count').exists()).toBe(false)
    expect(row({ useCount: 0 }).find('.cr-count').exists()).toBe(false)
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
  it('主区点击/回车/空格 → select（整体是打开详情热区）；工具钮三族各发各的', async () => {
    const w = row()
    await w.find('.cr-main').trigger('click')
    await w.find('.cr-main').trigger('keydown', { key: 'Enter' })
    await w.find('.cr-main').trigger('keydown', { key: ' ' })
    expect(w.emitted('select')).toHaveLength(3)
    await w.find('.cr-preview').trigger('click') // preview 在热区内，冒泡同样只发一次
    expect(w.emitted('select')).toHaveLength(4)
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

  it('主区键盘可达（role=button + tabindex + 无障碍名），井位与图标装饰性 aria 隐藏', () => {
    const w = row()
    expect(w.find('.cr-main').attributes('role')).toBe('button')
    expect(w.find('.cr-main').attributes('tabindex')).toBe('0')
    expect(w.find('.cr-main').attributes('aria-label')).toBe('打开详情')
    expect(w.find('.cr-well').attributes('aria-hidden')).toBe('true')
    expect(w.find('.cr-well svg').attributes('aria-hidden')).toBe('true')
  })
})
