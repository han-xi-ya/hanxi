// runtimeIcons 通道特征测试（N27 红线运行期提取）：成功转 data URL、
// 并发去重、失败负缓存永不重试——渲染面据此"提取失败=现状不变"。
import { beforeEach, describe, expect, it, vi } from 'vitest'

// ensureRuntimeIcon 内动态 import 生成绑定 RuntimeIconService.IconPNG，
// vi.mock 同样拦截动态 import 形态（[]byte 经 wails 出货为 base64 字符串）。
const iconPNG = vi.hoisted(() => vi.fn())
vi.mock('../../../bindings/hanxi/internal/app', () => ({
  RuntimeIconService: { IconPNG: iconPNG },
}))

import {
  __resetRuntimeIconsForTest,
  ensureRuntimeIcon,
  runtimeIconFailed,
  runtimeIconReady,
  runtimeIconUrl,
} from '../runtimeIcons'

beforeEach(() => {
  __resetRuntimeIconsForTest()
  iconPNG.mockReset()
})

describe('ensureRuntimeIcon', () => {
  it('成功：[]byte 出货的 base64 串拼成 data URL 并坐实 ready', async () => {
    iconPNG.mockResolvedValue('iVBORw0KGgo=')
    await ensureRuntimeIcon('rammap')
    expect(iconPNG).toHaveBeenCalledTimes(1)
    expect(iconPNG).toHaveBeenCalledWith('rammap')
    expect(runtimeIconUrl('rammap')).toBe('data:image/png;base64,iVBORw0KGgo=')
    expect(runtimeIconReady('rammap')).toBe(true)
  })

  it('并发去重：同 ID 多次 ensure 只撞一次 RPC，全部拿到同一份结果', async () => {
    let resolve!: (v: string) => void
    iconPNG.mockReturnValue(new Promise<string>((r) => { resolve = r }))
    const all = Promise.all([ensureRuntimeIcon('vscode'), ensureRuntimeIcon('vscode'), ensureRuntimeIcon('vscode')])
    resolve('AAA=')
    await all
    expect(iconPNG).toHaveBeenCalledTimes(1)
    expect(runtimeIconUrl('vscode')).toBe('data:image/png;base64,AAA=')
  })

  it('失败：负缓存坐实，后续 ensure 不再重试（前端每次渲染零打扰）', async () => {
    iconPNG.mockRejectedValue(new Error('module disabled'))
    await ensureRuntimeIcon('recordly')
    expect(runtimeIconFailed('recordly')).toBe(true)
    expect(runtimeIconUrl('recordly')).toBeUndefined()
    await ensureRuntimeIcon('recordly')
    expect(iconPNG).toHaveBeenCalledTimes(1)
  })

  it('空返回按失败处理（后端理论上不回空，防 data URL 悬空）', async () => {
    iconPNG.mockResolvedValue('')
    await ensureRuntimeIcon('rammap')
    expect(runtimeIconFailed('rammap')).toBe(true)
  })

  it('空 moduleId：静默 no-op，不发 RPC', async () => {
    await ensureRuntimeIcon('')
    expect(iconPNG).not.toHaveBeenCalled()
  })

  it('成功后再 ensure 不重取（进程级缓存）', async () => {
    iconPNG.mockResolvedValue('AAA=')
    await ensureRuntimeIcon('rammap')
    await ensureRuntimeIcon('rammap')
    expect(iconPNG).toHaveBeenCalledTimes(1)
  })
})
