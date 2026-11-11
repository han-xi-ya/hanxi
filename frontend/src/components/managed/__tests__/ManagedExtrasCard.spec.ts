// ManagedExtrasCard 契约测试：联动与辅助设置卡的条目"缺项即缺席"与两条布尔开关
// （随关 / 数据模式）的同一执行纪律——乐观翻转、后端回执原样弹出、失败回滚真实值。
// 2026-11-11 随 Termora「数据模式」第二开关接入新增。
import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import ManagedExtrasCard from '../ManagedExtrasCard.vue'
import type { ManagedModuleAdapter } from '../adapter'
import { useToast } from '../../../composables/useToast'

function cardWith(extras: Record<string, unknown>) {
  return mount(ManagedExtrasCard, {
    props: { adapter: { extras } as unknown as ManagedModuleAdapter },
  })
}

const follow = () => ({
  get: vi.fn().mockResolvedValue(false),
  set: vi.fn().mockResolvedValue({ message: '随关已改' }),
})

const dataMode = (over: Partial<{ get: () => Promise<boolean>; set: (v: boolean) => Promise<unknown> }> = {}) => ({
  label: '数据随版本独立（默认共享 ~/.termora）',
  note: '（共享=与自装 Termora 同一份）',
  get: over.get ?? vi.fn().mockResolvedValue(false),
  set: over.set ?? vi.fn().mockResolvedValue({ message: '模式已切换（下次启动生效）' }),
})

afterEach(() => useToast().clearToast())

describe('ManagedExtrasCard', () => {
  it('空 extras 不渲染任何开关行（既有模块零变化）', async () => {
    const w = cardWith({})
    await w.vm.$nextTick()
    expect(w.findAll('input[type="checkbox"]')).toHaveLength(0)
    expect(w.find('.toggle-label').exists()).toBe(false)
  })

  it('两条开关各自回显：随关 false + 数据模式 true', async () => {
    const f = follow()
    const m = dataMode({ get: vi.fn().mockResolvedValue(true) })
    const w = cardWith({ followOnExit: f, dataMode: m })
    // 两行都在渲染前恒为 false（异步回显），故等值不等形：断言"回显到了 true"本身。
    await vi.waitFor(() =>
      expect((w.findAll('input[type="checkbox"]')[1].element as HTMLInputElement).checked).toBe(true))
    expect(w.findAll('input[type="checkbox"]')).toHaveLength(2)
    const boxes = w.findAll('input[type="checkbox"]')
    expect((boxes[0].element as HTMLInputElement).checked).toBe(false)
    expect((boxes[1].element as HTMLInputElement).checked).toBe(true)
    expect(w.text()).toContain('数据随版本独立（默认共享 ~/.termora）')
    expect(w.text()).toContain('（共享=与自装 Termora 同一份）')
  })

  it('点数据模式：只调自己的 set(true) 并弹出后端回执；随关键位不受牵连', async () => {
    const f = follow()
    const m = dataMode()
    const w = cardWith({ followOnExit: f, dataMode: m })
    await vi.waitFor(() => expect(w.findAll('input[type="checkbox"]')).toHaveLength(2))
    await w.findAll('input[type="checkbox"]')[1].setValue(true)
    await vi.waitFor(() => expect(useToast().toastMsg.value).toBe('模式已切换（下次启动生效）'))
    expect(m.set).toHaveBeenCalledWith(true)
    expect(f.set).not.toHaveBeenCalled()
  })

  it('数据模式 set 失败：勾选回滚到后端真实值并 toast 错误，不留假态', async () => {
    const m = dataMode({ set: vi.fn().mockRejectedValue(new Error('磁盘只读')) })
    const w = cardWith({ dataMode: m })
    await vi.waitFor(() => expect(w.findAll('input[type="checkbox"]')).toHaveLength(1))
    const box = w.find('input[type="checkbox"]')
    await box.setValue(true)
    await vi.waitFor(() => expect(useToast().toastMsg.value).toContain('磁盘只读'))
    expect((box.element as HTMLInputElement).checked).toBe(false)
  })

  it('dataDir 钮：存在即渲染，点击直达后端 open', async () => {
    const open = vi.fn().mockResolvedValue({})
    const w = cardWith({ dataDir: { label: '🗂 数据目录', open } })
    await vi.waitFor(() => expect(w.findAll('button').some((b) => b.text().includes('数据目录'))).toBe(true))
    await w.findAll('button').find((b) => b.text().includes('数据目录'))!.trigger('click')
    await vi.waitFor(() => expect(open).toHaveBeenCalledTimes(1))
  })
})
