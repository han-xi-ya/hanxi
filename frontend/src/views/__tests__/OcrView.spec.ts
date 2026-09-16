// 特征测试：OcrView 是「本地服务托管 + 识别转发」视图——
// 核心契约：五态 chip 与引导横幅、启停调用、三输入通道汇流 ImageRef、
// 识别结果/失败/stale 三形态、busy 防重入、复制走 useClipboard。
// 绑定与事件按仓库统一 vi.mock 打桩范式（照 DouzyView.spec）。
import { KeepAlive, defineComponent, h } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import OcrView from '../OcrView.vue'
import { useToast } from '../../composables/useToast'

const svc = vi.hoisted(() => ({
  GetStatus: vi.fn(),
  StartService: vi.fn(),
  StopService: vi.fn(),
  RecognizeImage: vi.fn(),
  PickImageDialog: vi.fn(),
  InspectImage: vi.fn(),
  SavePastedImage: vi.fn(),
  GetListenPort: vi.fn(),
  SetListenPort: vi.fn(),
  GetFollowOnExit: vi.fn(),
  SetFollowOnExit: vi.fn(),
  GetServiceExePath: vi.fn(),
  SetServiceExePath: vi.fn(),
  BrowseServiceExeDialog: vi.fn(),
  Logs: vi.fn(),
}))

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

vi.mock('../../../bindings/hanxi/internal/modules/ocr/ocrservice', () => svc)

const stoppedState = {
  state: 'stopped', online: false, managed: false, external: false, pid: 0,
  listenAddr: '127.0.0.1:53120', exePath: '', exeAuto: true, version: '', engine: '',
  engineRunning: false, hung: false, error: '', checkedAt: '',
}
const runningState = {
  ...stoppedState, state: 'running', online: true, managed: true,
  exePath: 'E:\\tools\\hanxi-ocr\\hanxi-ocr.exe', version: '0.2.0', engine: 'wxocr@8094', engineRunning: true,
}

function stubStatus(st = stoppedState) {
  svc.GetStatus.mockResolvedValue({ ...st })
  svc.GetListenPort.mockResolvedValue(53120)
  svc.GetFollowOnExit.mockResolvedValue(true)
}

async function mountView() {
  const Host = defineComponent({ render: () => h(KeepAlive, null, h(OcrView)) })
  const wrapper = mount(Host, { attachTo: document.body })
  await flushPromises()
  return wrapper
}

// jsdom FileReader 完成走宏任务：纯 flushPromises 等不到 onload，
// 泄漏到下一个用例会造成 mock 调用串台。paste/drop 用例统一用它收口。
async function settle(times = 5) {
  for (let i = 0; i < times; i++) {
    await new Promise((r) => setTimeout(r, 0))
    await flushPromises()
  }
}

const imageRef = {
  path: 'C:\\pics\\a.png', name: 'a.png', size: 20480,
  previewUrl: 'data:image/png;base64,AAA', temporary: false,
}
const okOutcome = {
  ok: true, text: '你好\n世界', lines: [{ text: '你好', x: 618, y: 117 }, { text: '世界', x: 1, y: 2 }],
  elapsedMs: 777, error: '',
}

afterEach(() => {
  vi.restoreAllMocks()
  useToast().clearToast()
})

describe('OcrView 状态与引导', () => {
  it('stopped：chip 未运行 + 引导横幅含「启动服务」；输入区禁用', async () => {
    stubStatus()
    const wrapper = await mountView()
    expect(svc.GetStatus).toHaveBeenCalled()
    expect(wrapper.find('.chip').text()).toBe('未运行')
    expect(wrapper.find('.banner-info').text()).toContain('识别服务未运行')
    expect(wrapper.find('.ocr-dropzone').attributes('aria-disabled')).toBe('true')
    wrapper.unmount()
  })

  it('点击启动：StartService + toast 回执 + 重新拉状态', async () => {
    stubStatus()
    svc.StartService.mockResolvedValue({ action: 'started', external: false, message: 'hanxi-ocr 服务已启动（127.0.0.1:53120）' })
    const wrapper = await mountView()
    const before = svc.GetStatus.mock.calls.length
    await wrapper.find('.ocr-banner-btn').trigger('click')
    await flushPromises()
    expect(svc.StartService).toHaveBeenCalled()
    expect(useToast().toastMsg.value).toContain('已启动')
    expect(svc.GetStatus.mock.calls.length).toBeGreaterThan(before)
    wrapper.unmount()
  })

  it('external：warning 横幅声明不接管；running chip 显示版本', async () => {
    stubStatus({ ...runningState, state: 'external', external: true, managed: false, online: true })
    const wrapper = await mountView()
    expect(wrapper.find('.chip').text()).toBe('外部运行')
    expect(wrapper.find('.banner-warn').text()).toContain('不接管')
    wrapper.unmount()
  })

  it('事件推送 running 快照 → chip 即时变化（不必等轮询）', async () => {
    stubStatus()
    const wrapper = await mountView()
    runtime.handlers['ocr:service-state']({ data: { ...runningState } })
    await flushPromises()
    expect(wrapper.find('.chip').text()).toBe('运行中')
    expect(wrapper.find('.ocr-ver').text()).toContain('0.2.0')
    wrapper.unmount()
  })
})

describe('OcrView 三输入通道', () => {
  it('对话框链路：Pick→Inspect→预览卡出现', async () => {
    stubStatus(runningState)
    svc.PickImageDialog.mockResolvedValue('C:\\pics\\a.png')
    svc.InspectImage.mockResolvedValue({ ...imageRef })
    const wrapper = await mountView()
    await wrapper.findAll('.btn').find((b) => b.text().includes('选择图片'))!.trigger('click')
    await flushPromises()
    expect(svc.InspectImage).toHaveBeenCalledWith('C:\\pics\\a.png')
    expect(wrapper.find('.ocr-thumb').exists()).toBe(true)
    expect(wrapper.find('img.ocr-thumb').attributes('src')).toBe(imageRef.previewUrl)
    wrapper.unmount()
  })

  it('取消对话框（空串）不打扰 Inspect', async () => {
    stubStatus(runningState)
    svc.PickImageDialog.mockResolvedValue('')
    const wrapper = await mountView()
    await wrapper.findAll('.btn').find((b) => b.text().includes('选择图片'))!.trigger('click')
    await flushPromises()
    expect(svc.InspectImage).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('粘贴通道：File→SavePastedImage→预览', async () => {
    stubStatus(runningState)
    svc.SavePastedImage.mockResolvedValue({ ...imageRef, temporary: true })
    const wrapper = await mountView()
    await wrapper.find('.ocr-dropzone').trigger('paste', {
      clipboardData: { items: [{ kind: 'file', getAsFile: () => new File(['abc'], 'shot.png', { type: 'image/png' }) }] },
    })
    await settle()
    expect(svc.SavePastedImage).toHaveBeenCalledTimes(1)
    const [name, dataURL] = svc.SavePastedImage.mock.calls[0]
    expect(name).toBe('shot.png')
    expect(String(dataURL).startsWith('data:image/png;base64,')).toBe(true)
    expect(wrapper.find('.ocr-preview').exists()).toBe(true)
    wrapper.unmount()
  })

  it('非图片文件被拒且不动后端', async () => {
    stubStatus(runningState)
    const wrapper = await mountView()
    await wrapper.find('.ocr-dropzone').trigger('drop', {
      dataTransfer: { files: [new File(['x'], 'note.txt', { type: 'text/plain' })] },
    })
    await settle()
    expect(svc.SavePastedImage).not.toHaveBeenCalled()
    expect(useToast().toastMsg.value).toContain('只支持图片')
    wrapper.unmount()
  })
})

describe('OcrView 识别与复制', () => {
  async function withImage(wrapper: Awaited<ReturnType<typeof mountView>>) {
    stubStatus(runningState)
    svc.PickImageDialog.mockResolvedValue('C:\\pics\\a.png')
    svc.InspectImage.mockResolvedValue({ ...imageRef })
    await wrapper.findAll('.btn').find((b) => b.text().includes('选择图片'))!.trigger('click')
    await flushPromises()
  }

  it('识别成功：全文 + 行数 meta + 单行复制调 clipboard', async () => {
    stubStatus(runningState)
    const wrapper = await mountView()
    await withImage(wrapper)
    svc.RecognizeImage.mockResolvedValue({ ...okOutcome })
    await wrapper.findAll('.btn').find((b) => b.text().includes('识别文字'))!.trigger('click')
    await flushPromises()
    expect(svc.RecognizeImage).toHaveBeenCalledWith('C:\\pics\\a.png')
    expect(wrapper.find('.ocr-text').text()).toContain('你好')
    expect(wrapper.findAll('.ocr-lines li')).toHaveLength(2)
    expect(wrapper.find('.ocr-meta').text()).toContain('777')

    // jsdom 默认 isSecureContext=false 会走 execCommand 降级；开安全上下文测主通道
    Object.defineProperty(window, 'isSecureContext', { value: true, configurable: true })
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true })
    await wrapper.findAll('.ocr-lines .link-button')[1].trigger('click')
    await flushPromises()
    expect(writeText).toHaveBeenCalledWith('世界')
    wrapper.unmount()
  })

  it('识别失败：错误直出中文并可保留图片重试', async () => {
    stubStatus(runningState)
    const wrapper = await mountView()
    await withImage(wrapper)
    svc.RecognizeImage.mockResolvedValue({ ok: false, text: '', lines: [], elapsedMs: 0, error: '识别超时(30s),引擎已复位,请重试' })
    await wrapper.findAll('.btn').find((b) => b.text().includes('识别文字'))!.trigger('click')
    await flushPromises()
    expect(wrapper.find('.error-box').text()).toContain('引擎已复位')
    expect(wrapper.find('.ocr-preview').exists()).toBe(true) // 图片保留
    wrapper.unmount()
  })

  it('busy 防重入：识别未回时二次点击不再调后端', async () => {
    stubStatus(runningState)
    const wrapper = await mountView()
    await withImage(wrapper)
    let resolveFn: (v: unknown) => void = () => {}
    svc.RecognizeImage.mockReturnValue(new Promise((r) => { resolveFn = r }))
    const btn = wrapper.findAll('.btn').find((b) => b.text().includes('识别文字'))!
    await btn.trigger('click')
    await btn.trigger('click')
    expect(svc.RecognizeImage).toHaveBeenCalledTimes(1)
    resolveFn({ ...okOutcome })
    await flushPromises()
    wrapper.unmount()
  })

  it('结果在手 + 服务掉线 → stale 提示保留旧结果', async () => {
    stubStatus(runningState)
    const wrapper = await mountView()
    await withImage(wrapper)
    svc.RecognizeImage.mockResolvedValue({ ...okOutcome })
    await wrapper.findAll('.btn').find((b) => b.text().includes('识别文字'))!.trigger('click')
    await flushPromises()
    runtime.handlers['ocr:service-state']({ data: { ...stoppedState } })
    await flushPromises()
    expect(wrapper.find('.ocr-stale').exists()).toBe(true)
    expect(wrapper.find('.ocr-text').exists()).toBe(true) // 数据未被抹掉
    wrapper.unmount()
  })
})
