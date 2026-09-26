// 皮肤账特征测试：缺省兼容（旧设置无此键/坏 JSON/脏值 → 默认皮肤不炸）与
// 切换持久化（save 合入语义 + read 回读闭环 + storage 键契约）。
import { beforeEach, describe, expect, it } from 'vitest'
import {
  DEFAULT_WHEEL_SKIN, FACE_ALPHA_MIN, STROKE_MAX, WHEEL_SKIN_STORAGE_KEY,
  normalizeWheelSkin, parseWheelSkin, readWheelSkin, saveWheelSkin, wheelSkinVars, wheelStrokeMixPercent,
} from '../wheelSkin'

beforeEach(() => localStorage.clear())

describe('缺省兼容（老 settings / 坏值读出默认皮肤）', () => {
  it('空存储读出默认', () => {
    expect(readWheelSkin()).toEqual(DEFAULT_WHEEL_SKIN)
  })

  it('null/空串/坏 JSON/非对象 JSON 全部回落默认，永不抛错', () => {
    for (const raw of [null, undefined, '', '{oops', '[]', '"str"', '42']) {
      expect(parseWheelSkin(raw as string | null | undefined)).toEqual(DEFAULT_WHEEL_SKIN)
    }
  })

  it('部分字段缺失：缺的用默认、有的保留', () => {
    expect(parseWheelSkin('{"preset":"ink"}')).toEqual({ ...DEFAULT_WHEEL_SKIN, preset: 'ink' })
  })

  it('未知预设/脏类型/越界数值全部归一（老版本写的新字段名被忽略不炸）', () => {
    const dirty = normalizeWheelSkin({
      preset: 'neon', faceAlpha: 0.01, stroke: 9, followModuleColor: 'yes', extraFutureField: 1,
    })
    expect(dirty.preset).toBe(DEFAULT_WHEEL_SKIN.preset)
    expect(dirty.faceAlpha).toBe(FACE_ALPHA_MIN) // 越界钳到域内
    expect(dirty.stroke).toBe(STROKE_MAX)
    expect(dirty.followModuleColor).toBe(false) // 非 true 一律视为关
  })
})

describe('切换持久化', () => {
  it('save 合入写盘 → read 回读闭环（切预设+拨开关+拖滑杆各存各的）', () => {
    let skin = saveWheelSkin({ preset: 'veil' })
    expect(skin.preset).toBe('veil')
    skin = saveWheelSkin({ followModuleColor: true }, skin)
    skin = saveWheelSkin({ faceAlpha: 0.6 }, skin)
    expect(readWheelSkin()).toEqual(skin)
    expect(readWheelSkin()).toEqual({ preset: 'veil', faceAlpha: 0.6, stroke: DEFAULT_WHEEL_SKIN.stroke, followModuleColor: true })
  })

  it('写盘 JSON 落在约定键上（弹窗侧靠该键判 storage 事件）', () => {
    saveWheelSkin({ preset: 'ink' })
    expect(JSON.parse(localStorage.getItem(WHEEL_SKIN_STORAGE_KEY) ?? '{}').preset).toBe('ink')
  })

  it('脏写回读即净：手改存储成坏值，读出仍是合法皮肤', () => {
    localStorage.setItem(WHEEL_SKIN_STORAGE_KEY, '{"faceAlpha":"很透"}')
    expect(readWheelSkin().faceAlpha).toBe(DEFAULT_WHEEL_SKIN.faceAlpha)
  })
})

describe('皮肤 → 盘面 CSS 变量', () => {
  it('描边强度 0–1 映射混色 10%–60%，透明度直发', () => {
    expect(wheelStrokeMixPercent(0)).toBe(10)
    expect(wheelStrokeMixPercent(1)).toBe(60)
    expect(wheelStrokeMixPercent(0.55)).toBe(38)
    const vars = wheelSkinVars({ ...DEFAULT_WHEEL_SKIN, faceAlpha: 0.5, stroke: 0 })
    expect(vars['--wf-face-a']).toBe('0.5')
    expect(vars['--wf-edge']).toBe('10%')
  })
})
