// MemoCard 独立契约测试（组件自洽、零后端桩）：
//   ① 行/卡双形态的呈现契约（标题/摘要切行/单行化/色彩封边/置顶描边/空态占位）；
//   ② 遮罩纪律的反证锁——masked 态整棵子树（含全部属性位）不得出现 title/text
//      任何明文，HTML title 属性同列泄漏面；MD 徽标随之熄灭；
//   ③ looksLikeMarkdown 联动（徽标只认 utils/markdown 单源判定）；
//   ④ 键盘可达（role=button + Enter/Space → select）与四枚 emits。
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import MemoCard from '../MemoCard.vue'

const SENT_TITLE = '生产库连接串-SENTINEL-T'
const SENT_TEXT = 'pw=topsecret-SENTINEL-C\n## 小节\n- 列表项'
const SENT_BODY_SHORT = 'SENTINEL-C'

/** 相对真实时钟偏移构造 updatedAt（fmtAgo 档位可断言且永不过期） */
const ago = (min: number) => new Date(Date.now() - min * 60000).toISOString()

const base = {
  title: SENT_TITLE,
  text: 'PORT=5432\nHOST=10.0.0.8\nUSER=app',
  tags: ['#SQL', '#运维'],
  pinned: false,
  masked: false,
  updatedAt: ago(120),
  variant: 'grid' as const,
}
const card = (over: Record<string, unknown> = {}, slots = {}) =>
  mount(MemoCard, { props: { ...base, ...over }, slots })

describe('MemoCard 卡/行双形态呈现', () => {
  it('grid 卡：标题/多行摘要/标签丸/相对时间齐备，形态类挂 mc-grid', () => {
    const w = card()
    expect(w.classes()).toContain('mc-grid')
    expect(w.find('.mc-title').text()).toBe(SENT_TITLE)
    expect(w.find('.mc-preview').text()).toContain('HOST=10.0.0.8')
    expect(w.findAll('.mc-tag').map((t) => t.text())).toEqual(['#SQL', '#运维'])
    expect(w.find('.mc-time').text()).toBe('2 小时前')
  })

  it('list 行：摘要只吃首非空行（后续行不进 DOM），形态类挂 mc-list', () => {
    const w = card({ variant: 'list' })
    expect(w.classes()).toContain('mc-list')
    const line = w.find('.mc-preview').text()
    expect(line).toBe('PORT=5432')
    expect(line).not.toContain('HOST')
  })

  it('摘要超 5 行截断补「…」（与 cardPreview 单源联动）', () => {
    const text = Array.from({ length: 7 }, (_, i) => `L${i + 1}`).join('\n')
    expect(card({ text }).find('.mc-preview').text()).toBe('L1\nL2\nL3\nL4\nL5\n…')
  })

  it('空正文给「（空便签）」占位；空白标题给「无标题便签」斜体档', () => {
    const w = card({ text: '  \n ', title: '   ' })
    expect(w.find('.mc-preview').text()).toBe('（空便签）')
    expect(w.find('.mc-title').text()).toBe('无标题便签')
    expect(w.find('.mc-title').classes()).toContain('mc-void')
  })

  it('色彩封边吃 memoColorHex 用户色板，未知值与缺省回落 blue', () => {
    expect(card({ color: 'rose' }).attributes('style')).toContain('border-left-color: #f43f5e')
    expect(card().attributes('style')).toContain('border-left-color: #3b82f6')
    expect(card({ color: 'green' }).attributes('style')).toContain('border-left-color: #3b82f6')
  })

  it('pinned 挂 is-pinned 描边档', () => {
    expect(card({ pinned: true }).classes()).toContain('is-pinned')
  })

  it('tags 传 null（MemoItem 原样）不渲染标签区且不炸', () => {
    const w = card({ tags: null })
    expect(w.find('.mc-tags').exists()).toBe(false)
  })

  it('非法 updatedAt：相对时间落「—」，time 不挂脏 title 提示', () => {
    const w = card({ updatedAt: 'nope' })
    expect(w.find('.mc-time').text()).toBe('—')
    expect(w.find('.mc-time').attributes('title')).toBeUndefined()
  })
})

describe('MemoCard 遮罩纪律（反证锁）', () => {
  it('masked 态整棵子树（含全部属性）不出现 title/text 明文', () => {
    const w = card({ masked: true, text: SENT_TEXT, title: SENT_TITLE })
    const html = w.html()
    expect(html).not.toContain(SENT_TITLE)
    expect(html).not.toContain('SENTINEL-T')
    expect(html).not.toContain('SENTINEL-C')
    expect(html).not.toContain('topsecret')
    expect(html).not.toContain('## 小节')
    // 逐节点属性面扫一遍：任何属性值都不得夹带明文
    for (const node of w.findAll('*')) {
      for (const v of Object.values(node.attributes())) {
        expect(v).not.toContain(SENT_TITLE)
        expect(v).not.toContain(SENT_BODY_SHORT)
      }
    }
  })

  it('masked 标题位是固定文案+圆点，且永不挂 title 属性（title 也是泄漏面）', () => {
    const w = card({ masked: true })
    expect(w.find('.mc-title').text()).toBe('敏感便签')
    expect(w.find('.mc-title').attributes('title')).toBeUndefined()
    expect(w.find('.mc-preview').text()).toContain('•••')
    expect(w.find('.mc-preview').classes()).toContain('masked')
    expect(w.find('.mc-preview').attributes('aria-label')).toBe('敏感信息已遮罩')
  })

  it('masked 熄灭 MD 徽标（结构泄漏也是泄漏）——即便正文满是 Markdown', () => {
    expect(card({ masked: true, text: SENT_TEXT }).find('.mc-md').exists()).toBe(false)
  })

  it('撤遮罩出口常驻：masked 态仍发 toggleMask', async () => {
    const w = card({ masked: true })
    expect(w.findAll('.mc-btn')[0].attributes('title')).toBe('揭示敏感信息')
    await w.findAll('.mc-btn')[0].trigger('click')
    expect(w.emitted('toggleMask')).toHaveLength(1)
  })

  it('非 masked 才有 hover 明文提示（title 属性回显标题）', () => {
    expect(card().find('.mc-title').attributes('title')).toBe(SENT_TITLE)
  })
})

describe('MemoCard MD 徽标与 looksLikeMarkdown 单源联动', () => {
  it('正文含块级 MD 结构才出徽标', () => {
    expect(card({ text: '## 标题\n正文' }).find('.mc-md').exists()).toBe(true)
    expect(card({ text: '```sql\nSELECT 1\n```' }).find('.mc-md').exists()).toBe(true)
  })

  it('SQL/cURL 类代码便签不误伤（无成对/块级形态则无徽标）', () => {
    expect(card({ text: 'SELECT * FROM users WHERE id=1' }).find('.mc-md').exists()).toBe(false)
    expect(card({ text: '' }).find('.mc-md').exists()).toBe(false)
  })
})

describe('MemoCard 交互 emits', () => {
  it('标题区 role=button：点击/Enter/Space 发 select，其它键不发', async () => {
    const w = card()
    const t = w.find('.mc-title')
    expect(t.attributes('role')).toBe('button')
    expect(t.attributes('tabindex')).toBe('0')
    await t.trigger('click')
    await t.trigger('keydown.enter')
    await t.trigger('keydown.space')
    expect(w.emitted('select')).toHaveLength(3)
    await t.trigger('keydown.a')
    expect(w.emitted('select')).toHaveLength(3)
  })

  it('摘要块点击也是 select 出口（鼠标侧与现视图点内容开详情一致）', async () => {
    const w = card()
    await w.find('.mc-preview').trigger('click')
    expect(w.emitted('select')).toHaveLength(1)
  })

  it('遮罩/置顶钮各发 toggleMask/togglePin，置顶钮 tooltip 随态翻转', async () => {
    const w = card()
    const [maskBtn, pinBtn] = w.findAll('.mc-btn')
    await maskBtn.trigger('click')
    await pinBtn.trigger('click')
    expect(w.emitted('toggleMask')).toHaveLength(1)
    expect(w.emitted('togglePin')).toHaveLength(1)
    expect(pinBtn.attributes('title')).toBe('固定置顶')
    expect(card({ pinned: true }).findAll('.mc-btn')[1].attributes('title')).toBe('取消置顶')
  })

  it('标签丸可点可键盘，发 tagSelect 带原样标签', async () => {
    const w = card()
    const tag = w.find('.mc-tag')
    expect(tag.attributes('role')).toBe('button')
    await tag.trigger('click')
    await w.findAll('.mc-tag')[1].trigger('keydown.enter')
    expect(w.emitted('tagSelect')).toEqual([['#SQL'], ['#运维']])
  })

  it('#actions 插槽透传宿主动作（编辑/删除等归视图，本件零感知）', () => {
    const w = card({}, { actions: '<button class="x-edit">✏️</button>' })
    expect(w.find('.mc-tools .x-edit').exists()).toBe(true)
  })
})
