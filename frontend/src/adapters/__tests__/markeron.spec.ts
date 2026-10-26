// ============================================================================
// MarkerOn adapter · annotateToggleView 六态矩阵契约
//
// 该纯函数是 MarkerOnView 主钮（#primary-action 槽自绘）label/sub/variant/
// disabled/hint/title 的唯一投影源：钮面渲染 = 本函数输出，一字之差即 UI 漂移。
// 断言取用户可见事实：标注进行中文案、外部无信使时的快捷键指引、启动中禁用、
// busy 双闸合并（input.busy ∨ annotateBusy）——后者正是「处理中…判定不拆两处」
// 的注释承诺所在。
// ============================================================================

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('../../../bindings/hanxi/internal/modules/markeron/markeronservice', () => ({
  ToggleAnnotate: vi.fn(),
  StopAnnotate: vi.fn(),
  GetStatus: vi.fn(),
}))
// 模块顶层 import 会真实装载 Wails 运行时与绑定——照例 mock 短路
vi.mock('../../../bindings/hanxi/internal/app', () => ({ AppService: { OpenPath: vi.fn() } }))
vi.mock('../../composables/useWailsEvent', () => ({ useWailsEvent: vi.fn() }))
vi.mock('../../composables/useConfirm', () => ({ useConfirm: () => ({ confirm: vi.fn() }) }))

import { annotateBusy, annotateToggleView } from '../markeron'

describe('annotateToggleView · 六态矩阵', () => {
  beforeEach(() => {
    annotateBusy.value = false
  })
  afterEach(() => {
    annotateBusy.value = false
  })

  it('stopped：主钮召唤「启动 MarkerOn」且可点', () => {
    const v = annotateToggleView({ state: 'stopped', drawing: false, busy: false, installedCount: 1 })
    expect(v.label).toBe('启动 MarkerOn')
    expect(v.sub).toBe('后台静默运行，不进入标注')
    expect(v.variant).toBe('btn-toggle-primary')
    expect(v.disabled).toBe(false)
    expect(v.hint).toBe('')
    expect(v.title).toBe(v.sub)
  })

  it('starting：启动中禁用（防连点重复拉起）', () => {
    const v = annotateToggleView({ state: 'starting', drawing: false, busy: false, installedCount: 1 })
    expect(v.label).toBe('启动中…')
    expect(v.sub).toBe('等待 MarkerOn 就绪（≤20 秒）')
    expect(v.disabled).toBe(true)
  })

  it('running×drawing 双向：开启态实心 toggle / 标注中可退出', () => {
    const off = annotateToggleView({ state: 'running', drawing: false, busy: false, installedCount: 1 })
    expect(off.label).toBe('开启标注')
    expect(off.sub).toBe('MarkerOn 已在后台运行')
    expect(off.variant).toBe('btn-toggle-outline')
    expect(off.disabled).toBe(false)

    const on = annotateToggleView({ state: 'running', drawing: true, busy: false, installedCount: 1 })
    expect(on.label).toBe('退出标注')
    expect(on.sub).toBe('桌面覆盖层已开启，点击关闭')
    expect(on.variant).toBe('btn-toggle-active')
    expect(on.disabled).toBe(false)
  })

  it('external 有信使可借力；无已装版本时禁用并给快捷键指引（hint 顶掉 sub 成 title）', () => {
    const withHost = annotateToggleView({ state: 'external', drawing: false, busy: false, installedCount: 1 })
    expect(withHost.label).toBe('切换标注')
    expect(withHost.variant).toBe('btn-toggle-warn')
    expect(withHost.disabled).toBe(false)
    expect(withHost.hint).toBe('')
    expect(withHost.title).toBe('外部实例状态未知，点击切换')

    const noHost = annotateToggleView({ state: 'external', drawing: false, busy: false, installedCount: 0 })
    expect(noHost.disabled).toBe(true)
    expect(noHost.hint).toBe('外部实例无法代为切换（无可用信使程序），请使用快捷键 Ctrl+Shift+D')
    // 用户读钮全靠 title：hint 在场时 title 必须是 hint，不能仍是 sub
    expect(noHost.title).toBe(noHost.hint)
  })

  it('failed：异常退出后钮面改判「重试启动」danger 档', () => {
    const v = annotateToggleView({ state: 'failed', drawing: false, busy: false, installedCount: 1 })
    expect(v.label).toBe('重试启动')
    expect(v.sub).toBe('上次异常退出，点击重新启动')
    expect(v.variant).toBe('btn-toggle-danger')
    expect(v.disabled).toBe(false)
  })

  it('busy 双闸合并：共享动作闩或 annotateBusy 任一为真都进「处理中…」并禁用', () => {
    const viaStore = annotateToggleView({ state: 'running', drawing: true, busy: true, installedCount: 1 })
    expect(viaStore.label).toBe('处理中…')
    expect(viaStore.sub).toBe('正在切换标注状态…')
    expect(viaStore.disabled).toBe(true)

    annotateBusy.value = true
    const viaLatch = annotateToggleView({ state: 'stopped', drawing: false, busy: false, installedCount: 1 })
    expect(viaLatch.label).toBe('处理中…')
    expect(viaLatch.disabled).toBe(true)
  })
})
