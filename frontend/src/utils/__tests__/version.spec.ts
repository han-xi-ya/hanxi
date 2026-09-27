// utils/version 单源比较器 spec（版本比较器统一波）。
// 矩阵格纪律：每一格注明「收编前哪份实现会给出什么答案」——被统一掉的
// 3 段截断 / 字典序退化 / NaN 静默判旧 / 双胞胎 v 前缀漂移 四类旧语义在此锁死，
// 任何回退都会碰掉断言。
import { describe, expect, it } from 'vitest'
import { compareCore, compareStrict, isCanonical, normalizeVersion, upgradeAvailableCore } from '../version'

describe('normalizeVersion', () => {
  it('trim 空白 + 剥数字起头的 v/V 前缀；词面 v 不动', () => {
    expect(normalizeVersion(' 1.2.3 ')).toBe('1.2.3')
    expect(normalizeVersion('v1.3.5')).toBe('1.3.5')
    expect(normalizeVersion('V2.9.4')).toBe('2.9.4')
    expect(normalizeVersion('1.3.5')).toBe('1.3.5')
    // `(?=\d)` 守卫：非数字起头的 v 词面不是前缀
    expect(normalizeVersion('vista')).toBe('vista')
    expect(normalizeVersion('v')).toBe('v')
    expect(normalizeVersion('2v2')).toBe('2v2')
    // 后缀原样保留（抹后缀是 compareCore 的事）
    expect(normalizeVersion('v1.2.3-beta.1')).toBe('1.2.3-beta.1')
  })
})

describe('isCanonical', () => {
  it('归一后纯数值点分段为真；两段年式/四段/多段均合法', () => {
    expect(isCanonical('1.2.3')).toBe(true)
    expect(isCanonical('2026.2')).toBe(true)
    expect(isCanonical('4.1.15.9')).toBe(true)
    expect(isCanonical('1.0.0.0.0')).toBe(true)
    expect(isCanonical('v1.2.3')).toBe(true)
    expect(isCanonical(' 1.2.3 ')).toBe(true)
  })
  it('预发布后缀/imported 兜底/字母尾段/空串/断段为假', () => {
    expect(isCanonical('0.8.0-beta.1')).toBe(false)
    expect(isCanonical('imported-20260915-010203')).toBe(false)
    expect(isCanonical('2026.2.0.d4636e4')).toBe(false)
    expect(isCanonical('')).toBe(false)
    expect(isCanonical('1.2.')).toBe(false)
    expect(isCanonical('1..2')).toBe(false)
  })
})

describe('compareStrict — 严格全长 + 非规范 null', () => {
  it('矩阵格：4.1.15.9 vs 4.1.15.10 → -1（数值比，lex 会判反；旧 recordly/paseo 三段截断会判 0 漏报）', () => {
    expect(compareStrict('4.1.15.9', '4.1.15.10')).toBe(-1)
    expect(compareStrict('4.1.15.10', '4.1.15.9')).toBe(1)
  })
  it('矩阵格：2026.2 vs 2026.2.0 → 0（缺段补零；旧 recordly/paseo 三段截断判 -1 伪序）', () => {
    expect(compareStrict('2026.2', '2026.2.0')).toBe(0)
    expect(compareStrict('2026.2.0', '2026.2')).toBe(0)
  })
  it('矩阵格：v1.0.0 vs 1.0.0 → 0（统一剥 v；旧 paseo 不剥 v，字典序判 -1「永旧」装完横幅不熄）', () => {
    expect(compareStrict('v1.0.0', '1.0.0')).toBe(0)
    expect(compareStrict('1.0.0', 'v1.0.0')).toBe(0)
  })
  it('矩阵格：1.2.3.4.5 vs 1.2.3.9.9 → -1（全长判对；旧三段截断会判 0 相等）', () => {
    expect(compareStrict('1.2.3.4.5', '1.2.3.9.9')).toBe(-1)
    expect(compareStrict('1.2.3.9.9', '1.2.3.4.5')).toBe(1)
  })
  it('矩阵格：imported-* 对数字号 → null（旧 recordly/paseo 字典序伪判「永新」+1）', () => {
    expect(compareStrict('imported-20260915-010203', '0.8.0')).toBeNull()
    expect(compareStrict('0.8.0', 'imported-20260915-010203')).toBeNull()
  })
  it('矩阵格：0.8.0-beta.1 vs 0.8.0 → null（strict 不吃后缀，非规范拒比；core 口径才互认，见下）', () => {
    expect(compareStrict('0.8.0-beta.1', '0.8.0')).toBeNull()
  })
  it('TB 字母尾段不可比（旧 NanaZipView 实现对此 NaN 静默判旧且自反 -1，反对称性破坏）', () => {
    expect(compareStrict('2026.2.0.d4636e4', '2026.2')).toBeNull()
    expect(compareStrict('2026.2.0.d4636e4', '2026.2.0.d4636e4')).toBeNull()
  })
  it('数值段大小跨位数不误判（10>9 而非 lex）', () => {
    expect(compareStrict('10.0.0', '9.99.99')).toBe(1)
  })
  it('与旧 cmpTBVersion 语义同形：全长补零逐格等值（translucenttb 薄包装零漂移）', () => {
    expect(compareStrict('2026.10', '2026.2')).toBe(1)
    expect(compareStrict('2025.1', '2026.2')).toBe(-1)
    expect(compareStrict('imported-20260906-150405', '2026.2')).toBeNull()
  })
})

describe('compareCore — 抹后缀核心比较（NSIS 互认口径）', () => {
  it('矩阵格：0.8.0-beta.1 vs 0.8.0 → 0 互认（与 strict 的 null 刻意分叉）', () => {
    expect(compareCore('0.8.0-beta.1', '0.8.0')).toBe(0)
    expect(compareCore('0.8.0', '0.8.0-beta.1')).toBe(0)
  })
  it('两侧皆 beta 亦互认（旧 recordly 语义逐字保留）', () => {
    expect(compareCore('1.3.5-beta.2', '1.3.5-beta.9')).toBe(0)
  })
  it('矩阵格：imported-* 对数字号 → null（后缀抹剩 "imported" 非规范；不再产生「永新」伪序）', () => {
    expect(compareCore('imported-20260915-010203', '0.8.0')).toBeNull()
    expect(compareCore('0.8.0', 'imported-20260915-010203')).toBeNull()
  })
  it('四段防御面：1.3.5.1 vs 1.3.5 → 1（旧三段截断判 0 互认属漏报，本波有意修正）', () => {
    expect(compareCore('1.3.5.1', '1.3.5')).toBe(1)
    expect(compareCore('1.3.5', '1.3.5.1')).toBe(-1)
  })
  it('双胞胎漂移收编：v1.3.5 vs 1.3.6 两侧归一后判 -1（旧 paseo 不剥 v 走字典序）', () => {
    expect(compareCore('v1.3.5', '1.3.6')).toBe(-1)
    expect(compareCore('V2.9.4', '2.9.4')).toBe(0)
  })
  it('三段自家数据与旧口径等价：1.0.0 vs 1.2.0-beta1 → -1（recordly 视图 spec 实格）', () => {
    expect(compareCore('1.0.0', '1.2.0-beta1')).toBe(-1)
  })
})

describe('upgradeAvailableCore — 谓词 null 纪律与 NSIS 等号不提示', () => {
  it('严格更早才真；相等/更晚/不可比一律假', () => {
    expect(upgradeAvailableCore('0.8.0', '0.9.0')).toBe(true)
    expect(upgradeAvailableCore('0.9.0', '0.8.0')).toBe(false)
    expect(upgradeAvailableCore('0.8.0', '0.8.0')).toBe(false)
    expect(upgradeAvailableCore('imported-20260915-010203', '9.9.9')).toBe(false)
  })
  it('NSIS beta 互认等号：beta 已装对同号正式版不提示（旧 <0 词面逐字保真）', () => {
    expect(upgradeAvailableCore('0.8.0-beta.1', '0.8.0')).toBe(false)
    expect(upgradeAvailableCore('1.3.5-beta.2', '1.3.5-beta.9')).toBe(false)
  })
  it('矩阵格：4.1.15.9 → 4.1.15.10 提示升级（旧三段截断判等永不提示）', () => {
    expect(upgradeAvailableCore('4.1.15.9', '4.1.15.10')).toBe(true)
  })
  it('v 前缀两侧归一：1.3.5 对 v1.3.6 提示、对 v1.3.5 不提示', () => {
    expect(upgradeAvailableCore('1.3.5', 'v1.3.6')).toBe(true)
    expect(upgradeAvailableCore('1.3.5', 'v1.3.5')).toBe(false)
  })
})
