// usePrompt 单例契约：提交回字符串（含空串），取消/Esc 回 null，并发不悬挂。
import { describe, expect, it } from 'vitest'
import { usePrompt } from '../usePrompt'

const { prompt, promptState, settlePrompt } = usePrompt()

describe('usePrompt', () => {
  it('打开并合并默认按钮文案，提交返回输入值', async () => {
    const p = prompt({ title: '设备备注', initialValue: 'NAS' })
    expect(promptState.open).toBe(true)
    expect(promptState.options.confirmLabel).toBe('确认')
    settlePrompt('家用存储')
    await expect(p).resolves.toBe('家用存储')
    expect(promptState.open).toBe(false)
  })

  it('空串是合法提交值（清空语义交给调用方判断）', async () => {
    const p = prompt({ title: '备注' })
    settlePrompt('')
    await expect(p).resolves.toBe('')
  })

  it('取消返回 null；并发时旧请求先落定 null 不悬挂', async () => {
    const first = prompt({ title: '旧' })
    const second = prompt({ title: '新' })
    await expect(first).resolves.toBe(null)
    settlePrompt(null)
    await expect(second).resolves.toBe(null)
  })
})
