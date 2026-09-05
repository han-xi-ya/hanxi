// 拆分护航（Phase 6）：TraceroutePanel——跳表渲染与 v-model:max-hops 的 number 语义。
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import TraceroutePanel from '../TraceroutePanel.vue'
import type { TracerouteSummary } from '../../../../bindings/hanxi/internal/modules/publicip/models'

const traceResult = {
  target: 'www.taobao.com',
  ip: '140.205.60.1',
  complete: true,
  hops: [
    { hop: 1, ip: '192.168.1.1', success: true, rttMs: 2 },
    { hop: 2, ip: '*', success: false, rttMs: 0 },
    { hop: 3, ip: '140.205.60.1', success: true, rttMs: 33 },
  ],
} as unknown as TracerouteSummary

function factory(props: Partial<{
  target: string
  maxHops: number
  loading: boolean
  result: TracerouteSummary | null
  error: string
}> = {}) {
  return mount(TraceroutePanel, {
    props: { target: '1.1.1.1', maxHops: 20, loading: false, result: traceResult, error: '', ...props },
    attachTo: document.body,
  })
}

describe('TraceroutePanel 表单', () => {
  it('目标与最大跳数经 update:* 上抛，跳数保持 number', async () => {
    const w = factory()
    await w.find('.text-input').setValue('www.taobao.com')
    await w.find('.select-input').setValue('30')
    await w.find('.btn-primary').trigger('click')
    expect(w.emitted('update:target')).toEqual([['www.taobao.com']])
    expect(w.emitted('update:maxHops')).toEqual([[30]])
    expect(w.emitted('run')).toHaveLength(1)
    w.unmount()
  })

  it('加载中禁用并改文案；常用目标先回填再发起', async () => {
    const busy = factory({ loading: true })
    expect(busy.find('.tool-panel .btn-primary').text()).toBe('正在追踪路由节点…')
    busy.unmount()

    const w = factory({ target: '' })
    expect(w.find('.tool-panel .btn-primary').attributes('disabled')).toBeDefined()
    await w.findAll('.btn-quick')[1].trigger('click')
    expect(w.emitted('update:target')).toEqual([['1.1.1.1']])
    expect(w.emitted('run')).toHaveLength(1)
    w.unmount()
  })
})

describe('TraceroutePanel 结果卡', () => {
  it('汇总栏三列 + * 跳兜底文案 + 最终目标徽章', () => {
    const w = factory()
    expect(w.findAll('.summary-col').map(c => c.text())).toEqual([
      '追踪目标www.taobao.com (140.205.60.1)',
      '总跳数3 跳',
      '追踪状态已到达目标主机',
    ])
    const rows = w.findAll('.tbl tbody tr')
    expect(rows).toHaveLength(3)
    expect(rows[0].find('.node-ip').text()).toBe('192.168.1.1')
    expect(rows[0].find('.status-badge').classes()).toContain('ok')
    expect(rows[1].find('.text-subtle').text()).toContain('节点不响应 ICMP')
    expect(rows[1].find('.status-badge').classes()).toContain('timeout')
    expect(rows[2].find('.status-badge').classes()).toContain('target')
    w.unmount()
  })

  it('未完全响应走 text-warn；hops 为 null 时总跳数落 0', () => {
    const w = factory({ result: { ...traceResult, complete: false, hops: null } as TracerouteSummary })
    expect(w.findAll('.summary-col')[2].find('.s-val').classes()).toContain('text-warn')
    expect(w.findAll('.summary-col')[1].text()).toBe('总跳数0 跳')
    expect(w.find('.table-container').exists()).toBe(true)
    expect(w.findAll('.tbl tbody tr')).toHaveLength(0)
    w.unmount()
  })
})
