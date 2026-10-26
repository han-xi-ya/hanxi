// ============================================================================
// QuickLook adapter · 启动托管型 + reset 槽重载动词 + zip 词族方言
//
//  - reset 槽：后端 Reload 回的是裸字符串话术，回执必须 {message} 原样包装
//    透传——话术本身（成功/失败/未运行）由后端分化，前端不二次加工；
//  - primary 是「启动托管」语义：running 即禁用（无唤窗路径，样式走托盘），
//    与 OpenWindow 家族 running 可点形成可断言的分叉；
//  - zip 解压词族：verify=「哈希校验…」、extract=「解压安装…」；
//    firstUseDownloadLabel 只报版本号不带体积（与 paseo/recordly 的
//    「（约 X）」词族刻意不同——下载钮词面即各模块声明的事实）；
//  - 事件名逐字 + 单键形。
// ============================================================================

import { beforeEach, describe, expect, it, vi } from 'vitest'

const svc = vi.hoisted(() => ({
  GetStatus: vi.fn(),
  ListInstalledVersions: vi.fn(),
  ListReleases: vi.fn(),
  GetActiveVersion: vi.fn(),
  SetActiveVersion: vi.fn(),
  DownloadVersion: vi.fn(),
  RemoveVersion: vi.fn(),
  ImportLocal: vi.fn(),
  OpenDir: vi.fn(),
  StartQuickLook: vi.fn(),
  Quit: vi.fn(),
  Reload: vi.fn(),
  GetFollowOnExit: vi.fn(),
  SetFollowOnExit: vi.fn(),
  RepositoryURL: vi.fn(),
  OpenRepository: vi.fn(),
}))

const events = vi.hoisted(() => [] as Array<{ name: string; handler: (data: unknown) => void }>)
const ui = vi.hoisted(() => ({ confirm: vi.fn(), prompt: vi.fn() }))

vi.mock('../../../bindings/hanxi/internal/modules/quicklook/quicklookservice', () => svc)
vi.mock('../../composables/useWailsEvent', () => ({
  useWailsEvent: vi.fn((name: string, handler: (data: unknown) => void) => {
    events.push({ name, handler })
    return () => {}
  }),
}))
vi.mock('../../composables/useConfirm', () => ({ useConfirm: () => ui }))
vi.mock('../../composables/usePrompt', () => ({ usePrompt: () => ui }))

import { createQuickLookAdapter } from '../quicklook'

function lastHandler() {
  const entry = events[events.length - 1]
  if (!entry) throw new Error('没有捕获到任何事件订阅')
  return entry
}

describe('quicklook · reset 槽与启动托管方言', () => {
  beforeEach(() => {
    events.length = 0
    vi.clearAllMocks()
  })

  it('reset 槽把 Reload 的裸字符串话术原样装进回执（不吞不改写，成功态也不刷版本区）', async () => {
    const adapter = createQuickLookAdapter()
    svc.Reload.mockResolvedValueOnce('配置已重载，即时生效')
    expect(await adapter.reset!.run()).toEqual({ message: '配置已重载，即时生效' })
    expect(svc.Reload).toHaveBeenCalledTimes(1)
  })

  it('primary 走 StartQuickLook 且 running 即禁用（启动托管≠唤窗家族）；事件名逐字', async () => {
    const adapter = createQuickLookAdapter()
    svc.StartQuickLook.mockResolvedValueOnce({ message: 'QuickLook 已启动' })
    expect(await adapter.control!.primary!.run()).toEqual({ message: 'QuickLook 已启动' })
    expect(adapter.control!.primary!.disabledFor!('running')).toBe(true)
    expect(adapter.control!.primary!.disabledFor!('stopped')).toBe(false)
    expect(adapter.control!.primary!.cssClass).toBe('btn-primary')
    expect(adapter.control!.quit!.disabledFor!('external')).toBe(false)

    const snap = vi.fn()
    adapter.subscribeInstanceState!(snap)
    expect(lastHandler().name).toBe('quicklook:instance-state')
    const cb = vi.fn()
    adapter.subscribeProgress(cb)
    expect(lastHandler().name).toBe('quicklook:version-download')
    lastHandler().handler({ version: '4.5.0', stage: 'done', done: 8, total: 8, message: '完成' })
    expect(cb).toHaveBeenCalledWith({ key: '4.5.0', stage: 'done', done: 8, total: 8, message: '完成' })
  })

  it('zip 词族：verify/extract 各有专词、其余回空；首用下载钮只报版本不带体积；官方徽标词「官方 zip」', () => {
    const adapter = createQuickLookAdapter()
    expect(adapter.copy!.stageWord!('verify', undefined as never)).toBe('哈希校验…')
    expect(adapter.copy!.stageWord!('extract', undefined as never)).toBe('解压安装…')
    expect(adapter.copy!.stageWord!('downloading', undefined as never)).toBe('')
    expect(adapter.copy!.firstUseDownloadLabel!({ version: '4.5.0', size: 10485760 } as never)).toBe('安装最新版 4.5.0')
    expect(adapter.copy!.officialBadge).toBe('官方 zip')
    expect(adapter.copy!.importLabel).toBe('⇥ 导入本地便携目录')
  })
})
