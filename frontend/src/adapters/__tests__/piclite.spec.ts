// ============================================================================
// PicLite adapter · MSI 提取族（唤窗型）接入核验（薄测）
//
//  - 事件名逐字 + 单键进度形（与 keyviz 同为 MSI 提取族但动词回到 OpenWindow
//    唤窗形制——族内动词也有分叉，接线名与动词各断各的）；
//  - remove 确认闸、import 去空白、回执词面走共享单源；
//  - quit 的 running 态指引如实预告「进行中的批量压缩会中断」（卸载/退出
//    类风险必须前置告知，事后 toast 救不回被中断的压缩任务）。
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
  OpenWindow: vi.fn(),
  Quit: vi.fn(),
  GetFollowOnExit: vi.fn(),
  SetFollowOnExit: vi.fn(),
  CreateDesktopShortcut: vi.fn(),
  OpenConfigDir: vi.fn(),
  RepositoryURL: vi.fn(),
  OpenRepository: vi.fn(),
}))

const events = vi.hoisted(() => [] as Array<{ name: string; handler: (data: unknown) => void }>)
const ui = vi.hoisted(() => ({ confirm: vi.fn(), prompt: vi.fn() }))

vi.mock('../../../bindings/hanxi/internal/modules/piclite/picliteservice', () => svc)
vi.mock('../../composables/useWailsEvent', () => ({
  useWailsEvent: vi.fn((name: string, handler: (data: unknown) => void) => {
    events.push({ name, handler })
    return () => {}
  }),
}))
vi.mock('../../composables/useConfirm', () => ({ useConfirm: () => ui }))
vi.mock('../../composables/usePrompt', () => ({ usePrompt: () => ui }))

import { createPicLiteAdapter } from '../piclite'

function lastHandler() {
  const entry = events[events.length - 1]
  if (!entry) throw new Error('没有捕获到任何事件订阅')
  return entry
}

describe('piclite · MSI 提取族（唤窗形制）接入', () => {
  beforeEach(() => {
    events.length = 0
    vi.clearAllMocks()
  })

  it('事件名逐字；主钮动词回 OpenWindow（族内形制分叉）；进度票据单键形', async () => {
    const adapter = createPicLiteAdapter()
    adapter.subscribeInstanceState(vi.fn())
    expect(lastHandler().name).toBe('piclite:instance-state')

    const cb = vi.fn()
    adapter.subscribeProgress(cb)
    expect(lastHandler().name).toBe('piclite:version-download')
    lastHandler().handler({ version: '1.5.2', stage: 'downloading', done: 2, total: 5, message: '下载中' })
    expect(cb).toHaveBeenCalledWith({ key: '1.5.2', stage: 'downloading', done: 2, total: 5, message: '下载中' })

    svc.OpenWindow.mockResolvedValueOnce({ message: '已唤起主窗口' })
    expect(await adapter.control!.primary!.run()).toEqual({ message: '已唤起主窗口' })
    expect(adapter.control!.primary!.titleFor!('running')).toBe('唤起主窗口（单实例协议）')
    expect(adapter.control!.quit!.titleFor!('running')).toContain('批量压缩会中断')
  })

  it('remove 取消零 RPC / 确认走共享卸载词；import 去空白且取消不触后端', async () => {
    const adapter = createPicLiteAdapter()

    ui.confirm.mockResolvedValueOnce(false)
    expect(await adapter.versions.remove({ version: '1.5.2' } as never)).toEqual({})
    expect(svc.RemoveVersion).not.toHaveBeenCalled()
    ui.confirm.mockResolvedValueOnce(true)
    expect(await adapter.versions.remove({ version: '1.5.2' } as never)).toEqual({
      message: '已卸载 1.5.2',
      reloadVersions: true,
    })

    ui.prompt.mockResolvedValueOnce('  D:\\Tools\\PicLite  ')
    svc.ImportLocal.mockResolvedValueOnce({ version: '1.5.0' })
    expect(await adapter.versions.importLocal!()).toEqual({
      message: '已导入 PicLite 1.5.0',
      reloadVersions: true,
    })
    expect(svc.ImportLocal).toHaveBeenCalledWith('D:\\Tools\\PicLite')

    svc.DownloadVersion.mockResolvedValueOnce('already-installed')
    expect(await adapter.versions.download({ version: '1.5.2' } as never)).toEqual({
      message: '版本 1.5.2 已安装',
      reloadVersions: true,
    })
  })
})
