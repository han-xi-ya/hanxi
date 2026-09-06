// 特征测试：ElevateRestart——requireAdministrator 模块（BCU/Rufus/LiteMonitor）
// 被 740 直拒后的一键提权重启入口。锁定三态：已提权隐身、绑定异常隐身
// （宁缺毋滥）、正常态经 ConfirmDialog 确认后携带当前路由调 RestartElevated，
// 且 UAC 取消时回滚对话框并 toast 报错。
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { useToast } from '../../composables/useToast'

const appSvc = vi.hoisted(() => ({
  IsElevated: vi.fn(),
  RestartElevated: vi.fn(),
}))
vi.mock('../../../bindings/hanxi/internal/app', () => ({ AppService: appSvc }))

import ElevateRestart from '../ElevateRestart.vue'

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

  it('未提权 → 渲染按钮；点击先弹确认框而非直接重启', async () => {
    appSvc.IsElevated.mockResolvedValue(false)
    const w = factory()
    await flushPromises()
    expect(w.find('.elevate-restart').exists()).toBe(true)
    expect(document.querySelector('.workbench-confirm-backdrop')).toBeNull()
    await w.find('button').trigger('click')
    expect(document.querySelector('.workbench-confirm-backdrop')).not.toBeNull()
    // 确认框弹出本身不触发 RPC
    expect(appSvc.RestartElevated).not.toHaveBeenCalled()
  })

  it('确认 → RestartElevated 携带当前路由（重启后回航本页）', async () => {
    appSvc.IsElevated.mockResolvedValue(false)
    appSvc.RestartElevated.mockResolvedValue(undefined)
    const w = factory()
    await flushPromises()
    await w.find('button').trigger('click')
    ;(document.querySelector('.workbench-confirm-btn.primary') as HTMLButtonElement).click()
    await flushPromises()
    expect(appSvc.RestartElevated).toHaveBeenCalledWith('/ext/bcu')
  })

  it('UAC 取消（RPC 报错）→ 关闭确认框、回滚 busy 并 toast 失败原因', async () => {
    appSvc.IsElevated.mockResolvedValue(false)
    appSvc.RestartElevated.mockRejectedValue(new Error('已在 UAC 提示中取消，未执行提权重启'))
    const w = factory()
    await flushPromises()
    await w.find('button').trigger('click')
    ;(document.querySelector('.workbench-confirm-btn.primary') as HTMLButtonElement).click()
    await flushPromises()
    const { toastMsg } = useToast()
    expect(toastMsg.value).toContain('提权重启失败')
    expect(toastMsg.value).toContain('已在 UAC 提示中取消')
    expect(document.querySelector('.workbench-confirm-backdrop')).toBeNull()
  })
})
