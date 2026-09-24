// N5-C1 共享编辑面特征测试：挂载拉取候选与现有条目 → 勾选添加置脏 → 保存调用
// SetTrayMenu 全清单并上抛 saved；保存失败 toast 中文错误并回滚重拉。
// 分组/外部程序等完整交互矩阵仍由 SettingsSections.spec 经 TraySection 宿主覆盖。
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import TrayItemsEditor from '../TrayItemsEditor.vue'
import { useToast } from '../../../composables/useToast'

const appSvc = vi.hoisted(() => ({
  ListTrayMenuOptions: vi.fn(),
  GetTrayMenu: vi.fn(),
  SetTrayMenu: vi.fn(),
  PickExeFile: vi.fn(),
}))
vi.mock('../../../../bindings/hanxi/internal/app', () => ({ AppService: appSvc }))

function stubs() {
  appSvc.ListTrayMenuOptions.mockResolvedValue([{ type: 'command', ref: 'frpc/start', label: '启动 frpc', moduleName: 'frpc' }])
  appSvc.GetTrayMenu.mockResolvedValue([{ type: 'route', ref: '/logs', path: '', args: '', label: '日志', enabled: true }])
  appSvc.SetTrayMenu.mockResolvedValue(undefined)
}

async function mountReady() {
  stubs()
  const w = mount(TrayItemsEditor)
  await flushPromises()
  return w
}

afterEach(() => {
  vi.clearAllMocks()
  useToast().clearToast()
})

describe('TrayItemsEditor', () => {
  it('挂载即拉候选与现有条目；非脏时保存钮禁用', async () => {
    const w = await mountReady()
    expect(appSvc.ListTrayMenuOptions).toHaveBeenCalledTimes(1)
    expect(appSvc.GetTrayMenu).toHaveBeenCalledTimes(1)
    expect(w.findAll('.tray-row')).toHaveLength(1)
    expect((w.find('.tray-name').element as HTMLInputElement).value).toBe('日志')
    expect(w.find('.tray-footer .btn-primary').attributes('disabled')).toBeDefined()
  })

  it('勾选添加→脏→保存调用 SetTrayMenu 全清单，成功后复位脏并上抛 saved', async () => {
    const w = await mountReady()
    await w.findAll('.option-item input')[0].setValue(true)
    expect(w.findAll('.tray-row')).toHaveLength(2)
    expect(w.find('.tray-footer .btn-primary').attributes('disabled')).toBeUndefined()
    await w.find('.tray-footer .btn-primary').trigger('click')
    await flushPromises()
    expect(appSvc.SetTrayMenu).toHaveBeenCalledTimes(1)
    const saved = appSvc.SetTrayMenu.mock.calls[0][0]
    expect(saved).toHaveLength(2)
    expect(saved[1]).toMatchObject({ type: 'command', ref: 'frpc/start', enabled: true })
    expect(useToast().toastMsg.value).toBe('托盘右键菜单已保存并即时生效')
    expect(w.emitted('saved')).toHaveLength(1)
    // 保存成功后脏复位，保存钮回到禁用
    expect(w.find('.tray-footer .btn-primary').attributes('disabled')).toBeDefined()
  })

  it('保存失败：toast 呈现中文错误并回滚重拉，不上抛 saved', async () => {
    stubs()
    appSvc.SetTrayMenu.mockRejectedValue(new Error('配置写入失败'))
    const w = mount(TrayItemsEditor)
    await flushPromises()
    await w.findAll('.option-item input')[0].setValue(true)
    await w.find('.tray-footer .btn-primary').trigger('click')
    await flushPromises()
    expect(useToast().toastMsg.value).toContain('配置写入失败')
    expect(appSvc.GetTrayMenu.mock.calls.length).toBe(2) // 失败回滚重拉
    expect(w.emitted('saved')).toBeUndefined()
  })
})
