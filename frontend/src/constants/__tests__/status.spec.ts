// 特征测试：锁定两张状态表的文案/图标/色调与回退语义，
// 迁移（EnvCheckView 副本删除、托管视图 stateText 收编）后必须全绿。
// 另覆盖 Wave 0 四维状态统一词表：每维每值三通道齐备、未知值兜底、
// 联合类型与表键穷举一致（satisfies 让 vue-tsc 把编译期穷举也当门禁）。
import { describe, expect, it } from 'vitest'
import { ICON_NAMES } from '../icons'
import {
  DELIVERY_META,
  ENV_STATUS_META,
  HEALTH_META,
  OPERATION_KIND_META,
  OPERATION_STATUS_META,
  PHASE_META,
  POLICY_META,
  PRIMARY_ACTION_META,
  RUNTIME_META,
  SUMMARY_META,
  TOOL_STATE_META,
  deliveryMeta,
  envStatusMeta,
  healthMeta,
  operationKindMeta,
  operationPhaseText,
  operationStatusMeta,
  policyMeta,
  runtimeMeta,
  toolStateMeta,
  updateAvailableText,
  type DeliveryStateValue,
  type HealthStateValue,
  type OperationKindValue,
  type OperationPhaseValue,
  type OperationStatusValue,
  type PolicyStateValue,
  type PrimaryActionValue,
  type RuntimeStateValue,
  type StateTone,
  type SummaryKeyValue,
  type UiButtonVariant,
} from '../status'

const TONES: StateTone[] = ['positive', 'information', 'warning', 'danger', 'neutral']

describe('toolStateMeta', () => {
  it('已知托管状态返回正确语义（对齐托管视图六态矩阵通用口径）', () => {
    expect(toolStateMeta('running')).toEqual({ text: '运行中', icon: '●', tone: 'positive' })
    expect(toolStateMeta('starting')).toEqual({ text: '启动中…', icon: '◐', tone: 'information' })
    expect(toolStateMeta('stopped')).toEqual({ text: '未运行', icon: '○', tone: 'neutral' })
    expect(toolStateMeta('failed')).toEqual({ text: '异常退出', icon: '!', tone: 'danger' })
    expect(toolStateMeta('external')).toEqual({ text: '外部运行', icon: '◍', tone: 'warning' })
  })

  it('未知/空/大小写不符状态回退 stopped——状态不明不误报在跑', () => {
    expect(toolStateMeta('weird')).toBe(TOOL_STATE_META.stopped)
    expect(toolStateMeta('')).toBe(TOOL_STATE_META.stopped)
    expect(toolStateMeta('RUNNING')).toBe(TOOL_STATE_META.stopped)
  })
})

describe('envStatusMeta', () => {
  it('四项已知检测状态的 text/icon 与 EnvCheckView 旧内联表逐字一致', () => {
    expect(envStatusMeta('installed')).toEqual({ text: '已安装', icon: '✓', tone: 'positive' })
    expect(envStatusMeta('missing')).toEqual({ text: '未安装', icon: '○', tone: 'neutral' })
    expect(envStatusMeta('error')).toEqual({ text: '检测失败', icon: '!', tone: 'danger' })
    expect(envStatusMeta('store-stub')).toEqual({ text: '商店存根', icon: '⚠', tone: 'warning' })
  })

  it('未知检测状态回退 error（同旧 metaOf ?? STATUS_META.error 口径）', () => {
    expect(envStatusMeta('nope')).toBe(ENV_STATUS_META.error)
    expect(envStatusMeta(undefined as unknown as string)).toBe(ENV_STATUS_META.error)
  })
})

describe('tone 值域', () => {
  it('两张表的 tone 均落在 StateTone 联合（与 .chip-{tone} 原子类词表同步）', () => {
    for (const meta of Object.values(TOOL_STATE_META)) expect(TONES).toContain(meta.tone)
    for (const meta of Object.values(ENV_STATUS_META)) expect(TONES).toContain(meta.tone)
  })
})

// ── Wave 0 四维状态统一词表 ──

const UNKNOWN = { text: '状态未知', tone: 'neutral', icon: 'help-circle' }

// 编译期穷举门禁：下列对象字面量必须与联合类型逐键相等——
// 联合类型加/删值而字面量未跟上时，vue-tsc --noEmit 直接报错；
// 运行时再用 Object.keys 比对，锁死"表键 == 联合键 == catalog.go 契约"。
const DELIVERY_KEYS = {
  absent: true, installing: true, installed: true, updating: true,
  removing: true, repairing: true, orphaned: true,
} satisfies Record<DeliveryStateValue, true>
const POLICY_KEYS = {
  enabled: true, disabled: true, blocked: true,
  'pending-consent': true, mandatory: true,
} satisfies Record<PolicyStateValue, true>
const RUNTIME_KEYS = {
  inactive: true, activating: true, active: true, busy: true,
  stopping: true, crashed: true, failed: true,
} satisfies Record<RuntimeStateValue, true>
const HEALTH_KEYS = {
  current: true, 'update-available': true, pinned: true, incompatible: true,
  corrupt: true, revoked: true, unverified: true, degraded: true, 'offline-stale': true,
} satisfies Record<HealthStateValue, true>

const DIMENSIONS = [
  ['delivery', DELIVERY_META, DELIVERY_KEYS, deliveryMeta],
  ['policy', POLICY_META, POLICY_KEYS, policyMeta],
  ['runtime', RUNTIME_META, RUNTIME_KEYS, runtimeMeta],
  ['health', HEALTH_META, HEALTH_KEYS, healthMeta],
] as const

describe('四维状态词表', () => {
  for (const [name, table, keys, getter] of DIMENSIONS) {
    it(`${name}：表键与联合类型逐键一致（穷举门禁）`, () => {
      expect(Object.keys(table).sort()).toEqual(Object.keys(keys).sort())
    })

    it(`${name}：每值三通道齐备——非空 text + 合法 tone + icons.ts 已注册 icon`, () => {
      for (const [value, meta] of Object.entries(table)) {
        expect(meta.text, value).toBeTruthy()
        expect(TONES, `${value}.tone`).toContain(meta.tone)
        expect(ICON_NAMES as string[], `${value}.icon`).toContain(meta.icon)
      }
    })

    it(`${name}：取值函数命中表项，未知值兜底"状态未知"且不抛错`, () => {
      for (const [value, meta] of Object.entries(table)) {
        expect(getter(value)).toBe(meta)
      }
      expect(getter('no-such-state')).toEqual(UNKNOWN)
      expect(getter('')).toEqual(UNKNOWN)
      expect(getter(undefined as unknown as string)).toEqual(UNKNOWN)
    })
  }
})

describe('updateAvailableText（有可用更新 + 版本短语统一口径）', () => {
  it('带 remoteVersion 时追加" → 版本号"，基底锚定 HEALTH_META 词表原文', () => {
    expect(updateAvailableText('2.3.4')).toBe(`${HEALTH_META['update-available'].text} → 2.3.4`)
  })

  it('无版本记录（空串/缺省/null）只回词表原文——绝不编造版本', () => {
    const base = HEALTH_META['update-available'].text
    expect(updateAvailableText()).toBe(base)
    expect(updateAvailableText('')).toBe(base)
    expect(updateAvailableText(null)).toBe(base)
    expect(updateAvailableText(undefined)).toBe(base)
  })
})

describe('PRIMARY_ACTION_META', () => {
  const ACTION_KEYS = {
    install: true, enable: true, open: true, update: true, repair: true,
    retry: true, disable: true, uninstall: true, none: true,
  } satisfies Record<PrimaryActionValue, true>

  const VARIANTS: UiButtonVariant[] = ['primary', 'secondary', 'danger', 'ghost']

  it('表键与 PrimaryActionValue 逐键一致，label 非空且 variant 落在 UiButton 词表', () => {
    expect(Object.keys(PRIMARY_ACTION_META).sort()).toEqual(Object.keys(ACTION_KEYS).sort())
    for (const [key, meta] of Object.entries(PRIMARY_ACTION_META)) {
      expect(meta.label, key).toBeTruthy()
      expect(VARIANTS, key).toContain(meta.variant)
    }
  })

  it('色调分级裁决：install/enable/open→primary，update/repair/retry/disable→secondary，uninstall→danger，none→ghost+禁用', () => {
    for (const action of ['install', 'enable', 'open'] as const) {
      expect(PRIMARY_ACTION_META[action].variant, action).toBe('primary')
    }
    for (const action of ['update', 'repair', 'retry', 'disable'] as const) {
      expect(PRIMARY_ACTION_META[action].variant, action).toBe('secondary')
    }
    expect(PRIMARY_ACTION_META.uninstall.variant).toBe('danger')
    expect(PRIMARY_ACTION_META.none).toEqual({ label: '无可用操作', variant: 'ghost', disabled: true })
    for (const [key, meta] of Object.entries(PRIMARY_ACTION_META)) {
      expect(meta.disabled, key).toBe(key === 'none')
    }
  })
})

describe('SUMMARY_META', () => {
  const SUMMARY_KEYS = {
    'not-installed': true, 'installed-disabled': true, 'installed-enabled': true,
    running: true, 'running-update': true, blocked: true, faulted: true, 'in-progress': true,
  } satisfies Record<SummaryKeyValue, true>

  it('表键与 SummaryKeyValue 逐键一致，每键映射非空短语', () => {
    expect(Object.keys(SUMMARY_META).sort()).toEqual(Object.keys(SUMMARY_KEYS).sort())
    for (const [key, phrase] of Object.entries(SUMMARY_META)) {
      expect(phrase, key).toBeTruthy()
    }
  })

  it('锁定关键短语口径（首页/模块中心/详情共用）', () => {
    expect(SUMMARY_META['running-update']).toBe('运行中，有更新')
    expect(SUMMARY_META['in-progress']).toBe('操作进行中')
    expect(SUMMARY_META.faulted).toBe('异常，可重试')
    expect(SUMMARY_META['not-installed']).toBe('未安装')
  })
})

// ── Wave 4 统一 Operation 观察面词表 ──

describe('OPERATION_KIND_META', () => {
  const KIND_KEYS = {
    install: true, activate: true, stop: true, update: true,
    rollback: true, repair: true, remove: true, invoke: true,
  } satisfies Record<OperationKindValue, true>

  it('表键与 OperationKindValue 逐键一致，每值三通道齐备（text + 合法 tone + icons.ts 注册 icon）', () => {
    expect(Object.keys(OPERATION_KIND_META).sort()).toEqual(Object.keys(KIND_KEYS).sort())
    for (const [key, meta] of Object.entries(OPERATION_KIND_META)) {
      expect(meta.text, key).toBeTruthy()
      expect(TONES, `${key}.tone`).toContain(meta.tone)
      expect(ICON_NAMES as string[], `${key}.icon`).toContain(meta.icon)
    }
  })

  it('取值函数命中表项；未知 kind 兜底"未知操作"不抛错（后端加词不炸前端）', () => {
    for (const [key, meta] of Object.entries(OPERATION_KIND_META)) {
      expect(operationKindMeta(key)).toBe(meta)
    }
    expect(operationKindMeta('teleport')).toEqual({ text: '未知操作', tone: 'neutral', icon: 'help-circle' })
    expect(operationKindMeta('')).toEqual({ text: '未知操作', tone: 'neutral', icon: 'help-circle' })
  })
})

describe('OPERATION_STATUS_META', () => {
  const STATUS_KEYS = {
    queued: true, running: true, succeeded: true, failed: true, cancelled: true,
  } satisfies Record<OperationStatusValue, true>

  it('表键与 OperationStatusValue 逐键一致，图标三通道齐备', () => {
    expect(Object.keys(OPERATION_STATUS_META).sort()).toEqual(Object.keys(STATUS_KEYS).sort())
    for (const [key, meta] of Object.entries(OPERATION_STATUS_META)) {
      expect(meta.text, key).toBeTruthy()
      expect(TONES, `${key}.tone`).toContain(meta.tone)
      expect(ICON_NAMES as string[], `${key}.icon`).toContain(meta.icon)
    }
  })

  it('首页最近任务图标裁决：成功=对勾、失败=警示、取消=电源；未知状态兜底"状态未知"', () => {
    expect(OPERATION_STATUS_META.succeeded.icon).toBe('check-square')
    expect(OPERATION_STATUS_META.failed.icon).toBe('alert-triangle')
    expect(OPERATION_STATUS_META.cancelled.icon).toBe('power')
    expect(operationStatusMeta('nope')).toEqual({ text: '状态未知', tone: 'neutral', icon: 'help-circle' })
  })
})

describe('PHASE_META', () => {
  const PHASE_KEYS = {
    resolve: true, download: true, verify: true, unpack: true, place: true, done: true,
  } satisfies Record<OperationPhaseValue, true>

  it('表键与 OperationPhaseValue 逐键一致，每值映射非空中文短语', () => {
    expect(Object.keys(PHASE_META).sort()).toEqual(Object.keys(PHASE_KEYS).sort())
    for (const [key, phrase] of Object.entries(PHASE_META)) {
      expect(phrase, key).toBeTruthy()
    }
  })

  it('已知阶段取中文；未登记步骤名原样透出（后端按模块自定义步骤，不做假映射）', () => {
    expect(operationPhaseText('download')).toBe('下载')
    expect(operationPhaseText('unpack')).toBe('解压')
    expect(operationPhaseText('self-heal')).toBe('self-heal')
  })
})
