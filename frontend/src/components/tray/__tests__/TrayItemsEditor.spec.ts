// 共享编辑面特征测试（轮盘独立批重写：保存钮/脏标记整退，实时保存 + scope 双账）：
// 挂载按 scope 成对拉账（tray↔Get/SetTrayMenu、wheel↔Get/SetWheelMenu，互不串账）；
// 结构性操作即时落盘全清单并上抛 saved；文本 blur 即时 + 800ms 防抖合流；在途写尾随
// 合流不丢最后一次；失败 toast 中文错误并回滚重拉（不上抛 saved、本地不假成功）；
// 分组草稿门（名为空/零子条目不发必拒账，补全后自动落盘）；候选目录收口（route 退出
// 供选、command/webapp 在列、存量 route 行仍渲染并标"不再供选"）。
// 分组/外部程序等完整交互矩阵仍由 SettingsSections.spec 经 TraySection 宿主覆盖（第四 lane 接续）。
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import TrayItemsEditor from '../TrayItemsEditor.vue'
import { useToast } from '../../../composables/useToast'

const appSvc = vi.hoisted(() => ({
  ListTrayMenuOptions: vi.fn(),
  GetTrayMenu: vi.fn(),
  SetTrayMenu: vi.fn(),
  GetWheelMenu: vi.fn(),
  SetWheelMenu: vi.fn(),
  PickExeFile: vi.fn(),
}))
vi.mock('../../../../bindings/hanxi/internal/app', () => ({ AppService: appSvc }))

const CMD1 = { type: 'command', ref: 'frpc/start', label: '启动 frpc', moduleName: 'frpc' }
const CMD2 = { type: 'command', ref: 'gonavi/launch', label: '启动 GoNavi', moduleName: 'gonavi' }
const LOGS = { type: 'route', ref: '/logs', path: '', args: '', label: '日志', enabled: true }

function stubs() {
  appSvc.ListTrayMenuOptions.mockResolvedValue([CMD1])
  appSvc.GetTrayMenu.mockResolvedValue([{ ...LOGS }])
  appSvc.SetTrayMenu.mockResolvedValue(undefined)
  appSvc.GetWheelMenu.mockResolvedValue([])
  appSvc.SetWheelMenu.mockResolvedValue(undefined)
}

async function mountReady(props?: Record<string, string>) {
  stubs()
  const w = mount(TrayItemsEditor, props ? { props } : undefined)
  await flushPromises()
  return w
}

afterEach(() => {
  vi.clearAllMocks()
  vi.useRealTimers()
  useToast().clearToast()
})

describe('TrayItemsEditor 双账与实时保存', () => {
  it('默认 scope=tray：挂载拉 GetTrayMenu 账目、不碰 wheel RPC；保存钮已退役', async () => {
    const w = await mountReady()
    expect(appSvc.ListTrayMenuOptions).toHaveBeenCalledTimes(1)
    expect(appSvc.GetTrayMenu).toHaveBeenCalledTimes(1)
    expect(appSvc.GetWheelMenu).not.toHaveBeenCalled()
    expect(w.findAll('.tray-row')).toHaveLength(1)
    expect((w.find('.tray-name').element as HTMLInputElement).value).toBe('日志')
    expect(w.find('.tray-footer .btn-primary').exists()).toBe(false)
  })

  it('scope=wheel：挂载拉 GetWheelMenu、不碰 GetTrayMenu', async () => {
    const w = await mountReady({ scope: 'wheel' })
    expect(appSvc.GetWheelMenu).toHaveBeenCalledTimes(1)
    expect(appSvc.GetTrayMenu).not.toHaveBeenCalled()
    w.unmount()
  })

  it('勾选候选即落 SetTrayMenu 全清单并上抛 saved；成功静默不 toast', async () => {
    const w = await mountReady()
    await w.findAll('.option-item input')[0].setValue(true)
    await flushPromises()
    expect(w.findAll('.tray-row')).toHaveLength(2)
    expect(appSvc.SetTrayMenu).toHaveBeenCalledTimes(1)
    expect(appSvc.SetWheelMenu).not.toHaveBeenCalled()
    const saved = appSvc.SetTrayMenu.mock.calls[0][0]
    expect(saved).toHaveLength(2)
    expect(saved[1]).toMatchObject({ type: 'command', ref: 'frpc/start', enabled: true })
    expect(useToast().toastMsg.value).toBe('')
    expect(w.emitted('saved')).toHaveLength(1)
  })

  it('wheel 账勾选落 SetWheelMenu 全清单，SetTrayMenu 零调用（Get/Set 成对不串账）', async () => {
    const w = await mountReady({ scope: 'wheel' })
    await w.findAll('.option-item input')[0].setValue(true)
    await flushPromises()
    expect(appSvc.SetWheelMenu).toHaveBeenCalledTimes(1)
    expect(appSvc.SetTrayMenu).not.toHaveBeenCalled()
    expect(appSvc.SetWheelMenu.mock.calls[0][0]).toHaveLength(1)
    expect(w.emitted('saved')).toHaveLength(1)
  })

  it('label blur 即时落盘；键入 800ms 防抖且连打合流一次', async () => {
    stubs()
    vi.useFakeTimers()
    const w = mount(TrayItemsEditor)
    await flushPromises()
    const name = w.find('.tray-name')
    await name.setValue('新名')
    await name.trigger('blur') // blur 抢跑防抖头，即时落盘
    expect(appSvc.SetTrayMenu).toHaveBeenCalledTimes(1)
    expect(appSvc.SetTrayMenu.mock.calls[0][0][0]).toMatchObject({ label: '新名' })
    await name.setValue('甲')
    await name.setValue('乙')
    expect(appSvc.SetTrayMenu).toHaveBeenCalledTimes(1) // 防抖窗内未落盘
    await vi.advanceTimersByTimeAsync(800)
    expect(appSvc.SetTrayMenu).toHaveBeenCalledTimes(2) // 到时合流为一次尾批
    expect(appSvc.SetTrayMenu.mock.calls[1][0][0]).toMatchObject({ label: '乙' })
    w.unmount()
  })

  it('在途写合流：首笔未回时二次勾选合并尾随写，尾批含全部改动、每笔落盘各上抛一次 saved', async () => {
    appSvc.ListTrayMenuOptions.mockResolvedValue([CMD1, CMD2])
    appSvc.GetTrayMenu.mockResolvedValue([{ ...LOGS }])
    const resolvers: Array<() => void> = []
    appSvc.SetTrayMenu.mockImplementation(() => new Promise<void>((r) => { resolvers.push(r) }))
    const w = mount(TrayItemsEditor)
    await flushPromises()

    await w.findAll('.option-item input')[0].setValue(true) // 第一笔：同步进入在途
    expect(appSvc.SetTrayMenu).toHaveBeenCalledTimes(1)
    await w.findAll('.option-item input')[1].setValue(true) // 在途期间第二笔：只置尾随标记
    expect(appSvc.SetTrayMenu).toHaveBeenCalledTimes(1)
    resolvers[0]()
    await flushPromises()
    expect(appSvc.SetTrayMenu).toHaveBeenCalledTimes(2) // 尾随写补发
    resolvers[1]()
    await flushPromises()
    const final = appSvc.SetTrayMenu.mock.calls[1][0]
    expect(final).toHaveLength(3) // 存量 + 两勾选全在尾批
    expect(w.emitted('saved')).toHaveLength(2) // 两笔都真实落盘，各上抛一次供宿主刷预览
  })

  it('保存失败：toast 中文错误并回滚重拉服务端真相，不上抛 saved、勾选翻转', async () => {
    stubs()
    appSvc.SetTrayMenu.mockRejectedValue(new Error('配置写入失败'))
    const w = mount(TrayItemsEditor)
    await flushPromises()
    await w.findAll('.option-item input')[0].setValue(true)
    await flushPromises()
    const msg = useToast().toastMsg.value
    expect(msg).toContain('保存托盘菜单失败')
    expect(msg).toContain('配置写入失败')
    expect(appSvc.GetTrayMenu.mock.calls.length).toBe(2) // 失败回滚重拉
    expect(w.emitted('saved')).toBeUndefined()
    expect(w.findAll('.tray-row')).toHaveLength(1) // 本地草稿作废，回到服务端真相
    expect((w.findAll('.option-item input')[0].element as HTMLInputElement).checked).toBe(false)
  })

  it('wheel 保存失败回滚重拉走 GetWheelMenu（重拉也不串账）', async () => {
    stubs()
    appSvc.SetWheelMenu.mockRejectedValue(new Error('轮盘配置写入失败'))
    const w = mount(TrayItemsEditor, { props: { scope: 'wheel' } })
    await flushPromises()
    await w.findAll('.option-item input')[0].setValue(true)
    await flushPromises()
    expect(useToast().toastMsg.value).toContain('保存轮盘菜单失败')
    expect(appSvc.GetWheelMenu.mock.calls.length).toBe(2)
    expect(appSvc.GetTrayMenu).not.toHaveBeenCalled()
    expect(w.emitted('saved')).toBeUndefined()
  })

  it('分组草稿门：名为空/零子条目时挂起自动保存并如实提示，补全后即时落盘', async () => {
    appSvc.ListTrayMenuOptions.mockResolvedValue([CMD1])
    appSvc.GetTrayMenu.mockResolvedValue([])
    appSvc.SetTrayMenu.mockResolvedValue(undefined)
    const w = mount(TrayItemsEditor)
    await flushPromises()

    await w.find('.tray-tools button').trigger('click') // 新建分组：空草稿不发必拒账
    expect(w.findAll('.tray-row-group')).toHaveLength(1)
    expect(w.find('.autosave-state').text()).toContain('等待补全')
    expect(appSvc.SetTrayMenu).not.toHaveBeenCalled()

    await w.find('.tray-row-group .tray-name').setValue('工具组')
    await w.find('.tray-row-group .tray-name').trigger('blur') // 有名无子：仍挂起
    expect(appSvc.SetTrayMenu).not.toHaveBeenCalled()
    expect(w.find('.autosave-state').text()).toContain('分组还没有子条目')

    await w.find('.tray-child-select').setValue('command|frpc/start')
    await w.findAll('.tray-child-add .btn')[0].trigger('click') // 补子条目：门开即落盘
    await flushPromises()
    expect(appSvc.SetTrayMenu).toHaveBeenCalledTimes(1)
    const payload = appSvc.SetTrayMenu.mock.calls[0][0]
    expect(payload[0]).toMatchObject({ type: 'group', label: '工具组' })
    expect(payload[0].children).toHaveLength(1)
    expect(payload[0].children[0]).toMatchObject({ type: 'command', ref: 'frpc/start' })
    expect(w.emitted('saved')).toHaveLength(1)
  })

  it('候选目录收口：route 退出供选、command 与网页应用命令在列；存量 route 行仍渲染并标不再供选', async () => {
    appSvc.ListTrayMenuOptions.mockResolvedValue([
      CMD2,
      { type: 'command', ref: 'webapp/open:abc', label: '打开网页窗', moduleName: 'webapp' },
      { type: 'route', ref: '/frpc', label: 'frpc 页面' },
    ])
    appSvc.GetTrayMenu.mockResolvedValue([{ ...LOGS }, { type: 'command', ref: 'gonavi/launch', path: '', args: '', label: '', enabled: true }])
    appSvc.SetTrayMenu.mockResolvedValue(undefined)
    const w = mount(TrayItemsEditor)
    await flushPromises()

    const labels = w.findAll('.option-item .option-label').map(n => n.text())
    expect(labels).toEqual(['启动 GoNavi', '打开网页窗']) // route 整族不在候选
    expect(w.findAll('.tray-row')).toHaveLength(2) // 存量 route 照常渲染，不腰斩不报错
    expect(w.findAll('.tray-row')[0].text()).toContain('不再供选')
    expect(w.findAll('.tray-row')[1].text()).not.toContain('不再供选')
  })
})

// N5-C3 点上格互换：宿主经 locate 身份定位选中，其它行出「⇄ 互换」；
// 实时保存批改造：互换即落盘全清单顺序对调（不再经保存钮中转）。
describe('点上格互换（N5-C3）', () => {
  it('locate 命中行高亮；他行现互换钮；互换后即时落盘全清单顺序对调', async () => {
    appSvc.ListTrayMenuOptions.mockResolvedValue([])
    appSvc.GetTrayMenu.mockResolvedValue([
      { type: 'route', ref: '/logs', path: '', args: '', label: '日志', enabled: true },
      { type: 'route', ref: '/about', path: '', args: '', label: '关于', enabled: true },
    ])
    appSvc.SetTrayMenu.mockResolvedValue(undefined)
    const w = mount(TrayItemsEditor)
    await flushPromises()

    expect(w.vm.locate({ type: 'route', hint: '/about', label: '' })).toBe(true)
    expect(w.vm.locate({ type: 'route', hint: '/nope', label: '' })).toBe(false)
    await flushPromises()
    const rows = w.findAll('.tray-row')
    expect(rows[1].classes()).toContain('tray-selected')
    // 未选中行（第 0 行）应现互换钮；选中行自身不出
    const swap = rows[0].findAll('button').find((b) => b.text().includes('互换'))!
    expect(rows[1].findAll('button').some((b) => b.text().includes('互换'))).toBe(false)
    await swap.trigger('click')
    await flushPromises()
    expect(appSvc.SetTrayMenu).toHaveBeenCalledTimes(1) // 互换即时落盘
    const payload = appSvc.SetTrayMenu.mock.calls[0][0]
    expect(payload.map((r: { ref: string }) => r.ref)).toEqual(['/about', '/logs'])
    w.unmount()
  })
})
