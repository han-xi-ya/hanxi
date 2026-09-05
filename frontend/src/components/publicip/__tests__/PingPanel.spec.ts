// 拆分护航（Phase 6）：PingPanel——v-model 双向链路与「先回填目标、再发起」的执行序。
// 该顺序是跨 Tab 快捷联动的命门：宿主在 run 时必须已读到新目标（emit 同步送达）。
import { defineComponent, h, ref } from 'vue'
import { mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it } from 'vitest'
import PingPanel from '../PingPanel.vue'
import type { PingSummary } from '../../../../bindings/hanxi/internal/modules/publicip/models'

const result = {
  target: '1.1.1.1',
  ip: '1.1.1.1',
  sent: 4,
  received: 3,
  lossRate: 25,
  minRtt: 10.2,
  avgRtt: 20.5,
  maxRtt: 40.8,
  results: [
    { seq: 1, ip: '1.1.1.1', success: true, rttMs: 10.2, ttl: 56, errorMsg: '' },
    { seq: 2, ip: '1.1.1.1', success: false, rttMs: 0, ttl: 0, errorMsg: '' },
  ],
} as unknown as PingSummary

function factory(props: Partial<{
  target: string
  count: number
  loading: boolean
  result: PingSummary | null
  error: string
}> = {}) {
  return mount(PingPanel, {
    props: { target: '1.1.1.1', count: 4, loading: false, result: null, error: '', ...props },
    attachTo: document.body,
  })
}

/** v-model 宿主：记录 run 时看到的值，用于锁定「先写后跑」顺序。 */
const seq: string[] = []

const Host = defineComponent({
  setup() {
    const target = ref('1.1.1.1')
    const count = ref(4)
    return () =>
      h(PingPanel, {
        target: target.value,
        count: count.value,
        loading: false,
        result: null,
        error: '',
        'onUpdate:target': (value: string) => {
          target.value = value
          seq.push(`target:${value}`)
        },
        'onUpdate:count': (value: number) => {
          count.value = value
          seq.push(`count:${value}`)
        },
        onRun: () => seq.push(`run:${target.value}/${count.value}`),
      })
  },
})

beforeEach(() => {
  seq.length = 0
})

describe('PingPanel 表单', () => {
  it('输入与次数下拉经 update:* 上抛（次数保持 number 语义）', async () => {
    const w = factory()
    await w.find('.text-input').setValue('8.8.8.8')
    await w.find('.select-input').setValue('16')
    expect(w.emitted('update:target')).toEqual([['8.8.8.8']])
    expect(w.emitted('update:count')).toEqual([[16]])
    w.unmount()
  })

  it('空目标与加载中均禁用发起按钮，文案随加载态切换', async () => {
    const blank = factory({ target: '  ' })
    expect(blank.find('.tool-panel .btn-primary').attributes('disabled')).toBeDefined()
    blank.unmount()

    const busy = factory({ loading: true })
    expect(busy.find('.tool-panel .btn-primary').attributes('disabled')).toBeDefined()
    expect(busy.find('.tool-panel .btn-primary').text()).toBe('Ping 探测中…')
    busy.unmount()
  })

  it('常用目标：宿主在 run 时已看到新目标；Enter 键直接发起', async () => {
    const w = mount(Host, { attachTo: document.body })
    await w.findAll('.btn-quick')[3].trigger('click')
    await w.find('.text-input').setValue('223.5.5.5')
    await w.find('.text-input').trigger('keyup', { key: 'Enter' })
    expect(seq).toEqual([
      'target:8.8.8.8',
      'run:8.8.8.8/4',
      'target:223.5.5.5',
      'run:223.5.5.5/4',
    ])
    w.unmount()
  })
})

describe('PingPanel 结果卡', () => {
  it('有结果：汇总栏、丢包告警色与逐行 RTT/状态徽章', () => {
    const w = factory({ result })
    const cols = w.findAll('.summary-col').map(c => c.text())
    expect(cols).toEqual([
      '目标主机1.1.1.1',
      '发包 / 收包4 / 3',
      '丢包率25.0%',
      '延迟 (最小/平均/最大)10.2 / 20.5 / 40.8 ms',
    ])
    expect(w.find('.val-warn').exists()).toBe(true)
    const rows = w.findAll('.tbl tbody tr')
    expect(rows[0].find('.rtt-tag').classes()).toContain('fast')
    expect(rows[1].find('.status-badge').classes()).toContain('fail')
    expect(rows[1].text()).toContain('请求超时')
    w.unmount()
  })

  it('零丢包无告警色、收包为 0 时不出延迟列；无结果不落卡片', () => {
    const w = factory({ result: { ...result, received: 0, lossRate: 0 } as PingSummary })
    expect(w.findAll('.summary-col')).toHaveLength(3)
    expect(w.find('.val-warn').exists()).toBe(false)
    w.unmount()

    const idle = factory({ result: null })
    expect(idle.find('.diag-card').exists()).toBe(false)
    idle.unmount()

    const failed = factory({ error: 'Ping 执行失败: 权限不足' })
    expect(failed.find('.error-box').text()).toBe('Ping 执行失败: 权限不足')
    failed.unmount()
  })
})
