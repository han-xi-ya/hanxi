// 随手记 Markdown 子集渲染器（零依赖自研，服务 MemoView 的预览/阅读态）。
//
// 安全纪律（优先级高于一切渲染效果）：
//   1. 先整体 HTML 转义、再做结构替换——用户输入永远不可能带进裸标签/属性；
//   2. 链接协议白名单仅 http/https：`javascript:`、`data:`、协议相对、站内
//      相对路径一律降级为纯文本（大小写不敏感，控制字符/空白先剔除再判定，
//      防 `java\tscript:` 类拆分绕过）；
//   3. 占位符（\u0000 + 序号）在入口剥离输入自带的 NUL，外部无法伪造；
//      占位符内部内容不再参与后续结构替换，防"链接里做粗体、代码里做链接"
//      的嵌套注乱。
//
// 支持子集（刻意不贪全 CommonMark）：
//   标题 #..###### ／ 粗体 **x** __x__ ／ 斜体 *x_ _x_ ／ 删除线 ~~x~~ ／
//   行内代码 `x` ／ 围栏代码块 ``` ~~~（带语言名→class）／ 无序列表 - + * ／
//   有序列表 1. 1)（首项编号→start，非首项续行合并）／ 链接 [文本](url) ／
//   引用 >（可嵌套，经递归再渲染）／ 分隔线 --- *** ___ ／ 段落与列表项内
//   单换行按硬换行渲染（随手记语义：回车即换行，不做 Markdown 软折行合并）。
//
// 已知简化（对便签场景足够，勿当 bug 修）：列表不嵌套、引用外的块间空行
// 断列表、setext 标题与表格不支持、链接不支持 title。

const ESCAPE_MAP: Record<string, string> = {
  '&': '&amp;',
  '<': '&lt;',
  '>': '&gt;',
  '"': '&quot;',
  "'": '&#39;',
}

/** HTML 转义（& < > " ' 五字符集，属性位与文本位通用）。 */
export function escapeHtml(s: string): string {
  return s.replace(/[&<>"']/g, (c) => ESCAPE_MAP[c])
}

/** http/https 白名单链接校验；通过返回净化后的 URL，否则 null（降级纯文本）。 */
function safeHttpUrl(raw: string): string | null {
  // 剔除空白与控制字符（含 NUL/Tab/换行）后必须整体匹配 http(s):// 前缀
  const cleaned = raw.replace(/[\s\u0000-\u001f]+/g, '')
  return /^https?:\/\/[^\s\u0000-\u001f]+$/i.test(cleaned) ? cleaned : null
}

/**
 * 行内渲染：入参为未转义的原文（可含 \n），返回已转义+已渲染的 HTML 片段。
 * breaks=true 时把换行渲染为 <br />（段落/列表项硬换行语义）。
 */
export function renderInline(text: string, breaks = false): string {
  const codes: string[] = []
  const links: string[] = []
  let s = escapeHtml(text)

  // ① 行内代码先占位（其内容不参与任何后续结构替换）
  s = s.replace(/`([^`\n]+)`/g, (_m, c: string) => {
    codes.push(c)
    return `\u0000C${codes.length - 1}\u0000`
  })

  // ② 链接（URL 禁含空白与右括号；协议白名单不过关则原样留在文本里）
  s = s.replace(/\[([^\]]*)\]\(([^)\s]+)\)/g, (m, label: string, url: string) => {
    const href = safeHttpUrl(url)
    if (href === null) return m
    links.push(
      `<a href="${href}" target="_blank" rel="noopener noreferrer">${emphasis(label)}</a>`,
    )
    return `\u0000L${links.length - 1}\u0000`
  })

  // ③ 强调与删除线（只产出固定字面标签，操作对象全是已转义文本）
  s = emphasis(s)

  // ④ 还原占位：先链接（其 HTML 内可能嵌着代码占位）再代码
  s = s.replace(/\u0000L(\d+)\u0000/g, (_m, n: string) => links[Number(n)] ?? '')
  s = s.replace(/\u0000C(\d+)\u0000/g, (_m, n: string) => `<code>${codes[Number(n)] ?? ''}</code>`)

  if (breaks) s = s.replace(/\n/g, '<br />\n')
  return s
}

/** 强调链（入参须为已转义文本；三粗→双粗→单斜→删除线，下划线带词边界守卫防 snake_case 误伤）。 */
function emphasis(escaped: string): string {
  return escaped
    .replace(/\*\*\*([^*\n]+)\*\*\*/g, '<strong><em>$1</em></strong>')
    .replace(/\*\*([^*\n]+)\*\*/g, '<strong>$1</strong>')
    .replace(/(^|[^\w])__([^_\n]+)__(?![\w])/g, '$1<strong>$2</strong>')
    .replace(/(^|[^\w*_])\*([^*\n]+)\*(?![\w*])/g, '$1<em>$2</em>')
    .replace(/(^|[^\w_])_([^_\n]+)_(?![\w_])/g, '$1<em>$2</em>')
    .replace(/~~([^~\n]+)~~/g, '<del>$1</del>')
}

/** 块级 Markdown → HTML 片段（整段产出即为可信转义后的渲染结果，可安全 v-html）。 */
export function renderMarkdown(src: string): string {
  if (!src) return ''
  // 入口剥离 NUL：堵死外部伪造 \u0000C0\u0000 占位符的通道
  const lines = src.replace(/\u0000/g, '').replace(/\r\n?/g, '\n').split('\n')
  const out: string[] = []
  const para: string[] = []

  function flushPara() {
    if (para.length === 0) return
    out.push(`<p>${renderInline(para.join('\n'), true)}</p>`)
    para.length = 0
  }

  let i = 0
  while (i < lines.length) {
    const line = lines[i]
    const t = line.trim()

    if (t === '') {
      flushPara()
      i++
      continue
    }

    // 围栏代码块：``` / ~~~，闭合须为同字符且不少于开栏长度的一行；EOF 未闭合按到结尾收
    const fence = t.match(/^(`{3,}|~{3,})\s*([A-Za-z0-9+#._-]*)\s*$/)
    if (fence) {
      flushPara()
      const marker = fence[1]
      const lang = fence[2]
      const body: string[] = []
      i++
      const closeRe = new RegExp(`^\\${marker[0]}{${marker.length},}\\s*$`)
      while (i < lines.length && !closeRe.test(lines[i].trim())) {
        body.push(lines[i])
        i++
      }
      i++ // 跳过闭合栏（EOF 时越界无碍）
      const cls = lang ? ` class="language-${escapeHtml(lang)}"` : ''
      out.push(`<pre><code${cls}>${escapeHtml(body.join('\n'))}</code></pre>`)
      continue
    }

    // 标题
    const head = t.match(/^(#{1,6})\s+(.+?)\s*#*\s*$/)
    if (head) {
      flushPara()
      const n = head[1].length
      out.push(`<h${n}>${renderInline(head[2])}</h${n}>`)
      i++
      continue
    }

    // 分隔线（去空白后整行同字符 ≥3）
    if (/^(-{3,}|\*{3,}|_{3,})$/.test(t.replace(/\s+/g, ''))) {
      flushPara()
      out.push('<hr />')
      i++
      continue
    }

    // 引用：连续 > 行收集，剥掉一层 > 后递归整块渲染（天然支持嵌套引用/引用内列表）
    if (/^>/.test(t)) {
      flushPara()
      const inner: string[] = []
      while (i < lines.length && /^>/.test(lines[i].trim())) {
        inner.push(lines[i].trim().replace(/^>\s?/, ''))
        i++
      }
      out.push(`<blockquote>${renderMarkdown(inner.join('\n'))}</blockquote>`)
      continue
    }

    // 列表（同级平铺）：无序 - + * ／有序 数字. 数字)，两种家族互不混编
    const ulItem = t.match(/^[-+*]\s+(.+)$/)
    const olItem = t.match(/^(\d+)[.)]\s+(.+)$/)
    if (ulItem || olItem) {
      flushPara()
      const ordered = !!olItem
      const startNum = ordered ? Number(olItem![1]) : 1
      const items: string[][] = []
      const firstText = ordered ? olItem![2] : ulItem![1]
      items.push([firstText])
      i++
      while (i < lines.length) {
        const lt = lines[i].trim()
        if (lt === '') break // 空行断列表（紧凑列表纪律）
        const nUl = lt.match(/^[-+*]\s+(.+)$/)
        const nOl = lt.match(/^(\d+)[.)]\s+(.+)$/)
        if (ordered && nOl) items.push([nOl[2]])
        else if (!ordered && nUl && !nOl) items.push([nUl[1]])
        else if (nUl || nOl || /^#{1,6}\s/.test(lt) || /^>/.test(lt)) break // 异家族/块级标记 → 列表止
        else items[items.length - 1].push(lt) // 非空非标记行 = 当前项续行
        i++
      }
      const startAttr = ordered && startNum !== 1 ? ` start="${startNum}"` : ''
      const tag = ordered ? 'ol' : 'ul'
      out.push(
        `<${tag}${startAttr}>${items
          .map((parts) => `<li>${renderInline(parts.join('\n'), true)}</li>`)
          .join('')}</${tag}>`,
      )
      continue
    }

    // 兜底：普通段落行
    para.push(line)
    i++
  }
  flushPara()
  return out.join('\n')
}

/**
 * 粗略判断文本是否含 Markdown 块级结构（列表/标题/围栏/引用等）。
 * 供 UI 决定"按 Markdown 渲染还是按纯文本等宽展示"（SQL/cURL 等
 * 代码类便签不该被斜体规则误伤成花样排版）。
 */
export function looksLikeMarkdown(src: string): boolean {
  if (!src) return false
  // 行内标记单独出现太易误伤（乘法 *、下划线路径），只认成对/块级形态
  return /(^|\n)(#{1,6} |(`{3,}|~{3,})|> {1,3}\S|[-*+] |\d+[.)] |\*\*[^*\n]+\*\*|~~[^~\n]+~~|\[[^\]]+\]\(https?:)/i.test(
    src,
  )
}
