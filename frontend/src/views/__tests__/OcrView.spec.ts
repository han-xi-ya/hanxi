// 特征测试：OcrView 是「本地服务托管 + 识别转发」视图（双引擎并存，计划 §5.5）——
// 核心契约：五态 chip 与引导横幅、启停调用、引擎列表（GetEngines/切换/带病警示）、
// 三输入通道汇流 ImageRef、识别结果/失败/stale 三形态、busy 防重入、复制走 useClipboard。
// 绑定与事件按仓库统一 vi.mock 打桩范式（照 DouzyView.spec）。
import { KeepAlive, defineComponent, h } from 'vue'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import OcrView from '../OcrView.vue'
import { useToast } from '../../composables/useToast'
import { useConfirm } from '../../composables/useConfirm'

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
  ImportServiceExe: vi.fn(),
  ImportServiceExeDialog: vi.fn(),
  ImportPaddleDir: vi.fn(),
  ImportPaddleDirDialog: vi.fn(),
  GetEngines: vi.fn(),
  SetActiveEngine: vi.fn(),
  ListHostedVersions: vi.fn(),
  InstallHostedZip: vi.fn(),
  InstallHostedZipDialog: vi.fn(),
  UninstallHostedVersion: vi.fn(),
  HandleNativeDrop: vi.fn(),
  SnipAndRecognize: vi.fn(),
  RecognizeClipboardImage: vi.fn(),
  GetSnipHotkey: vi.fn(),
  SetSnipHotkey: vi.fn(),
  SetSnipHotkeyEnabled: vi.fn(),
  GetSnipResult: vi.fn().mockResolvedValue([{ ok: false, text: '', lineCount: 0, elapsedMs: 0, error: '', copied: false, cancelled: false }, false]),
  SnipCopyText: vi.fn(),
  SnipCardDismiss: vi.fn(),
  GetAutoCopy: vi.fn(),
  SetAutoCopy: vi.fn(),
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

const hist = vi.hoisted(() => ({ List: vi.fn(), Delete: vi.fn(), Clear: vi.fn() }))

vi.mock('../../../bindings/hanxi/internal/modules/ocr/ocrservice', () => svc)
vi.mock('../../../bindings/hanxi/internal/history/historyservice', () => hist)

const stoppedState = {
  state: 'stopped', online: false, managed: false, external: false, pid: 0,
  listenAddr: '127.0.0.1:53120', exePath: '', exeAuto: true, version: '', engine: '',
  engineMode: '', engineID: 'wechat', engineError: '',
  engineRunning: false, hung: false, error: '', checkedAt: '',
}
const runningState = {
  ...stoppedState, state: 'running', online: true, managed: true,
  exePath: 'E:\\tools\\hanxi-ocr\\hanxi-ocr.exe', version: '0.2.0', engine: 'wxocr@8094', engineRunning: true,
}

// 引擎注册表夹具：默认微信已装且活跃（自动发现）、PP-OCR 未装（中文指引）
const wechatEngine = {
  id: 'wechat', label: '微信引擎', installed: true, active: true, version: '0.3.1',
  path: 'E:\\tools\\hanxi-ocr\\hanxi-ocr.exe', auto: true, error: '',
}
const paddleMissing = {
  id: 'paddle', label: 'PP-OCR 开源引擎', installed: false, active: false, version: '',
  path: '', auto: false, error: '未发现 PP-OCR 引擎：解压公开分发包后点「导入目录」，或把目录放入 Hanxi 数据目录 ocr-engines 下',
}
const paddleInstalled = {
  id: 'paddle', label: 'PP-OCR 开源引擎', installed: true, active: false, version: '0.4.0-alpha',
  path: 'D:\\ocr\\hanxi-ocr-paddle\\hanxi-ocr.exe', auto: false, error: '',
}

function stubStatus(st = stoppedState, engs: Array<Record<string, unknown>> = [{ ...wechatEngine }, { ...paddleMissing }], hosted: Array<Record<string, unknown>> = []) {
  svc.GetStatus.mockResolvedValue({ ...st })
  svc.GetEngines.mockResolvedValue(engs.map((e) => ({ ...e })))
  svc.ListHostedVersions.mockResolvedValue(hosted.map((v) => ({ ...v })))
  svc.GetListenPort.mockResolvedValue(53120)
  svc.GetFollowOnExit.mockResolvedValue(true)
  svc.GetAutoCopy.mockResolvedValue(true)
  hist.List.mockResolvedValue([]) // 历史面板自取数：默认空桶
  svc.GetSnipHotkey.mockResolvedValue({ enabled: true, accel: 'Ctrl+Alt+T', registered: true })
}

async function mountView() {
  const Host = defineComponent({ render: () => h(KeepAlive, null, h(OcrView)) })
  const wrapper = mount(Host, { attachTo: document.body, global: { stubs: { teleport: true } } })
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

describe('OcrView 组件导入', () => {
  it('无组件首启：设置面板自动展开且导入区带原生拖放标记', async () => {
    stubStatus()
    const wrapper = await mountView()
    const zone = wrapper.find('#ocr-import-target')
    expect(zone.exists()).toBe(true)
    expect(zone.attributes('data-file-drop-target')).toBe('true')
    wrapper.unmount()
  })

  it('点击导入区走 zip 托管安装对话框（提示统一由回执事件负责）', async () => {
    stubStatus(runningState)
    const wrapper = await mountView()
    await wrapper.findAll('.btn').find((b) => b.text().includes('服务设置'))!.trigger('click')
    await wrapper.find('#ocr-import-target').trigger('click')
    await flushPromises()
    expect(svc.InstallHostedZipDialog).toHaveBeenCalledTimes(1)
    expect(svc.ImportServiceExeDialog).not.toHaveBeenCalled() // 微信 exe 对话框已归引擎行「更换组件」
    expect(useToast().toastMsg.value).toBeFalsy() // 回执未回，不抢提示
    wrapper.unmount()
  })

  it('导入成功回执：提示 + 自动启动托管', async () => {
    stubStatus()
    const wrapper = await mountView()
    svc.StartService.mockResolvedValue({ action: 'started', external: false, message: 'hanxi-ocr 服务已启动（127.0.0.1:53120）' })
    runtime.handlers['ocr:file-drop-result']({
      data: { kind: 'import', ok: true, exePath: 'D:\\x\\hanxi-ocr.exe', image: null, message: '已导入 hanxi-ocr.exe（47.9 MB），正在启动服务' },
    })
    await flushPromises()
    // 单条全局 toast：导入提示随后被启动成功回执顶掉，证明两步链路都走完
    expect(useToast().toastMsg.value).toContain('已启动')
    expect(svc.StartService).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })

  it('导入校验失败回执：只提示原因，绝不自动启动', async () => {
    stubStatus()
    const wrapper = await mountView()
    runtime.handlers['ocr:file-drop-result']({
      data: { kind: 'import', ok: false, exePath: '', image: null, message: '该文件仅 9.0 MB 且同级没有引擎文件，无法独立运行（v0.3 起请收发单文件版 hanxi-ocr.exe，约 48 MB）' },
    })
    await flushPromises()
    expect(useToast().toastMsg.value).toContain('单文件版')
    expect(svc.StartService).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('图片真实路径回执：直接设为待识别对象，不走 dataURL 落盘', async () => {
    stubStatus(runningState)
    const wrapper = await mountView()
    runtime.handlers['ocr:file-drop-result']({
      data: { kind: 'image', ok: true, exePath: '', image: { ...imageRef }, message: '' },
    })
    await flushPromises()
    expect(svc.SavePastedImage).not.toHaveBeenCalled()
    expect(wrapper.find('.ocr-preview').exists()).toBe(true)
    wrapper.unmount()
  })

  it('取消导入（空回执）保持静默', async () => {
    stubStatus()
    const wrapper = await mountView()
    runtime.handlers['ocr:file-drop-result']({ data: { kind: 'import', ok: false, exePath: '', image: null, message: '' } })
    await flushPromises()
    expect(useToast().toastMsg.value).toBe('')
    expect(svc.StartService).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})

describe('OcrView 引擎列表（双引擎并存）', () => {
  async function mountWith(st: Record<string, unknown>, engs: Array<Record<string, unknown>>) {
    stubStatus(st as never, engs)
    const wrapper = await mountView()
    const toggle = wrapper.findAll('.btn').find((b) => /服务设置|收起设置/.test(b.text()))!
    if (toggle.attributes('aria-expanded') !== 'true') await toggle.trigger('click')
    await flushPromises()
    return wrapper
  }
  function rowBtn(wrapper: Awaited<ReturnType<typeof mountView>>, rowIdx: number, text: string) {
    return wrapper.findAll('.ocr-engine-row')[rowIdx].findAll('.btn').find((b) => b.text().includes(text))!
  }

  it('两行卡：当前徽标/安装态/版本/路径/自动发现；未安装行直出中文指引且不可切换', async () => {
    const wrapper = await mountWith(runningState, [{ ...wechatEngine }, { ...paddleMissing }])
    const rows = wrapper.findAll('.ocr-engine-row')
    expect(rows).toHaveLength(2)
    expect(rows[0].text()).toContain('微信引擎')
    expect(rows[0].text()).toContain('当前引擎')
    expect(rows[0].text()).toContain('已安装')
    expect(rows[0].text()).toContain('0.3.1')
    expect(rows[0].text()).toContain('自动发现')
    expect(rows[0].text()).toContain('上游型号 wxocr@8094')
    expect(rows[0].findAll('.btn-primary')).toHaveLength(0) // 活跃行无「设为当前」
    expect(rows[1].text()).toContain('未安装')
    expect(rows[1].text()).toContain('未发现 PP-OCR 引擎') // error 中文指引直出
    expect(rows[1].findAll('.btn-primary')).toHaveLength(0) // 未安装不可切换
    expect(rows[1].text()).toContain('导入目录')
    wrapper.unmount()
  })

  it('engineError 非空：页面警示横幅 + 当前行警示态，中文原因不吞', async () => {
    const wrapper = await mountWith(
      { ...runningState, engineError: '模型缺失：models/det.onnx 未找到' },
      [{ ...wechatEngine }, { ...paddleInstalled }],
    )
    expect(wrapper.find('.banner-warn').text()).toContain('模型缺失')
    const rows = wrapper.findAll('.ocr-engine-row')
    expect(rows[0].classes()).toContain('ocr-engine-sick')
    expect(rows[0].text()).toContain('当前 · 引擎异常')
    expect(rows[0].text()).toContain('模型缺失')
    expect(rows[1].text()).toContain('设为当前') // 另一已装引擎可作切换逃生口
    wrapper.unmount()
  })

  it('运行中切换：先经确认框说明将重启识别，确认后才调 SetActiveEngine', async () => {
    svc.SetActiveEngine.mockResolvedValue({ action: 'switched', external: false, message: '已切换为 PP-OCR 开源引擎，新引擎已启动' })
    const wrapper = await mountWith(runningState, [{ ...wechatEngine }, { ...paddleInstalled }])
    const { confirmState, settleConfirm } = useConfirm()
    await rowBtn(wrapper, 1, '设为当前').trigger('click')
    await flushPromises()
    expect(svc.SetActiveEngine).not.toHaveBeenCalled() // 未经确认绝不擅自重启服务
    expect(confirmState.open).toBe(true)
    expect(confirmState.options.description).toContain('重启')
    settleConfirm(true)
    await settle()
    expect(svc.SetActiveEngine).toHaveBeenCalledWith('paddle')
    expect(useToast().toastMsg.value).toContain('已切换')
    wrapper.unmount()
  })

  it('取消确认：不切换引擎', async () => {
    const wrapper = await mountWith(runningState, [{ ...wechatEngine }, { ...paddleInstalled }])
    const { confirmState, settleConfirm } = useConfirm()
    await rowBtn(wrapper, 1, '设为当前').trigger('click')
    await flushPromises()
    expect(confirmState.open).toBe(true)
    settleConfirm(false)
    await settle()
    expect(svc.SetActiveEngine).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('服务停止时切换：免确认直接登记切换，不擅自拉起', async () => {
    svc.SetActiveEngine.mockResolvedValue({ action: 'switched', external: false, message: '已切换为 PP-OCR 开源引擎（服务未启动，启动或截屏识别时自动拉起）' })
    const wrapper = await mountWith(stoppedState, [{ ...wechatEngine }, { ...paddleInstalled }])
    const { confirmState } = useConfirm()
    await rowBtn(wrapper, 1, '设为当前').trigger('click')
    await settle()
    expect(confirmState.open).toBe(false)
    expect(svc.SetActiveEngine).toHaveBeenCalledWith('paddle')
    expect(svc.StartService).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('external 在跑：后端 external-unmanaged 拒绝的中文指引原样直出', async () => {
    svc.SetActiveEngine.mockResolvedValue({ action: 'external-unmanaged', external: true, message: '外部 hanxi-ocr 实例正在服务，Hanxi 不接管其启停；请先退出该实例再切换引擎' })
    const wrapper = await mountWith({ ...runningState, state: 'external', external: true, managed: false }, [{ ...wechatEngine }, { ...paddleInstalled }])
    const { confirmState } = useConfirm()
    await rowBtn(wrapper, 1, '设为当前').trigger('click')
    await settle()
    expect(confirmState.open).toBe(false) // 不弹无意义的重启确认
    expect(svc.SetActiveEngine).toHaveBeenCalledWith('paddle')
    expect(useToast().toastMsg.value).toContain('外部')
    expect(svc.StartService).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('PP-OCR 导入目录：后端对话框一体导入（取消静默），回执事件统一播报', async () => {
    const wrapper = await mountWith(runningState, [{ ...wechatEngine }, { ...paddleMissing }])
    svc.ImportPaddleDirDialog.mockResolvedValue({ kind: 'import' }) // 取消：后端空回执，不发事件不播报
    await rowBtn(wrapper, 1, '导入目录').trigger('click')
    await settle()
    expect(svc.ImportPaddleDirDialog).toHaveBeenCalledTimes(1)
    expect(useToast().toastMsg.value).toBeFalsy()

    const statusBefore = svc.GetStatus.mock.calls.length
    const enginesBefore = svc.GetEngines.mock.calls.length
    runtime.handlers['ocr:file-drop-result']({
      data: { kind: 'import', ok: true, exePath: 'D:\\ocr\\hanxi-ocr-paddle\\hanxi-ocr.exe', image: null, message: '已登记 PP-OCR 引擎目录（v0.4.0-alpha）；已切换为 PP-OCR 开源引擎' },
    })
    await flushPromises()
    expect(useToast().toastMsg.value).toContain('已登记')
    expect(svc.GetStatus.mock.calls.length).toBeGreaterThan(statusBefore) // 回执后状态…
    expect(svc.GetEngines.mock.calls.length).toBeGreaterThan(enginesBefore) // …与注册表同频刷新
    wrapper.unmount()
  })

  it('微信引擎行「更换组件」走文件框（对话框通道）；导入区文案改指 zip 托管通道', async () => {
    const wrapper = await mountWith(runningState, [{ ...wechatEngine }, { ...paddleInstalled }])
    await rowBtn(wrapper, 0, '更换组件').trigger('click')
    await flushPromises()
    expect(svc.ImportServiceExeDialog).toHaveBeenCalledTimes(1)
    const zone = wrapper.find('#ocr-import-target')
    expect(zone.attributes('data-file-drop-target')).toBe('true')
    expect(zone.text()).toContain('.zip') // 托管通道主推文案
    expect(zone.text()).toContain('.sha256')
    wrapper.unmount()
  })
})

describe('OcrView 托管版本（F7 zip 安装 / 列表 / 卸载）', () => {
  const hv = (over: Record<string, unknown>) => ({
    engine: 'wechat', version: '4.1.15.9',
    dir: 'C:\\hx\\versions\\hanxi-ocr\\wechat-4.1.15.9',
    exePath: 'C:\\hx\\versions\\hanxi-ocr\\wechat-4.1.15.9\\hanxi-ocr.exe',
    size: 50 * 1024 * 1024, installedAt: '2026-09-18 10:00:00',
    note: '微信 4.0 离线 OCR 引擎', state: 'ready', effective: false, error: '',
    ...over,
  })
  const hostedFixture = [
    hv({}), hv({ version: '4.1.9.0', state: 'broken', error: '入口文件缺失或损坏' }),
    hv({ engine: 'paddle', version: '0.4.0-alpha', effective: true, size: 36 * 1024 * 1024 }),
  ]

  async function mountHosted(st: Record<string, unknown> = runningState) {
    stubStatus(st as never, [{ ...wechatEngine }, { ...paddleInstalled }], hostedFixture)
    const wrapper = await mountView()
    const toggle = wrapper.findAll('.btn').find((b) => /服务设置|收起设置/.test(b.text()))!
    if (toggle.attributes('aria-expanded') !== 'true') await toggle.trigger('click')
    await flushPromises()
    return wrapper
  }

  it('引擎行内子表：版本/生效徽标/大小/时间渲染，损坏版给危险徽标；按引擎归属分流', async () => {
    const wrapper = await mountHosted()
    const rows = wrapper.findAll('.ocr-engine-row')
    const wechatHv = rows[0].findAll('.ocr-hosted-row')
    const paddleHv = rows[1].findAll('.ocr-hosted-row')
    expect(wechatHv).toHaveLength(2)
    expect(paddleHv).toHaveLength(1)
    expect(wechatHv[0].text()).toContain('4.1.15.9')
    expect(wechatHv[0].text()).toContain('2026-09-18')
    expect(wechatHv[1].text()).toContain('损坏')
    expect(paddleHv[0].text()).toContain('0.4.0-alpha')
    expect(paddleHv[0].text()).toContain('生效') // 生效徽标只落在 effective 行
    expect(wechatHv[0].text()).not.toContain('生效')
    wrapper.unmount()
  })

  it('卸载走确认框：确认后调 UninstallHostedVersion 并全量刷新', async () => {
    svc.UninstallHostedVersion.mockResolvedValue({ action: 'uninstalled', external: false, message: '已卸载 微信引擎 v4.1.15.9' })
    const wrapper = await mountHosted()
    const { confirmState, settleConfirm } = useConfirm()
    await wrapper.findAll('.ocr-hosted-uninstall')[0].trigger('click')
    await flushPromises()
    expect(confirmState.open).toBe(true)
    expect(confirmState.options.description).toContain('installers/') // 非生效版：说明包原件保留可重装
    settleConfirm(true)
    await settle()
    expect(svc.UninstallHostedVersion).toHaveBeenCalledWith('wechat', '4.1.15.9')
    expect(useToast().toastMsg.value).toContain('已卸载')
    wrapper.unmount()
  })

  it('生效版本卸载确认文案带降级说明；取消确认不卸', async () => {
    const wrapper = await mountHosted()
    const { confirmState, settleConfirm } = useConfirm()
    await wrapper.findAll('.ocr-hosted-uninstall')[2].trigger('click') // paddle 生效行
    await flushPromises()
    expect(confirmState.options.description).toContain('当前生效')
    settleConfirm(false)
    await settle()
    expect(svc.UninstallHostedVersion).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('在用拒卸：后端 refused-in-use 的中文指引原样直出，不静默', async () => {
    svc.UninstallHostedVersion.mockResolvedValue({
      action: 'refused-in-use', external: false,
      message: 'PP-OCR 开源引擎 v0.4.0-alpha 正在被识别服务使用，无法卸载；请先停止服务（或切换到其他引擎）再卸载',
    })
    const wrapper = await mountHosted()
    const { confirmState, settleConfirm } = useConfirm()
    await wrapper.findAll('.ocr-hosted-uninstall')[2].trigger('click')
    await flushPromises()
    settleConfirm(true)
    await settle()
    expect(confirmState.open).toBe(false)
    expect(useToast().toastMsg.value).toContain('正在被识别服务使用')
    wrapper.unmount()
  })

  it('「安装引擎包…」走 zip 对话框；成功回执经事件刷新列表与状态', async () => {
    const wrapper = await mountHosted()
    await wrapper.findAll('.btn').find((b) => b.text().includes('安装引擎包'))!.trigger('click')
    await flushPromises()
    expect(svc.InstallHostedZipDialog).toHaveBeenCalledTimes(1)

    const enginesBefore = svc.GetEngines.mock.calls.length
    const hostedBefore = svc.ListHostedVersions.mock.calls.length
    runtime.handlers['ocr:file-drop-result']({
      data: { kind: 'import', ok: true, exePath: 'C:\\hx\\versions\\hanxi-ocr\\paddle-0.5.0\\hanxi-ocr.exe', image: null, message: '已安装托管引擎 PP-OCR 开源引擎 v0.5.0（36.2 MB）' },
    })
    await flushPromises()
    expect(useToast().toastMsg.value).toContain('已安装托管引擎')
    expect(svc.GetEngines.mock.calls.length).toBeGreaterThan(enginesBefore) // 回执后注册表…
    expect(svc.ListHostedVersions.mock.calls.length).toBeGreaterThan(hostedBefore) // …与托管树同频刷新
    wrapper.unmount()
  })

  it('无托管版本时引擎行不渲染子表（旧式引用件世界零打扰）', async () => {
    stubStatus(runningState as never, [{ ...wechatEngine }, { ...paddleInstalled }], [])
    const wrapper = await mountView()
    const toggle = wrapper.findAll('.btn').find((b) => /服务设置|收起设置/.test(b.text()))!
    if (toggle.attributes('aria-expanded') !== 'true') await toggle.trigger('click')
    await flushPromises()
    expect(wrapper.findAll('.ocr-hosted-row')).toHaveLength(0)
    wrapper.unmount()
  })
})

describe('OcrView 框选截屏识别', () => {
  it('点击截屏按钮：调 SnipAndRecognize，成功回执 toast 指路悬浮卡', async () => {
    stubStatus(runningState)
    svc.SnipAndRecognize.mockResolvedValue({ ok: true, text: '字', lineCount: 1, elapsedMs: 90, error: '', copied: true, cancelled: false })
    const wrapper = await mountView()
    await wrapper.findAll('.btn').find((b) => b.text().includes('框选识别'))!.trigger('click')
    await flushPromises()
    expect(svc.SnipAndRecognize).toHaveBeenCalledTimes(1)
    expect(useToast().toastMsg.value).toContain('悬浮卡')
    wrapper.unmount()
  })

  it('用户放弃选区（cancelled）：完全静默', async () => {
    stubStatus(runningState)
    svc.SnipAndRecognize.mockResolvedValue({ ok: false, text: '', lineCount: 0, elapsedMs: 0, error: '', copied: false, cancelled: true })
    const wrapper = await mountView()
    await wrapper.findAll('.btn').find((b) => b.text().includes('框选识别'))!.trigger('click')
    await flushPromises()
    expect(useToast().toastMsg.value).toBeFalsy()
    wrapper.unmount()
  })

  it('截屏链路报错：toast 含原因；busy 期间按钮禁用', async () => {
    stubStatus(runningState)
    let rejectFn: (e: unknown) => void = () => {}
    svc.SnipAndRecognize.mockReturnValue(new Promise((_, rej) => { rejectFn = rej }))
    const wrapper = await mountView()
    const btn = wrapper.findAll('.btn').find((b) => b.text().includes('框选识别'))!
    await btn.trigger('click')
    expect(btn.attributes('disabled')).toBeDefined() // 45s 等待期内防重入
    rejectFn(new Error('截屏识别需要 hanxi-ocr 组件：请先在文字识别页导入'))
    await flushPromises()
    expect(useToast().toastMsg.value).toContain('组件')
    wrapper.unmount()
  })

  it('自动复制开关：回显 + 切换落盘', async () => {
    stubStatus(runningState)
    const wrapper = await mountView()
    await wrapper.findAll('.btn').find((b) => b.text().includes('服务设置'))!.trigger('click')
    const box = wrapper.findAll('input[type="checkbox"]')[1] // [0]=随退出 [1]=自动复制
    expect((box.element as HTMLInputElement).checked).toBe(true)
    await box.setValue(false)
    await flushPromises()
    expect(svc.SetAutoCopy).toHaveBeenCalledWith(false)
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

describe('OcrView 历史弹窗（统一历史接入）', () => {
  it('打开历史：面板按 ocr 桶自取数；双击应用行经 InspectImage 回填图片并关窗', async () => {
    stubStatus(runningState)
    hist.List.mockResolvedValue([
      { id: 7, funcType: 'ocr', summary: '识别 a.png → 2 字', input: 'C:\pics\a.png', output: '你好', extra: 'ui', createdAt: '2026-09-17T10:00:00+08:00' },
    ])
    svc.InspectImage.mockResolvedValue({ ...imageRef })
    const wrapper = await mountView()

    await wrapper.findAll('.ocr-head-actions .btn').find((b) => b.text().includes('历史'))!.trigger('click')
    await flushPromises()
    expect(wrapper.find('.hist-dialog').exists()).toBe(true)
    expect(hist.List).toHaveBeenCalledWith('ocr', '')

    await wrapper.find('.hp-list tbody tr').trigger('dblclick')
    await flushPromises()
    expect(svc.InspectImage).toHaveBeenCalledWith('C:\pics\a.png')
    expect(wrapper.find('.hist-dialog').exists()).toBe(false) // 回填即关窗
    expect(wrapper.find('.ocr-preview').exists()).toBe(true) // 图片已落输入区
    wrapper.unmount()
  })
})

describe('OcrView 剪贴板识图与识图热键', () => {
  async function openSettings(wrapper: Awaited<ReturnType<typeof mountView>>) {
    // 无组件的 stopped 态会被首启提示自动展开设置面板——仅未展开时才点开关
    if (!wrapper.find('.ocr-settings').exists()) {
      await wrapper.findAll('.btn').find((b) => /服务设置|收起设置/.test(b.text()))!.trigger('click')
      await flushPromises()
    }
  }

  it('页头「剪贴板识图」按钮：成功与无图失败都给回执（不弹覆盖层链路）', async () => {
    stubStatus()
    svc.RecognizeClipboardImage.mockResolvedValue({
      ok: true, text: '你好', lineCount: 1, elapsedMs: 10, error: '', copied: true, cancelled: false,
    })
    const wrapper = await mountView()
    const btn = wrapper.findAll('button').find((b) => b.text().includes('剪贴板识图'))!
    await btn.trigger('click')
    await flushPromises()
    expect(svc.RecognizeClipboardImage).toHaveBeenCalledTimes(1)
    expect(useToast().toastMsg.value).toBe('识别完成，结果已在悬浮卡中')

    svc.RecognizeClipboardImage.mockRejectedValue(
      new Error('剪贴板中没有图片：请先复制截图或图片（复制文字、文件不算）'),
    )
    await btn.trigger('click')
    await flushPromises()
    expect(useToast().toastMsg.value).toContain('剪贴板识图失败')
    wrapper.unmount()
  })

  it('键位录入：组合键提交规范串；占用冲突红字直出且回显不脏写', async () => {
    const st = { enabled: true, accel: 'Ctrl+Alt+T', registered: true }
    stubStatus()
    svc.GetSnipHotkey.mockImplementation(() => Promise.resolve({ ...st }))
    const wrapper = await mountView()
    await openSettings(wrapper)
    const input = wrapper.find('.ocr-hotkey-input')
    expect((input.element as HTMLInputElement).value).toBe('Ctrl+Alt+T')

    svc.SetSnipHotkey.mockResolvedValue(undefined)
    await input.trigger('click')
    await input.trigger('keydown', { key: 'r', ctrlKey: true, altKey: true })
    await flushPromises()
    expect(svc.SetSnipHotkey).toHaveBeenCalledWith('Ctrl+Alt+R')

    // 冲突：后端已回滚，UI 红字点名占用，键位框仍显实际生效键
    svc.SetSnipHotkey.mockRejectedValue(new Error('组合键 Ctrl+Alt+P 已被占用（可能被其他软件抢注），请到设置页改键'))
    await input.trigger('click')
    await input.trigger('keydown', { key: 'p', ctrlKey: true, altKey: true })
    await flushPromises()
    expect(wrapper.find('.ocr-hotkey-err').text()).toContain('已被占用')
    expect((wrapper.find('.ocr-hotkey-input').element as HTMLInputElement).value).toBe('Ctrl+Alt+T')
    wrapper.unmount()
  })

  it('热键开关与未注册实况警示', async () => {
    const st = { enabled: true, accel: 'Ctrl+Alt+T', registered: false }
    stubStatus()
    svc.GetSnipHotkey.mockImplementation(() => Promise.resolve({ ...st }))
    const wrapper = await mountView()
    await openSettings(wrapper)
    // enabled 但系统未绑：警示行提示改键重试
    expect(wrapper.find('.ocr-hotkey-err').text()).toContain('未注册')

    svc.SetSnipHotkeyEnabled.mockImplementation((v: boolean) => { st.enabled = v; return Promise.resolve() })
    const cb = wrapper.find('.ocr-hotkey-row input[type="checkbox"]')
    await cb.setValue(false) // checkbox 单向 :checked + @change 驱动
    await flushPromises()
    expect(svc.SetSnipHotkeyEnabled).toHaveBeenCalledWith(false)
    // 禁用后不再有"未注册"实况告警（禁而不用是正常态）
    expect(wrapper.find('.ocr-hotkey-err').exists()).toBe(false)
    wrapper.unmount()
  })
})
