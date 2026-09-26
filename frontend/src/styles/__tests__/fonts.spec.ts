// N36 字体层结构锁（纯文件文本断言，不渲染组件）：
// fonts.css 存在、@font-face 字族声明与 :root 字族 token 栈严格一致、
// 桥接委托行在位、每个 src url() 指向的 woff2 素材真实存在且魔数正确、
// 许可文本随字体同仓。字体素材或字面栈任一漂移即红。
import { readFileSync, existsSync } from 'node:fs'
import { join, resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

// happy-dom 环境下 import.meta.url 非 file: 协议，素材路径一律锚定
// vitest 进程 cwd（= frontend 工程根，见 vitest.config.ts）。
const stylesDir = join(process.cwd(), 'src', 'styles')
const cssPath = join(stylesDir, 'fonts.css')
const css = readFileSync(cssPath, 'utf8')

/** 抽取全部 @font-face 块（family / weight / font-display / src url） */
function parseFontFaces(text: string) {
  return [...text.matchAll(/@font-face\s*{([^}]*)}/g)].map(([, body]) => ({
    family: /font-family:\s*"([^"]+)"/.exec(body)?.[1] ?? '',
    weight: /font-weight:\s*([^;]+);/.exec(body)?.[1].trim() ?? '',
    display: /font-display:\s*([^;]+);/.exec(body)?.[1].trim() ?? '',
    url: /src:\s*url\("([^"]+)"\)/.exec(body)?.[1] ?? '',
  }))
}

/** 抽取 :root 中某自定义属性的声明值（不含分号后内容） */
function tokenValue(name: string): string | null {
  const m = new RegExp(`--${name}:\\s*([^;]+);`).exec(css)
  return m ? m[1].trim() : null
}

const faces = parseFontFaces(css)

describe('fonts.css（N36 字体层）', () => {
  it('@font-face 声明齐全且字族命名与预期严格一致', () => {
    const byFamily = new Map<string, string[]>()
    for (const f of faces) {
      byFamily.set(f.family, [...(byFamily.get(f.family) ?? []), f.weight])
    }
    expect([...byFamily.keys()].sort()).toEqual([
      'JetBrains Mono',
      'LXGW WenKai GB Screen',
      'Noto Sans SC',
    ])
    expect(byFamily.get('LXGW WenKai GB Screen')).toEqual(['400'])
    expect(byFamily.get('Noto Sans SC')).toEqual(['100 900'])
    expect(byFamily.get('JetBrains Mono')?.sort()).toEqual(['400', '500', '600', '700'])
  })

  it('每个 @font-face 均显式 font-display: swap 且带 woff2 format', () => {
    expect(faces.length).toBeGreaterThanOrEqual(6)
    for (const f of faces) {
      expect(f.display, f.family + ' ' + f.weight).toBe('swap')
      expect(f.url).toMatch(/\.woff2$/)
    }
    expect(css).toContain('format("woff2")')
  })

  it('--font-ui / --font-mono token 在 :root 字面定义且涵盖全部 @font-face 字族', () => {
    const ui = tokenValue('font-ui')
    const mono = tokenValue('font-mono')
    expect(ui).not.toBeNull()
    expect(mono).not.toBeNull()
    const families = [...new Set(faces.map((f) => f.family))]
    for (const fam of families) {
      // 文楷为 UI 主族且 mono 栈含其中文回落；Noto 补集两栈皆在；JBM 仅 mono 主场
      if (fam === 'JetBrains Mono') {
        expect(mono).toContain(fam)
      } else {
        expect(ui, fam).toContain(fam)
        expect(mono, fam).toContain(fam)
      }
    }
    // 栈首序即设计决策：UI 中文主角是文楷，等宽主角是 JBM
    expect(ui).toMatch(/^"LXGW WenKai GB Screen",\s*"Noto Sans SC"/)
    expect(mono).toMatch(/^"JetBrains Mono"/)
  })

  it('存量桥接行在位：--font-text / --font-display 委托 var(--font-ui)', () => {
    expect(tokenValue('font-text')).toBe('var(--font-ui)')
    expect(tokenValue('font-display')).toBe('var(--font-ui)')
  })

  it('每个 src url() 指向的 woff2 素材存在且为合法 WOFF2 魔数', () => {
    for (const f of faces) {
      const p = resolve(stylesDir, f.url)
      expect(existsSync(p), f.url).toBe(true)
      const buf = readFileSync(p)
      expect(buf.subarray(0, 4).toString('latin1'), f.url).toBe('wOF2')
      expect(buf.length, f.url).toBeGreaterThan(1024)
    }
  })

  it('许可文本随字体同仓（OFL 再分发义务）', () => {
    for (const lic of [
      'licenses/OFL-LXGW-WenKai.txt',
      'licenses/OFL-NotoSansSC.txt',
      'licenses/OFL-JetBrainsMono.txt',
    ]) {
      const p = join(stylesDir, '..', 'assets', 'fonts', lic)
      expect(existsSync(p), lic).toBe(true)
      expect(readFileSync(p, 'utf8')).toMatch(/SIL OPEN FONT LICENSE|Open Font License/i)
    }
  })

  it('许可文本分发面双保险接线在位（N36 尾账）', () => {
    // 路一：vite 构建尾部把 licenses/ 显式拷入 dist（fonts.css 未引用它们，vite 不会自动带上）
    const viteConfig = readFileSync(join(process.cwd(), 'vite.config.ts'), 'utf8')
    expect(viteConfig).toContain('assets/fonts/licenses')
    expect(viteConfig).toContain('cpSync')
    expect(viteConfig).toContain('font-licenses')
    // 路二：关于页 ?raw 内联全文（文本进 JS bundle → EXE 内可查）
    const aboutView = readFileSync(join(process.cwd(), 'src', 'views', 'AboutView.vue'), 'utf8')
    for (const lic of ['OFL-LXGW-WenKai', 'OFL-NotoSansSC', 'OFL-JetBrainsMono']) {
      expect(aboutView, lic).toContain(`licenses/${lic}.txt?raw`)
    }
  })
})
