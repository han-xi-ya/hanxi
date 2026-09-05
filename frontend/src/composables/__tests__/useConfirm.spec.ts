// useConfirm 单例契约：Promise 化裁决、默认值合并、并发防御、settle 幂等。
import { describe, expect, it } from 'vitest'
import { useConfirm } from '../useConfirm'

const { confirm, confirmState, settleConfirm } = useConfirm()

describe('useConfirm', () => {
  it('confirm() 打开对话框并合并默认文案', async () => {
    const p = confirm({ title: '卸载', description: '不可恢复' })
    expect(confirmState.open).toBe(true)
    expect(confirmState.options.confirmLabel).toBe('确认')
    expect(confirmState.options.tone).toBe('default')
    settleConfirm(true)
    await expect(p).resolves.toBe(true)
    expect(confirmState.open).toBe(false)
  })

  it('settle(false) 裁决为取消', async () => {
    const p = confirm({ title: 'a', description: 'b' })
    settleConfirm(false)
    await expect(p).resolves.toBe(false)
  })

  it('并发防御：前一个未决请求按取消落定，不悬挂', async () => {
    const first = confirm({ title: '旧', description: '' })
    const second = confirm({ title: '新', description: '' })
    await expect(first).resolves.toBe(false)
    expect(confirmState.options.title).toBe('新')
    settleConfirm(true)
    await expect(second).resolves.toBe(true)
  })

  it('未打开时 settle 是安全的空操作（cancel 与 update:open 双回调不双落定）', async () => {
    const p = confirm({ title: 'x', description: '' })
    settleConfirm(true)
    settleConfirm(false) // 宿主若双触发，结果不得被改写
    await expect(p).resolves.toBe(true)
  })
})
