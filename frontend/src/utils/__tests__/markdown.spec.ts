// 随手记 Markdown 子集渲染器契约：结构渲染能力 + 安全红线（XSS 用例为第一优先级，
// 任何一条失败都禁止合入——渲染结果直接进 v-html）。
import { describe, expect, it } from 'vitest'
import { escapeHtml, looksLikeMarkdown, renderInline, renderMarkdown } from '../markdown'

describe('escapeHtml', () => {
  it('五字符全集转义，其余原样', () => {
    expect(escapeHtml(`<a href="x">&'它</a>`)).toBe(
      '&lt;a href=&quot;x&quot;&gt;&amp;&#39;它&lt;/a&gt;',
    )
    expect(escapeHtml('普通文本 plain123')).toBe('普通文本 plain123')
  })
})

describe('renderMarkdown 块级结构', () => {
  it('标题 h1–h6，尾随 # 号剥离', () => {
    expect(renderMarkdown('# 一级')).toBe('<h1>一级</h1>')
    expect(renderMarkdown('###### 六级 #')).toBe('<h6>六级</h6>')
    // 七枚 # 不是标题，落段落
    expect(renderMarkdown('####### 过界')).toBe('<p>####### 过界</p>')
  })

  it('段落：普通行合并成段，单换行按硬换行渲染', () => {
    expect(renderMarkdown('第一行\n第二行')).toBe('<p>第一行<br />\n第二行</p>')
    expect(renderMarkdown('A\n\nB')).toBe('<p>A</p>\n<p>B</p>')
  })

  it('围栏代码块：内容零渲染、语言名进 class、EOF 未闭合也收口', () => {
    expect(renderMarkdown('```sql\nSELECT * FROM <t>\n```')).toBe(
      '<pre><code class="language-sql">SELECT * FROM &lt;t&gt;</code></pre>',
    )
    expect(renderMarkdown('~~~\nplain\n~~~')).toBe('<pre><code>plain</code></pre>')
    expect(renderMarkdown('```\n未闭合')).toBe('<pre><code>未闭合</code></pre>')
    // 围栏内的伪标记（标题/链接）不得泄漏为结构
    expect(renderMarkdown('```\n# 不是标题\n[x](http://a.cn)\n```')).toBe(
      '<pre><code># 不是标题\n[x](http://a.cn)</code></pre>',
    )
  })

  it('无序列表与有序列表（首项编号进 start，空行断列表）', () => {
    expect(renderMarkdown('- a\n- **b**\n* c')).toBe(
      '<ul><li>a</li><li><strong>b</strong></li><li>c</li></ul>',
    )
    expect(renderMarkdown('1. 一\n2. 二')).toBe('<ol><li>一</li><li>二</li></ol>')
    expect(renderMarkdown('3) 起步\n4) 继续')).toBe('<ol start="3"><li>起步</li><li>继续</li></ol>')
    expect(renderMarkdown('- a\n\n- b')).toBe('<ul><li>a</li></ul>\n<ul><li>b</li></ul>')
    // 缩进续行并入当前项，按硬换行呈现
    expect(renderMarkdown('- 标题\n  续行')).toBe('<ul><li>标题<br />\n续行</li></ul>')
  })

  it('引用：剥一层 > 递归渲染，嵌套引用成形', () => {
    expect(renderMarkdown('> 引用 **加粗**')).toBe(
      '<blockquote><p>引用 <strong>加粗</strong></p></blockquote>',
    )
    expect(renderMarkdown('> > 套娃')).toBe(
      '<blockquote><blockquote><p>套娃</p></blockquote></blockquote>',
    )
  })

  it('分隔线三形态；与列表减号区分（- 后无空格且满三枚才算线）', () => {
    expect(renderMarkdown('---\n***\n___')).toBe('<hr />\n<hr />\n<hr />')
    expect(renderMarkdown('- - -')).toBe('<hr />')
  })
})

describe('renderInline 行内结构', () => {
  it('粗体/斜体/粗斜体/删除线/行内代码', () => {
    expect(renderInline('**粗** 和 __粗2__')).toBe('<strong>粗</strong> 和 <strong>粗2</strong>')
    expect(renderInline('*斜* 与 _斜2_')).toBe('<em>斜</em> 与 <em>斜2</em>')
    expect(renderInline('***兼***')).toBe('<strong><em>兼</em></strong>')
    expect(renderInline('~~删~~')).toBe('<del>删</del>')
    expect(renderInline('`a < b`')).toBe('<code>a &lt; b</code>')
    expect(renderInline('行内 `a * b` 星号不炸')).toBe('行内 <code>a * b</code> 星号不炸')
  })

  it('snake_case 与乘法星号不被误伤成斜体', () => {
    expect(renderInline('user_name_field')).toBe('user_name_field')
    expect(renderInline('a__b__c')).toBe('a__b__c')
  })

  it('链接：http/https 成链并带安全属性；非法协议降级纯文本', () => {
    expect(renderInline('[官网](https://example.com/a?b=1&c=2)')).toBe(
      '<a href="https://example.com/a?b=1&amp;c=2" target="_blank" rel="noopener noreferrer">官网</a>',
    )
    expect(renderInline('[x](http://a.cn)')).toContain('<a href="http://a.cn"')
    for (const evil of [
      '[x](javascript:alert(1))',
      '[x](JaVaScRiPt:alert(1))',
      '[x](java\tscript:alert(1))',
      '[x](data:text/html;base64,PHN2Zz4=)',
      '[x](vbscript:msgbox(1))',
      '[x](/relative/path)',
      '[x](#anchor)',
      '[x](mailto:a@b.c)',
      '[x](//evil.cn)',
    ]) {
      const html = renderInline(evil)
      expect(html).not.toContain('<a ')
      expect(html).not.toContain('href')
    }
  })

  it('链接文本内允许强调与代码（label 经完整行内链）', () => {
    expect(renderInline('[**重点**](https://a.cn)')).toContain(
      '><strong>重点</strong></a>',
    )
  })

  it('renderInline 支持跨行硬换行模式', () => {
    expect(renderInline('一\n二', true)).toBe('一<br />\n二')
  })
})

describe('XSS 安全红线（渲染结果直进 v-html，逐条锁死）', () => {
  it('<script> 整段与半截标签全部转义', () => {
    const html = renderMarkdown('<script>alert(1)</script>')
    expect(html).not.toContain('<script')
    expect(html).toContain('&lt;script&gt;')
    const half = renderMarkdown('<img src=x onerror=alert(1)>')
    // 关键在"角括号必须已转义"——字面量 onerror 作为纯文本无害留存
    expect(half).not.toContain('<img')
    expect(half).toContain('&lt;img')
  })

  it('HTML 实体二次编码不进属性：转义先行杜绝引号逃逸', () => {
    const html = renderMarkdown('[a](https://x.cn/"onmouseover="alert(1))')
    // URL 里的引号已被转义为 &quot;，无法闭合 href 属性
    expect(html).not.toContain('"onmouseover')
    const matches = html.match(/href="[^"]*"/g) ?? []
    for (const m of matches) expect(m).toMatch(/^href="https?:\/\/[^"]*"$/)
  })

  it('围栏代码块内的一切保持字面量', () => {
    expect(renderMarkdown('```\n<script>x</script>\n```')).toBe(
      '<pre><code>&lt;script&gt;x&lt;/script&gt;</code></pre>',
    )
  })

  it('伪造占位符（\u0000C0\u0000）在入口被剥离，不能借还原通道注 HTML', () => {
    const html = renderMarkdown('正常\u0000C0\u0000注入')
    expect(html).not.toContain('\u0000')
    // 剥 NUL 后只剩无害字面量 "C0"，绝不触发占位还原
    expect(html).toBe('<p>正常C0注入</p>')
  })

  it('注释/条件注释/SVG 全部无害化', () => {
    const html = renderMarkdown('<!-- <a href="javascript:x"> -->\n<svg onload=alert(1)>')
    expect(html).not.toContain('<!--')
    expect(html).not.toContain('<a')
    expect(html).not.toContain('<svg')
  })
})

describe('looksLikeMarkdown 判别', () => {
  it('块级/成对结构 → true', () => {
    expect(looksLikeMarkdown('# 标题')).toBe(true)
    expect(looksLikeMarkdown('- 条目\n- 条目2')).toBe(true)
    expect(looksLikeMarkdown('```\ncode\n```')).toBe(true)
    expect(looksLikeMarkdown('**重点**')).toBe(true)
    expect(looksLikeMarkdown('[链接](https://a.cn)')).toBe(true)
  })
  it('纯代码片段/散文 → false（SQL 乘法星号、URL 单贴不误判）', () => {
    expect(looksLikeMarkdown('SELECT * FROM users WHERE a*b>1')).toBe(false)
    expect(looksLikeMarkdown('sk-abc123 记一次密钥')).toBe(false)
    expect(looksLikeMarkdown('https://example.com/plain')).toBe(false)
    expect(looksLikeMarkdown('')).toBe(false)
  })
})
