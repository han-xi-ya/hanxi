// useAsyncAction：busy 单飞、成功/失败双通道、重入拒绝。
import { describe, expect, it, vi } from 'vitest'
import { useAsyncAction } from '../useAsyncAction'

function deferred<T>() {
  let resolve!: (v: T) => void
  let reject!: (e: unknown) => void
  const promise = new Promise<T>((res, rej) => {
    resolve = res
    reject = rej
  })
  return { promise, resolve, reject }
}

describe('useAsyncAction', () => {
  it('成功路径：busy 全程为 true，resolve 后返回 ok+data 并复位', async () => {
    const { busy, run } = useAsyncAction()
    const d = deferred<string>()
    const p = run(() => d.promise)
    expect(busy.value).toBe(true)
    d.resolve('done')
    expect(await p).toEqual({ ok: true, data: 'done' })
    expect(busy.value).toBe(false)
  })

  it('失败路径：原样携带 error，不吞错且复位 busy', async () => {
    const { busy, run } = useAsyncAction()
    const boom = new Error('后端拒绝')
    const res = await run(() => Promise.reject(boom))
    expect(res).toEqual({ ok: false, error: boom })
    expect(busy.value).toBe(false)
  })

  it('重入：busy 期间第二次 run 不执行 fn，直接返回「操作进行中」', async () => {
    const { run } = useAsyncAction()
    const d = deferred<number>()
    const second = vi.fn(async () => 1)
    const first = run(() => d.promise)
    const rejected = await run(second)
    expect(rejected.ok).toBe(false)
    expect((rejected as { error: Error }).error.message).toBe('操作进行中')
    expect(second).not.toHaveBeenCalled()
    d.resolve(7)
    expect(await first).toEqual({ ok: true, data: 7 })
  })

  it('前一动作 settle 后可再次 run（busy 不粘滞）', async () => {
    const { run } = useAsyncAction()
    expect(await run(async () => 'a')).toEqual({ ok: true, data: 'a' })
    expect(await run(async () => 'b')).toEqual({ ok: true, data: 'b' })
  })
})
