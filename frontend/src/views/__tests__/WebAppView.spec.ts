// 「网页应用」管理页特征测试：defaultOpen（默认打开方式）前端层回归锁。
// 覆盖：表单默认段渲染/切换、保存链双兼容 payload（SaveEntry 第 5 参 + SetEntryDefaultOpen）、
// 行级「默认：窗口/浏览器」小字标（含存量未设值归一化）、说明卡显式>默认文案、
// 既有「打开/默认浏览器打开」双钮原样保留。
// W1（后端）未落盘前 bindings 无 defaultOpen/SetEntryDefaultOpen 符号，此处全 mock；
// vue-tsc 对桥接符号的缺省属预期中间态，绑定再生后 mock 面即为契约面。
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import WebAppView from '../WebAppView.vue'
import { useToast } from '../../composables/useToast'

const svc = vi.hoisted(() => ({
  ListEntries: vi.fn(),
  Open: vi.fn(),
  Collapse: vi.fn(),
  OpenExternal: vi.fn(),
  CollapseAll: vi.fn(),
  DeleteEntry: vi.fn(),
  SaveEntry: vi.fn(),
  // W1 独立导出路线：mock 存在以锁双兼容写的第二条通道
  SetEntryDefaultOpen: vi.fn(),
}))

vi.mock('../../../bindings/hanxi/internal/modules/webapp', () => ({ WebAppService: svc }))

const runtime = vi.hoisted(() => ({
  handlers: {} as Record<string, (event: { data: unknown }) => void>,
  unlisten: vi.fn(),
}))
vi.mock('@wailsio/runtime', () => ({
  Events: {
    On: (name: string, cb: (event: { data: unknown }) => void) => {
      runtime.handlers[name] = cb
      return runtime.unlisten
    },
  },
}))

// 默认无 defaultOpen 字段——即存量数据形态；extra 可注入 defaultOpen 各档。
const entry = (id: string, name: string, extra: Record<string, unknown> = {}) => ({
  id, name, url: `https://${id}.example.com`, icon: '',
  createdAt: '2026-09-26T00:00:00Z', windowOpen: false, windowHidden: false, ...extra,
})

async function mountView(entries: unknown[]) {
  svc.ListEntries.mockResolvedValue(entries)
  svc.SetEntryDefaultOpen.mockResolvedValue(undefined)
  const wrapper = mount(WebAppView)
  await flushPromises()
  return wrapper
}

/** 打开新增表单（页头主按钮） */
async function openCreateForm(wrapper: ReturnType<typeof mount>) {
  const btn = wrapper.findAll('button').find(b => b.text().includes('新增网址'))
  expect(btn, '页头应存在「新增网址」按钮').toBeTruthy()
  await btn!.trigger('click')
}

/** 填必填两项后提交表单 */
async function fillAndSubmit(wrapper: ReturnType<typeof mount>, name: string, url: string) {
  await wrapper.find('.webapp-name-input').setValue(name)
  await wrapper.find('.webapp-url-input').setValue(url)
  await wrapper.find('form.webapp-form').trigger('submit')
  await flushPromises()
}

const segs = (wrapper: ReturnType<typeof mount>) => wrapper.findAll('.webapp-seg-btn')

afterEach(() => {
  vi.clearAllMocks()
  useToast().clearToast()
})

describe('WebAppView 默认打开方式表单段', () => {
  it('新增表单缺省选中「独立窗口」', async () => {
    const wrapper = await mountViewReady()
    await openCreateForm(wrapper)
    const buttons = segs(wrapper)
    expect(buttons).toHaveLength(2)
    expect(buttons[0].text()).toBe('独立窗口')
    expect(buttons[1].text()).toBe('系统浏览器')
    expect(buttons[0].classes()).toContain('active')
    expect(buttons[0].attributes('aria-checked')).toBe('true')
    expect(buttons[1].classes()).not.toContain('active')
    wrapper.unmount()
  })

  it('未触碰默认段：保存链 payload 为 window（双通道）', async () => {
    const wrapper = await mountViewReady()
    svc.SaveEntry.mockResolvedValue('new-1')
    await openCreateForm(wrapper)
    await fillAndSubmit(wrapper, 'GitHub', 'https://github.com')
    // 全量快照路线：第 5 参 defaultOpen
    expect(svc.SaveEntry).toHaveBeenCalledWith('', 'GitHub', 'https://github.com', '', 'window')
    // 独立导出路线：绑定存在即补写，ID 取 SaveEntry 定稿返回值
    expect(svc.SetEntryDefaultOpen).toHaveBeenCalledWith('new-1', 'window')
    wrapper.unmount()
  })

  it('切到「系统浏览器」后保存：payload 为 browser，且段选中态迁移', async () => {
    const wrapper = await mountViewReady()
    svc.SaveEntry.mockResolvedValue('new-2')
    await openCreateForm(wrapper)
    await segs(wrapper)[1].trigger('click')
    expect(segs(wrapper)[1].classes()).toContain('active')
    expect(segs(wrapper)[0].classes()).not.toContain('active')
    await fillAndSubmit(wrapper, 'Docs', 'https://docs.example.com')
    expect(svc.SaveEntry).toHaveBeenCalledWith('', 'Docs', 'https://docs.example.com', '', 'browser')
    expect(svc.SetEntryDefaultOpen).toHaveBeenCalledWith('new-2', 'browser')
    wrapper.unmount()
  })

  it('编辑存量浏览器条目：预填选中「系统浏览器」', async () => {
    const wrapper = await mountViewReady([entry('e2', '二号', { defaultOpen: 'browser' })])
    await wrapper.find('button[aria-label="编辑"]').trigger('click')
    expect(wrapper.find('.webapp-name-input').element.value).toBe('二号')
    expect(segs(wrapper)[1].classes()).toContain('active')
    svc.SaveEntry.mockResolvedValue('e2')
    await wrapper.find('form.webapp-form').trigger('submit')
    await flushPromises()
    expect(svc.SaveEntry).toHaveBeenCalledWith('e2', '二号', 'https://e2.example.com', '', 'browser')
    expect(svc.SetEntryDefaultOpen).toHaveBeenCalledWith('e2', 'browser')
    wrapper.unmount()
  })
})

describe('WebAppView 行级默认小字标', () => {
  it('browser 显示「默认：浏览器」；未设值与空串归一化为「默认：窗口」', async () => {
    const wrapper = await mountViewReady([
      entry('e1', '甲', { defaultOpen: 'browser' }),
      entry('e2', '乙', { defaultOpen: '' }),
      entry('e3', '丙'), // 存量：无 defaultOpen 字段
    ])
    const chips = wrapper.findAll('.webapp-default')
    expect(chips).toHaveLength(3)
    expect(chips[0].text()).toBe('默认：浏览器')
    expect(chips[0].classes()).toContain('chip-neutral')
    expect(chips[1].text()).toBe('默认：窗口')
    expect(chips[2].text()).toBe('默认：窗口')
    wrapper.unmount()
  })

  it('说明卡讲清默认作用域与显式优先', async () => {
    const wrapper = await mountViewReady([entry('e1', '甲')])
    const note = wrapper.find('.webapp-note')
    expect(note.text()).toContain('轮盘/托盘里点击该应用时，按此默认执行')
    expect(note.text()).toContain('优先于默认')
    wrapper.unmount()
  })

  it('既有双显式钮原样保留：打开走 Open、浏览器走 OpenExternal', async () => {
    const wrapper = await mountViewReady([entry('e1', '甲')])
    svc.Open.mockResolvedValue(undefined)
    svc.OpenExternal.mockResolvedValue(undefined)
    await wrapper.findAll('button[title="以独立网页窗打开（已收起的窗口点击即恢复）"]')[0].trigger('click')
    await flushPromises()
    expect(svc.Open).toHaveBeenCalledWith('e1')
    await wrapper.find('button[aria-label="默认浏览器打开"]').trigger('click')
    await flushPromises()
    expect(svc.OpenExternal).toHaveBeenCalledWith('e1')
    wrapper.unmount()
  })
})

/** 先装一次列表供挂载即有数据（mountView 内部已 flush） */
async function mountViewReady(entries: unknown[] = [entry('e1', '一号')]) {
  return mountView(entries)
}
