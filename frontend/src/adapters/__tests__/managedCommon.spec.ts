// ============================================================================
// managedCommon · 托管适配器家族共享骨架的对外契约
//
// 断言对象是共享件对**消费方**的可验证事实，而非内部实现形状：
//  - 事件名原样透传（store/视图不接触 Wails 事件名，名字错一个字符=收不到推送）；
//  - 空载荷/缺 version 载荷必须被丢弃（store 以回调次数驱动快照投影）；
//  - NormalizedProgress 恰好五字段、key 即面板行匹配位（多带/少带字段都是契约破坏）；
//  - 回执词面即用户可见 toast 文案（`已将 X 设为使用版本`/`版本 X 已安装`/
//    `已卸载 X`），reloadVersions/activeVersion 决定共享件是否重拉与高亮。
// 本件是 adapter 家族同形接线与回执模板的单源——本 spec 即该单源的词面/键形锚点。
// ============================================================================

import { beforeEach, describe, expect, it, vi } from 'vitest'

// 捕获型假件：记录每次订阅的事件名与 handler，测试按真 useWailsEvent 的解包
// 语义直接以载荷调用 handler（真件在 Events.On 回调里做 event.data 解包）。
const events = vi.hoisted(() => [] as Array<{ name: string; handler: (data: unknown) => void }>)
vi.mock('../../composables/useWailsEvent', () => ({
  useWailsEvent: vi.fn((name: string, handler: (data: unknown) => void) => {
    events.push({ name, handler })
    return () => {}
  }),
}))

import {
  bindInstanceState,
  bindVersionProgress,
  downloadOutcome,
  setActiveResult,
  uninstalledResult,
  type VersionProgressPayload,
} from '../managedCommon'

function lastHandler() {
  const entry = events[events.length - 1]
  if (!entry) throw new Error('没有捕获到任何事件订阅')
  return entry
}

describe('bindInstanceState · 实例态事件接线', () => {
  beforeEach(() => {
    events.length = 0
  })

  it('事件名逐字透传（错一个字符=订阅不到后端推送）', () => {
    const cb = vi.fn()
    bindInstanceState('example:instance-state', cb)
    expect(events.map((e) => e.name)).toEqual(['example:instance-state'])
  })

  it('空载荷被丢弃；对象载荷按引用原样交付（不得克隆/裁剪字段）', () => {
    const cb = vi.fn()
    bindInstanceState('m:instance-state', cb)
    const { handler } = lastHandler()

    handler(null)
    handler(undefined)
    expect(cb).not.toHaveBeenCalled()

    const snap = { state: 'running', pid: 42, startedAt: '2026-10-26T00:00:00Z' }
    handler(snap)
    expect(cb).toHaveBeenCalledTimes(1)
    expect(cb).toHaveBeenCalledWith(snap)
    // 引用同一性：store 投影层依赖不做深拷贝摊平——方言字段（drawing 等）必须存活
    const delivered = cb.mock.calls[0][0]
    expect(delivered).toBe(snap)
  })
})

describe('bindVersionProgress · 下载进度归一', () => {
  const full: VersionProgressPayload & { extraDialectField?: string } = {
    version: '1.2.3',
    stage: 'downloading',
    done: 512,
    total: 1024,
    message: '下载中',
  }

  beforeEach(() => {
    events.length = 0
  })

  it('事件名透传；五字段归一恰形（key=version，多余方言字段不混入票据）', () => {
    const cb = vi.fn()
    bindVersionProgress('m:version-download', cb)
    expect(lastHandler().name).toBe('m:version-download')

    const { handler } = lastHandler()
    handler({ ...full, extraDialectField: 'es-component-tag' })
    expect(cb).toHaveBeenCalledTimes(1)
    expect(cb.mock.calls[0][0]).toEqual({
      key: '1.2.3',
      stage: 'downloading',
      done: 512,
      total: 1024,
      message: '下载中',
    })
    expect(Object.keys(cb.mock.calls[0][0]).sort()).toEqual(['done', 'key', 'message', 'stage', 'total'])
  })

  it('空载荷与缺 version 载荷整体丢弃（无 version 即无行可对齐，放行=面板幽灵票据）', () => {
    const cb = vi.fn()
    bindVersionProgress('m:version-download', cb)
    const { handler } = lastHandler()

    handler(null)
    handler(undefined)
    handler({ version: '', stage: 'downloading', done: 1, total: 2, message: '' })
    handler({ stage: 'error', done: 0, total: 0, message: '无版本号的畸形帧' })
    expect(cb).not.toHaveBeenCalled()
  })

  it('keyOf 覆写复合键形态，且仅在通过守卫后才被调用', () => {
    const cb = vi.fn()
    const keyOf = vi.fn((t: VersionProgressPayload & { variant?: string }) => `${t.version}|${t.variant || 'portable'}`)
    bindVersionProgress('bcu:version-download', cb, keyOf)
    const { handler } = lastHandler()

    handler({ ...full, variant: 'fdd' })
    expect(cb).toHaveBeenCalledWith(
      expect.objectContaining({ key: '1.2.3|fdd', stage: 'downloading' }),
    )

    // 缺 variant 帧回落便携版词（与 progressKey 反查的回落口径同形）
    handler({ ...full })
    expect(cb).toHaveBeenLastCalledWith(expect.objectContaining({ key: '1.2.3|portable' }))

    handler({ version: '', stage: 'done', done: 1, total: 1, message: '' })
    expect(keyOf).toHaveBeenCalledTimes(2) // 畸形帧不得驱动键构造
  })
})

describe('回执模板 · 用户可见 toast 词面与共享件行为位', () => {
  it('setActiveResult：词面逐字 + activeVersion 供面板高亮切换', () => {
    expect(setActiveResult('v3.20.0')).toEqual({
      message: '已将 v3.20.0 设为使用版本',
      activeVersion: 'v3.20.0',
    })
  })

  it('downloadOutcome：already-installed 弹词并要求重拉版本区；其余词静默回空', () => {
    expect(downloadOutcome('already-installed', '1.2.3')).toEqual({
      message: '版本 1.2.3 已安装',
      reloadVersions: true,
    })
    expect(downloadOutcome('started', '1.2.3')).toEqual({})
    expect(downloadOutcome('', '1.2.3')).toEqual({})
    expect(downloadOutcome(null, '1.2.3')).toEqual({})
    expect(downloadOutcome(undefined, '1.2.3')).toEqual({})
  })

  it('uninstalledResult：词面逐字 + reloadVersions（末版卸载后空态需面板重投影）', () => {
    expect(uninstalledResult('2.9.4')).toEqual({
      message: '已卸载 2.9.4',
      reloadVersions: true,
    })
  })
})
