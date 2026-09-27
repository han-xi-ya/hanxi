// ============================================================================
// 版本号比较器单源（版本比较器统一波 · 五实现收编）
//
// 收编前散落五处的实现与语义差异（逐处对现文复核过的定稿）：
//  - adapters/recordly coreCompare：3 段截断 + NaN→整串字典序退化 + 剥 `^v(?=\d)/i`
//    + 抹 `-suffix`（NSIS beta 互认为刻意语义）；
//  - adapters/paseo coreCompare：同上但 **不剥 v 不 trim**——与 recordly 已漂移的
//    双胞胎（`v1.0.0` vs `1.0.0` 旧口径判 -1 伪序，装完横幅仍常亮）；
//  - adapters/translucenttb cmpTBVersion：全长补零 + 非规范回 null（本波唯一
//    有 null 纪律的一份，收编即其形状升格为全局标准）；
//  - views/NanaZipView compareVersion：全长补零但 NaN 段静默判旧，且非数输入
//    自反比较恒 -1——反对称性被破坏（缺陷）；
//  - adapters/markeron 与 FrpcVersionsTab：展示/等号判定各持一份大小写敏感的
//    `replace(/^v/, '')` 剥离副本（markeron 无比较器，仅展示）。
//
// 统一口径：
//  1. 一切比较/判定/展示剥离先过 normalizeVersion（trim + 剥数字起头的 v/i 前缀）。
//  2. 废除「字典序退化」：不可解析一律 null（不可比），由消费端显式处置——
//     imported-* 对数字号自此回 null，旧伪序「永新」裁决不再产生。
//  3. 废除「3 段截断」：全长比较。recordly/paseo 自家三段数据上与旧口径等价，
//     ≥4 段（PE 版本 `1.3.5.1` 型）自此不再被抹平成互认（漏报消除，有意修正）。
// ============================================================================

/** 纯数值点分版本判据（归一后消费）：`^\d+(\.\d+)*$`。TB 两段年式/NanaZip 四段均合法。 */
const CANONICAL_RE = /^\d+(\.\d+)*$/

/**
 * 比较与展示前的统一归一：trim 空白 + 剥数字起头的 v/V 前缀（`^v(?=\d)/i`）。
 * `(?=\d)` 负样本守卫让 `vista`/`v` 之类词面不被误剥。展示面消费点（markeron
 * 卸载文案 / FrpcVersionsTab 等号判定与卸载文案）自本波复用本函数——相对两处
 * `^v`（大小写敏感、不 trim）旧口径，`V2.9.4` 形态现在也会被剥成 `2.9.4`
 * （现网 GitHub tag 恒小写 v，属防漂移微差，登记于统一波报告）。
 */
export function normalizeVersion(v: string): string {
  return v.trim().replace(/^v(?=\d)/i, '')
}

/** 归一后是否为规范版本号（纯数值点分段）。`4.1.15.9`→true；`0.8.0-beta.1`/`imported-x`/`2026.2.0.d4636e4`→false。 */
export function isCanonical(v: string): boolean {
  return CANONICAL_RE.test(normalizeVersion(v))
}

/**
 * 严格全长比较：归一后两侧必须恒为规范号，缺段按 0 补齐后逐段数值比。
 * a>b→1，a<b→-1，相等→0；任一侧非 `^\d+(\.\d+)*$`→null（不可比）。
 * 绝不退化为字符串伪序——`4.1.15.9` vs `4.1.15.10` 这类lex/数值分歧格以此为准。
 */
export function compareStrict(a: string, b: string): number | null {
  const na = normalizeVersion(a)
  const nb = normalizeVersion(b)
  if (!CANONICAL_RE.test(na) || !CANONICAL_RE.test(nb)) return null
  const pa = na.split('.').map(Number)
  const pb = nb.split('.').map(Number)
  for (let i = 0; i < Math.max(pa.length, pb.length); i++) {
    const x = pa[i] ?? 0
    const y = pb[i] ?? 0
    if (x !== y) return x > y ? 1 : -1
  }
  return 0
}

/**
 * 核心比较（NSIS 单目录互认口径）：归一后先抹 `-.*` 预发布后缀再走 compareStrict，
 * 全长不截断。`0.8.0-beta.1` 与 `0.8.0` 同核心→0（recordly PE 版本与 tag 互认的
 * 刻意语义）；两侧皆 beta（`1.3.5-beta.2` vs `1.3.5-beta.9`）亦互认相等。
 * 与旧 3 段截断的差异只在 ≥4 段输入（`1.3.5.1` vs `1.3.5` 旧判等、新判 1）——
 * 自家三段数据等价，四段防御面不再漏报；imported-* 后缀抹剩 `imported` 后
 * 非规范→null，不再产字典序伪裁决。
 */
export function compareCore(a: string, b: string): number | null {
  return compareStrict(normalizeVersion(a).replace(/-.*$/, ''), normalizeVersion(b).replace(/-.*$/, ''))
}

/**
 * core 口径升级谓词：compareCore < 0 才为真。相等（含 beta 互认抹平）不提示
 * ——NSIS 覆盖安装语义逐字保留；不可比（null）同样不提示——「无法判断」
 * 自此是显式纪律而非字典序巧合。
 */
export function upgradeAvailableCore(installedVersion: string, latestVersion: string): boolean {
  const c = compareCore(installedVersion, latestVersion)
  return c !== null && c < 0
}
