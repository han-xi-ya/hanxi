import { defineComponent, h } from 'vue'
import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { DONE_TICK_RETIRE_MS, useDoneTicketRetire } from '../useDoneTicketRetire'

/** setup 期内建调度器并挂载宿主（track 形制的 onUnmounted 钩子须组件实例上下文）。 */
function mountScheduler(options?: Parameters<typeof useDoneTicketRetire>[0]) {
  let schedule!: ReturnType<typeof useDoneTicketRetire>
  const Host = defineComponent({
    setup() {
      schedule = useDoneTicketRetire(options)
      return () => h('div')
    },
  })
  const wrapper = mount(Host)
  return { schedule, wrapper }
}

afterEach(() => {
  vi.useRealTimers()
})

describe('useDoneTicketRetire 缺省形制（store/Frpc/RustDesk/vscode 现值锚定）', () => {
  it('驻留 800ms 后摘除；799ms 边界仍在场', async () => {
    vi.useFakeTimers()
    const { schedule, wrapper } = mountScheduler()
    const retire = vi.fn()

    schedule(retire)
    await vi.advanceTimersByTimeAsync(DONE_TICK_RETIRE_MS - 1)
    expect(retire).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(1)
    expect(retire).toHaveBeenCalledTimes(1)

    wrapper.unmount()
  })

  it('ms 形参生效：自定义驻留时长按新值摘除', async () => {
    vi.useFakeTimers()
    const { schedule, wrapper } = mountScheduler({ ms: 300 })
    const retire = vi.fn()

    schedule(retire)
    await vi.advanceTimersByTimeAsync(299)
    expect(retire).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(1)
    expect(retire).toHaveBeenCalledTimes(1)

    wrapper.unmount()
  })

  it('不登记不复核：驻留窗口内新票据取代旧 done，陈旧计时器照摘（迁移前手抄形制原样锚定）', async () => {
    vi.useFakeTimers()
    const { schedule, wrapper } = mountScheduler()
    // 票据表模拟：done 后 400ms 同版本被在飞新票取代；缺省形制无 guard，
    // 陈旧计时器 800ms 到点仍执行摘除——store/Frpc/RustDesk 的历史洞，
    // M4 零变化铁律下不擅补，此例即该现形制的锚。
    const map: Record<string, { stage: string }> = { v1: { stage: 'done' } }
    schedule(() => {
      delete map.v1
    })
    await vi.advanceTimersByTimeAsync(400)
    map.v1 = { stage: 'downloading' } // 新票据到达
    await vi.advanceTimersByTimeAsync(DONE_TICK_RETIRE_MS - 400 + 1)
    expect(map.v1).toBeUndefined()

    wrapper.unmount()
  })

  it('卸载不拦截迟到摘除：timer 一次性自灭，到点仍兑现（store/Frpc/RustDesk 现形制）', async () => {
    vi.useFakeTimers()
    const { schedule, wrapper } = mountScheduler()
    const retire = vi.fn()

    schedule(retire)
    await vi.advanceTimersByTimeAsync(400)
    wrapper.unmount()
    await vi.advanceTimersByTimeAsync(DONE_TICK_RETIRE_MS)
    expect(retire).toHaveBeenCalledTimes(1)
  })
})

describe('useDoneTicketRetire track 形制（Douzy/SubnetDesk 现值锚定）', () => {
  it('卸载清尽：驻留未至即 unmount，摘除永不兑现', async () => {
    vi.useFakeTimers()
    const { schedule, wrapper } = mountScheduler({ track: true })
    const retire = vi.fn()

    schedule(retire)
    await vi.advanceTimersByTimeAsync(400)
    wrapper.unmount()
    await vi.advanceTimersByTimeAsync(DONE_TICK_RETIRE_MS * 10)
    expect(retire).not.toHaveBeenCalled()
  })

  it('多笔驻留并存互不干扰；到点自销号，卸载清尽未达期的那一笔', async () => {
    vi.useFakeTimers()
    const { schedule, wrapper } = mountScheduler({ track: true })
    const first = vi.fn()
    schedule(first)
    await vi.advanceTimersByTimeAsync(400)

    const second = vi.fn()
    schedule(second) // 同版本新 done 再驻留：旧计时器不撤（五处现形制皆如此），并存
    await vi.advanceTimersByTimeAsync(400) // t=800：第一笔到点摘除
    expect(first).toHaveBeenCalledTimes(1)

    wrapper.unmount() // t=800：第二笔尚余 400ms，卸载清尽
    await vi.advanceTimersByTimeAsync(DONE_TICK_RETIRE_MS * 2)
    expect(second).not.toHaveBeenCalled()
  })
})

describe('useDoneTicketRetire guard 形制（SubnetDesk 现值锚定）', () => {
  it('驻留窗口内新票据取代旧 done：陈旧计时器作废，不误删在飞票', async () => {
    vi.useFakeTimers()
    const { schedule, wrapper } = mountScheduler({ track: true })
    const map: Record<string, { stage: string }> = { v1: { stage: 'done' } }
    const retire = vi.fn(() => {
      delete map.v1
    })

    schedule(retire, () => map.v1?.stage === 'done')
    await vi.advanceTimersByTimeAsync(400)
    map.v1 = { stage: 'downloading' } // 新票据取代：本轮驻留作废
    await vi.advanceTimersByTimeAsync(DONE_TICK_RETIRE_MS - 400 + 1)
    expect(retire).not.toHaveBeenCalled()
    expect(map.v1).toEqual({ stage: 'downloading' })

    wrapper.unmount()
  })

  it('摘除时刻票据仍是当前 done：guard 放行，只摘该票不碰其他在途票', async () => {
    vi.useFakeTimers()
    const { schedule, wrapper } = mountScheduler({ track: true })
    const map: Record<string, { stage: string }> = {
      v1: { stage: 'done' },
      v2: { stage: 'downloading' },
    }

    schedule(
      () => {
        delete map.v1
      },
      () => map.v1?.stage === 'done',
    )
    await vi.advanceTimersByTimeAsync(DONE_TICK_RETIRE_MS + 1)
    expect(map.v1).toBeUndefined()
    expect(map.v2).toEqual({ stage: 'downloading' })

    wrapper.unmount()
  })
})
