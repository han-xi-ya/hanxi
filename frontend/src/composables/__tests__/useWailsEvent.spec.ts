// useWailsEvent：订阅解包 + 自动清理 + 作用域外降级告警。
// 测试 seam 与 MarkerOnView.spec 同一约定：@wailsio/runtime 整体桩掉。
import { defineComponent } from 'vue'
import { mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const runtime = vi.hoisted(() => ({
  handlers: {} as Record<string, (event: { data?: unknown }) => void>,
  unlisten: vi.fn(),
}))

vi.mock('@wailsio/runtime', () => ({
  Events: {
    On: (name: string, cb: (event: { data?: unknown }) => void) => {
      runtime.handlers[name] = cb
      return runtime.unlisten
    },
  },
}))

import { useWailsEvent } from '../useWailsEvent'

beforeEach(() => {
  for (const key of Object.keys(runtime.handlers)) delete runtime.handlers[key]
  runtime.unlisten.mockReset()
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('useWailsEvent', () => {
  it('setup 期注册事件，payload 解包 { data } 后交给 handler', () => {
    const received: number[] = []
    const Comp = defineComponent({
      setup() {
        useWailsEvent<number>('evt:test', (data) => received.push(data))
        return () => null
      },
    })
    const wrapper = mount(Comp)
    expect(runtime.handlers['evt:test']).toBeTypeOf('function')

    runtime.handlers['evt:test']({ data: 42 })
    expect(received).toEqual([42])
    wrapper.unmount()
  })

  it('组件卸载时自动 unlisten', () => {
    const Comp = defineComponent({
      setup() {
        useWailsEvent<string>('evt:cleanup', () => {})
        return () => null
      },
    })
    const wrapper = mount(Comp)
    wrapper.unmount()
    expect(runtime.unlisten).toHaveBeenCalledTimes(1)
  })

  it('返回手工 unlisten；作用域外调用告警一次且不自建清理', () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})
    const un = useWailsEvent<number>('evt:manual', () => {})
    expect(un).toBe(runtime.unlisten)
    expect(warn).toHaveBeenCalledTimes(1)
    expect(warn.mock.calls[0][0]).toContain('evt:manual')
    un()
    expect(runtime.unlisten).toHaveBeenCalledTimes(1)
  })
})
