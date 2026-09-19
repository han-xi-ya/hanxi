// ManagedControlBar 契约测试（Wave 5 · 批 0）：
// 锁死状态头的 adapter 消费面——五态词表映射与未知态回退、快照扩展字段经
// stateText 覆写投影、UiBanner/hint-line 互斥条件、#primary-action 槽注入
// （markeron 六态钮的批 1 通路）与声明式启停钮的成功/失败回执口径。
import { defineComponent, h } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import ManagedControlBar from '../ManagedControlBar.vue'
import type { ManagedModuleAdapter, ManagedSnapshot, NormalizedProgress } from '../adapter'
import { useToast } from '../../../composables/useToast'

const snap = (over: Partial<ManagedSnapshot>): ManagedSnapshot => ({
  state: 'stopped',
  version: '',
  pid: 0,
  error: '',
  startedAt: '',
  ...over,
})

function fakeAdapter(adapter: Partial<ManagedModuleAdapter> & { versions?: Partial<ManagedModuleAdapter['versions']> } = {}) {
  const events: { instance: Array<(s: ManagedSnapshot) => void>; progress: Array<(p: NormalizedProgress) => void> } = {
    instance: [],
    progress: [],
  }
  const { versions: versionOverrides, ...rest } = adapter
  const full: ManagedModuleAdapter = {
    getStatus: vi.fn(async () => snap({})) as never,
    subscribeInstanceState: (cb) => events.instance.push(cb),
    subscribeProgress: (cb) => events.progress.push(cb),
    versions: {
      listInstalled: vi.fn(async () => []) as never,
      listReleases: vi.fn(async () => []) as never,
      download: vi.fn(async () => ({})) as never,
      remove: vi.fn(async () => ({})) as never,
      openDir: vi.fn(async () => ({})) as never,
      ...versionOverrides,
    },
    ...rest,
  }
  return { full, events }
}

async function mountBar(adapter: ManagedModuleAdapter) {
  const w = mount(ManagedControlBar, { props: { adapter } })
  await flushPromises()
  return w
}

afterEach(() => {
  vi.restoreAllMocks()
  useToast().clearToast()
})

describe('ManagedControlBar 状态映射', () => {
  it('五态词表：running 展示版本号/PID/uptime，未知态回退未运行', async () => {
    const { full } = fakeAdapter({ getStatus: vi.fn(async () => snap({ state: 'running', version: 'v3.4.2', pid: 777, startedAt: new Date().toISOString() })) as never })
    const w = await mountBar(full)
    expect(w.find('.status-word').text()).toBe('运行中')
    expect(w.find('.status-light').classes()).toContain('running')
    expect(w.find('.ver-pill').text()).toBe('v3.4.2')
    expect(w.find('.pid-tag').text()).toBe('PID 777')
    expect(w.find('.uptime-tag').exists()).toBe(true)
    w.unmount()

    const { full: odd } = fakeAdapter({ getStatus: vi.fn(async () => snap({ state: 'weird' })) as never })
    const w2 = await mountBar(odd)
    expect(w2.find('.status-word').text()).toBe('未运行')
    w2.unmount()
  })

  it('业务扩展：stateText 覆写读快照可选字段（markeron drawing 型）', async () => {
    type Drawing = ManagedSnapshot & { drawing?: boolean }
    const { full } = fakeAdapter({
      getStatus: vi.fn(async (): Promise<Drawing> => ({ ...snap({ state: 'running', version: 'v1' }), drawing: true })) as never,
      stateText: (s) => ((s as Drawing).drawing ? '标注已开启' : '已启动（未标注）'),
    })
    const w = await mountBar(full)
    expect(w.find('.status-word').text()).toBe('标注已开启')
    w.unmount()
  })

  it('instance-state 事件即时改写界面（订阅回调直推，不等轮询）', async () => {
    const { full, events } = fakeAdapter()
    const w = await mountBar(full)
    events.instance[0](snap({ state: 'running', version: 'v2', pid: 1 }))
    await w.vm.$nextTick()
    expect(w.find('.status-word').text()).toBe('运行中')
    w.unmount()
  })
})

describe('ManagedControlBar banner/hint 互斥', () => {
  it('running+ok banner：UiBanner 挂 slim；hint 缺席', async () => {
    const { full } = fakeAdapter({
      getStatus: vi.fn(async () => snap({ state: 'running', version: 'v1' })) as never,
      banner: (s) => (s.state === 'running' ? { tone: 'ok' as const, text: '正在运行' } : null),
      hint: (s) => (s.state === 'stopped' ? '尚未运行引导' : null),
    })
    const w = await mountBar(full)
    const banner = w.find('.banner')
    expect(banner.classes()).toContain('banner-ok')
    expect(banner.classes()).toContain('slim')
    expect(banner.text()).toBe('正在运行')
    expect(w.find('.hint-line').exists()).toBe(false)
    w.unmount()
  })

  it('无 banner 态回落 hint-line；bannerSlim=false 摘除 slim', async () => {
    const { full } = fakeAdapter({
      banner: () => null,
      hint: (s) => (s.state === 'stopped' ? '尚未运行：点击「打开窗口」启动' : null),
    })
    const w = mount(ManagedControlBar, { props: { adapter: full, bannerSlim: false } })
    await flushPromises()
    expect(w.find('.banner').exists()).toBe(false)
    expect(w.find('.hint-line').text()).toContain('尚未运行')
    w.unmount()
  })

  it('failed 态 error banner 透出后端错误串', async () => {
    const { full } = fakeAdapter({
      getStatus: vi.fn(async () => snap({ state: 'failed', error: '单实例协议超时' })) as never,
      banner: (s) => (s.state === 'failed' ? { tone: 'error' as const, text: s.error || '异常退出' } : null),
    })
    const w = await mountBar(full)
    expect(w.find('.banner').classes()).toContain('banner-error')
    expect(w.find('.banner').text()).toBe('单实例协议超时')
    w.unmount()
  })
})

describe('ManagedControlBar 钮区（声明 + #primary-action 槽）', () => {
  const control = (run = vi.fn(async () => ({ message: '已启动并唤起窗口' }))) => ({
    primary: {
      run,
      label: '🗔 打开窗口',
      cssClass: 'btn-secondary',
      disabledFor: (state: string) => state === 'starting',
      titleFor: (state: string) => (state === 'running' ? '唤起窗口' : '启动并打开窗口'),
    },
    quit: {
      run: vi.fn(async () => ({ message: '已发送退出' })),
      label: '⏻ 退出',
      disabledFor: (state: string) => state !== 'running' && state !== 'starting' && state !== 'external',
      titleFor: (state: string) => (state === 'external' ? '外部实例请经其托盘退出' : '优雅退出'),
    },
  })

  it('声明式双钮：label/cssClass/disabledFor/titleFor 全量生效，点击成功弹回执并刷新', async () => {
    const spec = control()
    const { full } = fakeAdapter({
      getStatus: vi.fn(async () => snap({ state: 'running', version: 'v1' })) as never,
      control: spec,
    })
    const w = await mountBar(full)
    const [open, quit] = w.findAll('.control-btns .btn')
    expect(open.text()).toBe('🗔 打开窗口')
    expect(open.classes()).toContain('btn-secondary')
    expect(open.attributes('title')).toBe('唤起窗口')
    expect(quit.text()).toBe('⏻ 退出')
    await open.trigger('click')
    await flushPromises()
    expect(spec.primary.run).toHaveBeenCalled()
    expect(useToast().toastMsg.value).toBe('已启动并唤起窗口')
    // 控制动词成功后恒刷一次快照（现状双分支口径）
    expect((full.getStatus as ReturnType<typeof vi.fn>).mock.calls.length).toBeGreaterThan(1)
    w.unmount()
  })

  it('stopped 态退出禁用；starting 态主钮禁用；失败 toast 前缀 primary 裸串 / quit 带「退出失败: 」', async () => {
    const { full } = fakeAdapter({
      getStatus: vi.fn(async () => snap({})) as never,
      control: control(),
    })
    const w = await mountBar(full)
    const [, quit] = w.findAll('.control-btns .btn')
    expect(quit.attributes('disabled')).toBeDefined()
    w.unmount()

    const { full: starting } = fakeAdapter({
      getStatus: vi.fn(async () => snap({ state: 'starting' })) as never,
      control: control(vi.fn(async () => { throw new Error('Tauri 单实例通道失败') })),
    })
    const w2 = await mountBar(starting)
    const [open2, quit2] = w2.findAll('.control-btns .btn')
    expect(open2.attributes('disabled')).toBeDefined()
    // starting 态退出钮可用（现状：running/starting/external 均可点）
    await quit2.trigger('click')
    await flushPromises()
    expect(useToast().toastMsg.value).toBe('已发送退出')
    w2.unmount()

    const { full: failed } = fakeAdapter({
      getStatus: vi.fn(async () => snap({})) as never,
      control: {
        primary: { run: vi.fn(async () => { throw new Error('拉起失败') }), label: '▶ 启动' },
        quit: { run: vi.fn(async () => { throw new Error('通道断开') }), label: '⏻ 退出' },
      },
    })
    const w3 = await mountBar(failed)
    await w3.findAll('.control-btns .btn')[0].trigger('click')
    await flushPromises()
    expect(useToast().toastMsg.value).toBe('拉起失败')
    await w3.findAll('.control-btns .btn')[1].trigger('click')
    await flushPromises()
    expect(useToast().toastMsg.value).toBe('退出失败: 通道断开')
    w3.unmount()
  })

  it('#primary-action 槽注入自绘钮（markeron 六态通路），声明钮照常按位并存', async () => {
    const { full } = fakeAdapter({
      getStatus: vi.fn(async () => snap({ state: 'running' })) as never,
    })
    const toggleRun = vi.fn(async () => ({ message: '标注已开启' }))
    const Host = defineComponent({
      setup: () => () =>
        h(ManagedControlBar, { adapter: full }, {
          'primary-action': ({ state }: { state: string }) =>
            h('button', { class: 'btn btn-toggle-active btn-small', onClick: toggleRun }, `✎ 退出标注 · ${state}`),
        }),
    })
    const w = mount(Host)
    await flushPromises()
    const btns = w.findAll('.control-btns .btn')
    expect(btns[0].text()).toBe('✎ 退出标注 · running')
    await btns[0].trigger('click')
    expect(toggleRun).toHaveBeenCalled()
    w.unmount()
  })
})
