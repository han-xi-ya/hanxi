// Wave 0 DoD 门禁：共同模型 fixture 在 Go 与 TypeScript 两端一致。
//
// 三层锁定，任何一层漂移本 spec 直接红：
//  1. bindings 生成的 JS enum 对象（wails3 自 internal/extapi 生成）的值全集
//     == status.ts 各词表的键全集——前端展示词汇与绑定枚举逐值对齐；
//  2. internal/extapi/catalog.go 的 const 值全集 == bindings enum 值全集——
//     防止"重新生成绑定时遗漏/改名/手改产物"造成的 Go→TS 漂移；
//  3. scripts/fixture 基线：module_catalog.json（schema==1、47 项）与
//     composition_contract.json 的 app/appservice.js 关键导出可见性。
//
// 纪律：bindings 为生成物，发现问题改 Go 源或重跑生成，绝不在本 spec 迁就产物；
// $zero（Go 零值 ""）是生成器附加的占位，比对时剔除，不视为业务枚举值。
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, it } from 'vitest'
import {
  DeliveryKind,
  DeliveryState,
  Entrypoint,
  HealthState,
  PolicyState,
  PrimaryAction,
  RuntimeState,
  SummaryKey,
} from '../../../bindings/hanxi/internal/extapi/index.js'
import {
  DELIVERY_META,
  ENTRYPOINT_META,
  HEALTH_META,
  POLICY_META,
  PRIMARY_ACTION_META,
  RUNTIME_META,
  SUMMARY_META,
} from '../status'
import moduleCatalog from '../../../../scripts/fixture/module_catalog.json'
import compositionContract from '../../../../scripts/fixture/composition_contract.json'

/** 去重排序后的值集，比对以集合语义进行，报错信息保持可读。 */
function toSortedSet(values: string[]): string[] {
  return [...new Set(values)].sort()
}

/** bindings enum 对象 → 业务值全集（剔除生成器占位的 $zero / Go 零值 ""）。 */
function enumValues(enumObject: Record<string, string>): string[] {
  return toSortedSet(
    Object.entries(enumObject)
      .filter(([key, value]) => key !== '$zero' && value !== '')
      .map(([, value]) => value),
  )
}

/** 双向 diff：失败时分别列出"词表有而枚举无"与"枚举有而词表无"，一眼定位漂移方向。 */
function expectExactMatch(label: string, left: string[], right: string[]): void {
  const missing = right.filter((value) => !left.includes(value))
  const extra = left.filter((value) => !right.includes(value))
  expect(missing, `${label}：词表侧有、枚举侧缺（漂移=枚举被改名或删除）`).toEqual([])
  expect(extra, `${label}：枚举侧有、词表侧缺（漂移=词表未跟进新增值）`).toEqual([])
}

// ── 第 1 层：bindings enum 对象 ↔ status.ts 词表键 ──

const TS_FAMILIES: ReadonlyArray<readonly [string, Record<string, string>, Record<string, unknown>]> = [
  ['DeliveryState ↔ DELIVERY_META', DeliveryState, DELIVERY_META],
  ['PolicyState ↔ POLICY_META', PolicyState, POLICY_META],
  ['RuntimeState ↔ RUNTIME_META', RuntimeState, RUNTIME_META],
  ['HealthState ↔ HEALTH_META', HealthState, HEALTH_META],
  ['Entrypoint ↔ ENTRYPOINT_META', Entrypoint, ENTRYPOINT_META],
  ['PrimaryAction ↔ PRIMARY_ACTION_META', PrimaryAction, PRIMARY_ACTION_META],
  ['SummaryKey ↔ SUMMARY_META', SummaryKey, SUMMARY_META],
]

describe('bindings enum 与 status.ts 词表键一致', () => {
  for (const [label, enumObject, meta] of TS_FAMILIES) {
    it(label, () => {
      expectExactMatch(label, enumValues(enumObject), toSortedSet(Object.keys(meta)))
    })
  }
})

// ── 第 2 层：catalog.go const 值 ↔ bindings enum 对象 ──

const goSource = readFileSync(join(process.cwd(), '..', 'internal', 'extapi', 'catalog.go'), 'utf8')

/** 从 Go 源文本提取 `Name TypeName = "value"` 形态的 const 值全集（按类型名锁定，跨行结构体字段不误伤）。 */
function goEnumValues(typeName: string): string[] {
  const pattern = new RegExp(`\\w+\\s+${typeName}\\s*=\\s*"([^"]+)"`, 'g')
  return toSortedSet([...goSource.matchAll(pattern)].map((match) => match[1]))
}

const GO_FAMILIES: ReadonlyArray<readonly [string, string, Record<string, string>]> = [
  ['DeliveryState', 'DeliveryState', DeliveryState],
  ['PolicyState', 'PolicyState', PolicyState],
  ['RuntimeState', 'RuntimeState', RuntimeState],
  ['HealthState', 'HealthState', HealthState],
  ['Entrypoint', 'Entrypoint', Entrypoint],
  ['PrimaryAction', 'PrimaryAction', PrimaryAction],
  ['SummaryKey', 'SummaryKey', SummaryKey],
]

describe('catalog.go 常量与 bindings enum 值一致（Go→TS 单向锁）', () => {
  for (const [label, typeName, enumObject] of GO_FAMILIES) {
    it(label, () => {
      const goValues = goEnumValues(typeName)
      // 提取本身也要有产出，防止正则失效时"空集==空集"假绿。
      expect(goValues.length, `catalog.go 未提取到 ${typeName} 任何常量，正则或源结构已变`).toBeGreaterThan(0)
      expectExactMatch(label, goValues, enumValues(enumObject))
    })
  }
})

// ── 第 3 层：fixture 基线 ──

describe('module_catalog.json 契约基线', () => {
  it('顶层 schema==1 且共 47 个模块项', () => {
    expect(moduleCatalog.schema).toBe(1)
    expect(moduleCatalog.items).toHaveLength(47)
  })

  it('每个模块项的 deliveryKind/entrypoints 取值均落在 bindings 枚举值集内', () => {
    // 注意：ModuleCatalogItem.deliveryKind 是静态交付形态(与 ModuleState.delivery 生命周期维度刻意异名) DeliveryKind，非动态交付维度 DeliveryState。
    const kindValues = new Set(enumValues(DeliveryKind))
    const entrypointValues = new Set(enumValues(Entrypoint))
    for (const item of moduleCatalog.items) {
      expect(kindValues.has(item.deliveryKind), `${item.id} 的 deliveryKind=${item.deliveryKind} 不在枚举内`).toBe(true)
      for (const entry of item.entrypoints ?? []) {
        expect(entrypointValues.has(entry), `${item.id} 的 entrypoint=${entry} 不在枚举内`).toBe(true)
      }
    }
  })
})

describe('composition_contract.json 锁定 AppService 新契约导出', () => {
  it('bindings["app/appservice.js"] 含 Wave 0 三方法（前端可见性）', () => {
    const exports = compositionContract.bindings['app/appservice.js']
    expect(exports).toEqual(
      expect.arrayContaining(['ListCatalog', 'ListModuleStates', 'SetModuleInstalled']),
    )
  })
})
