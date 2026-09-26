// PiikView 特征测试（五路并行 · piik 前端线 C，对位 service.go 冻结面）：
// 标准壳（ManagedConsoleShell）装配 + 第三钮「打开界面」（#primary-action 槽，
// stopped 态禁用指路）+ 邀请面板四态渲染（未装 / stopped / running 链接全量 /
// running 仅本机）、三行链接复制走 clipboard 单源断言、「在浏览器打开」经
// OpenWindow RPC、metaHints 透传后端六条逐字上屏、访问口令徽标 + **口令值
// not.toContain 反证锁**（快照被喂入泄漏原文也不上屏）、退出确认消费
// QuitAdvisory 预告、末版卸载预告与 followOnExit 开关（联动卡）。
// 绑定面经 vi.mock 打桩（真实生成物落盘前由 vitest.config 的解析缝兜底）；
// 事件经 @wailsio/runtime 打桩。
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import PiikView from '../PiikView.vue'
import { useToast } from '../../composables/useToast'
import { useConfirm } from '../../composables/useConfirm'

const svc = vi.hoisted(() => ({
  GetStatus: vi.fn(),
  ListInstalledVersions: vi.fn(),
  ListReleases: vi.fn(),
  GetActiveVersion: vi.fn(),
  SetActiveVersion: vi.fn(),
  DownloadVersion: vi.fn(),
  RemoveVersion: vi.fn(),
  ImportLocal: vi.fn(),
  OpenDir: vi.fn(),
  Start: vi.fn(),
  OpenWindow: vi.fn(),
  QuitAdvisory: vi.fn(),
  Quit: vi.fn(),
  MetaHints: vi.fn(),
  OpenDataDir: vi.fn(),
  GetFollowOnExit: vi.fn(),
  SetFollowOnExit: vi.fn(),
  RepositoryURL: vi.fn(),
  OpenRepository: vi.fn(),
}))

vi.mock('../../../bindings/hanxi/internal/modules/piik/piikservice', () => svc)
vi.mock('@wailsio/runtime', () => ({
  Events: { On: () => vi.fn() },
}))

const writeText = vi.hoisted(() => vi.fn().mockResolvedValue(undefined))

const CONSOLE_URL = 'http://127.0.0.1:8788/'
const LAN_URL = 'http://192.168.10.23:8788/join/7f3a'
const PUBLIC_URL = 'https://piik-demo.trycloudflare.com/join/7f3a'

// 后端 MetaHints() 六条的替身账（前端零自造——上屏词必须逐字来自该 RPC）
const BACKEND_HINTS = ['第1条钉版账', '第2条0.0.0.0账', '第3条隧道账', '第4条不代管账', '第5条子进程账', '第6条数据留存账']

function statusOf(partial: Record<string, unknown> = {}) {
  return {
    state: 'stopped',
    version: '',
    pid: 0,
    error: '',
    startedAt: '',
    listenPort: 8787,
    consoleUrl: '',
    localAccessOpen: true,
    passwordSet: false,
    lanInvitation: '',
    publicInvitation: '',
    noBrowser: false,
    dataDir: 'D:\\hanxi-data\\piik',
    configPath: 'D:\\hanxi-data\\piik\\client.json',
    logDir: 'D:\\hanxi-data\\piik\\logs',
    drifted: false,
    driftNote: '',
    ...partial,
  }
}

const installed165 = {
  version: 'v1.6.5',
  exePath: 'D:\\piik\\v1.6.5\\piik-app.exe',
  dir: 'D:\\piik\\v1.6.5',
  size: 46 * 1024 * 1024,
  installedAt: '2026-09-20',
  isImport: false,
  source: '',
}

const release166 = { version: 'v1.6.6', published: '2026-09-24T00:00:00Z', size: 46 * 1024 * 1024, form: 'portable' }

function stubDefaults(snap: Record<string, unknown>, installed: unknown[] = [installed165], releases: unknown[] = [release166]) {
  svc.GetStatus.mockResolvedValue(statusOf(snap))
  svc.ListInstalledVersions.mockResolvedValue(installed)
  svc.ListReleases.mockResolvedValue(releases)
  svc.GetActiveVersion.mockResolvedValue((installed[0] as { version?: string })?.version ?? '')
  svc.GetFollowOnExit.mockResolvedValue(false)
  svc.RepositoryURL.mockResolvedValue('https://github.com/TNTcraftHIM/Piik')
  svc.MetaHints.mockResolvedValue(BACKEND_HINTS)
  svc.QuitAdvisory.mockResolvedValue('退出托管会关闭 piik 分享服务：正在观看的观众当场断播。')
}

async function flushMicrotasks(times = 20) {
  for (let i = 0; i < times; i++) await Promise.resolve()
}

async function mountAndOpenVersions() {
  const wrapper = mount(PiikView, { attachTo: document.body })
  await flushMicrotasks()
  const tabs = wrapper.findAll('.main-tab-btn')
  await tabs[1].trigger('click')
  await flushMicrotasks()
  return wrapper
}

beforeEach(() => {
  Object.defineProperty(globalThis.navigator, 'clipboard', { value: { writeText }, configurable: true })
  vi.stubGlobal('isSecureContext', true) // useClipboard 两级策略走安全上下文主路
})

afterEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  writeText.mockClear()
  useToast().clearToast()
  document.body.innerHTML = ''
})

describe('PiikView 邀请面板四态', () => {
  it('未装态：状态口径回落「未安装」，邀请面板缺席不摆空牌；「打开界面」钮禁用指路「启动」', async () => {
    stubDefaults({ state: 'stopped' }, [])
    const wrapper = mount(PiikView, { attachTo: document.body })
    await flushMicrotasks()
    expect(wrapper.find('.status-word').text()).toBe('未安装')
    expect(wrapper.find('.share-card').exists()).toBe(false)
    const openBtn = wrapper.findAll('.control-btns button').find((b) => b.text().includes('打开界面'))!
    expect(openBtn.attributes('disabled')).toBeDefined()
    expect(openBtn.attributes('title')).toContain('须点「启动」明示')
    wrapper.unmount()
  })

  it('stopped（已装未跑）：hint 双引导「机读不自动弹→点打开界面」，面板缺席', async () => {
    stubDefaults({ state: 'stopped' })
    const wrapper = mount(PiikView, { attachTo: document.body })
    await flushMicrotasks()
    expect(wrapper.find('.hint-line').text()).toContain('机读模式不自动弹浏览器')
    expect(wrapper.find('.hint-line').text()).toContain('打开界面')
    expect(wrapper.find('.share-card').exists()).toBe(false)
    wrapper.unmount()
  })

  it('running 链接全量：本机/LAN/公网三行 + Cloudflare 中转注记 + 同网段小注 + 口令徽标；口令值反证锁', async () => {
    // 快照里塞入一个后端「结构性不可能回带」的口令原文字段（gateViewFromSnapshot
    // 只出布尔）：即便 B 线意外泄漏，视图只读 PiikStatus 投影字段，
    // DOM 任何角落都不得出现原文——反证锁。
    stubDefaults({
      state: 'running',
      version: 'v1.6.5',
      pid: 4242,
      startedAt: new Date().toISOString(),
      listenPort: 8788,
      consoleUrl: CONSOLE_URL,
      lanInvitation: LAN_URL,
      publicInvitation: PUBLIC_URL,
      passwordSet: true,
      accessPassword: 'open-sesame-42', // 故意投喂的泄漏原文（正常契约不存在此键）
    })
    const wrapper = mount(PiikView, { attachTo: document.body })
    await flushMicrotasks()

    const card = wrapper.find('.share-card')
    expect(card.exists()).toBe(true)
    const rows = card.findAll('.share-row')
    expect(rows).toHaveLength(3)
    expect(rows[0].text()).toContain(CONSOLE_URL)
    expect(rows[1].text()).toContain(LAN_URL)
    expect(rows[1].text()).toContain('同网段设备可直接加入')
    expect(rows[2].text()).toContain(PUBLIC_URL)
    expect(rows[2].find('.chip-information').text()).toBe('经 Cloudflare 中转')
    expect(card.find('.pwd-chip').text()).toBe('已设访问口令')

    // 反证锁：口令原文绝不上屏
    expect(wrapper.text()).not.toContain('open-sesame-42')
    expect(wrapper.html()).not.toContain('open-sesame-42')

    // 三行复制各归各：clipboard.writeText 逐行收到本行链接原文
    await rows[0].findAll('button')[0].trigger('click')
    await flushPromises()
    expect(writeText).toHaveBeenLastCalledWith(CONSOLE_URL)
    await rows[1].findAll('button')[0].trigger('click')
    await flushPromises()
    expect(writeText).toHaveBeenLastCalledWith(LAN_URL)
    await rows[2].findAll('button')[0].trigger('click')
    await flushPromises()
    expect(writeText).toHaveBeenLastCalledWith(PUBLIC_URL)
    expect(writeText).toHaveBeenCalledTimes(3)
    wrapper.unmount()
  })

  it('running 仅本机：只有本机界面地址一行，LAN/公网行与口令徽标缺席；「在浏览器打开」走 OpenWindow RPC 并 toast 后端回执', async () => {
    stubDefaults({
      state: 'running',
      version: 'v1.6.5',
      pid: 4242,
      startedAt: new Date().toISOString(),
      consoleUrl: CONSOLE_URL,
    })
    svc.OpenWindow.mockResolvedValue({ action: 'opened', external: false, message: `已在浏览器打开 piik 界面 ${CONSOLE_URL}`, port: 8787, url: CONSOLE_URL })
    const wrapper = mount(PiikView, { attachTo: document.body })
    await flushMicrotasks()

    const card = wrapper.find('.share-card')
    expect(card.exists()).toBe(true)
    expect(card.findAll('.share-row')).toHaveLength(1)
    expect(card.text()).not.toContain('局域网邀请链接')
    expect(card.text()).not.toContain('公网邀请链接')
    expect(card.find('.pwd-chip').exists()).toBe(false)

    const openBtn = card.findAll('.share-row')[0].findAll('button').find((b) => b.text() === '在浏览器打开')!
    await openBtn.trigger('click')
    await flushPromises()
    expect(svc.OpenWindow).toHaveBeenCalledTimes(1)
    expect(useToast().toastMsg.value).toBe(`已在浏览器打开 piik 界面 ${CONSOLE_URL}`)
    wrapper.unmount()
  })
})

describe('PiikView 版本面与联动', () => {
  it('metaHints 透传后端六条逐字上屏（版本页 meta-info，前端零自造词）', async () => {
    stubDefaults({ state: 'stopped' })
    const wrapper = await mountAndOpenVersions()
    const hints = wrapper.findAll('.meta-info .hint-dim')
    expect(hints).toHaveLength(6)
    expect(hints.map((h) => h.text())).toEqual(BACKEND_HINTS)
    wrapper.unmount()
  })

  it('退出确认消费 QuitAdvisory 预告（断播账说在点确认之前），确认后走 Quit + 回执 toast', async () => {
    stubDefaults({ state: 'running', version: 'v1.6.5', pid: 4242, consoleUrl: CONSOLE_URL, startedAt: new Date().toISOString() })
    svc.Quit.mockResolvedValue({ stopped: true, external: false, message: 'piik 已退出（优雅停通道收口，界面端口 8787 已释放）', port: 8787 })
    const wrapper = mount(PiikView, { attachTo: document.body })
    await flushMicrotasks()
    const { confirmState, settleConfirm } = useConfirm()

    const quitBtn = wrapper.findAll('.control-btns button').find((b) => b.text().includes('退出'))!
    await quitBtn.trigger('click')
    await flushMicrotasks()
    expect(svc.QuitAdvisory).toHaveBeenCalledTimes(1)
    expect(confirmState.open).toBe(true)
    expect(confirmState.options.description).toContain('当场断播')
    settleConfirm(true)
    await flushPromises()
    expect(svc.Quit).toHaveBeenCalledTimes(1)
    expect(useToast().toastMsg.value).toContain('优雅停通道收口')
    wrapper.unmount()
  })

  it('末版卸载预告复用：确认文案含「最后一个版本」与「不删数据」明示，确认后走 RemoveVersion + toast', async () => {
    stubDefaults({ state: 'stopped' }, [installed165])
    const wrapper = await mountAndOpenVersions()
    const { confirmState, settleConfirm } = useConfirm()
    svc.RemoveVersion.mockResolvedValue(undefined)

    const uninstall = wrapper.findAll('.installed-card button').find((b) => b.text() === '卸载')!
    await uninstall.trigger('click')
    await flushMicrotasks()
    expect(confirmState.open).toBe(true)
    expect(confirmState.options.title).toBe('确定卸载 Piik v1.6.5？')
    expect(confirmState.options.description).toContain('这是最后一个版本，卸载后将回到未安装状态')
    expect(confirmState.options.description).toContain('卸载任何托管版本都不删数据')
    settleConfirm(true)
    await flushPromises()
    expect(svc.RemoveVersion).toHaveBeenCalledWith('v1.6.5')
    expect(useToast().toastMsg.value).toBe('已卸载 v1.6.5')
    wrapper.unmount()
  })

  it('followOnExit 开关：勾选送 SetFollowOnExit(true)，回执点名子进程树同灭，注记限定外部实例不管', async () => {
    stubDefaults({ state: 'stopped' })
    const wrapper = mount(PiikView)
    await flushMicrotasks()

    expect(wrapper.find('.extras-card .toggle-label .hint-dim').text()).toContain('外部自行启动')
    svc.SetFollowOnExit.mockResolvedValue(undefined)
    await wrapper.find('.extras-card .toggle-label input').setValue(true)
    await flushPromises()
    expect(svc.SetFollowOnExit).toHaveBeenCalledWith(true)
    expect(useToast().toastMsg.value).toContain('连带终止')
    expect(useToast().toastMsg.value).toContain('cloudflared/piik-capture')
    wrapper.unmount()
  })
})
