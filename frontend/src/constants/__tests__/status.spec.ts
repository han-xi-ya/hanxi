// 特征测试：锁定两张状态表的文案/图标/色调与回退语义，
// 迁移（EnvCheckView 副本删除、托管视图 stateText 收编）后必须全绿。
import { describe, expect, it } from 'vitest'
import {
  ENV_STATUS_META,
  TOOL_STATE_META,
  envStatusMeta,
  toolStateMeta,
  type StateTone,
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
