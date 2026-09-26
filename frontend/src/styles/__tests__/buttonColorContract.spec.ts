// N37 按钮配色返工防回潮守卫（六路终结位交付；纯文件文本断言，不渲染组件，
// 形制循 constants/__tests__/contract-enum.spec.ts 与 quickmenu/__tests__/wheelGeometry.contract.spec.ts
// 先例：提取空集即红、失效豁免条目即红，防"正则失效假绿"）。
//
// 三则契约 + 一枚拍板值锁：
//  a) components.css 按钮家族块：禁裸 hex/rgb() 字面色、禁 glow token 引用/彩色发光回流。
//     豁免走显式登记清单（selector 片段 + 理由），新增裸色不登记=红，登记了但裸色已没=红（失效即删）。
//  b) views/components 的 scoped `.btn-danger` 形制整抄（同一规则同时覆写背景+前景）"只降不升"上限锁：
//     标准形在 components.css :where(.btn-danger)，形制副本禁止再新增。基线写时（2026-09-26）
//     按现盘实测钉死：历史三份副本（LanScannerView / PortScanView / FileShareHero「运行中变红」档）
//     已由并行视图线删净落回（各文件留有"scoped 副本删净"注记），现盘=0 份/0 行，上限即地板零容忍。
//     注：并行收敛若在某时点尚有在途残副本，基线常量应降档登记为当盘实测值直至 0——该常量只许变小，永不变大。
//  c) tokens.css 十块（teal 默认 :root 明/暗 + 四板×明暗覆写块）：on-primary / on-accent 两件必存，
//     逐块逐名恰一条、显式 hex 字面值（禁 var()/color-mix 兜底——"每板逐对核过"的账必须落在字面上才可复算）；
//     浅底白字铁律、深底禁白字铁律同批锁。on-danger/on-positive/on-warning 拆档件按"要么全无、要么十块全有"
//     的完整性锁收（半拆=各板消费面不可靠，红）——拍板点 2 现盘终裁"不拆"（token 线 2026-09-26 注释在案），
//     若机主终裁改为"拆"，把名目加回 REQUIRED 数组即升级，本锁两口径均不脆断。
//     ⚠ 口径冲突在账：本路派单原文写"on-primary/on-accent/on-danger/on-positive 四件必存"，与派单后
//     token 线落库的终裁"不拆"相抵；守卫暂按现盘终裁锁核心两件，冲突由收口报呈机主裁决，勿静默二选一。
//  d) teal 拍板案 A 压暗值（#0d8284 / hover #0b7577）不回退旧值——如再拍板改值，
//     须同步 PLAN_N37_ONCOLOR.md 与本锁，不允许静默漂移。
import { readdirSync, readFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'

// happy-dom 环境下 import.meta.url 非 file: 协议，路径一律锚定 vitest 进程 cwd（= frontend 工程根）。
const srcDir = join(process.cwd(), 'src')
const componentsCss = readFileSync(join(srcDir, 'styles', 'components.css'), 'utf8')
const tokensCss = readFileSync(join(srcDir, 'styles', 'tokens.css'), 'utf8')

/** 去 CSS 注释：落回/删副本的说明注释里大量出现 ".btn-danger" 等类名，不去注释必假阳性。 */
function stripComments(css: string): string {
  return css.replace(/\/\*[\s\S]*?\*\//g, '')
}

/** 展平规则提取（选择器 / 声明体 / 原文）。选择器字符类排除 @ 与 { }：
 *  @media/@keyframes 外壳不会被当成规则，其内层规则逐条正常命中（嵌套收尾花括号落入
 *  下一段的"选择器"里也因含 } 被字符类拒绝而自然跳过）。 */
function iterRules(css: string): Array<{ selector: string; body: string; raw: string }> {
  const rules: Array<{ selector: string; body: string; raw: string }> = []
  for (const m of stripComments(css).matchAll(/([^{}@]+)\{([^{}]*)\}/g)) {
    rules.push({ selector: m[1].trim(), body: m[2], raw: m[0] })
  }
  return rules
}

const LITERAL_COLOR = /#(?:[0-9a-fA-F]{8}|[0-9a-fA-F]{6}|[0-9a-fA-F]{4}|[0-9a-fA-F]{3})\b|\brgba?\s*\(/g

/** 按钮家族块判定：选择器含 .btn 家族前缀，或与按钮同层收编的 .link-button 原子。 */
function isButtonRule(selector: string): boolean {
  return /\.btn\b|\.btn-|\.link-button\b/.test(selector)
}

/**
 * a) 按钮家族裸色字面豁免清单——只允许"显式登记 selector 片段 + 理由"的形态存在。
 * 写时现盘实测：components.css 全文件零裸色字面（实心档全部走 tokens.css 派生档
 * --btn-solid-edge / --btn-danger-fill-hover 等），故豁免集为空；将来确需豁免
 * （如终端色类不可派生场景），在此登记并同步 PLAN_N37_ONCOLOR.md，禁止默默加色。
 */
const BUTTON_LITERAL_EXEMPTIONS: ReadonlyArray<{ selectorIncludes: string; reason: string }> = []

describe('a) components.css 按钮家族块——零裸色与零发光回流锁', () => {
  const buttonRules = iterRules(componentsCss).filter((r) => isButtonRule(r.selector))

  it('按钮族规则可提取（选择器形制大改致提取空集=本锁失效，须检修）', () => {
    expect(buttonRules.length).toBeGreaterThanOrEqual(10)
  })

  it('块内无裸 hex/rgb() 字面色（实底色单一真相在 tokens.css；豁免须显式登记）', () => {
    const found = buttonRules
      .map((r) => ({ selector: r.selector, hits: [...r.body.matchAll(LITERAL_COLOR)].map((h) => h[0]) }))
      .filter((r) => r.hits.length > 0)
    const unexempted = found.filter((r) => !BUTTON_LITERAL_EXEMPTIONS.some((e) => r.selector.includes(e.selectorIncludes)))
    expect(
      unexempted.map((r) => `${r.selector} → ${r.hits.join(', ')}`),
      '按钮家族块新增裸色——改引 tokens.css 派生档，或（极少数正当场景）登记豁免清单并在 PLAN 入账',
    ).toEqual([])
    const stale = BUTTON_LITERAL_EXEMPTIONS.filter((e) => !found.some((r) => r.selector.includes(e.selectorIncludes)))
    expect(stale.map((e) => e.selectorIncludes), '豁免条目已失效（对应裸色不在盘上），删除以免守卫钝化').toEqual([])
  })

  it('块内不引用任何 glow token（②拍板：彩色发光只许「进行中」脉冲，按钮族零发光）', () => {
    // 中性层次影 var(--shadow-small) 等不在禁止之列；*-glow 引用即刺眼发光语义，一律红。
    const withGlow = buttonRules.filter((r) => /-glow\b/.test(r.body))
    expect(withGlow.map((r) => r.selector), '按钮家族 glow 引用回流（降级形制=实底+提亮描边）').toEqual([])
  })
})

/** 递归收集 .vue 文件（不走 git ls-files——未跟踪新文件正是本锁要抓的对象）。 */
function listVue(dir: string): string[] {
  return readdirSync(dir, { withFileTypes: true }).flatMap((e) => {
    const p = join(dir, e.name)
    if (e.isDirectory()) return listVue(p)
    return e.name.endsWith('.vue') ? [p] : []
  })
}

/** 提取 .vue 的 scoped style 块内容（lang/postcss 等属性并存时仍命中）。 */
function scopedStyles(file: string): string[] {
  return [...readFileSync(file, 'utf8').matchAll(/<style[^>]*\bscoped\b[^>]*>([\s\S]*?)<\/style>/g)].map((m) => m[1])
}

// 写时基线（2026-09-26 现盘实测：三份 scoped 副本已由并行线删净，0 份/0 行）。只降不升。
const BTN_DANGER_COPY_MAX_BLOCKS = 0
const BTN_DANGER_COPY_MAX_LINES = 0

describe('b) scoped .btn-danger 形制整抄——只降不升零容忍锁', () => {
  it(`形制副本不超基线（上限 ${BTN_DANGER_COPY_MAX_BLOCKS} 份 / ${BTN_DANGER_COPY_MAX_LINES} 行；标准形=components.css :where(.btn-danger)）`, () => {
    const copies: string[] = []
    let lines = 0
    for (const file of [...listVue(join(srcDir, 'views')), ...listVue(join(srcDir, 'components'))]) {
      for (const style of scopedStyles(file)) {
        for (const r of iterRules(style)) {
          if (!/\.btn-danger(?![\w-])/.test(r.selector)) continue
          const hasBg = /background(-color)?\s*:/.test(r.body)
          const hasFg = /(?:^|[^-\w])color\s*:/.test(r.body) // border-color/outline-color 不算覆写前景
          if (!(hasBg && hasFg)) continue // 只抓"形制整抄"（底+前景同覆）；单属性微调不在此约
          copies.push(`${file.replace(process.cwd() + '/', '')}: { ${r.selector.replace(/\s+/g, ' ')} }`)
          lines += r.raw.split('\n').length - 1
        }
      }
    }
    expect(copies.length, `scoped .btn-danger 形制整抄 ${copies.length} 份 > 基线 ${BTN_DANGER_COPY_MAX_BLOCKS}，禁止再新增：\n${copies.join('\n')}`).toBeLessThanOrEqual(BTN_DANGER_COPY_MAX_BLOCKS)
    expect(lines, `形制副本行数 ${lines} > 基线 ${BTN_DANGER_COPY_MAX_LINES}`).toBeLessThanOrEqual(BTN_DANGER_COPY_MAX_LINES)
  })
})

const TOKEN_CONTRACT_REQUIRED = ['color-on-primary', 'color-on-accent'] as const
/** 拆档件完整性锁：十块全有或全无（半拆=消费面逐板不可靠，红）。现盘终裁"不拆"=全无态通过；
 *  拍板点 2 若终裁改"拆"，将相应名目移入 REQUIRED 升级为本约。 */
const TOKEN_CONTRACT_OPTIONAL = ['color-on-danger', 'color-on-positive', 'color-on-warning'] as const
const EXPECTED_BLOCKS = [
  'teal·light', 'teal·dark',
  'sky·light', 'sky·dark',
  'iris·light', 'iris·dark',
  'jade·light', 'jade·dark',
  'onyx·light', 'onyx·dark',
] as const

/** :root 块 → "板·明暗" 键。teal 为默认回退板（:root / :root[data-theme="dark"] 无 data-accent）。 */
function parseTokenBlocks(css: string): { blocks: Map<string, string>; dupes: string[] } {
  const blocks = new Map<string, string>()
  const dupes: string[] = []
  for (const m of stripComments(css).matchAll(/:root((?:\[[^\]]+\])*)\s*\{([^}]*)\}/g)) {
    const attrs = m[1]
    const theme = attrs.includes('data-theme="dark"') ? 'dark' : 'light'
    const accent = /data-accent="([\w-]+)"/.exec(attrs)?.[1] ?? 'teal'
    const key = `${accent}·${theme}`
    if (blocks.has(key)) dupes.push(key)
    blocks.set(key, m[2])
  }
  return { blocks, dupes }
}

const tokenIndex = parseTokenBlocks(tokensCss)

function declarationValues(blockBody: string, name: string): string[] {
  return [...blockBody.matchAll(new RegExp(`--${name}:\\s*([^;]+);`, 'g'))].map((m) => m[1].trim())
}

/** 单块单名 on-color 契约核验：恰一条、字面 hex、方向铁律（浅=白 / 深≠白）。返回错误行，空=过。 */
function checkOnColorDecl(key: string, name: string): string[] {
  const body = tokenIndex.blocks.get(key)
  if (!body) return [`${key} 块缺失（先检修 :root 块结构提取）`]
  const values = declarationValues(body, name)
  if (values.length !== 1) return [`${key} 块 --${name} 声明数应恰为 1（实为 ${values.length}：缺失或重复）`]
  const value = values[0].toLowerCase()
  const errors: string[] = []
  if (!/^#(?:[0-9a-fA-F]{3,4}|[0-9a-fA-F]{6}|[0-9a-fA-F]{8})$/.test(value)) {
    errors.push(`${key} 块 --${name} 必须是显式字面 hex（禁 var()/color-mix 兜底——逐对核过的账要可复算），实值: ${values[0]}`)
  }
  if (key.endsWith('·light') && value !== '#ffffff') errors.push(`${key} 块 --${name} 违反浅底白字铁律（PLAN §2a 结论 1）: ${value}`)
  if (key.endsWith('·dark') && value === '#ffffff') errors.push(`${key} 块 --${name} 违反深底禁白字铁律（白字于五板深底 1.83–2.55 全爆）`)
  return errors
}

describe('c) tokens.css on-color 契约族——十块核心两件必存 + 拆档件全或无', () => {
  it(':root 块族提取齐全且无重复（10 板块；结构大改致提取失效=本锁须检修）', () => {
    expect(tokenIndex.dupes, '同一「板·明暗」出现重复 :root 块（后块静默覆盖前块，账不可核）').toEqual([])
    const missing = EXPECTED_BLOCKS.filter((k) => !tokenIndex.blocks.has(k))
    expect(missing, 'tokens.css 缺覆写块（选择器形制或已变，同步检修本 spec）').toEqual([])
  })

  it('核心族 on-primary/on-accent：十块必存、逐块恰一条、显式 hex 字面、两铁律成立', () => {
    const errors = EXPECTED_BLOCKS.flatMap((key) => TOKEN_CONTRACT_REQUIRED.flatMap((name) => checkOnColorDecl(key, name)))
    expect(errors, 'on-color 核心契约违规：\n' + errors.join('\n')).toEqual([])
  })

  it('拆档件 on-danger/on-positive/on-warning：全或无（半拆红）；在位时同样恰一条字面 hex + 铁律', () => {
    const errors: string[] = []
    for (const name of TOKEN_CONTRACT_OPTIONAL) {
      const present = EXPECTED_BLOCKS.filter((key) => declarationValues(tokenIndex.blocks.get(key) ?? '', name).length > 0)
      if (present.length > 0 && present.length < EXPECTED_BLOCKS.length) {
        errors.push(`--${name} 半拆（仅 ${present.join(' / ')} 定义）——十块补齐或删除干净；终裁"拆"则同步把名目移入 REQUIRED 升级本约`)
      }
      for (const key of present) errors.push(...checkOnColorDecl(key, name))
    }
    expect(errors, '拆档件完整性契约违规：\n' + errors.join('\n')).toEqual([])
  })

  it('teal 浅底拍板案 A 不回退：primary #0d8284 / hover #0b7577（旧值 #0f8b8d 白字 4.12 已废）', () => {
    const body = tokenIndex.blocks.get('teal·light')
    expect(body, 'teal·light 块缺失').toBeDefined()
    expect(declarationValues(body!, 'color-primary')[0]?.toLowerCase(), 'teal 浅 primary 已回退案 A 之前的不达标价').toBe('#0d8284')
    expect(declarationValues(body!, 'color-primary-hover')[0]?.toLowerCase()).toBe('#0b7577')
    // 注：本锁锁的是拍板决定（压暗一档），不是永久配色——将来再拍板换值时，同 PR 更新本锁与 PLAN_N37 §4。
  })
})
