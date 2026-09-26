import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import WslUsbPanel from '../WslUsbPanel.vue'

const api = vi.hoisted(() => ({
  GetUsbOverview: vi.fn(),
  BindUsbDevice: vi.fn(),
  UnbindUsbDevice: vi.fn(),
  UnbindAbsentUsbDevice: vi.fn(),
  AttachUsbDevice: vi.fn(),
  DetachUsbDevice: vi.fn(),
  SetUsbAutoAttach: vi.fn(),
  SetUsbShare: vi.fn(),
  SetUsbShareEnabled: vi.fn(),
  RemoveUsbShare: vi.fn(),
  ReplayUsbNow: vi.fn(),
  OpenUsbipdReleases: vi.fn(),
  InstallUsbipdViaWinget: vi.fn(),
}))

// confirm/toast 提升为受控替身：一键直通三岔（UAC 同意/取消、失败归因文案）逐条断言。
const confirmMock = vi.hoisted(() => vi.fn(async () => true))
const showToast = vi.hoisted(() => vi.fn())

vi.mock('../../../../bindings/hanxi/internal/modules/wsl/wslservice', () => api)
vi.mock('../../../composables/useToast', () => ({ useToast: () => ({ showToast }) }))
vi.mock('../../../composables/useConfirm', () => ({ useConfirm: () => ({ confirm: confirmMock }) }))
vi.mock('../../../composables/usePolling', () => ({ usePolling: (fn: () => unknown) => { void fn(); return {} } }))

const entry = {
  id: 'e1', busId: '2-3', instanceId: 'USB\\VID_AAAA&PID_0001\\SERIAL-A',
  vid: 'aaaa', pid: '0001', serial: 'SERIAL-A', guid: '', description: 'Serial A',
  distro: 'Ubuntu', enabled: true, addedAt: '2026-01-01 00:00:00',
}

function device(state: string, busId = '2-3') {
  return {
    busId, instanceId: entry.instanceId, vid: 'aaaa', pid: '0001', serial: 'SERIAL-A',
    guid: '', description: 'Serial A', clientIp: '', state, forced: false,
  }
}

function instance(name: string, opts: Partial<{ running: boolean; default: boolean; version: string }> = {}) {
  return {
    name, running: true, default: false, version: '2',
    stateText: '', basePath: '', vhdxPath: '', sizeBytes: 0, ...opts,
  }
}

function overview(devices: ReturnType<typeof device>[], over: Record<string, unknown> = {}) {
  return {
    installed: true, version: '5.3.0', devices, ledger: [], autoEnabled: false,
    replayBusy: false, releasesPage: 'https://github.com/dorssel/usbipd-win/releases', ...over,
  }
}

// 选择框走真 Teleport（teleport 桩会掐断插槽内依赖追踪，弹窗成了死 DOM）；
// 挂载点钉在 body，弹窗断言一律 document.body 直查，用例间清场。
async function mountPanel(ov: ReturnType<typeof overview>, instances: ReturnType<typeof instance>[] = []) {
  api.GetUsbOverview.mockResolvedValue(ov)
  const wrapper = mount(WslUsbPanel, { props: { instances }, attachTo: document.body })
  await flushPromises()
  return wrapper
}

function pickDialog(): HTMLElement | null {
  return document.body.querySelector('[role="dialog"]')
}
function pickRadio(dialog: HTMLElement, name: string): HTMLInputElement {
  return Array.from(dialog.querySelectorAll<HTMLInputElement>('input[type="radio"]')).find(r => r.value === name)!
}
function dialogButton(dialog: HTMLElement, text: string): HTMLButtonElement {
  return Array.from(dialog.querySelectorAll<HTMLButtonElement>('button')).find(b => b.textContent?.includes(text))!
}
async function chooseDistro(name: string) {
  const dialog = pickDialog()
  expect(dialog).not.toBeNull()
  const radio = pickRadio(dialog!, name)
  radio.checked = true
  radio.dispatchEvent(new Event('change', { bubbles: true }))
  await flushPromises()
  dialogButton(dialog!, '直通').click()
  await flushPromises()
}

const byText = (wrapper: Awaited<ReturnType<typeof mountPanel>>, text: string) =>
  wrapper.findAll('button').find(b => b.text().includes(text))

async function statusFor(devices: ReturnType<typeof device>[]) {
  api.GetUsbOverview.mockResolvedValue(overview(devices, { ledger: [entry], autoEnabled: true }))
  const wrapper = mount(WslUsbPanel, { props: { instances: [] } })
  await flushPromises()
  const ledgerTable = wrapper.findAll('table')[1]
  return ledgerTable.find('tbody tr').findAll('td')[3].text()
}

describe('WslUsbPanel 账本现态', () => {
  beforeEach(() => vi.clearAllMocks())

  it.each([
    ['attached', '已附加'],
    ['shared', '待附加'],
  ])('优先同 busid 后端现态 %s', async (state, expected) => {
    expect(await statusFor([device(state)])).toContain(expected)
  })

  it('空 busid 设备不可能误命中，呈现不在场', async () => {
    expect(await statusFor([device('shared', '')])).toContain('不在场')
  })
})

describe('WslUsbPanel 一键直通（N31 方案 A）', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    confirmMock.mockResolvedValue(true)
    // 高级操作折叠态持久化在 localStorage：清空防跨用例串味（默认收起）。
    localStorage.clear()
    // attachTo 挂载与 Teleport 弹窗都落在 body：逐例清场防串扰。
    document.body.innerHTML = ''
  })

  it('未装 usbipd：只渲染引导卡（复制命令+发布页出路），没有直通按钮', async () => {
    const w = await mountPanel(overview([], { installed: false }))
    expect(w.text()).toContain('需要 usbipd-win')
    expect(byText(w, '一键直通')).toBeUndefined()
    expect(byText(w, '📋 复制命令')).toBeDefined()
    expect(byText(w, '打开官方发布页')).toBeDefined()
  })

  it('唯一发行版+未共享：UAC 明示 → bind 成功后 attach（一次点击全链）', async () => {
    api.BindUsbDevice.mockResolvedValue({ success: true, message: '已共享' })
    api.AttachUsbDevice.mockResolvedValue({ success: true, message: '已附加' })
    const w = await mountPanel(overview([device('notshared')]), [instance('Ubuntu')])
    await byText(w, '一键直通')!.trigger('click')
    await flushPromises()
    // UAC 事前明示（确认文案点破"会弹系统授权"）
    expect(confirmMock).toHaveBeenCalledTimes(1)
    expect(JSON.stringify(confirmMock.mock.calls[0])).toContain('UAC')
    expect(api.BindUsbDevice).toHaveBeenCalledWith('2-3', false)
    expect(api.AttachUsbDevice).toHaveBeenCalledWith('2-3', 'Ubuntu')
    // bind 在前、attach 在后
    expect(api.BindUsbDevice.mock.invocationCallOrder[0]).toBeLessThan(api.AttachUsbDevice.mock.invocationCallOrder[0])
  })

  it('已共享+唯一发行版：不弹 UAC，直接 attach', async () => {
    api.AttachUsbDevice.mockResolvedValue({ success: true, message: '已附加' })
    const w = await mountPanel(overview([device('shared')]), [instance('Ubuntu')])
    await byText(w, '一键直通')!.trigger('click')
    await flushPromises()
    expect(confirmMock).not.toHaveBeenCalled()
    expect(api.BindUsbDevice).not.toHaveBeenCalled()
    expect(api.AttachUsbDevice).toHaveBeenCalledWith('2-3', 'Ubuntu')
  })

  it('UAC 预确认取消：bind 与 attach 都不发生', async () => {
    confirmMock.mockResolvedValue(false)
    const w = await mountPanel(overview([device('notshared')]), [instance('Ubuntu')])
    await byText(w, '一键直通')!.trigger('click')
    await flushPromises()
    expect(confirmMock).toHaveBeenCalledTimes(1)
    expect(api.BindUsbDevice).not.toHaveBeenCalled()
    expect(api.AttachUsbDevice).not.toHaveBeenCalled()
  })

  it('bind 回执失败（如系统 UAC 被拒）：中止 attach，中文归因提示分步重试', async () => {
    api.BindUsbDevice.mockResolvedValue({ success: false, message: '用户在 UAC 授权窗口点了取消' })
    const w = await mountPanel(overview([device('notshared')]), [instance('Ubuntu')])
    await byText(w, '一键直通')!.trigger('click')
    await flushPromises()
    expect(api.AttachUsbDevice).not.toHaveBeenCalled()
    const msg = showToast.mock.calls.map(c => String(c[0])).join('\n')
    expect(msg).toContain('共享（bind）未完成')
    expect(msg).toContain('用户在 UAC 授权窗口点了取消')
    expect(msg).toContain('高级操作')
  })

  it('attach 抛错：withOp 兜底中文回执且不误报成功', async () => {
    api.AttachUsbDevice.mockRejectedValue(new Error('附加失败: 设备尚未共享'))
    const w = await mountPanel(overview([device('shared')]), [instance('Ubuntu')])
    await byText(w, '一键直通')!.trigger('click')
    await flushPromises()
    const msg = showToast.mock.calls.map(c => String(c[0])).join('\n')
    expect(msg).toContain('附加失败')
  })

  it('多发行版无默认：先弹选择框，选定 Debian 才附加', async () => {
    api.AttachUsbDevice.mockResolvedValue({ success: true, message: '已附加' })
    const instances = [instance('Ubuntu', { running: false }), instance('Debian')]
    const w = await mountPanel(overview([device('shared')]), instances)
    await byText(w, '一键直通')!.trigger('click')
    await flushPromises()
    const dialog = pickDialog()
    expect(dialog?.textContent).toContain('直通给哪个发行版')
    expect(dialog?.textContent).toContain('顺手拉起') // 未运行提示如实标注
    expect(api.AttachUsbDevice).not.toHaveBeenCalled()
    await chooseDistro('Debian')
    expect(api.AttachUsbDevice).toHaveBeenCalledWith('2-3', 'Debian')
  })

  it('选择框点取消：整条直通链不动', async () => {
    const w = await mountPanel(overview([device('shared')]), [instance('Ubuntu'), instance('Debian')])
    await byText(w, '一键直通')!.trigger('click')
    await flushPromises()
    expect(pickDialog()).not.toBeNull()
    dialogButton(pickDialog()!, '取消').click()
    await flushPromises()
    expect(pickDialog()).toBeNull()
    expect(api.AttachUsbDevice).not.toHaveBeenCalled()
    expect(api.BindUsbDevice).not.toHaveBeenCalled()
  })

  it('选择框选定后按行记忆：第二次直通不再弹选', async () => {
    api.AttachUsbDevice.mockResolvedValue({ success: true, message: '已附加' })
    const instances = [instance('Ubuntu'), instance('Debian')]
    const w = await mountPanel(overview([device('shared')]), instances)
    await byText(w, '一键直通')!.trigger('click')
    await flushPromises()
    await chooseDistro('Debian')
    // 行级记忆生效：第二次点击不弹框，直接按 Debian 附加
    await byText(w, '一键直通')!.trigger('click')
    await flushPromises()
    expect(pickDialog()).toBeNull()
    expect(api.AttachUsbDevice).toHaveBeenCalledTimes(2)
    expect(api.AttachUsbDevice).toHaveBeenLastCalledWith('2-3', 'Debian')
  })

  it('零 WSL2 发行版：直通中止并引导去装发行版，不碰 usbipd 命令', async () => {
    const w = await mountPanel(overview([device('notshared')]), [instance('Legacy', { version: '1' })])
    await byText(w, '一键直通')!.trigger('click')
    await flushPromises()
    expect(api.BindUsbDevice).not.toHaveBeenCalled()
    expect(api.AttachUsbDevice).not.toHaveBeenCalled()
    expect(showToast.mock.calls.map(c => String(c[0])).join('\n')).toContain('还没有 WSL2 发行版')
  })

  it('高级操作默认收起；点「🔧 高级操作」展开后细粒度按钮回来且选择被记住', async () => {
    const w = await mountPanel(overview([device('shared')]), [instance('Ubuntu')])
    expect(byText(w, '取消共享')).toBeUndefined()
    expect(byText(w, '附加')).toBeUndefined()
    await byText(w, '高级操作')!.trigger('click')
    expect(byText(w, '✂ 取消共享')).toBeDefined()
    expect(byText(w, '▶ 附加')).toBeDefined()
    expect(byText(w, '⭐ 自动共享')).toBeDefined()
    expect(localStorage.getItem('hanxi.wsl.usb.advanced')).toBe('1')
  })

  it('高级操作展开态跨挂载记忆（localStorage）', async () => {
    localStorage.setItem('hanxi.wsl.usb.advanced', '1')
    const w = await mountPanel(overview([device('shared')]), [instance('Ubuntu')])
    expect(byText(w, '✂ 取消共享')).toBeDefined()
  })
})

describe('WslUsbPanel winget 一键安装（N31 方案 B）', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    confirmMock.mockResolvedValue(true)
    localStorage.clear()
    document.body.innerHTML = ''
  })

  // 未装态引导卡挂载（方案 B 主操作所在语境）。
  async function mountGuide() {
    return await mountPanel(overview([], { installed: false, version: '' }))
  }
  const toastText = () => showToast.mock.calls.map(c => String(c[0])).join('\n')

  it('未装引导卡：「📦 一键安装（winget）」是主操作钮，手动路径保留', async () => {
    const w = await mountGuide()
    const btn = byText(w, '一键安装（winget）')
    expect(btn).toBeDefined()
    expect(btn!.classes()).toContain('btn-primary')
    expect(byText(w, '📋 复制命令')).toBeDefined()
    expect(byText(w, '打开官方发布页')).toBeDefined()
  })

  it('确认 → 调安装 → 成功回执，随后必然后验复采', async () => {
    api.InstallUsbipdViaWinget.mockResolvedValue({ success: true, message: 'usbipd-win 已通过 winget 安装 v5.3.0' })
    const w = await mountGuide()
    const probesBefore = api.GetUsbOverview.mock.calls.length
    await byText(w, '一键安装（winget）')!.trigger('click')
    await flushPromises()
    expect(confirmMock).toHaveBeenCalledTimes(1)
    const dialog = JSON.stringify(confirmMock.mock.calls[0])
    expect(dialog).toContain('UAC')
    expect(dialog).toContain('联网')
    expect(dialog).toContain('数分钟')
    expect(api.InstallUsbipdViaWinget).toHaveBeenCalledTimes(1)
    expect(toastText()).toContain('已通过 winget 安装')
    expect(api.GetUsbOverview.mock.calls.length).toBeGreaterThan(probesBefore)
  })

  it('确认对话框被拒：安装调用一次不发', async () => {
    confirmMock.mockResolvedValue(false)
    const w = await mountGuide()
    await byText(w, '一键安装（winget）')!.trigger('click')
    await flushPromises()
    expect(api.InstallUsbipdViaWinget).not.toHaveBeenCalled()
  })

  it('UAC 取消回执（success=false）：如实播报，不掺成功语义', async () => {
    api.InstallUsbipdViaWinget.mockResolvedValue({ success: false, message: '已取消 UAC 授权，usbipd-win 未安装——需要时再点「一键安装」，或走手动路径' })
    const w = await mountGuide()
    await byText(w, '一键安装（winget）')!.trigger('click')
    await flushPromises()
    expect(toastText()).toContain('已取消 UAC 授权')
    expect(toastText()).not.toContain('✅')
  })

  it('已装回执（success=true"无需重复执行"）原样送达', async () => {
    api.InstallUsbipdViaWinget.mockResolvedValue({ success: true, message: 'usbipd-win v5.3.0 已经安装，无需重复执行' })
    const w = await mountGuide()
    await byText(w, '一键安装（winget）')!.trigger('click')
    await flushPromises()
    expect(toastText()).toContain('无需重复执行')
  })

  it('winget 缺席（后端前提报错 reject）：失败文案进 toast', async () => {
    api.InstallUsbipdViaWinget.mockRejectedValue(new Error('系统里找不到 winget（App Installer）'))
    const w = await mountGuide()
    await byText(w, '一键安装（winget）')!.trigger('click')
    await flushPromises()
    expect(toastText()).toContain('操作失败')
    expect(toastText()).toContain('App Installer')
  })

  it('安装进行中：钮变忙并禁用，防连点双发安装', async () => {
    let settle: ((v: { success: boolean; message: string }) => void) | null = null
    api.InstallUsbipdViaWinget.mockReturnValue(new Promise(resolve => { settle = resolve }))
    const w = await mountGuide()
    await byText(w, '一键安装（winget）')!.trigger('click')
    await flushPromises()
    const busy = byText(w, '正在安装')
    expect(busy).toBeDefined()
    expect(busy!.attributes('disabled')).toBeDefined()
    settle!({ success: true, message: 'usbipd-win 安装完成' })
    await flushPromises()
    expect(byText(w, '一键安装（winget）')).toBeDefined()
    expect(toastText()).toContain('安装完成')
  })
})
