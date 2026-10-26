// clipboardBridge 真桥归一化契约测试（kernel §7"视图永远见不到 null
// 线形"承诺的反证场）：
//   ① Go 侧 nil slice/map 上线为 null，wireEntry/wireStatus/wirePrefs 折叠成
//      kernel 契约形（Entry 可选域 undefined、Status/Prefs 空集），逐字段钉死；
//   ② List/QueueList 顶层 null → 空数组（不是 undefined、不是 throw）；
//   ③ 非空线形原样透传不吞字段（thumb/未知扩展字段走 spread 进账本）；
//   ④ 二十法方法名对位：门面调用必须原样打到绑定同名导出上（错名即 undefined
//      throw，显式断言转发参数与实参逐字对位）；
//   ⑤ install 幂等 + 注入后面门从"未接线可读错误"切到真桥。
// 绑定模块整体 vi.mock（本 spec 是除 clipboardBridge 自身外唯一碰绑定面的测试，
// mock 名与生成物 clipboardservice.js 导出逐字对位）。
import { beforeAll, describe, expect, it, vi } from 'vitest'

const CLIPAPI = vi.hoisted(() => ({
  List: vi.fn(),
  Get: vi.fn(),
  Set: vi.fn(),
  CreateText: vi.fn(),
  TogglePin: vi.fn(),
  Delete: vi.fn(),
  ClearAll: vi.fn(),
  SetPaused: vi.fn(),
  GetStatus: vi.fn(),
  CollapseOverlay: vi.fn(),
  Paste: vi.fn(),
  GetPrefs: vi.fn(),
  SetPrefs: vi.fn(),
  QueueAdd: vi.fn(),
  QueueRemove: vi.fn(),
  QueueClear: vi.fn(),
  QueueList: vi.fn(),
  QueueStep: vi.fn(),
  GetQueueHotkey: vi.fn(),
  SetQueueHotkey: vi.fn(),
}))

vi.mock('../../../bindings/hanxi/internal/modules/clipboard/clipboardservice', () => CLIPAPI)

import {
  clipboardClearAll,
  clipboardCollapseOverlay,
  clipboardCreateText,
  clipboardDelete,
  clipboardGet,
  clipboardGetPrefs,
  clipboardGetQueueHotkey,
  clipboardGetStatus,
  clipboardList,
  clipboardPaste,
  clipboardQueueAdd,
  clipboardQueueClear,
  clipboardQueueList,
  clipboardQueueRemove,
  clipboardQueueStep,
  clipboardSet,
  clipboardSetPaused,
  clipboardSetPrefs,
  clipboardSetQueueHotkey,
  clipboardTogglePin,
  setClipboardTransport,
} from '../clipboard'
import { installClipboardBridge } from '../clipboardBridge'

beforeAll(() => {
  setClipboardTransport(null) // 防御：不受其它 spec 残留注入影响
  installClipboardBridge() // main.ts 的副作用注入路径即被测路径
  installClipboardBridge() // 幂等：二次调用不重注、不抛
})

describe('wireEntry：Entry 可选域 null → undefined（契约形"省略"）', () => {
  it('List 行内 text/files/autoTags/blob/blobData/sourceApp/sourceExe 的 null 全部折叠，其余字段透传', async () => {
    CLIPAPI.List.mockResolvedValueOnce([
      {
        id: 'e1', hash: 'h1', kind: 'image', preview: 'p',
        text: null, files: null, autoTags: null, blob: null, blobData: null,
        sourceApp: null, sourceExe: null,
        thumb: 'data:image/jpeg;base64,/9j/AA', byteSize: 7, createdAt: 100,
      },
    ])
    const [row] = await clipboardList('', 'all', 100)
    expect(row).toMatchObject({ id: 'e1', kind: 'image', thumb: 'data:image/jpeg;base64,/9j/AA', byteSize: 7 })
    for (const k of ['text', 'files', 'autoTags', 'blob', 'blobData', 'sourceApp', 'sourceExe'] as const) {
      expect(row[k], `可选域 ${k} 不得带 null 进视图`).toBeUndefined()
    }
  })

  it('非空值原样保留（折叠只吃 null，不清白合法载荷）', async () => {
    CLIPAPI.List.mockResolvedValueOnce([
      { id: 'e2', kind: 'file', preview: 'p', files: ['D:\\a.zip'], autoTags: ['cjk'], text: null },
    ])
    const [row] = await clipboardList('', 'all', 100)
    expect(row.files).toEqual(['D:\\a.zip'])
    expect(row.autoTags).toEqual(['cjk'])
  })
})

describe('顶层与 Status/Prefs 的 null 线形折叠', () => {
  it('List 顶层 null → []（Go nil slice 形态）', async () => {
    CLIPAPI.List.mockResolvedValueOnce(null)
    expect(await clipboardList('', 'text', 50)).toEqual([])
  })

  it('QueueList null → []', async () => {
    CLIPAPI.QueueList.mockResolvedValueOnce(null)
    expect(await clipboardQueueList()).toEqual([])
  })

  it('GetStatus：excludedExes null → []、queueLen null → 0', async () => {
    CLIPAPI.GetStatus.mockResolvedValueOnce({ paused: true, entryCount: 3, excludedExes: null, queueLen: null })
    const st = await clipboardGetStatus()
    expect(st.excludedExes).toEqual([])
    expect(st.queueLen).toBe(0)
    expect(st.paused).toBe(true)
  })

  it('GetPrefs：六个集合/字符串域 null 全折叠（[]/{}/""），值域透传', async () => {
    CLIPAPI.GetPrefs.mockResolvedValueOnce({
      maxEntries: 500, maxBlobMB: 100, deleteAfterPaste: false,
      excludedFactory: null, excludedCustom: null, appRetentionDays: null,
      queueHotkey: null, maskKinds: null, maskCustomPatterns: null,
    })
    const p = await clipboardGetPrefs()
    expect(p.excludedFactory).toEqual([])
    expect(p.excludedCustom).toEqual([])
    expect(p.appRetentionDays).toEqual({})
    expect(p.queueHotkey).toBe('')
    expect(p.maskKinds).toEqual([])
    expect(p.maskCustomPatterns).toEqual([])
    expect(p.maxEntries).toBe(500)
  })

  it('SetPrefs 回显同样过 wirePrefs（整表替换语义的回程闸），载荷逐字转发绑定', async () => {
    const draft = {
      maxEntries: 800, maxBlobMB: 100, deleteAfterPaste: true,
      excludedFactory: ['1password.exe'], excludedCustom: [], appRetentionDays: {},
      queueHotkey: 'Ctrl+Alt+X', maskKinds: ['phone'], maskCustomPatterns: [],
    }
    CLIPAPI.SetPrefs.mockResolvedValueOnce({ ...draft, appRetentionDays: null })
    const echo = await clipboardSetPrefs(draft)
    expect(CLIPAPI.SetPrefs).toHaveBeenCalledWith(draft)
    expect(echo.appRetentionDays).toEqual({}) // 回显也归一化，视图拿不到 null
    expect(echo.maskKinds).toEqual(['phone'])
  })
})

describe('方法面对位：门面参数原样打到绑定（错名/错位即此处红）', () => {
  it('标量入参诸法转发绑定同名导出', async () => {
    CLIPAPI.Get.mockResolvedValueOnce({ id: 'x', kind: 'text', preview: 'p' })
    await clipboardGet('x')
    expect(CLIPAPI.Get).toHaveBeenCalledWith('x')
    CLIPAPI.Set.mockResolvedValueOnce(undefined)
    await clipboardSet('s1')
    expect(CLIPAPI.Set).toHaveBeenCalledWith('s1')
    CLIPAPI.CreateText.mockResolvedValueOnce({ id: 'n', kind: 'text', preview: 't' })
    await clipboardCreateText('t')
    expect(CLIPAPI.CreateText).toHaveBeenCalledWith('t')
    CLIPAPI.TogglePin.mockResolvedValueOnce({ id: 'x', kind: 'text', preview: 'p', pinned: true })
    await clipboardTogglePin('x')
    expect(CLIPAPI.TogglePin).toHaveBeenCalledWith('x')
    CLIPAPI.Delete.mockResolvedValueOnce(undefined)
    await clipboardDelete('d')
    expect(CLIPAPI.Delete).toHaveBeenCalledWith('d')
    CLIPAPI.SetPaused.mockResolvedValueOnce(undefined)
    await clipboardSetPaused(true)
    expect(CLIPAPI.SetPaused).toHaveBeenCalledWith(true)
    CLIPAPI.Paste.mockResolvedValueOnce(undefined)
    await clipboardPaste('p1')
    expect(CLIPAPI.Paste).toHaveBeenCalledWith('p1')
    CLIPAPI.CollapseOverlay.mockResolvedValueOnce(undefined)
    await clipboardCollapseOverlay()
    expect(CLIPAPI.CollapseOverlay).toHaveBeenCalledTimes(1)
    CLIPAPI.ClearAll.mockResolvedValueOnce(undefined)
    await clipboardClearAll()
    expect(CLIPAPI.ClearAll).toHaveBeenCalledTimes(1)
    CLIPAPI.QueueAdd.mockResolvedValueOnce(undefined)
    await clipboardQueueAdd('q')
    expect(CLIPAPI.QueueAdd).toHaveBeenCalledWith('q')
    CLIPAPI.QueueRemove.mockResolvedValueOnce(undefined)
    await clipboardQueueRemove('q')
    expect(CLIPAPI.QueueRemove).toHaveBeenCalledWith('q')
    CLIPAPI.QueueClear.mockResolvedValueOnce(undefined)
    await clipboardQueueClear()
    expect(CLIPAPI.QueueClear).toHaveBeenCalledTimes(1)
    CLIPAPI.QueueStep.mockResolvedValueOnce(undefined)
    await clipboardQueueStep()
    expect(CLIPAPI.QueueStep).toHaveBeenCalledTimes(1)
    CLIPAPI.GetQueueHotkey.mockResolvedValueOnce({ accel: 'Ctrl+Alt+X', registered: true })
    expect(await clipboardGetQueueHotkey()).toEqual({ accel: 'Ctrl+Alt+X', registered: true })
    CLIPAPI.SetQueueHotkey.mockResolvedValueOnce({ accel: '', registered: false })
    expect(await clipboardSetQueueHotkey('')).toEqual({ accel: '', registered: false })
    expect(CLIPAPI.SetQueueHotkey).toHaveBeenCalledWith('')
  })
})
