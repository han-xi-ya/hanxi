// EverythingConsoleBar 聚焦测试：锁拆分后控制条的展示态与事件上抛
// （ES 组件就绪三态与安装按钮显隐、运行态徽标、外部实例下按钮禁用矩阵）。
// 完整行为基线（含 keyword 同帧回写时序）仍由 views/__tests__/EverythingView.spec.ts 覆盖。
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import EverythingConsoleBar from '../EverythingConsoleBar.vue'

const esTicket = { component: 'es', version: '', stage: 'downloading', done: 0, total: 0, message: '' }

function mountBar(overrides: Record<string, unknown> = {}) {
  return mount(EverythingConsoleBar, {
    props: {
      state: 'stopped',
      stateText: '未运行',
      busy: false,
      runningVersion: '',
      uptimeSec: 0,
      searching: false,
      esReady: false,
      esBusy: false,
      esProgress: null,
      keyword: '',
      ...overrides,
    },
  })
}

describe('EverythingConsoleBar ES 组件态', () => {
  it('未就绪且无进度：显示安装入口；esBusy 时禁用；点击上抛 install-es', async () => {
    const w = mountBar({ esBusy: true })
    expect(w.find('.ev-status-pill').text()).toBe('ES 未装')
    const install = w.find('.search-line .link-button')
    expect(install.exists()).toBe(true)
    expect(install.attributes('disabled')).toBeDefined()
    await w.setProps({ esBusy: false })
    await w.find('.search-line .link-button').trigger('click')
    expect(w.emitted('install-es')).toHaveLength(1)
  })

  it('下载中显示进度文案并隐藏安装入口；就绪后显示 ES 就绪', async () => {
    const w = mountBar({ esProgress: esTicket })
    expect(w.find('.ev-status-pill').text()).toBe('组件安装中…')
    expect(w.find('.search-line .link-button').exists()).toBe(false)
    await w.setProps({ esProgress: null, esReady: true })
    expect(w.find('.ev-status-pill').text()).toBe('ES 就绪')
    expect(w.find('.search-line .link-button').exists()).toBe(false)
  })
})

describe('EverythingConsoleBar 状态徽标与按钮矩阵', () => {
  it('running + 版本：状态灯类名跟随 state，展示 ver-pill / PID / 时长', () => {
    const w = mountBar({ state: 'running', stateText: '后台运行中', runningVersion: '1.5.0.1371', pid: 4321, uptimeSec: 75 })
    expect(w.find('.ev-status-light').classes()).toContain('running')
    expect(w.find('.status-word').text()).toBe('后台运行中')
    expect(w.find('.ver-pill').text()).toBe('1.5.0.1371')
    expect(w.find('.pid-tag').text()).toBe('PID 4321')
    expect(w.find('.uptime-tag').text()).toBe('⏱ 01:15')
  })

  it('external：启动禁用且 title 提示外部实例；退出可用且 title 指向托盘', () => {
    const w = mountBar({ state: 'external', stateText: '外部运行' })
    const btns = w.findAll('.control-btns .btn')
    expect(btns[0].attributes('disabled')).toBeDefined()
    expect(btns[0].attributes('title')).toBe('外部实例已在运行')
    expect(btns[2].attributes('disabled')).toBeUndefined()
    expect(btns[2].attributes('title')).toBe('外部实例请在 Everything 托盘退出')
  })

  it('启停点击各自上抛对应事件（禁用态点击不发射）', async () => {
    const w = mountBar() // stopped：启动/开窗可用，退出禁用
    const btns = w.findAll('.control-btns .btn')
    await btns[0].trigger('click')
    await btns[1].trigger('click')
    await btns[2].trigger('click')
    expect(w.emitted('start-background')).toHaveLength(1)
    expect(w.emitted('open-window')).toHaveLength(1)
    expect(w.emitted('quit')).toBeUndefined()
    w.unmount()
    const r = mountBar({ state: 'running', stateText: '窗口运行中' })
    await r.findAll('.control-btns .btn')[2].trigger('click')
    expect(r.emitted('quit')).toHaveLength(1)
    r.unmount()
  })
})
