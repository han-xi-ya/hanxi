// 特征测试：ElevateRestart——requireAdministrator 模块（BCU/Rufus/LiteMonitor）
// 被 740 直拒后的一键提权重启入口。锁定三态：已提权隐身、绑定异常隐身
// （宁缺毋滥）、正常态经确认（全局 useConfirm 单例，收编后不再自挂弹层）后
// 携带当前路由调 RestartElevated，且 UAC 取消时 toast 报错可重新发起。
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { useToast } from '../../composables/useToast'
import { useConfirm } from '../../composables/useConfirm'

const appSvc = vi.hoisted(() => ({
  IsElevated: vi.fn(),
  RestartElevated: vi.fn(),
}))
vi.mock('../../../bindings/hanxi/internal/app', () => ({ AppService: appSvc }))

import ElevateRestart from '../ElevateRestart.vue'

const { confirmState, settleConfirm } = useConfirm()

function factory() {
  return mount(ElevateRestart, {
    props: { route: '/ext/bcu' },
    attachTo: document.body,
    global: { stubs: { teleport: true } },
  })
}

describe('ElevateRestart', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    useToast().clearToast()
    settleConfirm(false) // 兜底落定上个用例可能悬挂的确认请求
    document.body.innerHTML = '' // 清掉上个用例 attachTo body 的残留弹层，防跨用例误查
  })

  it('已提权运行 → 整个入口隐身（用户本来就管理员启动，无需按钮）', async () => {
    appSvc.IsElevated.mockResolvedValue(true)
    const w = factory()
    await flushPromises()
    expect(w.find('.elevate-restart').exists()).toBe(false)
  })

  it('IsElevated 查询异常 → 隐身兜底（宁缺毋滥，不给无效入口）', async () => {
    appSvc.IsElevated.mockRejectedValue(new Error('绑定未就绪'))
    const w = factory()
    await flushPromises()
    expect(w.find('.elevate-restart').exists()).toBe(false)
  })

  it('未提权 → 渲染按钮；点击只经全局 useConfirm 挂起确认，不自挂弹层、不触发 RPC', async () => {
    appSvc.IsElevated.mockResolvedValue(false)
    const w = factory()
    await flushPromises()
    expect(w.find('.elevate-restart').exists()).toBe(true)
    // 收编契约：组件内部不得再渲染 ConfirmDialog（KeepAlive 多视图同挂时无叠窗）
    expect(w.find('.workbench-confirm-backdrop').exists()).toBe(false)
    await w.find('button').trigger('click')
    expect(document.querySelector('.workbench-confirm-backdrop')).toBeNull()
    expect(confirmState.open).toBe(true)
    expect(confirmState.options.title).toBe('以管理员身份重启 Hanxi')
    expect(confirmState.options.confirmLabel).toBe('提权重启')
    expect(confirmState.options.tone).toBe('warning')
    expect(appSvc.RestartElevated).not.toHaveBeenCalled()
    settleConfirm(false)
    await flushPromises()
    expect(appSvc.RestartElevated).not.toHaveBeenCalled()
  })

  it('确认 → RestartElevated 携带当前路由（重启后回航本页）', async () => {
    appSvc.IsElevated.mockResolvedValue(false)
    appSvc.RestartElevated.mockResolvedValue(undefined)
    const w = factory()
    await flushPromises()
    await w.find('button').trigger('click')
    settleConfirm(true)
    await flushPromises()
    expect(appSvc.RestartElevated).toHaveBeenCalledWith('/ext/bcu')
    expect(useToast().toastMsg.value).toContain('正在以管理员身份重启')
  })

  it('UAC 取消（RPC 报错）→ 关闭确认态并 toast 失败原因，可重新发起', async () => {
    appSvc.IsElevated.mockResolvedValue(false)
    appSvc.RestartElevated.mockRejectedValue(new Error('已在 UAC 提示中取消，未执行提权重启'))
    const w = factory()
    await flushPromises()
    await w.find('button').trigger('click')
    settleConfirm(true)
    await flushPromises()
    const { toastMsg } = useToast()
    expect(toastMsg.value).toContain('提权重启失败')
    expect(toastMsg.value).toContain('已在 UAC 提示中取消')
    expect(confirmState.open).toBe(false)
    // 失败后可再次点击重走确认流
    await w.find('button').trigger('click')
    expect(confirmState.open).toBe(true)
    settleConfirm(false)
    await flushPromises()
  })
})
