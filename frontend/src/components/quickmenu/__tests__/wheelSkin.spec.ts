// 皮肤账特征测试：缺省兼容（旧账无此键/坏 JSON/脏值 → 默认皮肤不炸）、镜像
// 持久化（save 合入语义 + read 回读闭环 + storage 键契约）与后端真相通道
// （DTO 整数百分比双向往返、fetch/push 的失败上抛语义——降级决策在调用侧）。
import { beforeEach, describe, expect, it, vi } from 'vitest'
import {
  DEFAULT_WHEEL_SKIN, FACE_ALPHA_MIN, STROKE_MAX, WHEEL_SKIN_STORAGE_KEY,
  fetchWheelSkin, normalizeWheelSkin, parseWheelSkin, pushWheelSkin, readWheelSkin, saveWheelSkin,
  wheelSkinFromDto, wheelSkinToDto, wheelSkinVars, wheelStrokeMixPercent,
} from '../wheelSkin'

const api = vi.hoisted(() => ({ GetSkin: vi.fn(), SetSkin: vi.fn() }))
vi.mock('../../../../bindings/hanxi/internal/modules/quickmenu', () => ({ QuickMenuService: api }))

beforeEach(() => {
  localStorage.clear()
  vi.clearAllMocks()
})

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

describe('后端真相通道（DTO 线格式 + fetch/push）', () => {
  it('线格式 ↔ 本地皮肤双向往返：整数百分比化，脏线值同闸归一', () => {
    expect(wheelSkinToDto({ preset: 'veil', faceAlpha: 0.65, stroke: 0.2, followModuleColor: true }))
      .toEqual({ preset: 'veil', faceAlpha: 65, stroke: 20, followModuleColor: true })
    expect(wheelSkinFromDto({ preset: 'ink', faceAlpha: 35, stroke: 100 }))
      .toEqual({ preset: 'ink', faceAlpha: 0.35, stroke: 1, followModuleColor: false })
    // 脏线格式：野预设/超域/缺字段/非对象 → normalize 同一闸口
    expect(wheelSkinFromDto({ preset: 'neon', faceAlpha: 999, stroke: 'x' }))
      .toEqual({ preset: 'frost', faceAlpha: 1, stroke: DEFAULT_WHEEL_SKIN.stroke, followModuleColor: false })
    expect(wheelSkinFromDto(null)).toEqual(DEFAULT_WHEEL_SKIN)
  })

  it('fetch：后端载荷归一为皮肤；野预设回落、超域百分比钳边（缺省兼容到线格式侧）', async () => {
    api.GetSkin.mockResolvedValue({ preset: 'veil', faceAlpha: 60, stroke: 30, followModuleColor: true })
    expect(await fetchWheelSkin()).toEqual({ preset: 'veil', faceAlpha: 0.6, stroke: 0.3, followModuleColor: true })
    api.GetSkin.mockResolvedValue({ preset: 'bogus', faceAlpha: 150, stroke: -3 })
    expect(await fetchWheelSkin()).toEqual({ preset: 'frost', faceAlpha: 1, stroke: 0, followModuleColor: false })
  })

  it('fetch：RPC 失败与非对象载荷上抛（降级决策留调用侧，绝不静默出假真相）', async () => {
    api.GetSkin.mockRejectedValue(new Error('模块已停用'))
    await expect(fetchWheelSkin()).rejects.toThrow('模块已停用')
    api.GetSkin.mockResolvedValue(null)
    await expect(fetchWheelSkin()).rejects.toThrow()
  })

  it('push：出参为归一+百分比化线格式；回显覆写为真相皮肤', async () => {
    api.SetSkin.mockResolvedValue({ preset: 'ink', faceAlpha: 35, stroke: 0, followModuleColor: true })
    const eff = await pushWheelSkin({ preset: 'ink', faceAlpha: 0.2, stroke: 0, followModuleColor: true })
    // 出线前本地已归一（0.2 → 域下限 0.35 → 35），与后端合法域对齐、往返无损
    expect(api.SetSkin).toHaveBeenCalledWith({ preset: 'ink', faceAlpha: 35, stroke: 0, followModuleColor: true })
    // 回显即真相，调用侧照抄
    expect(eff.faceAlpha).toBeCloseTo(0.35)
    expect(eff.stroke).toBe(0)
  })
})
